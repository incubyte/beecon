# Spec: Beecon Phase 5 (registry sub-phase) — The Registry Speaks: external tool-registry service, `tool_` ids & pull → diff → activate

> Phase 5 ("Ecosystem") is delivered as sub-phases; **this document specs the registry
> sub-phase** — the external tool-registry service, the `tool_` entity ids minted at
> publish, and the installation-side **pull → diff → activate** flow that validates H6.
> This is the largest and most architecturally significant Phase 5 strand: it changes how
> provider definitions are stored, addressed, versioned, and updated at runtime (today
> they are YAML embedded in the binary, loaded once at boot).
>
> **Revived 2026-07-16** from a deferred draft (the registry was set aside on 2026-07-15 to
> lead Phase 5 with operator auth, which has now shipped). PD numbering continues from the
> operator-auth sub-phase (last live PD was **PD58**), so this sub-phase uses **PD59+**.
>
> Discovery: [beecon-discovery.md](./beecon-discovery.md) (Phase 5 milestone; H2, H6; the
> "Import", "Tool registry (separate service)", and bundle-versioning sections). ADRs:
> [0006 tool-addressing slugs-until-registry](../adr/0006-tool-addressing-slugs-until-registry.md)
> (this sub-phase delivers the hand-off it names), [0003 CUID2 prefixed ids](../adr/0003-cuid2-prefixed-identifiers.md)
> (the `tool_` prefix), [0001 single binary](../adr/0001-go-hexagonal-single-binary.md) &
> [0002 persistence](../adr/0002-persistence-postgres-sqlite-one-bun-adapter.md) (reconciled
> below), [0012 expression engine deferred](../adr/0012-expression-engine-deferred-cel-when-needed.md)
> (the registry stores/serves the **same** `formatVersion: 1` token-grammar format — no
> richer definition format). Boundaries: `.claude/BOUNDARIES.md` (`catalog/` owns "definition
> bundles and versions, registry-sync (import/diff/activate)"; `registry-service/` is a
> separate deployable sharing the definition-format package). Builds on shipped Phase 1–4
> and the shipped operator-auth sub-phase
> ([operator-auth spec](./beecon-phase-5-operator-auth-spec.md)).

## Overview

Through Phase 4 and the operator-auth sub-phase, every provider/tool/trigger definition
lives as YAML embedded in the installation binary (`catalog` loads it once at boot via
`DefaultProviderDefinitions`/`LoadProviderDefinitions` under the strict `formatVersion: 1`
loader), tools are addressed **only by slug** (`FindToolBySlug`, ADR-0006), and there is no
way to change a definition without redeploying the binary. This sub-phase delivers the
**ecosystem keystone**: a small, separately-deployed **registry service** that holds
semantically-versioned provider bundles, mints an **immutable `tool_<cuid2>` id** for every
tool at publish, validates that each tool's declared output schema actually matches a
recorded real response (the differentiator both Composio and Membrane fail), and lets an
installation **pull a version, review the diff, and activate it at runtime** — never the
reverse (H6: pull-sync only; the registry never reaches into installations). This unblocks
the discovery success criterion that a second installation (eCW) can be stood up from
documentation alone, pulling definitions from the shared registry.

Because ADR-0012 kept the definition format as the minimal `formatVersion: 1` token grammar
(no expression engine), the registry stores and serves **exactly that format** — the same
bytes the embedded loader already parses. The registry adds identity (`tool_` ids),
versioning, publish-time validation, and runtime distribution around an unchanged format,
not a richer one.

Risk: **HIGH** — a new separately-deployed service and a new authenticated trust boundary,
an id-system change (`tool_` ids joining slug addressing) that touches execution and
logging, and a runtime **activation** path that swaps what every org in an installation can
call. First-class failure modes, each covered by ACs below: a bad bundle activated
installation-wide, the registry unreachable at runtime, an output schema that lies, a
`tool_` id that is not stable across versions, in-flight executions dropped mid-activation,
and live connections / trigger-instances broken when a definition changes or a tool
disappears.

---

## Confirmed decisions (developer-approved)

> All ten decisions **confirmed by the developer on 2026-07-16**, each matching the
> recommended option. Numbering continues the project's PD sequence from PD58 (operator
> auth). These are now binding for the TDD planner and programmer.

- **PD59 — Registry deployment:** a **separate deployable Go binary in this monorepo**
  (`registry-service/`, e.g. `cmd/registry`), shared across installations, publish-side.
  The `beecon serve` installation stays a single binary and runs fully offline when pinned —
  the registry is not embedded and is not a runtime dependency; the two communicate only
  over the registry's authenticated HTTP pull API.
- **PD60 — Registry bundle storage:** **git-backed behind a storage port**; publish
  governance via git permissions.
- **PD61 — `tool_` ids:** **minted at publish, immutable, carried in bundles**, stable
  across versions; slug addressing stays working (additive, not a replacement); the
  installation records the slug→`tool_` mapping from the activated bundle.
- **PD62 — Bundle scope & versioning:** **one provider per bundle at a semver** (additive →
  minor, removal → major, bump direction enforced by the registry) plus a content hash for
  integrity.
- **PD63 — Publish gates:** authenticated publish; **the bundle must parse under the strict
  `formatVersion: 1` loader, and every tool must declare an output schema AND a recorded
  sample response, with the output schema validating that sample** (reusing
  `internal/schema`).
- **PD64 — Sync placement:** installation-side pull/diff/activate lives **in `catalog/`
  behind a new driven `RegistryClient` port** (HTTP adapter + in-memory fake).
- **PD65 — Activated-definition storage:** **DB-backed via the existing bun adapter** (new
  `catalog` tables, Postgres + SQLite + memory fake); embedded YAML stays the first-boot
  seed; the DB store is the source of truth after activation, survives restart, and needs
  no redeploy.
- **PD66 — Activation safety:** **atomic**; in-flight executions finish on the version they
  started on; a tool removed by the new version is **soft-deprecated** (still resolvable);
  a trigger-instance on a removed trigger is **paused with a clear status**; existing
  connections are untouched; rollback = activating a previous version.
- **PD67 — Trust:** **v1 = API-key auth + TLS + content-hash verification on pull**;
  cryptographic bundle signing deferred to a later hardening pass.
- **PD68 — Migration:** **two-sided, non-breaking** — publish the embedded Outlook & Hubspot
  YAML as their initial `1.0.0` bundles (minting `tool_` ids), plus an idempotent boot
  backfill that records the embedded seed as the initially-activated version and stamps
  `tool_` ids, so live connections and trigger-instances keep working by slug with zero
  interruption.

> **Already settled (not reopened):** pull-only sync (H6 — "Decisions Already Made, do not
> reopen"); the registry never pushes/auto-updates; installation-side mutating routes are
> guarded by the **shipped operator session + CSRF** auth (ConsoleAuth), not the demoted
> break-glass admin key.

---

## Slice 1 — Walking skeleton: publish → pull → activate → execute by `tool_` id

The thinnest end-to-end ecosystem path, one provider, happy path only: a catalog maintainer
publishes a single-provider bundle to the separate registry; the registry mints `tool_` ids
and assigns version `1.0.0`; an authenticated installation pulls it and activates it; and a
tool in that bundle can be executed **by its `tool_` id**, end to end.

- [x] A catalog maintainer can publish a single-provider bundle (provider identity + tools + triggers + schemas, `formatVersion: 1`) to the registry and receives back the assigned version
- [x] Publishing a provider's first bundle assigns version `1.0.0`
- [x] Each tool in the published bundle is assigned an immutable `tool_<cuid2>` id, returned in the publish response alongside its slug
- [x] An installation authenticated with a valid registry API key can pull a specific provider bundle version, receiving the full bundle including every tool's `tool_` id and its input and output schemas
- [x] An operator can activate a pulled bundle version, after which the installation's catalog serves that version's tools and triggers without a redeploy
- [x] After activation, a tool in that bundle can be executed by its `tool_` id, producing the same `{successful, error, data}` result shape as slug-addressed execution
- [x] Executing an unknown `tool_` id returns a not-found error distinct from an execution failure
- [x] The registry runs as a service separate from `beecon serve` — the installation binary does not host or embed it
- [x] A pull with a missing or invalid registry API key is rejected with an authentication error and returns no bundle data

## Slice 2 — Publish is guarded: output-schema-against-sample validation & semver rules

Publish is where the registry earns its keep: it refuses a bundle whose declared output
schema does not match a recorded real response, and it enforces the semver bump direction.

- [x] Publish is rejected, naming the offending tool, when a tool in the bundle has no recorded sample response
- [x] Publish is rejected with a field-naming error when a tool's declared output schema does not validate against its recorded sample response
- [x] Publish is rejected, naming the file/field, when the bundle does not parse under the strict `formatVersion: 1` loader (same `KnownFields` strictness the installation applies)
- [x] Publish is accepted when the bundle parses and every tool's output schema validates its recorded sample
- [x] A new bundle version must be strictly greater than the provider's current latest version; publish is rejected otherwise
- [x] Adding a tool or trigger relative to the previous version is accepted only at a minor (or major) bump
- [x] Removing a tool or trigger relative to the previous version is rejected unless the major version is bumped
- [x] Re-publishing an already-published version number is rejected with a version-conflict error and changes nothing
- [x] Re-publishing a later version keeps the same `tool_` id for a tool whose slug is unchanged, and the publish response reports the tools/triggers added and removed relative to the previous version

## Slice 3 — Review before adopting: pull the version list and diff

Before anything changes, an operator can see exactly what a target version would add,
change, or remove — pulled from the registry, applied to nothing.

- [x] An operator can list the bundle versions the registry offers for a provider, with the version currently active in this installation marked
- [x] An operator can request a diff between the active version and a target version, showing tools and triggers added, changed, and removed
- [x] A tool appears as "changed" in the diff when its input or output schema differs between the two versions
- [x] Requesting a diff pulls from the registry but activates nothing — the active version and the served catalog are unchanged
- [x] A diff request while the registry is unreachable returns a clear registry-unavailable error and leaves the active version untouched
- [x] A diff against a version the registry does not offer returns a not-found error

## Slice 4 — Atomic activation, pinning, and dependent-work safety (HIGH-risk core)

Activation is the only moment the installation's served catalog changes — deliberate,
atomic, reversible only by activating another version, and never at the cost of in-flight
work or live orgs. The installation never depends on the registry at runtime.

- [x] Activation is atomic — if the target version fails format/schema validation or its content hash does not match on activation, the previously active version stays fully in force with no partial swap
- [x] Activating a version whose `formatVersion` this installation build does not support is rejected with a clear error, leaving the active version in force
- [x] The installation stays pinned to its activated version and is unaffected by newer versions the registry later publishes until an operator explicitly activates one
- [x] An execution already in flight when a version is activated completes against the definition version it started on (no mid-flight definition swap)
- [x] A tool removed by the newly-activated version remains resolvable (by `tool_` id and slug) as deprecated/removed for existing references, rather than being hard-deleted
- [x] A trigger-instance bound to a trigger removed by the newly-activated version is paused with a clear status, not silently dropped
- [x] Activating a new version leaves existing connections (OAuth credentials) for that provider untouched — credentials belong to the stable provider identity, not the definition version
- [x] An org's governance (allow-list, visibility) continues to apply to the newly-activated version's integrations
- [x] With no registry configured or reachable, the installation keeps serving its currently-activated (or embedded-seed) definitions, surfacing no error to end users
- [x] The activate route is a mutating route guarded by the shipped operator session + CSRF auth (ConsoleAuth); an unauthenticated or CSRF-missing activate request is rejected, and the version-list/diff/activate console surface meets the console accessibility bar (visible `:focus-visible` ring, 44×44px targets, WCAG AA contrast, `prefers-reduced-motion` respected)

## Slice 5 — `tool_` ids are the canonical handle everywhere

The id-system change lands fully: tools carry immutable `tool_` ids, callable everywhere
slug is, stable across versions, and used for attribution in logs and events (ADR-0006's
hand-off complete).

- [x] The tools catalog API returns each tool's immutable `tool_` id alongside its slug
- [x] Executing by slug continues to work exactly as before, so existing SDK/rolai callers are unbroken during and after the transition
- [x] Executing the same tool by its `tool_` id and by its slug resolve to the same tool and produce the same result shape
- [x] A tool's `tool_` id is unchanged after activating a later version in which its slug did not change
- [x] Event-log and outbox records reference the executed (or deprecated) tool by its `tool_` id alongside its slug, for stable cross-version attribution
- [x] A tool detail lookup by `tool_` id returns the same tool a slug lookup returns, and an unknown `tool_` id returns the catalog not-found error

## Slice 6 — Migration: the embedded providers become registry bundles without breaking live work

The shipped Outlook and Hubspot definitions move into the registry model with zero
interruption to live connections and trigger-instances.

- [x] A one-time publish loads the current embedded Outlook and Hubspot YAML into the registry as their initial `1.0.0` bundles, minting each tool's `tool_` id
- [x] On boot, the installation backfill records the embedded seed as the initially-activated version and mints/records `tool_` ids for its already-embedded tools
- [x] The boot backfill is idempotent — running it again after it has caught up mints nothing new and logs how many tools it stamped (including zero), mirroring the existing client-secret backfill convention
- [x] Existing connections continue to resolve and execute their provider's tools by slug throughout and after the migration, with no re-auth required
- [x] Existing trigger-instances continue to poll/fire against their trigger definitions throughout and after the migration
- [x] After migration, every embedded tool is addressable by both its slug and its newly-minted `tool_` id, and the two resolve to the same tool

---

## API Shape (indicative)

```
=== Registry service (separate deployable, registry-service/ — PD59) ===

Publish  (catalog maintainer / CI; git-permission + publish-token auth — PD60/PD63)
POST /registry/v1/providers/{providerSlug}/bundles
     { formatVersion: 1, provider, version,
       tools:    [{ slug, inputSchema, outputSchema, sample, mapping, ... }],
       triggers: [{ slug, configSchema, payloadSchema, poll, ... }] }
  -> 201 { version: "1.1.0", contentHash,
           tools: [{ id: "tool_<cuid2>", slug }],
           added: { tools: [...], triggers: [...] }, removed: { tools: [...], triggers: [...] } }
  -> 409 version-conflict (already published)
  -> 422 output-schema-vs-sample mismatch | missing sample | illegal semver bump | strict-parse failure

Pull  (installation; registry API-key auth, pull-only — H6)
GET  /registry/v1/providers                                  -> { items: [{ slug, latestVersion }] }
GET  /registry/v1/providers/{providerSlug}/bundles           -> { items: [{ version, contentHash, publishedAt }] }
GET  /registry/v1/providers/{providerSlug}/bundles/{version} -> full bundle incl. every tool_ id + contentHash
     (401 on missing/invalid registry API key)

=== Installation side (catalog registry-sync — PD64; operator session + CSRF on mutations) ===

GET  /api/v1/registry/providers/{slug}/versions        -> { items: [{ version, active: bool }], activeVersion }
GET  /api/v1/registry/providers/{slug}/diff?to={ver}   -> { from, to, added, changed, removed }   (tools & triggers)
POST /api/v1/registry/providers/{slug}/activate        { version }  -> 200 { activeVersion }
     (503 registry-unavailable on pull failure; 404 unknown version;
      422 unsupported formatVersion / content-hash mismatch)

=== tool_ id addressing (additive to slug — PD61) ===
GET  /api/v1/tools                                     -> { items: [{ id: "tool_...", slug, name, ... }] }
POST /api/v1/tools/{toolIdOrSlug}/execute              { userId, connectionId, arguments } -> { successful, error, data }
```

## Out of Scope (registry sub-phase)

- **Registry → installation push / auto-update / webhooks-from-registry** (H6) — pull-only.
- **Cryptographic bundle signing / publisher keypairs** (PD67) — later hardening pass;
  v1 trust is API-key + TLS + content-hash.
- **A richer definition format / expression engine** — ADR-0012 keeps `formatVersion: 1`;
  the registry serves that exact format. A format v2 is a separate future strand.
- **Automatic breaking-change classification of arbitrary schema edits** — the publisher
  declares the semver bump; the registry enforces direction, it does not diff-classify edits.
- **A registry browsing UI / public marketplace.**
- **Provider #3** — the model must make it cheap, but this sub-phase ships only the mechanism.
- **The other Phase 5 strands** — SDK adapters + Membrane importer, service-bus delivery,
  Rolai adoption, engine-gaps (their own specs).
- **Removing the embedded-YAML boot path** — embedded definitions remain the first-boot seed
  (PD65/PD68).

## Technical Context

- **Registry deployment (PD59):** a separate deployable Go binary in this monorepo
  (`registry-service/`, e.g. `cmd/registry`), depending on no domain module and **sharing
  the definition-format package** with `catalog` (BOUNDARIES). Its own storage (git-backed
  behind a port, PD60) and its own API-key/publish auth (PD63). The `beecon serve`
  installation stays a **single binary** (ADR-0001) — the registry is not embedded and not a
  runtime dependency; a pinned installation runs fully offline.
- **Definition format (binding, ADR-0012):** the registry stores/serves the **same**
  `formatVersion: 1` token-grammar format the embedded loader already parses; publish reuses
  the strict `KnownFields` loader and `internal/schema` for output-schema validation. No new
  format, no expression engine.
- **Installation-side sync (PD64):** lives in `catalog/` behind a new driven
  `RegistryClient` port (HTTP adapter + in-memory fake). `tool_` ids live on the `Tool` in
  `catalog`. No new installation module.
- **Storage (PD65):** activated definitions move from the boot-time in-memory-only
  `map[string]ProviderDefinition` to a DB-backed store via the existing `driven/bun`
  (Postgres + SQLite) + `driven/memory` pattern; embedded YAML stays the first-boot seed.
- **Ids (binding, ADR-0003/0006):** the reserved `tool_` prefix is minted by the registry at
  publish (PD61), immutable, stable across versions, additive to slug addressing. CUID2 via
  `github.com/akshayvadher/cuid2`, injected minter (deterministic in tests), exactly as
  every other prefixed id.
- **Auth (binding):** installation-side registry-sync mutating routes are guarded by the
  shipped operator session + CSRF (ConsoleAuth) — not the demoted break-glass admin key.
  Registry-side pull is API-key-authenticated; publish uses git permissions + a publish
  token.
- **Conventions (binding):** `httpx` DomainError envelope; cursor pagination; module graph
  enforced by `arch/imports_test.go`; secrets/tokens never logged. Integrations remain
  installation-level (definitions are installation-wide), so the org-scope arch test does
  not apply to the new activation state — but org governance still filters what each org sees.
- **New config:** `BEECON_REGISTRY_URL` (optional — unset = no registry, installation runs
  on its embedded/activated definitions offline), `BEECON_REGISTRY_API_KEY`. The registry
  binary has its own `BEECON_REGISTRY_*` set (storage location, publish token).
- Risk level: **HIGH** — atomic activation, no dropped in-flight work, dependent-entity
  safety (removed tools/triggers, paused trigger-instances, untouched connections), and
  backward-compatible slug addressing throughout the migration are the load-bearing
  invariants.

---

- [x] Reviewed
