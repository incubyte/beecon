> # ⛔ DEFERRED — DO NOT BUILD YET
>
> **Status: DEFERRED (developer decision, 2026-07-15).** The developer chose to build and
> test the non-registry Phase 5 strands first, leading with **real operator authentication**
> (see [`beecon-phase-5-operator-auth-spec.md`](./beecon-phase-5-operator-auth-spec.md)).
> The registry service + `tool_` ids described below are set aside to be revisited at a
> later date. This artifact is preserved as-is; **nothing here is scheduled**.
>
> **PD numbering note:** operator auth has **reclaimed PD49+**. The PD49–PD57 numbers used
> in this deferred spec are therefore stale — when this sub-phase is revived, its PDs will
> be **renumbered** to continue from wherever the numbering stands at that point. Do not
> treat the PD numbers below as live.

---

# Spec: Beecon Phase 5 (registry sub-phase) — The Registry Speaks: external tool-registry service & `tool_` ids

> Phase 5 ("Ecosystem") is far too large for one spec, so it is delivered as sub-phases;
> **this document specs the registry sub-phase** — the external tool-registry service, the
> `tool_` entity ids minted at publish, and the installation-side pull → diff → activate
> flow that validates H6. **(Now DEFERRED — see banner above.)**
> Discovery: [beecon-discovery.md](./beecon-discovery.md) (Phase 5 of the Milestone Map,
> H6, the "Tool registry (separate service)" and Import/export sections).
> Boundaries: `.claude/BOUNDARIES.md` (`registry-service/`, `catalog/`). Context:
> `.claude/bee-context.local.md`. Design brief: `.claude/DESIGN.md`.
> Builds on shipped Phase 1–4: [phase-1](./beecon-phase-1-spec.md),
> [phase-2](./beecon-phase-2-spec.md), [phase-3](./beecon-phase-3-spec.md),
> [phase-4](./beecon-phase-4-spec.md).

## Overview

Through Phase 4, every provider/tool/trigger definition lives as YAML embedded in the
installation binary (`catalog` loads it at boot, `formatVersion: 1`), tools are addressed
only by **slug**, and there is no way to update a definition without redeploying. This
sub-phase delivers the **ecosystem keystone**: a small, separately-deployed **registry
service** that holds semantically-versioned provider bundles, mints an **immutable `tool_`
id** for every tool at publish, validates that a tool's declared output schema actually
matches a recorded real response (the differentiator both vendors fail), and lets an
installation **pull a version, review the diff, and activate it** — never the reverse (H6:
pull-sync only, the registry never reaches into installations). This unblocks the discovery
success criterion that a second installation (eCW) can be stood up from documentation
alone, pulling definitions from the shared registry.

Risk: **HIGH** — a new separately-deployed service, a new authenticated trust boundary
between installation and registry, an id-system change (`tool_` ids joining slug
addressing) touching execution and logging, and an activation path that swaps what every
org can call. Failure modes (a bad bundle activated installation-wide, the registry being
unreachable at runtime, an output schema that lies, a `tool_` id that is not actually
stable across versions) are first-class in the slices below.

## Proposed Decisions (synthesis — STALE numbering, will be renumbered on revival)

> **AskUserQuestion was unavailable in this session.** Every PD below is a reasoned proposal
> grounded in the discovery doc, `.claude/BOUNDARIES.md`, the rolai context, and the
> existing Go codebase. **The scope-shaping ones — PD49, PD50, PD51, PD52, PD54 — still
> need developer sign-off when this sub-phase is revived.**

1. **PD49 — Registry sub-phase ships as its own vertical slice group.** (Numbering stale.)
2. **PD50 — Sub-phase ordering.** Superseded by the 2026-07-15 decision to lead with
   operator auth; the registry is now to-be-scheduled.
3. **PD51 — The registry is a separate deployable Go binary in this monorepo
   (`registry-service/`), not a module in the `beecon serve` binary.** BOUNDARIES already
   declares `registry-service/` a separate deployable that depends on no domain module and
   *shares the definition-format package*. It is a second `main` (e.g. `cmd/registry`)
   built from the same repo, deployed independently, and **shared across installations**.
   The installation stays a **single binary** — the registry is not embedded in it and is
   not a runtime dependency; the two communicate only over the registry's authenticated
   HTTP pull API. **Confirm: separate binary in THIS repo (vs external repo).**
4. **PD52 — Registry bundle storage is git-backed behind a storage port; publish governance
   = git permissions; installation pulls are API-key-authenticated** (per discovery's
   resolved decision). **Confirm git-backed storage as the default.**
5. **PD53 — Installations pull only; the registry never pushes (H6).** All sync is
   installation-initiated (pull → diff → activate). Push/auto-update/webhooks-from-registry
   are out. Pull auth is a per-installation **registry API key**.
6. **PD54 — `tool_<cuid2>` ids minted at publish, immutable, additive to slug addressing,
   stable across versions.** Addressing stays backward-compatible (slug keeps working
   everywhere; `tool_` id becomes the canonical handle in logs/events). **Confirm minting-
   at-publish + additive semantics.**
7. **PD55 — Bundle versioning: semver; additive → minor, removal → major; pin + diff +
   activate.** The registry enforces bump direction/consistency; it does not auto-classify
   arbitrary schema edits as breaking.
8. **PD56 — Publish validates each tool's output schema against a recorded sample response**
   (the differentiator; reuses `server/internal/schema`).
9. **PD57 — Installation-side registry-sync lives in `catalog` behind a `RegistryClient`
   port; a pinned version runs offline** (the registry is never a runtime dependency).

---

## Slice 1 — The registry speaks: publish a bundle, mint `tool_` ids, pull it back

The walking skeleton — the thinnest end-to-end ecosystem path: a catalog maintainer
publishes a provider bundle to the separate registry service; the registry mints `tool_`
ids and assigns a semantic version; an authenticated installation pulls the bundle back.

- [ ] A catalog maintainer can publish a provider bundle (provider identity + tools + triggers + schemas) to the registry and receives back the assigned semantic version
- [ ] Publishing a provider's first bundle assigns version `1.0.0`
- [ ] Each tool in a published bundle is assigned an immutable `tool_<cuid2>` id, returned in the publish response alongside its slug
- [ ] Re-publishing a later version keeps the same `tool_` id for a tool whose slug is unchanged
- [ ] Re-publishing an already-published version number is rejected with a version-conflict error and changes nothing
- [ ] An installation authenticated with a valid registry API key can pull the list of providers and each provider's available versions
- [ ] A pull with a missing or invalid registry API key is rejected with an authentication error and returns no bundle data
- [ ] Pulling a specific bundle version returns the full bundle including every tool's `tool_` id, input schema, and output schema
- [ ] The registry runs as a service separate from `beecon serve` — the installation binary does not host or embed it

## Slice 2 — Publish is guarded: output-schema validation and semver rules

Publish is where the registry earns its keep — it refuses a bundle whose output schema
lies about a recorded response, and it enforces the semver bump direction.

- [ ] Publish is rejected, naming the offending tool, when a tool in the bundle has no recorded sample response
- [ ] Publish is rejected with a field-naming error when a tool's declared output schema does not validate against its recorded sample response
- [ ] Publish is accepted when every tool's output schema validates its recorded sample
- [ ] A new bundle version must be strictly greater than the provider's current latest version; publish is rejected otherwise
- [ ] Adding a tool or trigger relative to the previous version is accepted at a minor (or major) bump
- [ ] Removing a tool or trigger relative to the previous version is rejected unless the major version is bumped
- [ ] The publish response reports, for the bundle, the tools and triggers added and removed relative to the previous version

## Slice 3 — The installation reviews before it adopts: pull + diff

Before anything changes, an operator can see exactly what a target version would add,
change, or remove — pulled from the registry, applied to nothing.

- [ ] An operator can list the bundle versions the registry offers for a provider, with the version currently active in this installation marked
- [ ] An operator can request a diff between the active version and a target version, showing tools and triggers added, changed, and removed
- [ ] A tool appears as "changed" in the diff when its input or output schema differs between the two versions
- [ ] Requesting a diff pulls from the registry but activates nothing — the active version and served catalog are unchanged
- [ ] A diff request while the registry is unreachable returns a clear registry-unavailable error and leaves the active version untouched
- [ ] A diff against a version the registry does not offer returns a not-found error

## Slice 4 — Activate and pin (and survive the registry being down)

Activation is the only moment the installation's served catalog changes — deliberate,
atomic, and reversible only by activating another version. The installation never depends
on the registry at runtime.

- [ ] An operator can activate a pulled bundle version for a provider; afterwards the catalog serves that version's tools and triggers
- [ ] The installation stays pinned to its activated version and is unaffected by newer versions the registry publishes until an operator explicitly activates one
- [ ] Activation is atomic — if the target version fails format/schema validation on activation, the previously active version stays in force with no partial swap
- [ ] Activating a version whose `formatVersion` this installation build does not support is rejected with a clear error, leaving the active version in force
- [ ] An org's governance (allow-list, visibility, PD42) continues to apply to the newly-activated version's integrations
- [ ] With no registry configured or reachable, the installation keeps serving its currently-activated (or embedded seed) definitions, with no error surfaced to end users
- [ ] The arch test asserting `RequireWrite` guards every org-key/admin mutating route is extended to cover the new activation route (Phase 4 carry-forward)
- [ ] The registry-sync console area (version list, diff, activate) meets the design-brief accessibility bar: visible `:focus-visible` ring, 44×44px targets, WCAG AA contrast, `prefers-reduced-motion` respected

## Slice 5 — `tool_` ids are the canonical handle

The id-system change lands end-to-end: tools carry immutable `tool_` ids, callable
everywhere slug is, stable across versions, and used for attribution in logs and events.

- [ ] The tools catalog API returns each tool's immutable `tool_` id alongside its slug
- [ ] A tool can be executed by its `tool_` id, producing the same `{successful, error, data}` result shape as executing by slug
- [ ] Executing by slug continues to work (provider/integration-scoped) so existing SDK callers are unbroken
- [ ] A tool's `tool_` id is unchanged after a version upgrade in which its slug did not change
- [ ] Executing an unknown `tool_` id returns a not-found error distinct from an execution failure
- [ ] Event-log and outbox records reference the executed/deprecated tool by its `tool_` id (alongside slug) for stable cross-version attribution

---

## API Shape (indicative)

```
=== Registry service (separate binary, registry-service/ — PD51) ===

Publish  (catalog maintainer / CI; git-backed publish governance per PD52, or authed POST)
POST /registry/v1/providers/{providerSlug}/bundles
     { formatVersion, provider, tools: [{ slug, inputSchema, outputSchema, sample }], triggers: [...], version }
  -> 201 { version: "1.1.0",
           tools: [{ id: "tool_<cuid2>", slug }],
           added: { tools: [...], triggers: [...] }, removed: { tools: [...], triggers: [...] } }
  -> 409 version-conflict (already published) | 422 output-schema mismatch / missing sample / illegal bump

Pull  (installation; registry API-key auth, pull-only — PD53)
GET  /registry/v1/providers                                  -> { items: [{ slug, latestVersion }] }
GET  /registry/v1/providers/{providerSlug}/bundles           -> { items: [{ version, publishedAt }] }
GET  /registry/v1/providers/{providerSlug}/bundles/{version} -> full bundle incl. every tool_ id
     (401 on missing/invalid registry API key)

=== Installation side (catalog registry-sync — PD57; admin-guarded) ===

GET  /api/v1/registry/providers/{slug}/versions        -> { items: [{ version, active: bool }], activeVersion }
GET  /api/v1/registry/providers/{slug}/diff?to={ver}   -> { from, to, added, changed, removed }   (tools & triggers)
POST /api/v1/registry/providers/{slug}/activate        { version }  -> 200 { activeVersion }
     (503 registry-unavailable on pull failure; 404 unknown version; 422 unsupported formatVersion)

=== tool_ id addressing (additive — PD54) ===
GET  /api/v1/tools                                     -> { items: [{ id: "tool_...", slug, name, ... }] }
POST /api/v1/tools/{toolIdOrSlug}/execute              { userId, connectionId, arguments } -> { successful, error, data }
```

## Out of Scope (registry sub-phase)

- **Registry → installation push / auto-update** (H6, v1) — pull-only.
- **Automatic breaking-change classification of schema edits** (PD55).
- **A registry browsing UI / public marketplace.**
- **Provider #3** — the model must make it cheap, but this sub-phase ships only the
  mechanism.
- **The other Phase 5 strands** — SDK polish + Membrane importer, operator auth, service
  bus, Rolai adoption (their own sub-phases).
- **Rewriting the embedded-YAML boot path** — Phase 1–2's embedded definitions remain the
  installation's seed/bootstrap.

## Technical Context

- **Stack (binding):** the registry is a **separate deployable Go binary in this monorepo**
  (`registry-service/`, e.g. `cmd/registry`), depending on no domain module and **sharing
  the definition-format package** with `catalog` (BOUNDARIES). Its own storage (git-backed
  behind a port, PD52) and its own API-key auth (PD53). The `beecon serve` installation
  stays a **single binary** — the registry is not embedded and not a runtime dependency; a
  pinned installation runs fully offline (PD57).
- **Boundaries (binding):** installation-side registry-sync lives in `catalog/` via a new
  driven **RegistryClient port** (HTTP adapter + fake). `tool_` ids live on the `Tool` in
  `catalog`. No new installation module; the registry service is the only new deployable.
- **Ids (binding):** new prefix **`tool_`** joins the CUID2-with-type-prefix set
  (BOUNDARIES lists `tool_`), minted by the registry at publish, immutable, stable across
  versions (PD54).
- **Conventions (binding):** installation-side endpoints follow the existing hexagonal /
  `httpx` DomainError / cursor-pagination / admin-guard conventions; module graph enforced
  by `arch/imports_test.go`; org-scope arch test unaffected (definitions are
  installation-wide).
- **New config:** `BEECON_REGISTRY_URL` (optional — unset = no registry),
  `BEECON_REGISTRY_API_KEY`. Registry-service has its own `BEECON_REGISTRY_*` set.
- Risk level: **HIGH**

---

- [ ] Reviewed  <!-- DEFERRED 2026-07-15 — not scheduled; PDs to be renumbered on revival -->
