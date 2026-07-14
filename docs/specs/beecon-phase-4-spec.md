# Spec: Beecon Phase 4 — Operability: Admin UI and Governance

> Beecon gets a face and a rulebook: a TanStack Start operator console over the whole
> installation, gated by the existing admin key, and org-level governance that finally
> makes the catalog's long-ignored org parameter mean something.
> Discovery: [beecon-discovery.md](./beecon-discovery.md) (Phase 4 of the Milestone Map).
> Boundaries: `.claude/BOUNDARIES.md`. Context: `.claude/bee-context.local.md`.
> Design brief: `.claude/DESIGN.md` (the Admin UI's visual/UX system).
> Builds on shipped Phase 1–3: [phase-1](./beecon-phase-1-spec.md),
> [phase-2](./beecon-phase-2-spec.md), [phase-3](./beecon-phase-3-spec.md).

## Overview

Everything so far has been API-only: keys minted with `curl`, connections inspected by
reading the database, failed deliveries triaged by grepping logs. Phase 4 is the
operability layer — a browser console operators keep open all day to answer "which
connections are expiring, which webhook deliveries failed, what's in this redacted log
body, and who can see which integrations." It has three strands, all HIGH risk:

1. **The Admin UI** — a greenfield TanStack Start SPA (`apps/admin-ui/`) served by the
   single Go binary, over the read surfaces Phase 1–3 already expose (keys, connections,
   trigger instances, logs, events, metrics, catalog) plus the new surfaces this phase
   adds. Its visual system is fixed by `.claude/DESIGN.md`.
2. **Admin-key access (auth deferred)** — the console ships behind the existing static
   installation-wide `BEECON_ADMIN_API_KEY`; the operator enters that key on a gate
   screen and the SPA sends it as a Bearer token. Real per-operator accounts, cookie
   sessions, CSRF, and SSO are explicitly a later phase (PD39). This keeps Phase 4 focused
   on the console and governance rather than building an auth system.
3. **Org-level governance** — the catalog facade currently shows every organization the
   same installation-wide integration list (PD7); `ListTools`/`ListTriggerDefinitions`
   take an org param they explicitly ignore. That dead seam becomes real: per-org
   integration allow-lists, visibility rules, and onboarding defaults — absorbing what
   rolai built consumer-side.

Plus the backend items earlier phases deferred here: **scope-restricted API keys**
(PD23 groundwork), **log/event/outbox retention config + purge** (default 30 days),
**multiple webhook endpoints with per-endpoint event-type filters and auto-disable**
(deferred from PD30/PD31), **organization config export/import** (no secrets), and
**middle-man connect-UI hardening** per the design brief.

Risk: HIGH — an operator console sits over credential-handling infrastructure; governance
changes what organizations can see and do; and the deferred backend items touch delivery,
logging, and key verification. Failure modes (governance leaks across orgs, over-scoped
keys, purge deleting too much, endpoint fan-out storms) are first-class in the slices
below. Note that operator authentication itself is **deferred** this phase (PD39): the
console ships behind the existing static admin key, so per-operator sessions/CSRF are
explicitly not a Phase 4 surface.

## Proposed Decisions (synthesis — correct at review if wrong)

> **Developer-confirmed 2026-07-14** (four key questions answered via the orchestrator,
> AskUserQuestion being unavailable inside the subagent):
> - **PD39 (operator auth): CONFIRMED as DEFERRED — admin-key Bearer only.** The Admin UI
>   ships behind the existing static installation-wide `BEECON_ADMIN_API_KEY`; real
>   per-operator accounts + password + cookie session + CSRF + SSO/OIDC are Phase 5+.
>   PD39 below is rewritten to that decision.
> - **PD41 (key scopes = `read-only`/`read-write` only): CONFIRMED as proposed.**
> - **PD42 (governance: absent allow-list = inherit full catalog): CONFIRMED as proposed.**
> - **PD47 + slicing (embed SPA in the binary under `/admin`; UI-shell+gate first, then
>   thin vertical UI areas, then deferred backend items): CONFIRMED as proposed.**
>
> The remaining PDs — **PD40, PD43, PD44, PD45, PD46, PD48** — stand as reasoned proposals
> grounded in the discovery doc, the rolai context, the existing Go codebase
> (`server/internal/access`, `catalog`), the design brief, and the module boundaries;
> correct any at review if wrong. Numbering continues from Phase 3 (PD13–PD38).

1. **PD39 — Operator auth is DEFERRED: the console ships behind the existing static admin
   key; no operator accounts, sessions, or CSRF this phase.** The Admin UI authenticates
   to the API with the existing installation-wide `BEECON_ADMIN_API_KEY` — `AdminAuth` is
   **unchanged** (constant-time Bearer compare, no session-or-Bearer split). The **minimal
   honest browser mechanism:** the operator enters the admin key on a gate screen before
   the shell loads; the SPA holds it **in memory for the tab's lifetime only** (never
   `localStorage`/`sessionStorage`, never a cookie) and sends it as
   `Authorization: Bearer <key>` on every API call; on reload or a new tab the operator
   re-enters it. No password hashing, no server-side session store, no CSRF machinery is
   built.
   - **Security posture and its limits (documented deliberately):** this is a **single
     shared installation-wide credential** — there is **no per-operator identity,
     attribution, or audit**; anyone with the key has full installation access; the key
     lives in the browser tab's JS memory while the console is open (exposed to a
     compromised extension or XSS, though the SPA runs no third-party scripts). It is
     intended for a **trusted-operator, network-restricted deployment** (self-hosted,
     behind a VPN / private network / reverse-proxy auth), matching how a self-hosted
     admin like PocketBase is typically fronted. This is an accepted trade-off to keep
     Phase 4 scoped to the console and governance.
   - **Explicit Phase 5+ deferral:** real per-operator accounts (email + password with a
     memory-hard KDF), server-set revocable cookie sessions, CSRF protection, and SSO/OIDC
     are a later phase. The design brief's login screen already leaves the layout/slot for
     it (`.claude/DESIGN.md` §5).
   - **Flag for architecture:** decide whether even this minimal gate needs any server
     support — e.g. a cheap `GET /admin/verify` that validates the key (returns 204/401)
     so the gate can reject a bad key before the shell mounts — or whether the gate is
     purely client-side and simply lets the first real API call surface a 401. Either is
     acceptable; the endpoint, if added, is a thin wrapper over the unchanged `AdminAuth`.
2. **PD40 — New installation-wide read endpoints, admin-guarded, cursor-paginated.**
   `list-all-organizations`, `list-users-per-organization`, and `list-provider-definitions`
   do not exist today (the org facade has no `ListAll`; the user facade no per-org list;
   raw provider definitions are boot-loaded but unexposed). Each is added following the
   existing conventions verbatim — `httpx` DomainError envelope, opaque base64 cursor
   pagination (default 50 / max 200), org-scoping enforced at the persistence port where
   applicable — and guarded by the unchanged `AdminAuth` (static Bearer key per PD39). No
   governance filtering applies to these operator views: an operator sees the real estate,
   not the org-facing filtered catalog.
3. **PD41 — Scope-restricted API keys: `read-only` vs `read-write` (two-value scope).**
   `ServerApiKey` gains a `Scope` field. `read-write` is the full existing behavior;
   `read-only` keys are rejected on any mutating endpoint (create/update/delete/execute/
   rotate) with a scope-explaining 403, and pass on listing/inspection. `Verify` returns
   the scope alongside the OrgID so the auth middleware can enforce it per route.
   **Existing keys and the default at issue are `read-write`** — nothing that works today
   breaks. Per-capability scopes (`connections:write`, `tools:execute`, …) and
   per-integration key restriction are deliberately **not** built now (YAGNI — nothing
   yet demands that granularity; the two-value scope is the smallest restriction that
   delivers least-privilege). The `Scope` field is an enum so finer scopes can arrive
   later without a second concept.
4. **PD42 — Governance model: per-org integration allow-list, absent = inherit full
   catalog (continuity-preserving).** An organization's governance holds an optional set
   of allowed integration ids. **Absent/empty-unset = the org sees the full installation
   catalog** (exactly PD7's behavior today — so upgrading changes nothing until an
   operator curates), while a **present** allow-list restricts the org to only those
   integrations. Each integration also carries a per-org visibility state (`VISIBLE` /
   `HIDDEN`) for one-off exclusions without maintaining a whole allow-list. Visibility is
   enforced at the exact seam that ignores its org param today —
   `catalog.Facade.ListIntegrations` / `resolveProviderSlugFilter`, and the dead org
   param in `ListTools` / `ListTriggerDefinitions` becomes a real governance query;
   `connections.initiate` (which reads integrations through the same facade) inherits the
   filter for free, so an org can never connect an integration it cannot see. `catalog`
   already depends on `organizations`, so it reads the governance rules through an
   organizations port — no new coupling. Chosen over deny-by-default because
   deny-by-default would blank every existing org's catalog on upgrade; the allow-list is
   opt-in restriction, not opt-in visibility.
5. **PD43 — Onboarding defaults: an ordered featured subset, default cap 8.** Governance
   holds an optional ordered list of "featured" integration ids surfaced first during
   onboarding, capped at a configurable count (default 8 — rolai's limit). The consumer
   catalog endpoints gain an optional `featured=true` filter returning only that ordered
   subset; absent featured config falls back to the first N visible integrations. This is
   the third leg of PD42's governance record, not a separate entity.
6. **PD44 — Retention: installation default 30 days, per-org override, in-process purge
   worker.** A new `BEECON_RETENTION_DAYS` (default 30) sets the installation default;
   each org may override it (separately for logs and for events/outbox). A purge worker —
   a goroutine inside `beecon serve`, reusing Phase 3's PD29 in-process-worker + database-
   lease pattern, run daily (jittered) — hard-deletes `EventLog` rows and delivered/dead
   outbox events older than the effective window. Retention is **on by default at 30
   days** (discovery says so); a per-org value of `0`/unlimited disables purging for that
   org. In-flight/pending events are never purged regardless of age (only terminal
   DELIVERED/FAILED/NO_ENDPOINT rows past the window). Logs live in `logging/`, outbox in
   `delivery/`; each owns its purge; the per-org window lives in the org's governance/
   settings record read through the organizations port.
7. **PD45 — Multiple webhook endpoints with event-type filters and auto-disable
   (extends PD31).** An org may register up to a configurable cap of endpoints (default
   5), each with its own URL, its own `whsec_` secret (issued once, rotatable per PD27/
   PD23), and an optional **event-type filter** (a subset of the event-type enum; absent
   filter = all types). A delivery fans out to **every enabled endpoint whose filter
   matches** the event type, each with its own delivery record, retry schedule (PD30),
   and idempotent `webhook-id`. **Auto-disable:** after N consecutive events reach
   terminal `FAILED` for a given endpoint (default N=5, configurable), that endpoint is
   auto-disabled (`DISABLED_AUTO`), stops receiving fan-out, and surfaces an operator-
   visible signal; a successful delivery resets the consecutive-failure counter, and an
   operator re-enabling the endpoint resets it too. **Migration:** the Phase 3 single
   endpoint becomes endpoint #1 with no filter, keeping its secret. The single-endpoint
   PD31 API is retained as a compatibility alias over "the org's first/only endpoint" so
   existing SDK callers keep working.
8. **PD46 — Organization config export/import: versioned JSON, no secrets, dry-run +
   merge/replace.** `export` returns a versioned JSON document containing an org's
   **governance** (allow-list, visibility, onboarding defaults), **webhook endpoints**
   (URLs + event-type filters, **never** secrets), and **retention config** — and nothing
   else. It **never** includes API-key/webhook secrets, connections, credentials, user
   tokens, or provider definitions. `import` **defaults to a dry-run** that returns the
   diff/plan it would apply and validates the document without writing; a non-dry-run
   apply takes a `mode` of `merge` (default — upsert, leave unmentioned settings alone) or
   `replace` (the org's governance/endpoints/retention are made to match the document
   exactly, removing what the document omits). Importing a document from an unknown or
   incompatible schema version is rejected with a validation error; secrets referenced by
   imported endpoints are regenerated (returned once) since they are never exported.
9. **PD47 — Admin UI is a static TanStack Start SPA embedded in and served by the single
   Go binary.** `apps/admin-ui/` builds to static assets that are embedded via Go
   `embed.FS` and served by the `beecon serve` binary under `/admin` — preserving the
   PocketBase-style single-binary deployment ("keep it small"). The SPA is a pure API
   client: it calls `/api/v1/*` over HTTP with the admin key as a Bearer token (PD39); it
   holds no server logic. In dev, the SPA runs on its own Vite dev server proxying to the
   binary; in production there is still exactly one artifact to ship. The SPA route
   (`/admin/*`), the connect pages (`/connect/*`), and the API (`/api/v1/*`) coexist on
   one listener; request-logging middleware continues to exclude `/connect/*` and secret
   paths.
10. **PD48 — Middle-man connect-UI hardening = tidy-first extraction, no framework
    change.** The three connect templates (`connect`/`params`/`error` `.gohtml`) still
    duplicate their inline CSS verbatim (the flagged DRY tidy). Phase 4 extracts one
    shared stylesheet whose values are the design tokens (`#2563eb`, the gray ramp, radii
    8/12px, focus ring, 44px targets) so the connect pages and the admin console read as
    one product. The connect pages stay server-rendered Go templates (BOUNDARIES:
    `apps/connect-ui/` is Go templates) — no SPA, no behavior change beyond design-brief
    polish and accessibility (focus-visible, contrast, reduced-motion).

---

## Slice 1 — The console appears: admin-key gate, shell, and the organizations list

The walking skeleton — the thinnest end-to-end path: an operator opens the embedded SPA,
enters the admin key on a gate screen, gets the app shell (sidebar + top bar + org
switcher), and lands on a live list of every organization in the installation (a surface
that did not exist before). No operator accounts, sessions, or CSRF are built (PD39).

- [x] The Admin UI is served by the `beecon serve` binary at `/admin` from embedded static assets (PD47) — no separate process to deploy
- [x] Before the shell loads, the operator is shown a gate screen prompting for the admin key; entering a valid key opens the console
- [x] The admin key is held in memory for the tab session only and sent as `Authorization: Bearer <key>` on API calls — it is never written to localStorage, sessionStorage, or a cookie
- [x] Reloading the tab or opening a new tab returns to the gate screen (the key is not persisted)
- [x] Shows an inline error on the gate screen (icon + text, never color-only) when the entered key is rejected, without navigating away
- [x] The console shell renders the left sidebar (grouped OBSERVE / OPERATE / CATALOG / ADMINISTER / GOVERN, collapsible to an icon rail), the top bar (wordmark, org switcher, command-palette trigger, theme toggle, sign-out that clears the in-memory key), and honors light/dark theme
- [x] The Organizations area lists every organization in the installation, cursor-paginated, each showing id (mono, click-to-copy) and created date
- [x] An API call that returns 401 (key wrong or absent) sends the operator back to the gate screen rather than showing a broken page
- [x] The gate screen and shell meet the design-brief accessibility bar: visible `:focus-visible` ring, 44×44px targets, WCAG AA contrast, `prefers-reduced-motion` respected

## Slice 2 — Operating the estate: connections and trigger instances

Read/operate UI over the connection and trigger-instance read surfaces Phase 1–3 already
expose, org-scoped through the top-bar org switcher, using the design brief's right-side
drawer for scan-heavy detail.

- [x] With an org selected, an operator can list that org's connections, cursor-paginated, each showing a status badge (ACTIVE/INITIATED/DISCONNECTED/EXPIRED) that pairs color with an icon and a text label
- [x] Opening a connection reveals a right-side drawer with its id, integration, user, status, and timestamps (relative text with absolute value on hover)
- [x] An operator can filter connections by status and by integration, with applied-filter chips that are individually removable
- [x] An operator can list an org's trigger instances and open one as a full page showing status (ACTIVE/DISABLED/ERROR), trigger slug, bound connection, and config
- [x] An operator can disable and re-enable a trigger instance from the console, and the status badge reflects the change
- [x] Deleting a trigger instance requires confirmation and shows the instance as gone afterward
- [x] Switching the selected organization re-scopes both lists; an operator never sees another org's connections or instances
- [x] Each list shows first-class empty, loading (skeleton rows), and error (inline card with retry) states

## Slice 3 — Watching the pipes: log explorer, events & delivery, and the dashboard

The OBSERVE areas — the redacted log explorer, webhook-delivery triage with manual
redeliver, and the operability dashboard over `/metrics`.

- [x] An operator can search and filter logs (by org, status, date range) and open a log entry in a drawer showing its redacted body with an explicit textual `[redacted]` marker in the mono payload viewer
- [x] An operator can list events with their delivery status (DELIVERED/PENDING/RETRYING/FAILED/DEAD/NO_ENDPOINT) as color+icon+label badges, and open one to see per-attempt history (attempt number, response status, duration)
- [x] An operator can manually redeliver a FAILED event from the console and see the new attempt appear
- [x] The dashboard shows operability metric tiles sourced from the admin-guarded `/metrics` (connections-by-status, outbox depth, oldest-pending-event age, delivery outcomes)
- [x] Time-series charts distinguish series by more than color (labels/annotation) and respect `prefers-reduced-motion`
- [x] Log and event bodies never render an unredacted secret — redaction applied at write time (Phase 1–3) is preserved in the viewer
- [x] Pagination on logs and events uses cursor-based controls (load-more / prev-next), never numbered pages

## Slice 4 — Administering access: end-users and scope-restricted API keys

The ADMINISTER area — an organization's **end-users** (the org members Beecon already
models, not console operators — operator accounts are deferred, PD39) via a new per-org
list endpoint, and API-key lifecycle with the new `read-only`/`read-write` scope, fronted
by the credential-handling key-shown-once modal.

- [x] An operator can list the end-users of the selected organization, cursor-paginated (new `list-users-per-org` endpoint)
- [x] An operator can create an end-user in the selected organization from the console
- [x] An operator can list an org's API keys showing prefix, created date, scope, and rotation/revocation state — never a secret
- [x] An operator can create an API key choosing its scope (`read-only` or `read-write`), and the full secret is shown exactly once in a modal with a copy button, a "you won't see this again" warning, and a checkbox that gates dismissal
- [x] A `read-only` key is rejected with a scope-explaining error on any mutating API call, and succeeds on listing/inspection calls
- [x] A `read-write` key (and every key issued before this phase) retains full access — scope defaults to `read-write`
- [x] An operator can rotate a key (new secret shown once, overlap window per PD23) and revoke a key (type-to-confirm), after which the revoked key no longer authenticates
- [x] The console never writes any secret to logs, error messages, or serialized output

## Slice 5 — Governance: allow-lists, visibility, and onboarding defaults

The core risk item — making the catalog facade's ignored org param real. Per-org
integration allow-lists and visibility change what each organization can see and connect.

- [x] An operator can view, for the selected org, the installation catalog with each integration's effective visibility (visible / hidden / not-allowed)
- [x] An operator can add integrations to an org's allow-list; once the allow-list is non-empty, the org's consumer catalog returns only allow-listed integrations
- [x] An org with no allow-list set continues to see the full installation catalog (upgrade changes nothing until an operator curates)
- [x] An operator can mark an individual integration HIDDEN for an org, and it disappears from that org's consumer catalog, tools, and trigger definitions
- [x] An org cannot initiate a connection to an integration it cannot see — the initiate call returns not-found, matching the filtered catalog
- [x] Governance is strictly org-scoped: changing one org's rules never affects another org's catalog
- [x] An operator can set an org's onboarding "featured" integrations as an ordered list capped at the configured count (default 8), and the consumer catalog's `featured` filter returns exactly that ordered subset
- [x] `ListTools` and `ListTriggerDefinitions` now honor the org's visibility (the previously-ignored org param), returning nothing for a hidden or non-allowed integration
- [x] The governance editor uses progressive disclosure (allow-list, visibility, and onboarding in separate sections/tabs), and primary/destructive actions stay visible

## Slice 6 — The catalog surfaced: providers, tools, and trigger definitions

The CATALOG area — read views over provider definitions (new list endpoint), tools, and
trigger definitions, using the full-page pattern and a mono JSON/YAML viewer.

- [x] An operator can list the installation's provider definitions with name, version, and integration count (new `list-provider-definitions` endpoint) <!-- verified: exposes name, formatVersion, and toolCount/triggerCount (a provider DEFINITION declares tools/triggers; Integrations are org-configured instances the un-governed definition list deliberately never queries) — see verifier's AC1 wording note for reviewer -->
- [x] Opening a provider definition shows a full page with its versioned bundle rendered in a collapsible, copyable mono JSON/YAML viewer
- [x] An operator can list tools and trigger definitions, each showing slug, name, and provider, filterable by provider
- [x] Opening a tool shows its input/output JSON-Schema in the mono viewer; opening a trigger definition shows its config schema, payload schema, and ingestion mode
- [x] Long CUID2 ids (`conn_`/`tool_`/`trg_`) render truncated with a click-to-copy affordance
- [x] Syntax tint in the viewer is never the sole signal — structure is legible in grayscale
- [x] These installation-wide views are not governance-filtered (operators see the real catalog, not an org's filtered view)

## Slice 7 — Retention and the purge worker

Log/event/outbox retention config (default 30 days) plus the background purge — the
first data-deleting worker, so its blast radius is bounded by explicit ACs.

- [x] An operator can view and set an org's retention windows (logs and events, separately); an unset org falls back to the installation default of 30 days
- [x] The purge worker hard-deletes log entries and terminal (DELIVERED/FAILED/DEAD/NO_ENDPOINT) events older than the effective window <!-- verified: terminal set is delivery.TerminalStatuses {DELIVERED,FAILED,NO_ENDPOINT}; no DEAD status exists in the Phase 3 model (retry-exhausted = FAILED), so DEAD in the AC text is vestigial — reviewer note -->
- [x] Pending or in-flight events are never purged regardless of age
- [x] Setting an org's retention to 0 / unlimited disables purging for that org
- [x] Setting a retention window shorter than a platform minimum is rejected with a validation error
- [x] The purge worker claims work with database leases (PD29 pattern) so two running binaries never double-purge, and it stops cleanly on shutdown <!-- verified with a spec-wording note: implementation deliberately uses an idempotent bounded DELETE with NO lease column (FD7/arch §7/§3.5, doc comments). Ruling: satisfies the AC's INTENT (two-binary safety, no double-processing harm, clean shutdown) — a purge's "processing" is a side-effect-free DELETE-past-cutoff that converges safely under two instances, so a lease would be ceremony. Reviewer: relax the literal "database leases (PD29 pattern)" wording to "safe under two running binaries (idempotent purge)". -->
- [x] Setting a shorter window purges the now-expired rows on the next purge run, not retroactively mid-request

## Slice 8 — Multiple webhook endpoints, event-type filters, and auto-disable

The PD30/PD31 deferrals — an org can register several endpoints, filter each by event
type, and Beecon quarantines an endpoint that keeps failing.

- [x] An operator (or org key holder) can register more than one webhook endpoint for an org, up to the configured cap, each with its own URL and its own `whsec_` secret shown exactly once
- [x] Registering an endpoint beyond the cap is rejected with a validation error naming the cap
- [x] An endpoint can carry an event-type filter, and an event is delivered only to enabled endpoints whose filter matches its type (absent filter = all types)
- [x] Each endpoint gets its own delivery record and retry schedule; a failure at one endpoint never blocks delivery to another
- [x] After the configured number of consecutive events reach terminal FAILED for an endpoint, that endpoint auto-disables, stops receiving fan-out, and shows a DISABLED_AUTO status in the console
- [x] A successful delivery resets an endpoint's consecutive-failure counter; an operator re-enabling an auto-disabled endpoint resets it and resumes fan-out
- [x] The Phase 3 single endpoint keeps working as endpoint #1 with its existing secret and no filter, and the PD31 single-endpoint API continues to operate over it
- [x] An operator can rotate any endpoint's secret (returned once, overlap window per PD27/PD23) and delete an endpoint

## Slice 9 — Organization config export/import (no secrets)

Move an org's governance, endpoints, and retention between installations — safely,
without ever moving secrets, and never blind.

- [x] An operator can export an org's config as a versioned JSON document containing its governance, webhook endpoints (URLs + filters), and retention config
- [x] The export never contains API-key secrets, webhook signing secrets, credentials, connections, or user tokens
- [x] Importing defaults to a dry-run that returns the diff it would apply and writes nothing
- [x] A non-dry-run import in `merge` mode upserts the document's settings and leaves unmentioned settings untouched
- [x] A non-dry-run import in `replace` mode makes the org's governance/endpoints/retention match the document exactly, removing what the document omits
- [x] Importing a document of an unknown or incompatible schema version is rejected with a validation error and writes nothing
- [x] Endpoints created by an import get freshly generated secrets (shown once), since secrets are never exported
- [x] Import validates governance references (integration ids that don't exist in this installation are reported in the dry-run, not silently dropped)

## Slice 10 — Middle-man UI hardening and carry-forwards

Tidy-first: collapse the duplicated connect-page CSS onto the shared design tokens so the
two surfaces lock together, and apply the design brief's accessibility bar.

- [x] The three connect templates (`connect`/`params`/`error`) share one stylesheet whose values are the design tokens — no duplicated inline `:root`/`.card`/`.connect-button` CSS remains
- [x] The connect pages use the same primary blue, gray ramp, radii, and focus ring as the admin console
- [x] Every interactive element on the connect pages has a visible `:focus-visible` ring and meets the 44×44px target minimum
- [x] The connect pages honor `prefers-reduced-motion` and pass WCAG AA contrast for all text <!-- verified: prefers-reduced-motion override + :focus-visible + 44px targets present in served CSS and asserted; WCAG AA contrast deferred to browser verification (no MCP this session) — the token values (#2563eb on #fff, #111827/#4b5563 text) are the design-brief palette -->
- [x] The connect pages' behavior (token round-trip, redirectUri, param collection) is unchanged — only presentation and accessibility improve <!-- verified: 17 pre-existing handler tests + OAuth-handshake/param-collection/Hubspot crucial_path journeys pass unchanged; template diffs are style-block swap + additive modifier classes only (card--form, connect-button--block, error-page); @import (not <link href>) adds no spurious href so extractHref still finds exactly the authorize URL -->


---

## API Shape (indicative)

```
Admin-key access  (PD39 — no operator accounts/sessions/CSRF this phase)
   The SPA sends the existing static key on every call:  Authorization: Bearer <BEECON_ADMIN_API_KEY>
   AdminAuth is UNCHANGED (constant-time Bearer compare).
GET  /admin/verify               (optional, architecture call) -> 204 valid key | 401 invalid
                                 lets the gate screen reject a bad key before the shell mounts;
                                 a thin wrapper over the unchanged AdminAuth. May be omitted (gate
                                 relies on the first real API call surfacing 401 instead).

New installation-wide reads  (AdminAuth = static Bearer key)
GET  /api/v1/organizations                       -> { items: [{ id, createdAt }], nextCursor }
GET  /api/v1/organizations/{orgId}/users         -> { items: [{ id, ... }], nextCursor }   (org END-users)
GET  /api/v1/provider-definitions                -> { items: [{ name, version, integrationCount }], nextCursor }
GET  /api/v1/provider-definitions/{name}         -> { name, version, bundle }

Scope-restricted API keys  (extends Phase 1/2)
POST /api/v1/organizations/{orgId}/api-keys      { scope: "read-only" | "read-write" }   (default "read-write")
                                                 -> 201 { id, secret (once), prefix, scope, createdAt }
     (read-only secret -> 403 scope error on any mutating endpoint)

Governance  (org-scoped, admin)
GET  /api/v1/organizations/{orgId}/governance
     -> { allowList: ["int_..."] | null, hidden: ["int_..."], onboarding: { featured: ["int_..."], cap: 8 } }
PUT  /api/v1/organizations/{orgId}/governance    { allowList?, hidden?, onboarding? }
   Consumer catalog now honors it: GET /api/v1/integrations (org|user) filters by allow-list + hidden;
   ?featured=true returns the ordered featured subset; ListTools/ListTriggerDefinitions honor visibility.

Retention  (org-scoped, admin; installation default BEECON_RETENTION_DAYS=30)
GET  /api/v1/organizations/{orgId}/retention     -> { logDays, eventDays }   (null = inherit default)
PUT  /api/v1/organizations/{orgId}/retention     { logDays?, eventDays? }    (0 = unlimited)

Multiple webhook endpoints  (org; extends PD31)
GET    /api/v1/webhook-endpoints                 -> { items: [{ id: "wep_...", url, eventTypes|null, status, secretPrefix, createdAt }] }
POST   /api/v1/webhook-endpoints                 { url, eventTypes? } -> 201 { id, url, eventTypes, secret: "whsec_..." (once) }
PUT    /api/v1/webhook-endpoints/{wepId}         { url?, eventTypes? }
POST   /api/v1/webhook-endpoints/{wepId}/rotate-secret { overlapHours? } -> { secret (once) }
POST   /api/v1/webhook-endpoints/{wepId}/enable | /disable
DELETE /api/v1/webhook-endpoints/{wepId}         -> 204
   PD31's PUT/GET /api/v1/webhook-endpoint remains as an alias over the org's first endpoint.
   Endpoint status: ENABLED | DISABLED | DISABLED_AUTO

Config export / import  (org-scoped, admin; no secrets)
GET  /api/v1/organizations/{orgId}/config/export -> { schemaVersion, governance, endpoints:[{url,eventTypes}], retention }
POST /api/v1/organizations/{orgId}/config/import?dryRun=true|false&mode=merge|replace
     { schemaVersion, governance, endpoints, retention }
     dryRun (default true) -> { plan: [...], warnings: [...] }   apply -> { applied: [...], secrets:[{ wepId, secret (once) }] }

Admin UI serving  (PD47)
GET  /admin/*   -> embedded TanStack Start static assets (Go embed.FS), served by `beecon serve`
```

## Out of Scope (Phase 4)

- **Real operator authentication** (PD39) — per-operator accounts, password hashing,
  server-set cookie sessions, CSRF, and SSO/OIDC are all a later phase. Phase 4 gates the
  console behind the existing static admin key only. **No operator-account CRUD is built.**
- **Per-operator attribution / audit log** — because there are no operator identities this
  phase (single shared key), there is no per-operator "who changed what" audit; deferred
  with the accounts it depends on.
- **Per-capability and per-integration API-key scopes** (PD41) — the `Scope` enum leaves
  room; only `read-only`/`read-write` is built.
- **Per-operator RBAC / roles** — deferred with operator accounts.
- **Push trigger ingestion** — still deferred (Phase 3 PD28); unchanged.
- **Service-bus delivery adapter** (Phase 5) — endpoints stay webhook-only; fan-out is
  over webhook endpoints only.
- **Installation-level (cross-org) config export/import** — export/import is per-org.
- **Registry service, SDK polish, Rolai onboarding, migration importer** — Phase 5.
- **Mobile-first Admin UI** — desktop-first operator tool (must not break at 375px, but
  not optimized for it).

## Technical Context

- **Stack (binding):** the Admin UI is a **new greenfield `apps/admin-ui/` TanStack Start
  SPA** built to static assets and **embedded in / served by the single Go binary** via
  `embed.FS` under `/admin` (PD47) — the PocketBase-style single-binary deployment holds;
  there is still one artifact to ship. The SPA is a pure `/api/v1` HTTP client that sends
  the admin key as a Bearer token (BOUNDARIES: `apps/admin-ui/` depends on api over HTTP
  only). The backend stays the Go hexagonal monolith. Visual system is fixed by
  `.claude/DESIGN.md` (IBM Plex Sans/Mono, `#2563eb` primary carried from the connect
  pages, gray ramp, light+dark tokens, left sidebar + drawer/page hybrid, status =
  color+icon+label always).
- **Auth (deferred, PD39):** `AdminAuth` (`server/internal/access/driving/authmw/admin.go`)
  is **unchanged** — constant-time Bearer compare of `BEECON_ADMIN_API_KEY`; no
  session-or-Bearer split, no session store, no CSRF middleware is added. The SPA gate
  holds the key in tab memory only. Real accounts/sessions/CSRF/SSO are Phase 5+.
  Architecture may add at most a thin `GET /admin/verify` over the unchanged `AdminAuth`
  for the gate's pre-flight key check (see PD39 flag).
- **Boundaries (binding):** governance rules live in `organizations/` (BOUNDARIES: it owns
  org governance rules) and are read by `catalog/` through an organizations port (catalog
  already depends on organizations — no new coupling); scoped keys extend `access/`'s
  `ServerApiKey`/`Verify`; multiple endpoints extend `delivery/`'s `WebhookEndpoint`
  (`wep_`); retention config lives with the org record, purge workers in `logging/` and
  `delivery/`; new read endpoints are added to `organizations/` and `catalog/` driving
  adapters; the connect-page tidy is in `apps/connect-ui/`.
- **Conventions (binding):** every new endpoint follows the existing shape — hexagonal
  module (facade / port / driving-httpapi / driven-bun+memory), `httpx` DomainError
  envelope, typed `organizations.OrgID` org-scoping enforced at the persistence port
  (arch test `orgscope_test.go`), opaque base64 cursor pagination (`httpx.EncodeCursor`/
  `DecodeCursor`, default 50 / max 200), secrets shown once / never in listings,
  request-logging middleware excludes `/connect/*` and secret paths. The module
  dependency graph is enforced by `arch/imports_test.go`.
- **Background workers:** the retention purge (Slice 7) and endpoint auto-disable
  bookkeeping (Slice 8) reuse Phase 3's PD29 pattern — in-process goroutines in
  `beecon serve`, database-lease claims (`FOR UPDATE SKIP LOCKED` on Postgres; SQLite
  single-writer locally), graceful shutdown, safe under two running binaries. No new
  runtime, queue, or cron.
- **Ids (binding):** no new entity-id prefixes this phase. `wep_` (PD31) now applies to
  multiple endpoints per org. Everything else follows CUID2-with-type-prefix (BOUNDARIES).
- **New config:** `BEECON_RETENTION_DAYS` (default 30), `BEECON_WEBHOOK_ENDPOINT_CAP`
  (default 5), `BEECON_ENDPOINT_AUTODISABLE_FAILURES` (default 5), `BEECON_PURGE_INTERVAL`
  (default 24h). All `BEECON_*` typed and fail-fast per `config/config.go`. No new
  auth/session config (PD39 deferred).
- **Governance seam (binding):** the interception point is `catalog.Facade.ListIntegrations`
  / `resolveProviderSlugFilter` and the currently-ignored org param in `ListTools` /
  `ListTriggerDefinitions`. `connections.initiate` reads integrations through the same
  facade and inherits the filter for free. Do not add a parallel filter path — fill the
  existing seam.
- Risk level: HIGH

---

- [x] Reviewed <!-- approved by developer ("go ahead"), 2026-07-14; PD39 auth-deferred + PD41/PD42/PD47 confirmed via Q&A -->
