# Discovery: Beecon — Self-Hosted Integration Platform

> Status: draft PRD from discovery. Grounded in the developer's requirement list and the
> rolai context report (`.claude/bee-context.local.md`). Where the developer left a gap,
> this document proposes an answer and marks it as a hypothesis or open question.

## Why

Rolai (and soon eCW) depends on two third-party integration platforms — Membrane
(integration.app) and Composio — to let end users connect external accounts (Outlook,
Hubspot, Google Drive, ...) and to execute tools and receive triggers against those
accounts. This dependency is painful and risky:

- **Two vendors, one job.** Rolai maintains a routing layer (`IntegrationRoutingService`)
  that splits ~20 operations across two providers with different auth models, id formats,
  and behaviors. Every new integration means deciding "which vendor?" and living with
  that vendor's quirks.
- **The vendors' rough edges leak into rolai's code.** Reconnecting an account changes
  the connection id, so rolai string-replaces ids inside stored flow JSON. Composio has
  two id systems (UUID and nano id) with a translation API. Membrane triggers are flows
  with a naming convention and a webhook URL smuggled under eight alias keys. Provider
  output schemas are unreliable, so rolai captures response schemas at runtime.
- **Security gaps we can't fix from outside.** Webhooks arriving at rolai are unsigned
  today (an open TODO). Rate-limit handling is pushed onto the consumer.
- **No control over hosting or data.** Tokens and logs live in vendor clouds; each
  installation (Rolai, eCW) wants to run its own instance with its own data.

- **Self-hosting today is either impossible or heavyweight.** Composio cannot be
  self-hosted at all. Membrane can be, but its self-hosted footprint is large — far more
  infrastructure than the job requires. At its core this product is a thin wrapper
  around provider APIs plus a background job that auto-refreshes tokens; **it should be
  small**.

<!-- -------- bee-comment -------- -->
> **@developer**: A couple of reasons, Composio is not self hosted, Membrane is self hosted but it take a lot of resources, this is just a wrapper and a job for automatically refresh tokens, it should be small. Keep postgres but probably for local sqllite as well?
> **@bee**: Added a "Why" bullet capturing both motivations (Composio: no self-hosting; Membrane: heavyweight self-hosting) and made "lightweight footprint" an explicit design principle — it now also appears in Success Criteria. Updated H5 and the Stack section: Postgres stays the production database, and we add a SQLite adapter for local development/tests — which doubles as real proof that the persistence port is swappable (better than proving it "by boundary only").
> - [x] mark as resolved
<!-- -------- /bee-comment -------- -->

Beecon replaces both vendors with one lightweight, self-hosted platform that Rolai and
eCW each run themselves — designed from day one around the pain points the vendors
created. Adoption is incremental: Beecon first slots into rolai as a **third provider**
behind `IntegrationRoutingService`, alongside Membrane and Composio, then absorbs
integrations provider-by-provider until the other two are removed.

<!-- -------- bee-comment -------- -->
> **@developer**: this will become our third provider, however we will replace others eventually
> **@bee**: Reframed the adoption story: Beecon enters rolai as a third provider behind the existing routing seam (low-risk, incremental), with full replacement of Membrane and Composio as the end state. Updated the Phase 5 milestone to match (add as third provider → migrate provider-by-provider → remove the other two once parity is proven).
> - [x] mark as resolved
<!-- -------- /bee-comment -------- -->



## Who

- **Consuming developer** — builds product features against Beecon's TypeScript SDK
  (rolai's server team is the first). Wants one clean, injectable, mockable API instead
  of two vendor SDKs.
- **End user** — a person inside a consumer product who clicks "Connect Outlook",
  completes OAuth via Beecon's connect pages, and expects their integration to keep
  working (tokens refreshed silently, clear signal when re-auth is needed).
- **Organization admin** — governs which integrations an organization's users may
  connect (allow-lists, defaults). Today rolai implements this itself; Beecon absorbs it.
- **Installation operator** — the team that deploys and runs a Beecon instance
  (Rolai ops, eCW ops). Manages organizations, keys, providers, and logs through the
  Admin UI.
- **Catalog maintainer** — publishes and versions provider/tool/trigger definitions to
  the shared tool registry that all installations pull from.

## Success Criteria

- Rolai runs entirely on Beecon: Membrane and Composio SDKs, keys, and id-translation
  shims are removed from the rolai codebase.
- The eight vendor pain points are structurally impossible in Beecon: stable connection
  ids across re-auth, one canonical id format, first-class triggers, signed webhooks,
  normalized rate-limit errors, accurate tool output schemas, unified auth model, and
  built-in org-level governance.
- A second installation (eCW) can be stood up from documentation alone, pulling provider
  definitions from the shared registry — no Beecon-team hand-holding.
- Adding a new provider is primarily a registry-definition exercise, not a platform
  code change (small auth-hook code allowed).
- An end user's connected account survives token expiry invisibly (auto-refresh) and,
  when refresh fails, the consumer is notified via a signed event within minutes.
- The platform stays **small**: at its core a thin wrapper around provider APIs plus a
  token-refresh job — one deployable service (registry aside), runnable locally on
  SQLite, at a fraction of Membrane's self-hosted footprint.

## Problem Statement

Rolai's integration capability is built on two external vendors whose instability
(changing ids, hacked triggers, unreliable schemas) and opacity (unsigned webhooks,
vendor-hosted credentials) force rolai to carry compensating hacks and accept security
gaps. We are building Beecon, a self-hosted, multi-organization integration platform
with one clean contract, so each consumer installation owns its data, its credentials,
and a
provider model designed around the failures we've already lived through.

## Hypotheses

- **H1 — Contract sufficiency:** The ~20-operation surface rolai actually uses (catalog,
  connection lifecycle, expected params, tool catalog/execution, triggers, file upload,
  user tokens) is the complete v1 API. Anything neither rolai nor eCW calls today is out.
- **H2 — Declarative providers:** A provider is a declarative definition (auth config,
  tool mappings, trigger definitions, JSON Schemas) stored in the registry, plus at most
  a small code hook for OAuth quirks. Outlook and Hubspot both fit this model; provider
  #3 proves it.
- **H3 — Stable connection identity:** Keeping the same Connection id across re-auth
  eliminates rolai's flow-schema rewriting entirely (no consumer-side id migration code
  needed against Beecon).
- **H4 — "Mapping" means tool→endpoint mapping:** The developer's "Mapping" entity is
  the mapping from a canonical tool definition (slug + input/output JSON Schema) to the
  provider's actual endpoint and request/response shape, living inside the provider
  definition. *(✅ Confirmed by developer at review.)*
- **H5 — Postgres behind a port, SQLite proves it:** Hexagonal persistence ports keep
  the door open for other stores (e.g. Mongo). We ship the Postgres adapter for
  production **plus a SQLite adapter for local development/tests** (developer decision
  at review) — the second adapter is the proof that the port boundary actually works.
  Mongo remains possible later but is not built.
- **H6 — Registry as pull-sync:** Installations pull versioned definition bundles from
  the external registry on demand (import), review, and activate; the registry never
  reaches into installations. This is enough for Rolai + eCW; push/auto-update is not
  needed in v1.

## Decisions Already Made (do not reopen)

- V1 = full platform: core engine + Admin UI + TypeScript SDK + external tool registry,
  then onboard Rolai.
- Self-hosted per installation (Rolai, eCW each run their own); multiple isolated
  organizations (with their users) *within* each installation.
- Clean API — no Membrane compatibility layer; rolai rewrites its adapter against the
  Beecon SDK.
- UI visual design is deferred to a separate design brief after discovery.

## Proposed Decisions (made in synthesis; correct at review if wrong)

### Entity model and naming

The "User↔Outlook" entity is named **Connection**: a user's authorized link to one
external account at one provider.

- **Installation** — one self-hosted Beecon deployment (Rolai's, eCW's). Not a database
  entity so much as the deployment boundary.
- **Organization** — the top-level isolation unit inside an installation (developer
  decision: **Organization** is the one and only term — the word "tenant" is retired;
  layering is Installation → Organization → User). Each organization is both the
  **isolation unit** (server API keys, webhook
  secrets, and JWT signing secrets are organization-scoped; all data rows carry an
  organization id; isolation is enforced at the persistence port) and the **governance
  unit** (which providers/integrations its users may connect, onboarding defaults,
  visibility rules).
- **User** — an end user inside an organization (maps to rolai's `entityId`). Owns
  Connections.
- **Provider** — the external system itself ("Outlook"). Defined declaratively in the
  registry (auth scheme, OAuth endpoints, expected pre-auth params, branding) with
  optional code hooks. Carries no credentials of ours — purely the description of the
  system.
- **Integration** — a Provider made connectable in this installation: Provider + our
  OAuth client credentials ("Outlook with Rolai's client id and secret"). Created at
  installation level; each organization chooses whether it is visible to its users.
  (This is what Membrane calls an Integration and Composio calls an Auth Config.)
- **Tool** — a callable capability of a provider (canonical slug, description, input
  JSON Schema, *accurate* output JSON Schema, deprecation flag).
- **Mapping** — how a Tool binds to the provider's real endpoint: HTTP shape, field
  mapping, pagination convention, rate-limit interpretation. Part of the provider
  definition. *(Per H4.)*
- **Trigger definition** — a subscribable event type of a provider (slug, config schema,
  payload schema). First-class, not a disguised flow. Each definition declares its
  **ingestion mode: push or pull** (developer decision — push where the provider
  supports it, e.g. MS Graph change notifications / Hubspot webhooks, with polling as a
  per-trigger fallback declared in the provider definition; this matches the "Pull /
  Push" items under Triggers in the developer's notes).
- **Trigger instance** — a live subscription binding a trigger definition to a
  Connection with config; has its own stable id.
- **Connection** — user ↔ provider account. Fields include stable id (never changes,
  including across re-auth), status (INITIATED / ACTIVE / EXPIRED / DISCONNECTED),
  nickname, provider account metadata. Encrypted access/refresh tokens live in a
  separate credentials store keyed by Connection, never exposed via API or logs.
- **Event log** — request/response log entries (see Logging).

The three levels, disambiguated — one line each:

1. **Provider** = "Outlook" — the system, described in the registry.
2. **Integration** = "Outlook with our client id & secret" — installation-level;
   organizations choose visibility.
3. **Connection** = "my connected Outlook" — one user's account, with its tokens.

<!-- -------- bee-comment -------- -->
> **@developer**: I am usually confused between connection and integration and auth config. Is there non confusing thing? For example There is a "Outlook" then there is a "outlook with my organization's client id and secret" (By the way it is okay that we create outlook with Rolai's client id and secret at installtion level, each organization can decide wether to show or not), then there is "My connected outlook"
> **@bee**: Named all three levels explicitly and added the missing middle one as a first-class **Integration** entity: Provider (the system) → Integration (Provider + installation's OAuth client credentials, created at installation level, per-organization visibility — exactly your "created with Rolai's client id/secret, orgs decide whether to show") → Connection (a user's connected account). Added a one-line-each disambiguation table above so the three never blur again.
> - [x] mark as resolved
<!-- -------- /bee-comment -------- -->


Identifiers: **CUID2 with type prefixes** (developer decision at review) — ids look
like `conn_<cuid2>`, `tool_<cuid2>`, `trg_<cuid2>`. Generator:
`github.com/akshayvadher/cuid2` (Go port of paralleldrive/cuid2 — `CreateId()` /
`CreateIdOf(length)`, SHA3-512 based, configurable entropy). CUID2 fitness review:
collision-resistant (entropy + counter + host fingerprint hashed with SHA3-512,
designed for horizontal scaling), secure by design (non-guessable; deliberately **not**
time-sortable — the authors treat sortable ids as an information leak), URL-safe
lowercase base36 starting with a letter, default 24 chars. Accepted trade-off: no
UUIDv7-style B-tree insert locality — irrelevant at a self-hosted installation's
volume; order by `created_at` instead. The hard requirement stands: **exactly one
immutable id per entity, forever** — never a second id system (this is what kills
Composio's dual-id pain and enables stable Connection ids across re-auth).

<!-- -------- bee-comment -------- -->
> **@developer**: do we need cannoical id format? What is industry standard? What if there is no canonical id format?
> **@bee**: Separated the requirement from the recommendation. Required: one immutable id per entity (this is what fixes the Composio dual-id and reconnect-id pain — it's about *count*, not *format*). Recommended-but-optional: Stripe-style type prefixes (industry standard alongside plain UUIDs) for log readability and mix-up prevention.
> - [x] mark as resolved
<!-- -------- /bee-comment -------- -->

<!-- -------- bee-comment -------- -->
> **@developer**: let's just use CUID2, this if there are not go version available https://github.com/akshayvadher/cuid2, review if CUID2 is good enough
> **@bee**: Adopted CUID2 as the id generator (keeping the type prefixes, which compose fine: `conn_<cuid2>`). Reviewed it — good enough: collision-resistant via entropy + counter + fingerprint + SHA3-512, non-guessable, URL-safe base36 starting with a letter, built for horizontal scaling. The one trade-off vs UUIDv7 is that CUID2 is deliberately not time-sortable (its authors consider sortable ids an info leak), so no B-tree insert-locality benefit — negligible at self-hosted volume; we order by created_at. I verified your Go port exists and matches the original API (CreateId/CreateIdOf, configurable length/entropy).
> - [ ] mark as resolved
<!-- -------- /bee-comment -------- -->


### Access management

- **Organization server API keys** — long-lived, shown once, stored hashed,
  prefix-identifiable, rotatable with overlap window. Optionally **scope-restricted
  ("use-based")**: a key can be limited to capabilities (e.g. execute-only, read-only
  catalog).
- **User-scoped short-lived tokens** — minted by the consumer's server via SDK, signed
  with the organization's secret; used by the browser for the connect UI (replaces
  Membrane's per-user JWT). Default expiry ~2 hours.
- **Webhook signing secret** — per organization; every outbound event is HMAC-signed
  with a timestamp; SDK ships a one-line verifier.

### Token lifecycle

- Access tokens auto-refresh ahead of expiry via a scheduler (jittered, retried).
- Refresh failure → Connection status EXPIRED → **reverse message** (signed
  `connection.expired` event) delivered to the consumer's webhook endpoint, so the
  consumer can prompt the user to reconnect. Reconnect keeps the same Connection id.
- Reconciliation job periodically verifies ACTIVE connections against providers.

### Delivery architecture (webhooks and service bus)

- All outbound consumer notifications (trigger fired, connection expired/disconnected)
  flow through one internal **outbox**: events are persisted first, then delivered —
  so no delivery channel can lose events.
- **Webhooks** are the primary channel: HMAC-signed + timestamped, retried with
  exponential backoff, idempotency key on every delivery, per-connection ordering
  best-effort.
- **Service bus** is a second delivery adapter on the same outbox (e.g. Azure Service
  Bus for eCW), chosen per organization. Later phase.

### Import / export

- **Import:** versioned provider/tool/trigger definition bundles from the external
  registry (pull, review diff, activate — per H6).
- **Export/import of organization configuration:** governance rules, key metadata
  (never secrets) — for environment promotion and installation migration.
- Connections and credentials are deliberately **not** exportable in v1.

### Tool registry (separate service)

- A separately deployed service holding versioned provider definition bundles
  (provider + tools + mappings + triggers + schemas). Installations authenticate to it
  and pull; it never calls into installations. Can be shared across Rolai and eCW.
- Accurate `outputParameters` are a first-class requirement of a definition — validated
  against recorded real responses before publish (this is a differentiator; both vendors
  fail here).

- **Bundle versioning:** every provider bundle is semantically versioned. v1 of the
  Outlook bundle might carry only `list-messages`; v1.1 adds `get-message`. Additive
  changes (new tools/triggers) bump the minor version; breaking changes to an existing
  tool's schema bump the major. Installations see a diff (added / changed / removed
  tools) before activating a new version, and stay pinned to their current version
  until they explicitly upgrade.

<!-- -------- bee-comment -------- -->
> **@developer**: every new registry/marketplace can have versions - for example v1 might have outlook list email acction but v1.1 might have outlook list email action and outlook get email action
> **@bee**: Made bundle versioning explicit with your exact example: Outlook v1 = list-messages only, v1.1 adds get-message. Defined the semver rule (additive tools → minor bump, breaking schema changes → major bump) plus pinning-and-diff behavior on the installation side, so upgrades are always a deliberate, reviewable act.
> - [x] mark as resolved
<!-- -------- /bee-comment -------- -->


### Logging

- Every provider request/response and every consumer-facing delivery is logged with:
  organization/user/connection ids, tool slug, duration, status, and **redacted** bodies
  (tokens, auth headers, and configured sensitive fields stripped before persistence).
- Viewable in the Admin UI, filterable per organization/connection. Configurable
  retention, default 30 days.

### Stack (discovery-level)

- Backend: **Go**, hexagonal architecture (domain/ports/adapters) — developer decision
  at review, replacing the earlier NestJS proposal; `D:/rolai/source/rolai-university`
  is the team's reference implementation of this style. Go reinforces the "small"
  principle: one static binary, low memory, trivial self-hosting. Persistence:
  **Postgres (production) + SQLite (local dev/tests)** behind one persistence port —
  the two adapters prove swappability; Mongo possible later, not built (H5).
- Deployment shape: **deferred** — PocketBase-style single deployment (one binary
  serves API + connect pages + admin UI) vs separate backend and UI deployments.
  Decide at architecture time; the "small" principle leans single-binary.

<!-- -------- bee-comment -------- -->
> **@developer**: I am leaning towards Go for backend, D:/rolai/source/rolai-university is an example (Hexagonal Architecture), Tanstack Start for Admin framework, use Go templates for any server-rendered pages. Another deffered decision is whether to deploy it as if pocketbase where only one deployment hosts everything or two deployments for UI and backend.
> **@bee**: Switched the stack: Go backend (hexagonal, with rolai-university as the reference implementation — the architecture agent will study it), TanStack Start for the Admin UI, Go templates for the middle-man pages (served by the backend itself, no extra runtime). The SDK stays TypeScript since rolai is the consumer. Recorded the single-deployment-vs-split question as deferred to architecture, noted in Open Questions — the "small" principle leans PocketBase-style single binary.
> - [x] mark as resolved
<!-- -------- /bee-comment -------- -->

- SDK: **TypeScript** (the consumers are TS), constructor-injectable and mockable
  (rolai mocks services with `vi.fn`), with adapters/clean schemas usable from AI tool
  frameworks (OpenAI function tools / Mastra) the way rolai uses Composio's today.
- Middle-man UI: **Go templates** served by the backend (no React, no separate
  runtime) — connect, param-collection, "redirecting...", and OAuth-callback pages;
  token-authenticated via the short-lived user token; must work opened as a popup from
  a Next.js app and from a Chrome extension (postMessage relay), with `redirectUri`
  round-trip and allow-multiple-connections option.
- Admin UI: **TanStack Start**; visual design deferred to the design brief.

## Out of Scope (v1)

- Membrane/Composio **runtime API compatibility** (mirroring their endpoints or SDK
  shapes). Softened at review: cheap **migration aids** are in scope where they fall
  out naturally — most promising is a definition importer that converts Membrane YAML
  exports (like the samples in `temp/`) into Beecon provider definitions, plus a short
  per-operation migration guide for the SDK. Slotted into Phase 5 (Rolai onboarding).

<!-- -------- bee-comment -------- -->
> **@developer**: while this compatibility is not required, if possible we can have some, because that way migration is easy. Or if there is a way to migrate.
> **@bee**: Narrowed the exclusion to *runtime* API compatibility (still out — it would drag in Membrane's shapes forever) and added migration aids as in-scope: a Membrane-export → Beecon definition importer (your temp/ YAML samples are exactly its input format) and a per-operation migration guide. Added both to the Phase 5 milestone.
> - [x] mark as resolved
<!-- -------- /bee-comment -------- -->

- Providers beyond **Outlook** and **Hubspot** (the registry model must make #3 cheap,
  but we don't build #3).
- A second persistence adapter (Mongo) — port boundary only.
- Public SaaS hosting / marketplace; Beecon is self-hosted per installation.
- Billing, usage metering, quotas.
- An AI agent runtime — Beecon exposes clean tool schemas and SDK adapters; agents live
  in the consumer.
- Visual design of the UIs (separate design brief). Inspirations recorded for that
  brief: **Tailwind CSS** for styling; the product feel of **PocketBase**, **Temporal**,
  and **Linear**.

<!-- -------- bee-comment -------- -->
> **@developer**: Tailwind css, pocketbase, temporal, are two things I like. Linear as well.
> **@bee**: Recorded these as design-brief inputs: Tailwind CSS as the styling approach, with PocketBase, Temporal, and Linear as the reference aesthetic (clean, dense, developer-focused admin surfaces). The design agent will receive these when it produces the brief before the UI phases.
> - [x] mark as resolved
<!-- -------- /bee-comment -------- -->

- Export of connections/credentials between installations.
- A flow/workflow engine — flows remain the consumer's concern (rolai keeps its own).

## Milestone Map

Each phase is a vertical slice: shippable, testable end-to-end, and demoable.

### Phase 1 — Walking skeleton: one user connects Outlook and runs one tool
- Installation boots with Postgres; organization + user created via API.
- Organization server API key issued, verified on every request; organization data
  isolation enforced at the persistence layer.
- Outlook provider defined (locally, registry comes later): OAuth connect flow through
  the middle-man pages (initiate → param/consent → provider OAuth → callback →
  redirectUri back to consumer) → ACTIVE Connection with stable id.
- Encrypted token storage; one Outlook tool (e.g. "list messages") executes via the
  TypeScript SDK with `{successful, error, data}` result shape.
- Request/response logging with token redaction (queryable via API).

### Phase 2 — Real catalog: Hubspot proves the provider model
- Declarative provider-definition format finalized; Hubspot added mostly as definitions
  (validates H2). Expected pre-auth params supported.
- Tool catalog API: list tools per provider/integration with input **and accurate
  output** JSON Schemas; deprecation flags; pagination convention.
- Upstream rate limits normalized into one retriable shape (429 + Retry-After);
  platform-side retry policy so consumers stop writing backoff code.
- Connection lifecycle complete: get, disable, delete, **reconnect keeping the same id**
  (validates H3). User-scoped short-lived tokens for the browser connect UI.
- File upload → URI usable in tool arguments.

### Phase 3 — Events out: triggers, signed webhooks, token self-healing
- First-class triggers: list definitions (config + payload schemas), create/enable/
  delete instances bound to Connections.
- Outbox + webhook delivery: HMAC-signed, timestamped, retried, idempotency keys;
  SDK verification helper (validates H4-security... validates the signing design).
- Auto token refresh scheduler; refresh failure → EXPIRED + signed `connection.expired`
  reverse message; reconciliation job.

### Phase 4 — Operability: Admin UI and governance
- Admin UI: organizations, users, keys (create/rotate/scope), connections, trigger
  instances, log explorer with redacted bodies, provider definitions.
- Org-level governance: integration allow-lists, onboarding defaults, visibility rules
  (absorbs what rolai built consumer-side).
- Organization config export/import (no secrets). Middle-man UI hardening per design
  brief.

### Phase 5 — Ecosystem: registry service, SDK polish, Rolai onboarding
- External tool-registry service: versioned definition bundles, publish flow with
  output-schema validation, installation pull/diff/activate (validates H6).
- Service-bus delivery adapter on the outbox (per-organization channel choice).
- SDK polish: AI-framework tool adapters (OpenAI/Mastra shape), docs, quickstart.
- Migration aids: Membrane-export → Beecon definition importer (input format = the
  YAML samples in `temp/`) + per-operation SDK migration guide.
- Rolai adoption: add Beecon as a **third provider** in `IntegrationRoutingService`
  (alongside Membrane and Composio); migrate integrations provider-by-provider; remove
  Membrane + Composio once parity is proven. eCW installation stood up from docs.

## Module Structure

*(Greenfield — boundaries for the platform monorepo. Domain modules are hexagonal:
domain + ports inside, adapters outside.)*

<!-- -------- bee-comment -------- -->
> **@developer**: decide on whether we are going to call it organization or tenant and stick to one
> **@bee**: Decided: **Organization** is the single term, everywhere — it's what rolai already calls it and what the entity actually is; "tenant" is retired from the vocabulary (including module names: `tenancy/` → `organizations/`). Swept the whole document: module structure, phase map, personas, problem statement, and risk assessment now all say organization. The only place "tenant" survives is this card and the historical record of the earlier decision.
> - [ ] mark as resolved
<!-- -------- /bee-comment -------- -->

- `organizations/` — owns: Organization, User, org governance rules, data-isolation
  policy. Depends on: (none)
- `access/` — owns: ServerApiKey, UserToken, WebhookSigningSecret, key verification and
  rotation. Depends on: organizations
- `catalog/` — owns: Provider, Tool, Mapping, TriggerDefinition, definition bundles and
  versions, registry-sync (import/diff/activate). Depends on: organizations
- `connections/` — owns: Connection, CredentialVault (encrypted tokens), OAuth flows,
  token auto-refresh, reconciliation. Depends on: organizations, access, catalog
- `execution/` — owns: ToolExecution, rate-limit normalization, retry policy,
  pagination, FileUpload. Depends on: connections, catalog
- `triggers/` — owns: TriggerInstance, subscription lifecycle, inbound provider event
  normalization. Depends on: connections, catalog
- `delivery/` — owns: Outbox, WebhookDelivery (signing, retries, idempotency),
  ServiceBusDelivery, reverse messages. Depends on: access, organizations
- `logging/` — owns: EventLog, redaction rules, retention. Depends on: organizations
- `apps/api/` — HTTP adapter composing the modules (Go). Depends on: all above
- `apps/connect-ui/` — middle-man pages (Go templates, served by the api binary).
  Depends on: api
- `apps/admin-ui/` — admin web app (TanStack Start). Depends on: api (HTTP only)
- `packages/sdk/` — TypeScript SDK + webhook verifier + AI-framework adapters.
  Depends on: api contract only
- `registry-service/` — separate deployable: definition bundle storage, versioning,
  publish validation. Depends on: (none — shares the definition-format package)

## Decisions Resolved at Review (developer answers, 2026-07-10)

- **Mapping = tool→endpoint mapping (H4 confirmed).** Mapping lives inside provider
  definitions, per tool — matching the developer's indented notes (Mapping sits under
  Tools) and the sample Membrane action export (`temp/outlook-get-message.yaml`).
- **One entity, one word: Organization** (the earlier tenant/organization pair
  collapsed into it, and at review the term "tenant" was retired entirely). Layering is
  **Installation → Organization → User**; Organization is both the isolation unit
  (keys, secrets, data isolation) and the governance unit.
- **Trigger ingestion = push + poll fallback.** Push where the provider supports it
  (MS Graph change notifications, Hubspot webhooks); polling as a per-trigger fallback
  declared in the provider definition. Matches "Pull / Push" under Triggers in the
  developer's notes; today's Membrane trigger (`temp/outlook-message-received-trigger.yaml`)
  is pure polling at 60s intervals.
- **Raw-action pass-through: deferred.** Decide when speccing Phase 2 whether KB file
  browsing needs an escape hatch or is covered by regular tool definitions.
- **Stack: Go backend** (hexagonal, `rolai-university` as reference), **TanStack Start**
  Admin UI, **Go templates** for middle-man pages, TypeScript SDK. Deployment shape
  (single binary vs split) deferred to architecture.
- **Delivery channels:** Azure Service Bus confirmed for eCW; webhook-only acceptable
  for Rolai in v1. Delivery is a hexagonal port, channel configurable per organization.
- **Registry:** separate tiny service; API-key auth; storage may be git-backed
  (definitions pushed to a git repo, registry serves and versions them; publish
  governance = git permissions).
- **Integration entity added:** Provider ("Outlook") → Integration ("Outlook with the
  installation's client id/secret", per-organization visibility) → Connection ("my
  connected Outlook").
- **Migration aids in scope** (Phase 5): Membrane-export → Beecon definition importer
  + per-operation migration guide; runtime API compatibility stays out.

## Reference Samples (in `temp/`)

Real Membrane exports supplied by the developer — concrete inputs for the
provider-definition format design in Phase 2:

- `temp/Screenshot 2026-07-10 191218.png` — the original requirement notes with proper
  indentation (authoritative hierarchy: Mapping under Tools; Pull/Push under Triggers;
  Access/Refresh tokens under the User↔Outlook connection; Webhook/Service bus under
  Reverse message; the external registry under Import and export).
- `temp/outlook-integration.yaml` — Membrane integration record (key, name, logo,
  connector version).
- `temp/outlook-get-message.yaml` — Membrane action definition: input JSON Schema,
  HTTP request template with conditional paths + path/query parameter mapping, curated
  output schema. This is the shape our Tool + Mapping definitions must express.
- `temp/outlook-message-received-trigger.yaml` — Membrane trigger flow: parameters
  schema, polling data source (60s), node chain (find-by-id → folder filter →
  transform → POST to consumer webhook). Shows what our first-class Trigger definitions
  replace.

## Open Questions

<!-- -------- bee-comment -------- -->
> **@developer**: yes azure service bus, webhook only is okay but from hexagonal, make it configurable.
> **@bee**: Resolved and moved to "Decisions Resolved at Review": Azure Service Bus confirmed for eCW; webhook-only acceptable for Rolai in v1; delivery is a hexagonal port with per-organization channel configuration, so adding the bus adapter later touches no core code.
> - [x] mark as resolved
<!-- -------- /bee-comment -------- -->

<!-- -------- bee-comment -------- -->
> **@developer**: registry can be separate, auth could be just an API key. We can just push to a separate git repo as well and registry server could just server it. or something like that.
> **@bee**: Resolved and moved to "Decisions Resolved at Review": registry stays a separate, deliberately tiny service; installations authenticate with an API key; storage can be git-backed (definitions pushed to a git repo, registry serves and versions them — publish governance becomes git permissions). This fits the "small" principle perfectly.
> - [x] mark as resolved
<!-- -------- /bee-comment -------- -->

- **Log retention default** (proposed 30 days) and any compliance constraints from eCW
  (healthcare context suggests possible HIPAA-adjacent requirements — needs checking).
- **Deployment shape:** PocketBase-style single binary (API + connect pages + admin UI
  in one deployment) vs separate backend and UI deployments — decide at architecture.

## Revised Assessment

Size: EPIC
Greenfield: yes
Risk: HIGH (organization data isolation, credential security, replaces a production
dependency) — Phases 1–3 carry the security-critical work and get the deepest specs.

---

- [x] Reviewed
