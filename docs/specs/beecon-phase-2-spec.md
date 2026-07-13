# Spec: Beecon Phase 2 — Real Catalog: Hubspot Proves the Provider Model

> A second provider ships as pure definitions, the definition format is finalized,
> and connections get a full lifecycle with stable ids.
> Discovery: [beecon-discovery.md](./beecon-discovery.md) (Phase 2 of the Milestone Map).
> Boundaries: `.claude/BOUNDARIES.md`. Context: `.claude/bee-context.local.md`.
> Builds on the shipped Phase 1: [beecon-phase-1-spec.md](./beecon-phase-1-spec.md).

## Overview

Turn the Phase 1 walking skeleton into a real catalog platform: the declarative
provider-definition format is finalized (base URL + per-tool mapping, accurate output
schemas, deprecation flags, expected pre-auth params) and Hubspot is added almost
entirely as definitions — validating H2. Connections gain their complete lifecycle —
list, disable, delete, and reconnect **keeping the same id** (validating H3) — with
`EXPIRED`/`DISCONNECTED` statuses and on-demand token refresh so ACTIVE connections
survive access-token expiry. The browser gets user-scoped short-lived tokens for the
connect UI, upstream rate limits are normalized into one retriable shape with
platform-side retry, file upload yields URIs usable in tool arguments, and org API
keys become rotatable with an overlap window.

Risk: HIGH — this phase touches credential handling (client-secret encryption,
refresh-token rotation, credential destruction on delete), a new browser-facing auth
scheme (user tokens), and the definition format every later phase builds on. Edge
cases below are first-class.

## Proposed Decisions (synthesis — correct at review if wrong)

AskUserQuestion was unavailable during speccing; these are reasoned proposals, each
grounded in the discovery doc, the rolai context, and the Membrane samples in `temp/`.
Numbering continues from Phase 1 (PD1–PD12).

> **Developer-confirmed 2026-07-12** (via post-spec Q&A): PD14 (slugs until Phase 5),
> PD18 (on-demand refresh in Phase 2), PD20 (HS256 + encrypted signing secret),
> PD21 (HTTP 429 + Retry-After carve-out), PD22 (file retention deferred to Phase 4),
> PD23 (24h rotation overlap default). The remaining PDs stand as proposals — correct
> at review if wrong.

1. **PD13 — Definition format v1.** One YAML file per provider carrying
   `formatVersion: 1`; provider block (slug, name, logo, authScheme); `oauth` block
   (authorizeUrl, tokenUrl, userInfoUrl, scopes, plus a declared token-endpoint
   credential style — form-body vs basic-auth — so provider quirks stay in the
   definition, not in Go code); optional `expectedParams`; `tools` list where each
   tool = catalog fields (slug, name, description, inputSchema, **outputSchema
   required**, `deprecated` flag) + mapping (provider `baseUrl` + per-tool `path` with
   `{input.x}`/`{params.x}` templating, method, query/header/body mapping, optional
   `pagination` and file-typed inputs). A `triggers` section is reserved in the format
   but not implemented (Phase 3). Definitions still load from a local directory at
   boot; the registry service is Phase 5. This format supersedes Phase 1's
   full-URL-in-path Outlook file, which is migrated to it.
2. **PD14 — Tools stay slug-addressed.** The tool slug is the canonical immutable
   identifier of a catalog definition; `tool_` entity ids are not minted in Phase 2 —
   they arrive when the registry assigns ids at publish (Phase 5). Definitions are not
   database rows; generating fresh cuid2s per boot would violate "one immutable id
   forever".
3. **PD15 — Pagination convention.** (a) Every Beecon list API uses `cursor` +
   `limit` with a `nextCursor` in the response — the Phase 1 logs convention becomes
   platform-wide. (b) Tool-level pagination: a mapping may declare how the provider
   pages (param names + response cursor path); such tools accept canonical `pageSize`
   and `cursor` inputs, and the execution envelope gains an optional top-level
   `nextCursor` (the PD6 `data` payload itself is untouched). Graph `$top`/`$skip`
   and Hubspot `limit`/`after` both map onto this.
4. **PD16 — Phase 2 tool set.** Hubspot: `hubspot-list-contacts` (GET
   `/crm/v3/objects/contacts` — proves pagination), `hubspot-create-contact` (POST —
   proves JSON body mapping), `hubspot-upload-file` (POST `/files/v3/files` multipart
   — proves file inputs). Outlook additionally gains `outlook-get-message` (proves
   path-parameter templating; direct counterpart of `temp/outlook-get-message.yaml`).
   Hubspot OAuth: authorize `https://app.hubspot.com/oauth/authorize`, token
   `https://api.hubapi.com/oauth/v1/token` (form-body credentials), scopes
   `crm.objects.contacts.read crm.objects.contacts.write files`; account metadata
   (user email, hub domain) captured at callback from Hubspot's token-metadata
   endpoint via the definition's `userInfoUrl` mechanism.
5. **PD17 — Secrets encryption picked up.** Integration OAuth client secrets (a
   deliberate Phase 1 deferral) and secret-flagged expected-param values are encrypted
   with the existing AES-256-GCM vault key. Existing plaintext client-secret rows are
   migrated to encrypted form automatically at boot.
6. **PD18 — On-demand token refresh in Phase 2.** At execution time, an expired
   access token (or a provider 401) triggers a refresh using the stored refresh token
   and one retry of the call. Refresh failure marks the connection `EXPIRED`. Without
   this, "connection lifecycle complete" is hollow — Outlook tokens die hourly and
   `EXPIRED` could never occur. The proactive refresh scheduler, reconciliation job,
   and `connection.expired` reverse message remain Phase 3.
7. **PD19 — Lifecycle semantics.** Disable → status `DISCONNECTED`, credentials
   retained, execution blocked; recovery is reconnect (no separate enable — rolai
   never calls one). Delete → permanent: credentials destroyed immediately, the
   connection is gone from the API (not-found); log entries keep the id string.
   Reconnect → allowed from ACTIVE/EXPIRED/DISCONNECTED, runs through the standard
   connect pages, **always keeps the same `conn_` id**; until the new handshake
   completes, prior status and tokens are untouched (an ACTIVE connection keeps
   working during reconnect; a failed reconnect changes nothing).
8. **PD20 — User-scoped browser tokens.** New org-scoped **user-token signing
   secret** entity in `access/` (admin-issued, shown once, stored hashed-equivalent —
   verifiable but not recoverable in plaintext where the JWT algorithm allows;
   HS256 requires the raw secret, so it is stored encrypted with the vault key).
   The SDK mints the JWT **locally** (no network call), HS256, claims
   `{sub: user_..., iat, exp}` (default 2h), header `kid` = signing-secret id.
   Browser surface: list integrations, initiate connection (userId taken from the
   token, never the request), list/get **own** connections, start reconnect on own
   connections. Everything else stays server-key-only.
9. **PD21 — Rate-limit surface.** Platform-side retry: on a normalized upstream
   rate-limit (Graph 429/nested throttle codes, Hubspot 429/`RATE_LIMITS` category),
   retry up to 3 attempts honoring `Retry-After` (jittered backoff when absent,
   total wait budget 30s). If exhausted, the execute endpoint responds **HTTP 429
   with a `Retry-After` header** and PD5 body `{"error":{"code":"rate_limited",...}}`
   — a deliberate carve-out from the PD6 envelope, because it is the one
   transport-retriable condition and discovery mandates "one retriable shape
   (429 + Retry-After)". The SDK raises a typed `RateLimitedError` carrying
   `retryAfter`.
10. **PD22 — File storage.** `FileUpload` lives behind a storage port in
    `execution/`; the Phase 2 adapter is local disk (`BEECON_FILES_DIR`). Upload
    (multipart) returns `{id: "file_...", name, mimeType, size, downloadUrl}` where
    `downloadUrl` is served by Beecon and org-authenticated. Max size configurable,
    default 20 MB. Mappings declare file-typed inputs; execution resolves a `file_`
    id and streams the bytes to the provider. No auto-expiry/retention in Phase 2
    (retention config is Phase 4).
11. **PD23 — Key rotation semantics.** `POST .../api-keys/{keyId}/rotate` returns a
    new secret exactly once; the old secret keeps verifying for an overlap window
    (default 24h, settable per rotate call); after the window it is rejected. Revoke
    remains immediate and kills both secrets. Scope-restricted ("use-based") keys
    stay deferred to Phase 4 governance.
12. **PD24 — Metrics before production dependence** (reviewer note). A Prometheus
    text endpoint `GET /metrics`, guarded by the admin key, exposing: tool-execution
    counts and duration histograms by provider/status, upstream rate-limit hits and
    retries, OAuth handshake outcomes, and token-refresh outcomes. Alert rules and
    dashboards are the operator's concern (documented in ops notes); Admin UI
    surfacing is Phase 4.
13. **PD25 — `code` redaction scoped** (reviewer note). The generic `code` redaction
    key applies only to OAuth token-exchange log entries (where it would be an
    authorization code); tool-execution bodies keep legitimate `code` fields (error
    codes, country codes). OAuth authorization codes and `state` values are never
    persisted anywhere regardless.
14. **PD26 — No raw-action pass-through** (closing the question discovery deferred to
    this spec). KB-style file browsing is covered by regular tool definitions — the
    finalized mapping format expresses arbitrary HTTP shapes, so an escape-hatch
    proxy endpoint is not built (YAGNI, and it would bypass schema validation,
    logging redaction, and rate-limit normalization).

---

## Slice 1 — The catalog speaks: finalized definition format + tool catalog API

The format every later phase builds on, proven by re-expressing Outlook in it, plus
the catalog API rolai polls today (with the output schemas both vendors get wrong).

- [x] Provider definitions load at boot from the finalized format (`formatVersion: 1`); the Phase 1 Outlook definition is re-expressed in it with no change to any existing endpoint's behavior
- [x] Boot fails with a clear message naming the file, tool, and field when a definition is invalid — including when any tool lacks an input or output schema
- [x] Tool mappings separate the provider base URL from per-tool paths and support path, query, header, and body mapping from tool inputs
- [x] Consumer can execute `outlook-get-message` with a messageId and receive the message (proves path-parameter templating runs end-to-end, not just parses)
- [x] Consumer can list tools for an integration or provider slug, each with slug, name, description, input schema, output schema, and deprecation flag
- [x] Consumer can fetch a single tool by slug with the same detail
- [x] Tools marked deprecated in the definition are returned with their flag and can be excluded via a filter parameter
- [x] The tool list is cursor-paginated per the platform-wide convention (cursor + limit in, nextCursor out)
- [x] Listing tools for an unknown provider slug or an integration of another organization returns not-found

## Slice 2 — Hubspot proves it: second provider, encrypted secrets, connect and execute

H2's validation: provider #2 arrives as definitions, connects through the same pages,
and runs tools — with the client-secret encryption Phase 1 deferred.

- [x] The Hubspot provider definition loads at boot — adding Hubspot introduces no provider-specific Go code, only the definition file (token-endpoint credential style is declared in the definition)
- [x] Installation admin can create a Hubspot Integration with OAuth client id + client secret; response includes an `intg_`-prefixed id
- [x] All Integration client secrets — including Phase 1's existing Outlook rows — are stored encrypted; a database dump contains no plaintext client secret
- [x] End user completes Hubspot OAuth through the same middle-man pages into an ACTIVE connection with a stable `conn_` id
- [x] The Hubspot consent redirect carries the definition's scopes and a single-use CSRF state parameter
- [x] The ACTIVE Hubspot connection records account metadata (user email, hub domain) visible via get-connection
- [x] When the user denies consent at Hubspot, the browser returns to the consumer's redirectUri with an error status and the connection stays INITIATED
- [x] Consumer can execute `hubspot-list-contacts` and receive `{successful: true, error: null, data}` with the contacts
- [x] `hubspot-list-contacts` accepts canonical `pageSize`/`cursor` inputs and returns a `nextCursor` that fetches the following page
- [x] Consumer can execute `hubspot-create-contact` and receive the created contact (proves JSON body mapping); upstream Hubspot errors surface as `{successful: false, error, data: null}` with the provider's status and message

## Slice 3 — Ask before auth: expected pre-auth params + param-collection page

The mechanism rolai calls `getExpectedParamsForUser`: some providers need values
(subdomains, API keys) from the end user before OAuth can even start.

- [ ] A provider definition may declare expected pre-auth params (name, displayName, description, required, secret flag); invalid declarations fail boot with a clear message
- [ ] Consumer can fetch an integration's expected params (fields + provider name) via the API
- [ ] When a definition declares expected params, the connect page shows a param-collection form before forwarding to the provider
- [ ] Submitting the form with a required param missing shows an inline validation error and does not forward to the provider
- [ ] Params flagged secret are masked in the form input
- [ ] Providers without expected params (Outlook, Hubspot) skip the form entirely — their connect flow is unchanged
- [ ] Submitted param values are stored encrypted with the connection's credentials and appear in no API response and no log entry
- [ ] Collected values are usable via `{params.x}` templating in OAuth URLs and tool mappings (proven with a test-fixture provider definition, since neither Outlook nor Hubspot needs params)

## Slice 4 — Connections live and die: full lifecycle with stable ids

H3's validation: disable, delete, reconnect-with-the-same-id, the two missing
statuses, and on-demand refresh so ACTIVE actually stays usable.

- [ ] Consumer can list a user's connections (status, provider, account metadata), cursor-paginated
- [ ] Consumer can disable a connection: status becomes DISCONNECTED and execution against it returns `{successful: false, error, data: null}` with a status-explaining error
- [ ] Consumer can delete a connection: it is not-found afterwards and its stored tokens are destroyed (a database dump contains no credentials for it)
- [ ] Consumer can start a reconnect on a connection and receives a connect link through the standard middle-man pages — with the **same** `conn_` id
- [ ] Completing a reconnect makes the connection ACTIVE with the same id, fresh tokens, and refreshed account metadata
- [ ] A failed or abandoned reconnect leaves the connection's prior status and existing tokens untouched (a previously ACTIVE connection still executes)
- [ ] Executing with an expired access token transparently refreshes it using the refresh token and completes the call — the consumer sees a normal success
- [ ] When the provider returns a rotated refresh token during refresh, the stored refresh token is replaced
- [ ] When refresh fails (e.g. refresh token revoked), the connection becomes EXPIRED and execution returns a status-explaining envelope error
- [ ] Reconnecting an EXPIRED connection restores it to ACTIVE with the same id
- [ ] Disable, delete, and reconnect against another organization's connection return not-found

## Slice 5 — The browser gets a key: user-scoped short-lived tokens

Membrane's per-user JWT, done properly: minted locally by the consumer's server,
verified by Beecon, powering a client-driven connect UI.

- [ ] Installation admin can issue an organization's user-token signing secret; the secret is shown exactly once, and listing shows only id, prefix, and created date
- [ ] The SDK mints a user token locally (no network call) for a userId, signed with the configured signing secret, default expiry 2 hours
- [ ] A browser request with a valid user token can list the integrations available to its organization
- [ ] A browser request with a valid user token can initiate a connection — the userId comes from the token, and a userId in the request body is ignored
- [ ] A browser request with a valid user token can list and get its own user's connections; other users' connections return not-found
- [ ] A browser request with a valid user token can start a reconnect on its own connection
- [ ] An expired user token is rejected as unauthorized
- [ ] A token with a tampered payload or signed with the wrong secret is rejected as unauthorized
- [ ] A user token cannot call server-only endpoints (user creation, logs, tool execution, file upload) — rejected as unauthorized

## Slice 6 — Politeness under pressure: rate-limit normalization + platform retry

The end of consumer-side backoff code: Graph's nested throttle codes and Hubspot's
rate-limit category collapse into one retriable shape — plus the metrics the reviewer
asked for before production dependence.

- [ ] An upstream rate-limit response (Graph 429 and Hubspot 429/RATE_LIMITS shapes both) is retried platform-side, honoring the provider's Retry-After
- [ ] When a retry succeeds, the consumer receives a normal successful envelope — no rate-limit detail leaks
- [ ] When retries are exhausted, the execute endpoint responds HTTP 429 with a Retry-After header and error code `rate_limited`
- [ ] Non-retriable upstream errors (e.g. 400, 404) are not retried and surface once as envelope errors
- [ ] Every upstream attempt — including rate-limited ones — writes its own log entry marked as rate-limited where applicable
- [ ] A metrics endpoint (admin-guarded) exposes execution counts and durations by provider and status, rate-limit retries, OAuth handshake outcomes, and token-refresh outcomes
- [ ] `code` fields in tool-execution log bodies are no longer redacted; `code` remains redacted in OAuth token-exchange log entries

## Slice 7 — Files travel: upload → URI usable in tool arguments

The Membrane `POST /files` equivalent, proven end-to-end by pushing a file into
Hubspot's file manager.

- [ ] Consumer can upload a file (multipart) and receives `{id: "file_...", name, mimeType, size, downloadUrl}`
- [ ] The uploaded file is downloadable via its downloadUrl with the organization's auth; another organization's request returns not-found
- [ ] An upload exceeding the configured size limit (default 20 MB) is rejected with a validation error
- [ ] Executing `hubspot-upload-file` with a `file_` id in the arguments sends the stored file to Hubspot and returns the provider's file record (proves file-typed mapping inputs)
- [ ] Executing with an unknown or cross-organization `file_` id returns an envelope error without calling the provider
- [ ] File bytes never appear in log entries — the log records the file id and size only

## Slice 8 — Keys turn: API key rotation with overlap window

Zero-downtime credential hygiene for the org server key.

- [ ] Installation admin can rotate an organization API key; the new secret is returned exactly once
- [ ] The old secret continues to authenticate during the overlap window (default 24 hours, settable per rotation)
- [ ] After the overlap window ends, the old secret is rejected as unauthorized
- [ ] The new secret authenticates immediately after rotation
- [ ] Listing keys shows rotation state (rotated-at, overlap expiry) — never any secret
- [ ] Revoking a rotated key immediately rejects both the old and the new secret

## Slice 9 — The SDK catches up: Phase 2 surface in TypeScript

Everything above, typed and mockable — the contract rolai will migrate onto.

- [ ] `beecon.tools.list({ integrationId | providerSlug, includeDeprecated? })` returns typed tools with input and output schemas, cursor-paginated
- [ ] `beecon.tools.get(slug)` returns one tool's detail
- [ ] `beecon.integrations.getExpectedParams(integrationId)` returns the expected-param fields
- [ ] `beecon.connections.list({ userId })`, `.disable(id)`, `.delete(id)`, and `.reconnect(id, { redirectUri })` cover the full lifecycle; reconnect returns the same connection id with a fresh redirectUrl
- [ ] `beecon.userTokens.create({ userId, expiresIn? })` mints a token locally using the constructor-configured signing secret, and throws a clear error when no signing secret is configured
- [ ] `beecon.files.upload(...)` returns the file id and downloadUrl
- [ ] Tool execution results are typed with the optional `nextCursor`; an HTTP 429 from execute surfaces as a typed `RateLimitedError` carrying `retryAfter`
- [ ] The SDK never writes the signing secret to logs, errors, or serialized output (parity with the API-key guarantee)
- [ ] The quickstart is extended with: the browser-token connect flow, connecting Hubspot, paging through a list tool, and uploading a file into a tool call

---

## API Shape (indicative)

```
Auth additions:
  user-token endpoints -> Authorization: Bearer <JWT>   (HS256, kid = signing-secret id,
                                                         sub = user_..., exp ~2h)

Catalog
GET  /api/v1/tools?integrationId=|providerSlug=&includeDeprecated=&cursor=&limit=   (org | user token)
     -> { items: [{ slug, name, description, inputSchema, outputSchema, deprecated,
                    provider: { slug, name, logo } }], nextCursor }
GET  /api/v1/tools/{slug}                                                          (org)
GET  /api/v1/integrations/{intgId}/expected-params                                 (org | user token)
     -> { providerName, fields: [{ name, displayName, description, required, secret }] }

Connections
GET  /api/v1/connections?userId=&cursor=&limit=          (org; user token -> own user implied)
POST /api/v1/connections/{connId}/disable                (org)          -> { id, status: "DISCONNECTED" }
DELETE /api/v1/connections/{connId}                      (org)          -> 204
POST /api/v1/connections/{connId}/reconnect              (org | user token, own)
     { redirectUri } -> 201 { id: "conn_<same>", status, redirectUrl }
POST /api/v1/connections/initiate                        (org | user token: userId from token)

Execution
POST /api/v1/tools/{slug}/execute                        (org)
     -> 200 { successful, error, data, nextCursor? }
     -> 429 + Retry-After header, { "error": { "code": "rate_limited", "message": "..." } }

Files
POST /api/v1/files            (org, multipart)  -> 201 { id: "file_...", name, mimeType, size, downloadUrl }
GET  /api/v1/files/{fileId}/download             (org)

Access
POST /api/v1/organizations/{orgId}/signing-secrets            (admin) -> 201 { id: "usk_...", secret, prefix }
GET  /api/v1/organizations/{orgId}/signing-secrets            (admin) -> [{ id, prefix, createdAt }]
POST /api/v1/organizations/{orgId}/api-keys/{keyId}/rotate    (admin) { overlapHours? }
     -> 201 { id, key: "beecon_sk_...", prefix, overlapExpiresAt }

Operability
GET  /metrics                                                 (admin)  Prometheus text format

Connection statuses: INITIATED | ACTIVE | EXPIRED | DISCONNECTED
```

## Out of Scope (Phase 2)

- Triggers, trigger instances, outbox, signed webhooks, service bus, proactive
  token-refresh scheduler, reconciliation job, `connection.expired` reverse message
  (Phase 3) — PD18's on-demand refresh is the only refresh in this phase
- Admin UI, org-level governance (integration allow-lists/visibility — PD7's
  all-orgs visibility stands), scope-restricted API keys, log retention config,
  org config export/import (Phase 4)
- Registry service, definition bundle semver/pin/diff, Membrane-export importer
  (Phase 5) — Phase 2 definitions load from the local directory; `formatVersion`
  exists so the registry can version bundles later
- AI-framework SDK adapters (OpenAI/Mastra shapes) (Phase 5)
- Provider #3; any Outlook/Hubspot tool beyond PD16's set
- Raw-action pass-through proxy (decided against — PD26)
- File retention/expiry and non-local file storage adapters (port only)
- Alert rules/dashboards — Phase 2 ships the `/metrics` source only (PD24)

## Technical Context

- **Stack (binding):** Go hexagonal monolith under `server/` (modules: organizations,
  access, catalog, connections, execution, logging, connectweb, app); Go-template
  middle-man pages; TypeScript SDK at `packages/sdk`. Postgres + SQLite behind the
  persistence port; org isolation at the port (Phase 1 architecture tests stand).
- **Ids (binding):** CUID2 with type prefixes — new prefixes this phase: `file_`,
  `usk_` (user-token signing secret). Tools remain slug-addressed (PD14). One
  immutable id per entity forever; reconnect must never mint a second connection id.
- **Boundaries:** per `.claude/BOUNDARIES.md` — expected params and the finalized
  definition format live in `catalog/`; refresh + lifecycle in `connections/`;
  rate-limit normalization, retry, pagination, and FileUpload in `execution/`; user
  tokens, signing secrets, and rotation in `access/`; redaction changes in `logging/`.
- **Existing crypto:** reuse the Phase 1 AES-256-GCM vault (`BEECON_ENCRYPTION_KEY`)
  for client secrets, param values, and signing secrets — no second key or scheme.
- **Envelope (binding):** PD5 errors and the PD6 `{successful, error, data}` split
  stand; Phase 2 adds only the optional `nextCursor` (PD15) and the HTTP 429
  rate-limit carve-out (PD21).
- **Reference shapes:** `temp/outlook-get-message.yaml` (path cases, path/query
  mapping, curated output schema — what the format must express),
  `temp/outlook-integration.yaml` (integration record). The Membrane trigger sample
  matters in Phase 3, not here.
- **New config:** `BEECON_FILES_DIR`, `BEECON_FILE_MAX_BYTES` (default 20 MB).
- **Keep it small:** still one binary, no queues, no schedulers (on-demand refresh is
  request-path, not a job), no new runtimes.
- Risk level: HIGH

---

- [x] Reviewed <!-- approved by developer ("let's do it... I will review after done"), 2026-07-12; six PDs confirmed via Q&A, recorded in ADR-0006..0011 -->
