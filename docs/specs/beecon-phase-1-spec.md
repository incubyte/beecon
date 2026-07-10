# Spec: Beecon Phase 1 — Walking Skeleton

> One user connects Outlook and runs one tool.
> Discovery: [beecon-discovery.md](./beecon-discovery.md) (Phase 1 of the Milestone Map).
> Boundaries: `.claude/BOUNDARIES.md`. Context: `.claude/bee-context.local.md`.

## Overview

Stand up the thinnest end-to-end path of the Beecon platform: a Go installation boots
against Postgres (SQLite for local/tests) behind a hexagonal persistence port; an
Organization and User exist via API; an org-scoped API key authenticates every request
with data isolation enforced at the persistence layer; a locally-defined Outlook
provider + Integration lets a user complete OAuth through Beecon's middle-man pages
into an ACTIVE Connection with a stable id and encrypted tokens; one Outlook tool
executes through the TypeScript SDK returning `{successful, error, data}`; every
provider call is logged with token redaction and queryable via API.

Risk: HIGH — this phase carries the security-critical foundations (org isolation, API
key auth, encrypted token storage, OAuth CSRF safety). Edge cases below are first-class.

## Proposed Decisions (synthesis — correct at review if wrong)

AskUserQuestion was unavailable during speccing; these are reasoned proposals, each
grounded in the discovery doc and rolai context. Everything else in this spec follows
binding decisions and is not up for debate here.

1. **PD1 — Installation admin auth.** Installation-level operations (create
   organization, issue org API keys, create Integrations) authenticate with an
   installation admin key supplied via environment config (`BEECON_ADMIN_API_KEY`).
   No admin-user entity in Phase 1; the Admin UI (Phase 4) will revisit.
2. **PD2 — Who creates users.** The consumer's server creates its users with its
   organization API key (`POST /api/v1/users`, org inferred from the key) — mirrors how
   rolai provisions `entityId`s. Installation admin does not manage users in Phase 1.
3. **PD3 — API key value format.** Secret value `beecon_sk_<random>`; the key *entity*
   id is `key_<cuid2>`. First 12 chars of the secret stored as a plaintext lookup
   prefix, remainder hashed. Phase 1 ships issue + immediate revoke; rotation with
   overlap window is deferred (Phase 2/4).
4. **PD4 — redirectUri validation.** Each organization carries an
   `allowedRedirectUris` list (exact URL or origin match), settable by the installation
   admin. `initiate` rejects any redirectUri not on the list. Empty list = initiate
   with a redirectUri always fails (secure default, no open redirect).
5. **PD5 — Error response format.** Non-2xx responses use
   `{"error": {"code": "<machine_code>", "message": "<human text>"}}` with conventional
   HTTP status codes (`401 unauthorized`, `404 not_found`, `422 validation_failed`).
   Cross-org access always surfaces as `not_found` (no existence leak).
6. **PD6 — Tool execution envelope split.** Platform-level failures (bad key, unknown
   connection/tool, cross-org) are HTTP errors per PD5. Tool-level failures (invalid
   args against schema, non-ACTIVE connection, upstream provider error) return HTTP 200
   with `{successful: false, error, data: null}` — the shape rolai's retry logic
   already consumes.
7. **PD7 — Integration visibility.** Phase 1: an Integration is visible to all
   organizations in the installation. Per-organization visibility toggles are Phase 4
   governance.
8. **PD8 — First tool.** Slug `outlook-list-messages` (provider-prefixed kebab-case).
   Tools are addressed by slug in Phase 1; `tool_` entity ids arrive with the real
   catalog (Phase 2). Graph call: `GET /v1.0/me/messages` supporting `top`, `skip`,
   `select`, `filter` inputs.
9. **PD9 — OAuth scopes for Outlook.** `offline_access Mail.Read User.Read` —
   `offline_access` yields the refresh token we must store (auto-refresh itself is
   Phase 3), `User.Read` lets the callback capture account metadata (email/display
   name) for the Connection. Declared in the provider definition, not hardcoded.
10. **PD10 — Logs API minimal shape.** `GET /api/v1/logs` filtered by `connectionId`,
    `userId`, `toolSlug`, `from`/`to`, cursor-paginated, newest first. Entries carry
    ids, tool slug, HTTP status, duration, and redacted request/response bodies.
    Retention policy is out of scope for Phase 1 (config lands with Phase 4).
11. **PD11 — Failed OAuth leaves the Connection INITIATED.** Denial, state mismatch,
    or token-exchange failure never mint a new status; the user can retry from a fresh
    initiate. Statuses in Phase 1: `INITIATED`, `ACTIVE` (EXPIRED/DISCONNECTED arrive
    Phase 2/3).
12. **PD12 — Config surface.** `BEECON_DATABASE_DRIVER` (`postgres`|`sqlite`) +
    `BEECON_DATABASE_URL`, `BEECON_ADMIN_API_KEY`, `BEECON_ENCRYPTION_KEY` (32-byte,
    base64, AES-256-GCM for token storage), `BEECON_BASE_URL` (public URL used to build
    connect-page and OAuth-callback URLs).

---

## Slice 1 — Boots and breathes: installation, persistence port, organizations

The skeleton: one Go binary, two persistence adapters behind one port, the first
entity with the id scheme, and the installation admin key guarding it.

- [ ] `beecon serve` boots against Postgres using environment configuration and answers a health endpoint
- [ ] The same binary boots against SQLite when configured, with identical API behavior (this adapter is what tests run on)
- [ ] Database schema is created/migrated automatically at boot on both adapters
- [ ] Boot fails fast with a clear message when database config is missing or the database is unreachable
- [ ] Installation admin can create an organization with a name; response includes an immutable `org_`-prefixed CUID2 id
- [ ] Installation admin can fetch an organization by id
- [ ] Creating an organization with an empty or missing name shows a validation error
- [ ] Requests to installation-level endpoints with a missing or wrong admin key are rejected as unauthorized
- [ ] Domain and application code depend only on the persistence port — no Postgres/SQLite imports outside the adapters (verified by an architecture test)

## Slice 2 — Keys and walls: org API keys, users, data isolation

The security core: org-scoped keys verified on every request, and isolation that makes
cross-org reads impossible by construction.

- [ ] Installation admin can issue a server API key for an organization; the full secret is returned exactly once, at creation
- [ ] Issued key secrets carry an identifiable prefix (`beecon_sk_`) so they are recognizable in config and logs
- [ ] Listing an organization's keys shows key id, prefix, and created date — never the full secret
- [ ] The full key secret is not recoverable from the database (stored hashed; a database dump does not contain it)
- [ ] A request with a valid org key succeeds and operates only on that key's organization
- [ ] A request with a missing, malformed, or unknown key is rejected as unauthorized
- [ ] A request with a revoked key is rejected as unauthorized
- [ ] Consumer can create a user (name + optional consumer-side external id) in its organization; response includes a `user_`-prefixed id
- [ ] Consumer can fetch its own user by id
- [ ] Fetching a user that belongs to another organization returns not-found (no existence leak)
- [ ] Every org-scoped persistence-port operation requires an organization id — a query without org scope cannot be expressed (verified by an architecture/port test)

## Slice 3 — Outlook exists: provider definition, Integration, initiate

The catalog seed: Outlook described declaratively (registry comes in Phase 5),
made connectable with the installation's OAuth client credentials, and the connection
handshake started.

- [ ] The Outlook provider definition loads at boot from a local declarative file (name, logo, OAuth authorize/token endpoints, scopes, tool definitions)
- [ ] Boot fails with a clear message naming the file and field when a provider definition is invalid
- [ ] Installation admin can create an Outlook Integration with OAuth client id + client secret; response includes an `intg_`-prefixed id
- [ ] The OAuth client secret never appears in any API response after creation
- [ ] Installation admin can set an organization's allowed redirect URIs
- [ ] Consumer can list integrations (id, provider name, logo, auth scheme) available to its organization
- [ ] Consumer can initiate a connection with userId + integrationId + redirectUri; response is `{id, status: "INITIATED", redirectUrl}` with a `conn_`-prefixed id
- [ ] The returned redirectUrl points at Beecon's own connect page and is bound to exactly that connection attempt
- [ ] Initiating with a redirectUri not on the organization's allow-list is rejected with a validation error
- [ ] Initiating with an unknown userId or integrationId, or a userId from another organization, is rejected (cross-org as not-found)
- [ ] Consumer can fetch a connection by id showing status, provider, and user — connections of other organizations return not-found

## Slice 4 — The handshake: middle-man pages, OAuth callback, ACTIVE connection, encrypted tokens

The end user's journey: popup opens Beecon's page, Microsoft consent, back through
Beecon's callback, round-trip to the consumer — with a stable id and tokens that never
leave the vault.

- [ ] Opening the connect page (the redirectUrl from initiate) shows the provider name/logo and a Connect action, rendered by a Go template, usable inside a popup window
- [ ] Opening the connect page with an invalid, expired, or already-completed connect link shows an error page and never forwards to the provider
- [ ] Choosing Connect sends the browser to Microsoft's consent page carrying the Integration's client id, the provider definition's scopes, and a single-use CSRF state parameter
- [ ] After the user consents, the callback exchanges the code for tokens, the connection becomes ACTIVE, and the browser is redirected to the consumer's redirectUri with the connection id and a success status
- [ ] The ACTIVE connection keeps the exact id returned by initiate (stable id — never a second id)
- [ ] The connection records provider account metadata (account email / display name) visible via get-connection
- [ ] A callback whose state parameter is missing, unknown, expired, or already used shows an error page and the connection does not become ACTIVE
- [ ] When the user denies consent at Microsoft, the browser is returned to the consumer's redirectUri with an error status and the connection stays INITIATED
- [ ] When the token exchange fails at the provider, an error page is shown and the connection does not become ACTIVE
- [ ] Access and refresh tokens are stored encrypted at rest — raw token values appear nowhere in the database, in any API response, or in any log line
- [ ] Boot fails fast with a clear message when the token encryption key is missing or malformed

## Slice 5 — It does work: tool execution, logging with redaction

The payoff call: one Outlook tool runs against a live connection, and every provider
exchange is written down — minus the secrets.

- [ ] Consumer can execute `outlook-list-messages` with `{userId, connectionId, arguments}` and receive `{successful: true, error: null, data}` containing the mailbox messages
- [ ] Arguments are validated against the tool's input JSON Schema; invalid arguments return `{successful: false, error, data: null}` without calling the provider
- [ ] Executing an unknown tool slug returns not-found
- [ ] Executing against a connection that is not ACTIVE returns `{successful: false, error, data: null}` with a status-explaining error
- [ ] Executing against a connection of another organization returns not-found
- [ ] Executing with a connectionId that does not belong to the given userId returns an error without calling the provider
- [ ] An upstream provider error (4xx/5xx from Graph) returns `{successful: false, error, data: null}` surfacing the provider's status and message
- [ ] Every tool execution and OAuth token exchange writes a log entry with organization/user/connection ids, tool slug (where applicable), duration, and status
- [ ] Authorization headers, tokens, and OAuth client secrets are redacted from logged request/response bodies before persistence
- [ ] Consumer can query logs filtered by connectionId, userId, toolSlug, and time range, cursor-paginated, seeing only its own organization's entries

## Slice 6 — The consumer's hands: TypeScript SDK

The contract rolai will actually code against: constructor-injected, mockable, typed.

- [ ] `new Beecon({ apiKey, baseUrl })` constructs a client; the client's surface is exported as an interface so consumers can inject a mock (vi.fn-style)
- [ ] `beecon.users.create({ name, externalId? })` creates a user and returns its id
- [ ] `beecon.integrations.list()` returns the integrations available to the organization
- [ ] `beecon.connections.initiate({ userId, integrationId, redirectUri })` returns `{ id, status, redirectUrl }`
- [ ] `beecon.connections.get(id)` returns status and provider account metadata — never tokens
- [ ] `beecon.tools.execute(slug, { userId, connectionId, arguments })` returns a typed `{ successful, error, data }`
- [ ] `beecon.logs.list(filters)` returns redacted log entries with cursor pagination
- [ ] Platform HTTP errors surface as typed SDK errors carrying the API's error code and message
- [ ] The SDK never writes the API key to logs, errors, or serialized output
- [ ] A quickstart document walks the popup connect flow end-to-end: initiate on the server, open redirectUrl in a popup, receive the redirectUri round-trip, execute the first tool

---

## API Shape (indicative)

```
Auth: Authorization: Bearer <key>
  installation endpoints -> BEECON_ADMIN_API_KEY
  org endpoints          -> beecon_sk_... (org inferred from key)

POST /api/v1/organizations                     (admin)  { name, allowedRedirectUris? }
                                               -> 201 { id: "org_...", name, createdAt }
GET  /api/v1/organizations/{orgId}             (admin)
PATCH /api/v1/organizations/{orgId}            (admin)  { allowedRedirectUris }
POST /api/v1/organizations/{orgId}/api-keys    (admin)  -> 201 { id: "key_...", key: "beecon_sk_...", prefix }
GET  /api/v1/organizations/{orgId}/api-keys    (admin)  -> [{ id, prefix, createdAt }]
DELETE /api/v1/organizations/{orgId}/api-keys/{keyId} (admin, revoke)

POST /api/v1/integrations                      (admin)  { providerSlug: "outlook", clientId, clientSecret }
                                               -> 201 { id: "intg_...", providerSlug, name, logo }

POST /api/v1/users                             (org)    { name, externalId? } -> 201 { id: "user_..." }
GET  /api/v1/users/{userId}                    (org)
GET  /api/v1/integrations                      (org)
POST /api/v1/connections/initiate              (org)    { userId, integrationId, redirectUri }
                                               -> 201 { id: "conn_...", status: "INITIATED", redirectUrl }
GET  /api/v1/connections/{connectionId}        (org)    -> { id, status, providerSlug, userId, account? }
POST /api/v1/tools/{slug}/execute              (org)    { userId, connectionId, arguments }
                                               -> 200 { successful, error, data }
GET  /api/v1/logs?connectionId=&userId=&toolSlug=&from=&to=&cursor=&limit=  (org)

GET  /connect/{token}          middle-man connect page (Go template)
GET  /connect/oauth/callback   provider OAuth callback (code + state)

Errors: { "error": { "code": "unauthorized" | "not_found" | "validation_failed" | ..., "message": "..." } }
```

## Out of Scope (Phase 1)

- Hubspot; the finalized declarative provider-definition format (Phase 2 — Phase 1's
  Outlook definition file only needs to be good enough for Outlook)
- Pre-auth expected params / param-collection page (Outlook needs none; Phase 2)
- Connection lifecycle beyond initiate + get: disable, delete, reconnect-with-same-id,
  EXPIRED/DISCONNECTED statuses (Phase 2/3)
- User-scoped short-lived browser tokens (Phase 2) — Phase 1 connect pages are
  authenticated by the single-use connect link minted at initiate
- Auto token refresh, reconciliation, triggers, webhooks/outbox, service bus (Phase 3)
- Rate-limit normalization + platform-side retry, pagination convention, file upload
  (Phase 2)
- API key rotation with overlap window and scope-restricted keys (issue/revoke only)
- Admin UI, org-level governance/visibility rules, log retention config (Phase 4)
- Registry service, definition bundles/versioning, migration importer (Phase 5)
- AI-framework SDK adapters (OpenAI/Mastra shapes) (Phase 5)

## Technical Context

- **Stack (binding):** Go backend, hexagonal (domain/ports/adapters);
  `D:/rolai/source/rolai-university` is the reference implementation of the style.
  Middle-man pages are Go templates served by the api binary. SDK is TypeScript.
- **Ids (binding):** CUID2 with type prefixes via `github.com/akshayvadher/cuid2` —
  `org_`, `user_`, `key_`, `intg_`, `conn_` in this phase. One immutable id per entity
  forever.
- **Persistence (binding):** Postgres (prod) + SQLite (local/tests) behind one
  persistence port; org isolation enforced at the port.
- **Boundaries:** per `.claude/BOUNDARIES.md` — this phase touches `organizations/`,
  `access/`, `catalog/`, `connections/`, `execution/`, `logging/`, `apps/api/`,
  `apps/connect-ui/`, `packages/sdk/`. Modules import only declared dependencies.
- **Reference shapes:** `temp/outlook-integration.yaml` and
  `temp/outlook-get-message.yaml` show the Membrane definitions the local Outlook
  provider file replaces (input schema, HTTP mapping, curated output schema).
- **Keep it small:** thin wrapper ethos — no queues, no schedulers, no extra runtimes
  in this phase.
- Risk level: HIGH

---

- [x] Reviewed <!-- approved verbally by developer ("let's go ahead"), 2026-07-10 -->
