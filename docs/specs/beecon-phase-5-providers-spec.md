# Spec: Beecon Phase 5 — Broaden the Provider Catalog (Gmail, Google Calendar, Slack, GitHub)

## Overview
Author additional `formatVersion: 1` provider definitions — pure declarative YAML dropped into `server/internal/catalog/providers/`, auto-embedded and boot-loaded (`embed.go`) — so Beecon ships the providers Rolai's router references beyond the already-shipped Outlook and HubSpot. This strand adds **Gmail, Google Calendar, Slack, and GitHub** (Rolai references Google, Slack, GitHub, HubSpot; HubSpot + Outlook already ship). No provider-specific Go code: each definition must parse under the real `catalog.LoadProviderDefinitions` strict/`KnownFields` loader, and a representative tool per provider must render its mapping via the token grammar and execute end-to-end against a fake upstream, mirroring the existing `test/support/fake_*` + crucial-path pattern (`hubspot_journey_integration_test.go`).

Every OAuth block, base URL, scope, endpoint, and tool mapping below is grounded in each provider's **real public API docs** (see Research Notes). The real work is authoring accurate OAuth/endpoints and honestly flagging the flows the token grammar (per ADR-0012 — pure substitution, no conditionals; extended by the engine-gaps strand with embedded values + `inputSchema` defaults) cannot express.

## Recommended defaults (developer to review)
Decisions taken to the recommended option and finalized. Each is a one-line rationale; nothing below is blocked awaiting an answer.

- **Provider set = Gmail, Google Calendar, Slack, GitHub** (PD77) — the four OAuth2 providers Rolai's router needs that don't already ship; all real public REST APIs.
- **Scope per provider is representative, not exhaustive** (PD77) — enough tools to be useful to Rolai (list/get/send + one write per provider); everything else is explicitly deferred, matching how Outlook/HubSpot shipped a handful of tools.
- **Gmail + Calendar share one Google OAuth block** (PD78) — same authorize/token/userinfo endpoints and OpenID `email`/`profile` userInfo mapping; only scopes and `baseUrl` differ. `credentialStyle` omitted (defaults to `formBody`, which is what Google requires).
- **Gmail `send` exposes a pre-encoded `raw` input** (PD79) — Gmail's `messages.send` takes a base64url RFC 2822 message in `raw`; the token grammar can substitute a value but cannot build MIME, so the caller supplies `raw`. Flagged as a deviation, not a workaround hidden in the definition.
- **The one poll trigger lives on Google Calendar (`updatedMin`-based new/updated event)** (PD80) — Calendar's `events.list` returns `{items:[...], nextPageToken}` with a per-record RFC3339 `updated`/`created` and an `updatedMin` filter, which maps cleanly onto the RFC3339-watermark poll model. Gmail/Slack/GitHub triggers are deferred with reasons (see Deviations) — the task's "poll trigger where the API supports it" resolves to Calendar.
- **Slack ships with no `userInfo` mapping and is documented as `ok:false`-on-`HTTP 200`** (PD81) — Slack's Web API returns HTTP 200 with `{ok:false,error}` on failure, which the engine can't turn into a tool-level failure without a conditional (out per ADR-0012); Slack's account email is nested and requires identity scopes the flat `userInfo` mapping can't read. Both flagged; Slack still ships useful tools.
- **GitHub tools each declare a literal `User-Agent` header** (PD84) — GitHub's REST API rejects requests with no `User-Agent` (HTTP 403); a token-free literal header is expressible per-tool. The OAuth account-fetch path needs a one-line default `User-Agent` in `oauthhttp.Client` (PD83) — flagged as the single code change GitHub requires.
- **Slice = one provider each, four slices** (PD77) — each provider is independently releasable (loads, lists, executes one tool against a fake upstream) and fits under ~10 ACs.

## Research Notes (grounding — real public API docs)
- **Google OAuth 2.0**: authorize `https://accounts.google.com/o/oauth2/v2/auth`, token `https://oauth2.googleapis.com/token`, client creds in form body (`formBody`). Refresh token requires `access_type=offline` (+ `prompt=consent`) on the **authorize** request — a query param, not a scope. OpenID userinfo `https://www.googleapis.com/oauth2/v3/userinfo` → top-level `email`, `name`.
- **Gmail API** (`baseUrl https://gmail.googleapis.com/gmail/v1`): `GET /users/me/messages` (`q`, `maxResults`, `pageToken` → `{messages:[{id,threadId}], nextPageToken}`); `GET /users/me/messages/{id}` (`format`); `POST /users/me/messages/send` (body `{raw}` = base64url RFC 2822). Scopes `gmail.readonly`, `gmail.send`.
- **Google Calendar API** (`baseUrl https://www.googleapis.com/calendar/v3`): `GET /calendars/{calendarId}/events` (`timeMin`, `maxResults`, `singleEvents`, `orderBy`, `updatedMin`, `pageToken` → `{items:[...], nextPageToken}`); `POST /calendars/{calendarId}/events` (body `summary`, `start.dateTime`, `end.dateTime`). Scope `calendar.events`.
- **Slack**: authorize `https://slack.com/oauth/v2/authorize`, token `https://slack.com/api/oauth.v2.access` (form-body creds); bot token at root `access_token` (`token_type:"bot"`), tokens do not expire (no refresh unless token rotation is on). Web API base `https://slack.com/api`; `POST /chat.postMessage` (`channel`, `text`); `GET /conversations.list` (`types`, `limit`, `cursor` → `{ok, channels:[...], response_metadata:{next_cursor}}`). **Every method returns HTTP 200 with an `ok` boolean**, `ok:false`+`error` on failure. Bot scopes `chat:write`, `channels:read`.
- **GitHub**: authorize `https://github.com/login/oauth/authorize`, token `https://github.com/login/oauth/access_token` (form-body creds; returns form-encoded unless `Accept: application/json` — which `oauthhttp.Client` already sends). No refresh token for OAuth Apps. API base `https://api.github.com`; `GET /user` → top-level `login`, `name`, `email`; `GET /user/repos`, `GET /repos/{owner}/{repo}/issues` (**top-level JSON array**), `POST /repos/{owner}/{repo}/issues` (body `title`, `body`). **All requests require a `User-Agent` header** (403 otherwise). Scopes `repo`, `read:user`.

---

## Slice 1: Gmail (Google) — the walking skeleton
Establishes the shared Google OAuth block and proves a Google tool executes end-to-end. New file `providers/gmail.yaml`.

- [x] `gmail.yaml` parses under the real `catalog.LoadProviderDefinitions` strict/`KnownFields` loader with no error, so it boot-loads from the embedded `providers/` directory alongside Outlook/HubSpot.
- [x] Booted against the real embedded catalog, the tools list surfaces `gmail-list-messages`, `gmail-get-message`, and `gmail-send-message` under provider slug `gmail`, each with a non-empty input and output schema.
- [x] The Gmail OAuth block is well-formed: `authorizeUrl https://accounts.google.com/o/oauth2/v2/auth`, `tokenUrl https://oauth2.googleapis.com/token`, `userInfoUrl https://www.googleapis.com/oauth2/v3/userinfo`, scopes include `openid`, `email`, `profile`, `gmail.readonly`, `gmail.send`, and `userInfo` maps `email→email` and `displayName→name`.
- [x] `gmail-list-messages` renders its mapping (`GET /users/me/messages`, canonical `pageSize→maxResults`, `cursor→pageToken`, `nextCursorPath nextPageToken`) and, against a fake Google API server, returns the messages array and a `nextCursor` when a further page exists.
- [x] `gmail-get-message`'s path token `{input.messageId}` is substituted and URL-escaped into `/users/me/messages/{input.messageId}`, and the tool returns the fetched message as `Data` against the fake upstream.
- [x] `gmail-send-message` maps `POST /users/me/messages/send` with body `raw: "{input.raw}"`, and the fake upstream receives the caller-supplied base64url `raw` value unchanged in the request body.
- [x] An upstream Gmail error (e.g. HTTP 400) surfaces as a tool-level failure carrying the provider's status and message, not a platform HTTP error (mirrors the HubSpot create-contact error AC).
- [x] Gmail declares **no trigger** (deferred — see Deviations); the definition still validates and loads with an empty `triggers` list.

## Slice 2: Google Calendar — the clean poll trigger
Reuses the Slice 1 Google OAuth endpoints; adds the strand's one poll trigger. New file `providers/google-calendar.yaml`.

- [x] `google-calendar.yaml` parses under the real strict loader and boot-loads, surfacing `gcal-list-events` and `gcal-create-event` under provider slug `google-calendar`.
- [x] The Calendar OAuth block reuses the Google authorize/token/userinfo endpoints and OpenID `email`/`profile` userInfo mapping, adding the `https://www.googleapis.com/auth/calendar.events` scope; `baseUrl` is `https://www.googleapis.com/calendar/v3`.
- [x] `gcal-list-events` renders `GET /calendars/{input.calendarId}/events` with `calendarId` defaulting to `primary` via its `inputSchema` `default` (engine-gaps Gap C), and returns the `items` array with a `nextCursor` against the fake upstream.
- [x] `gcal-create-event` builds the nested JSON body from a flat input schema via dotted keys (`summary`, `start.dateTime`, `end.dateTime`) and the fake upstream receives the nested `{start:{dateTime},end:{dateTime}}` object (mirrors HubSpot `properties.*` body mapping).
- [x] The `gcal-event-updated` poll trigger parses under the strict loader with a non-empty `configSchema` (`calendarId`, default `primary`) and `payloadSchema`, `ingestion: poll`, and a complete `poll` mapping.
- [x] The trigger's poll mapping renders `GET /calendars/{config.calendarId}/events?updatedMin={watermark}&orderBy=updated&singleEvents=true`, with `recordsPath items`, `recordIdPath id`, `recordTimestampPath updated`, and a payload field map.
- [x] Against a fake upstream that gains a new event mid-test, `FetchTriggerRecords` extracts the event's id, RFC3339 timestamp, and mapped payload from the `items` array (mirrors the HubSpot contact-created poll journey).
- [x] An upstream Calendar error on a tool call surfaces as a tool-level failure carrying the provider's status and message.

## Slice 3: Slack — flagged deviations, still useful
New file `providers/slack.yaml`. Ships tools that work; documents the `ok:false@200` and account-metadata limitations in-definition and here.

- [x] `slack.yaml` parses under the real strict loader and boot-loads, surfacing `slack-post-message` and `slack-list-channels` under provider slug `slack`.
- [x] The Slack OAuth block is well-formed: `authorizeUrl https://slack.com/oauth/v2/authorize`, `tokenUrl https://slack.com/api/oauth.v2.access`, scopes `chat:write` and `channels:read`, `baseUrl https://slack.com/api`.
- [x] The Slack definition declares **no `userInfo` mapping** (`userInfoUrl` omitted), and still validates and loads — Slack's account email is nested under `authed_user`/`users.identity` behind identity scopes the flat `userInfo` mapping cannot read (flagged in Deviations).
- [x] `slack-post-message` maps `POST /chat.postMessage` with body `channel: "{input.channel}"` and `text: "{input.text}"`, and the fake upstream receives both values in the JSON body under a bearer token.
- [x] `slack-list-channels` maps `GET /conversations.list` with canonical `pageSize→limit`, `cursor→cursor`, `nextCursorPath response_metadata.next_cursor`, and returns the `channels` array plus a `nextCursor` against the fake upstream.
- [x] A Slack failure response (HTTP 200 with `{ok:false,error:"..."}`) is returned by the tool as a **successful** call whose `Data` carries `ok:false` — the test pins this documented limitation (the token grammar cannot convert `ok:false` into a tool-level failure; see Deviations).
- [x] Slack declares **no trigger** (deferred — see Deviations); the definition validates and loads with an empty `triggers` list.

## Slice 4: GitHub — per-tool User-Agent, deviations flagged
New file `providers/github.yaml`. Ships repo/issue tools; flags the account-fetch `User-Agent` gap and the deferred trigger.

- [x] `github.yaml` parses under the real strict loader and boot-loads, surfacing `github-list-repos`, `github-list-issues`, and `github-create-issue` under provider slug `github`.
- [x] The GitHub OAuth block is well-formed: `authorizeUrl https://github.com/login/oauth/authorize`, `tokenUrl https://github.com/login/oauth/access_token`, scopes `repo` and `read:user`, `userInfoUrl https://api.github.com/user`, `userInfo` mapping `email→email` and `displayName→login`, `baseUrl https://api.github.com`.
- [x] Every GitHub tool declares literal `header` entries `User-Agent: Beecon` and `Accept: application/vnd.github+json`, and the fake upstream observes the `User-Agent` on each tool request (GitHub rejects requests without it).
- [x] `github-list-issues` substitutes `{input.owner}` and `{input.repo}` into `GET /repos/{input.owner}/{input.repo}/issues` (URL-escaped) and returns the issues array as `Data` against the fake upstream.
- [x] `github-create-issue` maps `POST /repos/{input.owner}/{input.repo}/issues` with body `title: "{input.title}"` and `body: "{input.body}"`, and the fake upstream receives both in the JSON body.
- [x] `github-list-repos` maps `GET /user/repos` with `per_page`/`page`/`visibility` query mapping and returns the repos array as `Data`.
- [x] An upstream GitHub error (e.g. HTTP 422 on create-issue) surfaces as a tool-level failure carrying the provider's status and message.
- [x] GitHub declares **no trigger** (deferred — see Deviations); the definition validates and loads with an empty `triggers` list.

## Provider deviations (the real work — flagged, not hidden)
Each is grounded in verified engine/loader behavior (`connections/oauth.go`, `catalog/definition_v1.go`, `execution/poll.go`, `execution/driven/providerhttp`, `connections/driven/oauthhttp`).

- **Google — no refresh token (cross-cutting, both Google slices).** Google issues a refresh token only when `access_type=offline` (and typically `prompt=consent`) is present on the **authorize** request. `connections/oauth.go`'s `buildAuthorizeURL` hard-codes `client_id`/`response_type`/`redirect_uri`/`scope`/`state` and offers no field for extra authorize params, and the definition format has no place to declare them. **Consequence:** Google access tokens work but expire (~1h) with no refresh, so Google connections go stale and PD18 refresh-on-401 cannot self-heal them. **Recommendation:** a small follow-up to let a definition declare static authorize-request params (e.g. `access_type: offline`) — out of scope for this definitions-only strand, but the reviewer must weigh it before Google is production-useful for Rolai.
- **Gmail — no poll trigger.** Gmail's incremental model is `historyId` (`users.history.list`), not a timestamp watermark; `messages.list` items carry only `{id,threadId}` (no per-record timestamp without a second call) and `internalDate` is epoch-millis, not RFC3339. The poll model's RFC3339 `{watermark}` + `recordTimestampPath` cannot express this. Trigger deferred.
- **Gmail — `send` requires caller-encoded MIME.** `messages.send` takes a base64url RFC 2822 message in `raw`; the token grammar substitutes a value but cannot build MIME, so `gmail-send-message` exposes a `raw` input. A friendlier to/subject/body tool needs an encoder step the grammar lacks (revisit under CEL per ADR-0012).
- **Slack — failures return HTTP 200 (`ok:false`).** Every Web API method returns HTTP 200 with `{ok:false,error}` on failure. `execution` treats 2xx as success, so Slack errors surface as *successful* calls whose `Data` carries `ok:false`. Converting `ok:false` into a tool-level failure needs a conditional, which ADR-0012 defers to CEL. Documented; callers must inspect `ok`.
- **Slack — no account metadata.** `oauth.v2.access` carries no email; `users.identity` returns email/name nested under `user` behind identity scopes, and `oauthhttp.FetchAccount`'s `stringField` reads only top-level response fields. Slack ships with `userInfo` omitted (connection activates without email/displayName).
- **Slack — no refresh token.** Slack bot tokens do not expire unless token rotation is enabled; Beecon stores an empty refresh token and the refresh path is simply never exercised. No action needed.
- **GitHub — account-fetch needs a default `User-Agent` (one code change, PD83).** `oauthhttp.Client.FetchAccount`/`FetchUserInfo` set only `Authorization` and `Accept`, so the `GET https://api.github.com/user` account-fetch is rejected with 403. **Recommendation:** add a default `User-Agent: Beecon` header in `oauthhttp.Client` (benefits every provider). Until then, GitHub connections can't capture account metadata even though tool calls (which carry a per-tool `User-Agent`) work.
- **GitHub — no poll trigger (two blockers).** (1) `GET /repos/{owner}/{repo}/issues` returns a **top-level JSON array**; the poll engine supports a root-array via an empty `recordsPath` (`execution/poll.go` `lookupPollField`), but the strict loader's `validateTriggerPollMappingFileV1` **requires `recordsPath` non-empty** — an inconsistency to resolve before any root-array poll works. (2) `triggerPollMappingFileV1` has no `header` field, so the mandatory `User-Agent` cannot be sent on a poll request (403). `GET /search/issues` returns `{items:[...]}` (solving blocker 1) but not blocker 2. Trigger deferred pending root-array `recordsPath` support **and** poll-request header support.
- **GitHub — no refresh token.** OAuth Apps issue non-expiring tokens (only GitHub Apps issue expiring ones); refresh path unused. No action needed.

## Test approach (mirror the existing pattern)
- Reuse the `test/support/fake_*` scripted `httptest.Server` pattern (`fake_hubspot.go`, `fake_microsoft.go`, `fake_graph.go`) and `BootAppWithProviderDefinitions`. Recommended: one `fake_google` OAuth stand-in reused by both Google slices, plus per-API fake handlers for Gmail/Calendar; `fake_slack`; `fake_github`.
- Loader-parse ACs run against the **real** embedded `providers/` directory (like `TestHubspotJourney_DefinitionLoadsAtBootWithNoProviderSpecificGoCode`); tool-execution and poll ACs point a `catalog.ProviderDefinition`'s `BaseURL`/`TokenURL`/`UserInfoURL` at the fake server (like `hubspotDefinitionAgainst`).

## API Shape (indicative)
```
gmail-list-messages   GET  /users/me/messages            ?q&maxResults&pageToken → {messages:[{id,threadId}], nextPageToken}
gmail-get-message     GET  /users/me/messages/{id}        ?format                 → message
gmail-send-message    POST /users/me/messages/send        {raw}                   → {id, threadId}
gcal-list-events      GET  /calendars/{calendarId}/events ?timeMin&maxResults     → {items:[...], nextPageToken}
gcal-create-event     POST /calendars/{calendarId}/events {summary,start.dateTime,end.dateTime}
gcal-event-updated    poll GET /calendars/{config.calendarId}/events?updatedMin={watermark}&orderBy=updated
slack-post-message    POST /chat.postMessage              {channel,text}          → {ok, ...}   (ok:false@200 on error)
slack-list-channels   GET  /conversations.list            ?types&limit&cursor     → {ok, channels, response_metadata:{next_cursor}}
github-list-repos     GET  /user/repos                    ?visibility&per_page    → [repo, ...]  (+ User-Agent header)
github-list-issues    GET  /repos/{owner}/{repo}/issues   ?state&per_page         → [issue, ...] (+ User-Agent header)
github-create-issue   POST /repos/{owner}/{repo}/issues   {title,body}                           (+ User-Agent header)
```

## Out of Scope
- Any Go change to the OAuth authorize-URL builder, the poll mapping schema (root-array `recordsPath`, poll headers), or the `oauthhttp` default `User-Agent` — these are **flagged as prerequisites/follow-ups**, not built here. This strand authors definitions only.
- Gmail, Slack, and GitHub poll triggers (deferred with reasons above); only the Google Calendar trigger ships.
- Conditionals, MIME construction, response reshaping, or any expression beyond the token grammar (ADR-0012 defers these to CEL).
- Exhaustive tool coverage per provider — only the representative Rolai-useful set (labels/threads, calendar ACL, Slack users/files, GitHub PRs/commits, etc. are all out).
- The Membrane importer, registry service, service bus, and any non-definition Phase 5 strand.
- Any change to Outlook/HubSpot definitions.

## Technical Context
- **Patterns to follow:** `outlook.yaml`/`hubspot.yaml` for exact format + style; `definition_v1.go` strict `KnownFields` schema; `hubspot_journey_integration_test.go` + `fake_hubspot.go` for the load + execute-against-fake pattern; HubSpot `properties.*` dotted-body and `paging.next.after` pagination as the model for Calendar/Slack.
- **Key dependencies / integration points:** `catalog.LoadProviderDefinitions` + `embed.go` (auto-embed + boot-load); `execution/template.go` token grammar (`{input.x}`/`{params.x}`) + engine-gaps embedded values + `inputSchema` defaults; `execution/poll.go` (`recordsPath`/`recordIdPath`/`recordTimestampPath`, RFC3339 watermark); `execution/driven/providerhttp` (forwards declared tool headers; bearer + `Accept: application/json`); `connections/driven/oauthhttp` (form-body vs basic-auth token exchange, `{accessToken}` userInfo templating).
- **PD range used:** PD77–PD84 (continues from operator-auth ≤PD58, registry PD59–68, service-bus PD69–76).
- **Risk level:** MODERATE — no runtime code changes, but authoring accurate OAuth/endpoints/scopes is exacting, and four verified engine/loader deviations (Google refresh, Slack `ok:false@200`, GitHub `User-Agent`, GitHub root-array trigger) shape what can ship declaratively.

## Review
- [ ] Reviewed
