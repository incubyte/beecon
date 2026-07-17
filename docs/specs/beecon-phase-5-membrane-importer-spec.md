# Spec: Beecon Phase 5 — Membrane-export → Beecon provider-definition importer

## Overview
A converter that reads Membrane (integration.app) connector-export YAML and emits Beecon `formatVersion: 1` provider-definition YAML plus a human-readable conversion report of what translated cleanly, what translated partially, and what a human must finish. It is a **migration aid** for Rolai onboarding (discovery doc Phase 5: "Membrane-export → Beecon definition importer, input = the YAML samples in `temp/`"), not a runtime path — its output is reviewed and completed by a human before any provider goes live.

## Depends On (must land first)
- **`docs/specs/beecon-phase-5-engine-gaps-spec.md`** (specced + reviewed). The importer targets the **post-engine-gaps** definition format: it may emit embedded-token query/header/body values (Gap A) and may translate `$firstNotEmpty($.input.x, "literal")` into an `inputSchema` `default` (Gap C). If engine-gaps has not landed, Slice 2's default-emission and embedded-token ACs cannot be honored — do not start this strand until engine-gaps is GREEN.
- **Target format is the token grammar, not an expression engine.** ADR-0012 (`docs/adr/0012`) defers a runtime expression engine (CEL when needed), so the importer converts Membrane's DSL down to Beecon's `{input.x}`/`{params.x}` token format exactly as this spec assumes — there is no richer target to emit into.

## IP / legal stance (bake into output and report — not optional)
- The Membrane export is treated as a **scaffold / hint**, never re-encoded verbatim as an authoritative mapping. Converted output is grounded in the target provider's own public API where a human completes it.
- Every emitted definition and the report carry a banner stating the output is **machine-scaffolded and MUST be reviewed and completed by a human before use** — the importer never claims automatic correctness.
- The report flags every Membrane-authored mapping as "confirm against provider docs." Whether reusing Membrane connector *content* wholesale is permissible under integration.app's ToS is a legal question owned outside this strand; the spec does not assume it is settled.

## Confirmed decisions (developer-approved, folded into the ACs below)
- **Delivery form:** `beecon import-membrane <in-dir> -o <out-dir>` CLI subcommand in the one binary — a thin shell over a delivery-independent converter package.
- **Fidelity:** best-effort + conversion report (convert what maps, flag/skip the rest with reasons).
- **Untranslatable constructs:** rule-based — a single-fallback `$case` (scoped-vs-default, e.g. `/users/{userId}` vs `/me`) emits the default branch + flags the dropped condition; a `$case` with 2+ substantive value branches skips the tool + reports it.
- **OAuth/baseUrl gap:** known-provider preset table (microsoft/hubspot/google → real authorize/token/baseUrl matched on connector identity) with TODO-placeholder fallback for unknown connectors.
- **Output target:** staging out-dir for human review (never written straight into `server/internal/catalog/providers/`).
- **Trigger scope:** attempt best-effort conversion, honestly reporting most triggers as needs-human with the missing poll fields named; a needs-human trigger never fails the run.
- **Report:** Markdown `import-report.md` next to the emitted YAML — Converted / Partial / Skipped sections, per-item source path + slug + reason, summary counts.
- **Grouping:** group by shared `integrationUuid` into one Beecon provider.

---

## Slice 1: Walking skeleton — one integration + one action → a definition the real loader accepts
The thinnest end-to-end path: point the importer at the sample directory, group by `integrationUuid`, and emit a single Beecon provider-definition YAML carrying provider identity and one converted tool, plus a report. Correctness of the mapping DSL is Slice 2 — here the bar is "it parses under the real strict loader and identity is right."

- [x] Running the importer over the sample directory emits exactly one provider-definition YAML file for the `integrationUuid` the action and integration share.
- [x] The emitted YAML parses without error through the real `catalog.LoadProviderDefinitions` (strict `KnownFields` decode), i.e. it contains no unknown or misspelled fields.
- [x] The emitted definition sets `formatVersion: 1`.
- [x] The emitted `slug` is derived from the integration `key`/`name` (e.g. `outlook`), lower-kebab, and is non-empty.
- [x] The emitted `name` and `logo` are taken from the integration record's `name` and `logoUri`.
- [x] The integration action becomes one entry under `tools` whose `slug`, `name`, and `description` come from the Membrane action's `key`, `name`, and `inputSchema.description`.
- [x] The emitted tool carries a non-empty `inputSchema` and `outputSchema` (Membrane `inputSchema` and `customOutputSchema` copied through), satisfying the loader's required-schema checks.
- [x] Shows an error naming the file and the missing field when an input Membrane file omits `integrationUuid` (cannot be grouped) — that file is skipped, not silently dropped, and the run continues.
- [x] A conversion report is written alongside the output listing the converted provider and tool.
- [x] The importer is invoked as `beecon import-membrane <in-dir> -o <out-dir>` and writes the definition + report under the staging out-dir; a missing `-o` fails with a usage message.

## Slice 2: Faithful action mapping — DSL translation, `$case` rule, and defaults
Translate a Membrane `type: api-request-to-external-app` action's `config.request` into a Beecon `tool.mapping`, converting the expression DSL where a clean equivalent exists and flagging/handling where it does not. Anchored to `temp/outlook-get-message.yaml`.

- [x] A `$var: $.input.NAME` value becomes the Beecon token `{input.NAME}` (e.g. `pathParameters.messageId: {$var: $.input.messageId}` → `{input.messageId}`).
- [x] Membrane `pathParameters` are inlined into the emitted `mapping.path` as tokens, so `/users/{userId}/messages/{messageId}` with `userId`/`messageId` bound to inputs emits a path templated as `{input.userId}`/`{input.messageId}`.
- [x] The Membrane `config.request.method` becomes `mapping.method`.
- [x] A Membrane `query` map becomes `mapping.query`, each `$var` value converted to a whole `{input.NAME}` token (e.g. `$select: {$var: $.input.select}` → `$select: "{input.select}"`).
- [x] A single-fallback `$case` path (scoped branch guarded by `$and`/`isNotEmpty`/`isNot` vs a plain default branch, as in the get-message `/users/{userId}/...` vs `/me/...`) emits the default branch's path and records the dropped condition in the report as a partial conversion.
- [x] A `$case` with two or more substantive value branches (genuine branching logic) skips the tool and reports it as needs-human with the reason "conditional mapping has no Beecon equivalent."
- [x] A `$firstNotEmpty($.input.NAME, "literal")` becomes the tool's `inputSchema` property `NAME` with `default: literal`, and the report notes the default was inferred.
- [x] An `$and`/`isNot`/`isNotEmpty` predicate, `$eval`, or embedded-code expression that is not part of a handled single-fallback `$case` causes the affected tool (or field) to be reported as needs-human with the specific unsupported construct named — it is never emitted as a literal `$`-string into the mapping.
- [x] The emitted tool still parses through the real loader (path and method present and non-empty, schemas present).
- [x] The report's Partial section names, per tool, exactly which Membrane constructs were dropped or defaulted and the source file+key.

## Slice 3: OAuth and baseUrl population — preset table with TODO fallback
The exports carry no OAuth authorize/token URLs and no baseUrl, but the loader requires `oauth.authorizeUrl`, `oauth.tokenUrl`, at least one scope, and `mapping.baseUrl`. Fill them from a known-provider preset table, falling back to clearly-marked TODO placeholders.

- [x] For a connector matched to the preset table (e.g. microsoft/graph from the Outlook connector identity), the emitted `oauth.authorizeUrl`, `oauth.tokenUrl`, `oauth.userInfoUrl`, `oauth.scopes`, and `mapping.baseUrl` are the real known values and the definition parses through the real loader with no TODOs.
- [x] For an unknown connector, the emitted OAuth/baseUrl fields carry explicit TODO placeholder values (e.g. `TODO://set-authorize-url`) so the shape is complete, and every such field is listed in the report's Partial section as human-required.
- [x] The preset match keys on stable connector identity (connector `key`/`connectorUuid`/`logoUri` host), not on the free-text integration `name`, so a renamed integration still matches its preset.
- [x] The emitted `authScheme` is `oauth2` (or omitted, which the loader defaults to `oauth2`).
- [x] A preset-filled definition and a TODO-fallback definition both round-trip through `catalog.LoadProviderDefinitions` — the TODO one only because placeholder strings are non-empty; the report makes clear it is not yet usable.
- [x] The report's banner and the Partial section state that all OAuth/baseUrl values (preset or TODO) must be confirmed against the provider's own developer docs before use.

## Slice 4: The conversion report — converted / partial / skipped, with reasons
Make the report a first-class artifact the operator can act on: it is the map of what a human must finish.

- [x] The report has three clearly separated sections: Converted, Partial (converted with caveats), and Skipped (needs-human).
- [x] Each Converted item names the source Membrane file+key and the resulting Beecon provider slug + tool/trigger slug.
- [x] Each Partial item names the source, the resulting slug, and each specific caveat (dropped condition, inferred default, TODO OAuth/baseUrl field).
- [x] Each Skipped item names the source and a specific reason (e.g. "branching `$case`", "trigger flow over abstract collectionKey", "unsupported predicate `$and`/`isNot`").
- [x] The report shows a summary count line (converted / partial / skipped) so the operator sees the ratio at a glance.
- [x] The report opens with the IP/review banner: output is machine-scaffolded and must be human-reviewed and completed before use.
- [x] Running the importer over the three samples produces a report whose contents a fixture test asserts item-by-item (expected slugs, expected caveats, expected skip reasons).

## Slice 5: Triggers — best-effort, honestly reported as mostly needs-human
Membrane triggers are multi-node flow graphs (`data-record-created-trigger` → `find-data-record-by-id` → `transform-data` → `api-request-to-your-app`) over abstract `collectionKey`s, while Beecon's poll needs a concrete method/path + `recordsPath`/`recordIdPath`/`recordTimestampPath`. Attempt conversion; report what cannot be resolved. Anchored to `temp/outlook-message-received-trigger.yaml`.

- [x] The importer detects a Membrane trigger file (has `nodes:` with a `data-record-created-trigger`) and attempts a trigger conversion rather than treating it as an action.
- [x] The Membrane `parametersSchema` becomes the Beecon trigger `configSchema`, preserving a `default` (e.g. `folderId` default `Inbox`) so it round-trips as a config default.
- [x] The trigger node's `outputSchema` (record shape) is carried into a `payloadSchema` for the emitted trigger.
- [x] Because the trigger's data source is an abstract `collectionKey` (`messages`) with no concrete REST path, method, or record/timestamp paths, the emitted trigger's `poll` mapping cannot be fully populated, so the trigger is reported as needs-human with the missing poll fields named — and, if emitted at all, is emitted with TODO poll placeholders, never a silently invalid poll block.
- [x] A trigger reported as needs-human does not cause the whole run to fail — actions in the same integration still convert and the provider file is still emitted.
- [x] The report's Skipped/Partial section states, per trigger, exactly which poll fields (`method`, `path`, `recordsPath`, `recordIdPath`, `recordTimestampPath`, `payload`) a human must supply, referencing the provider's polling endpoint.
- [x] Over the sample set, the run reports the trigger as needs-human (or partial), matching the expectation that most Membrane triggers do not auto-convert; the fixture test asserts this disposition rather than asserting a fully-live trigger.

---

## CLI / package shape (indicative)
```
beecon import-membrane <in-dir> -o <out-dir>
  in-dir : directory of Membrane export YAML files (integration + actions + triggers)
  out-dir: staging directory; receives <slug>.yaml provider definitions + import-report.md

exit 0 : ran to completion (partial/skipped items are reported, not failures)
exit !0: bad flags, unreadable in-dir, or an internal conversion panic
```
Package layout (in-boundary): a converter package under `catalog/` (catalog owns "registry-sync: import"), a pure transform of `[]byte` Membrane YAML → definition YAML bytes + report; a thin CLI shell in `cmd/beecon` holds zero conversion logic (flag parsing, file IO, calling the converter).

## Test strategy (fixture-based, per the strand's shape)
- Copy `temp/outlook-integration.yaml`, `temp/outlook-get-message.yaml`, `temp/outlook-message-received-trigger.yaml` into a `testdata/` directory as the importer's fixtures (do not depend on `temp/` at test time).
- Every "emitted YAML parses" AC is verified by loading the emitted bytes through the real exported `catalog.LoadProviderDefinitions(fsys fs.FS)` (strict `KnownFields`), not a hand-rolled parser — a field typo must fail the test exactly as it fails boot.
- Field/token ACs assert specific emitted values (e.g. `path` contains `{input.messageId}`, `query.$select == "{input.select}"`, `inputSchema.properties.userId.default == "me"` if the `$firstNotEmpty`/`$case` default is applied).
- Report ACs assert the report lists expected converted/partial/skipped items with their reasons, item-by-item.

## Out of Scope
- Runtime Membrane/Composio API compatibility (mirroring their endpoints/SDK) — explicitly excluded by the discovery doc; this strand is definition-format conversion only.
- Making converted providers live (loading into the registry / activating them) — that is the registry-service strand; the importer only writes files for human review.
- Any change to the `catalog` definition format, the loader, or `execution`'s template engine — the importer targets the format as-is (post-engine-gaps) and must not require format changes.
- Executing the converted provider (real OAuth, real Graph/HubSpot calls) — out; correctness is asserted by parse + field assertions, not live calls.
- A general-purpose Membrane DSL interpreter — only the constructs present in the samples and named here are handled; anything else is reported, not silently transformed.
- Round-tripping Beecon → Membrane (reverse direction).
- The per-operation SDK migration guide (a separate Phase 5 migration aid).

## Technical Context
- **Module boundary:** converter lives in-boundary under `catalog/` (owns import/diff/activate per `.claude/BOUNDARIES.md`); the CLI shell in `cmd/beecon` composes it and does IO only. No new cross-module dependency.
- **Target schema:** `server/internal/catalog/definition_v1.go` (`definitionFileV1` and nested shapes); required fields enforced by `validateDefinitionFileV1` (slug, name, logo, oauth.authorizeUrl, oauth.tokenUrl, ≥1 scope, mapping.baseUrl; per-tool method/path/inputSchema/outputSchema; per-trigger configSchema/payloadSchema/ingestion/poll.*).
- **Real loader (round-trip anchor):** `catalog.LoadProviderDefinitions` (`definition.go:29`) — the exported strict-decode entry point.
- **Reference outputs (the target style):** `server/internal/catalog/providers/outlook.yaml`, `hubspot.yaml`. Note outlook.yaml already hand-encodes exactly what this importer automates for get-message (the `/me/...` default branch of the `$case`, `$select` query) and the trigger (first-class poll replacing the flow graph) — treat it as the golden shape the importer should approximate.
- **Source DSL constructs seen in samples:** `$var: $.input.x`, `$case`/`cases`/`filter`, `$and`, `$eval`, `isNotEmpty`, `isNot`, `is`, `$firstNotEmpty`, `$plain`; actions are `type: api-request-to-external-app` with `config.request` (path/method/pathParameters/query); triggers are `nodes:` flow graphs over `collectionKey`s.
- **Engine-gaps dependency:** embedded-token query/header/body values and `inputSchema` defaults are only legal to emit once `beecon-phase-5-engine-gaps-spec.md` lands. Per ADR-0012 the target is the token grammar, not an expression engine.
- **Risk level:** MODERATE — a dev/migration tool (easy to re-run, output human-reviewed before use), but its whole value is fidelity + honesty; the guardrails are "output parses under the real strict loader" and "never silently emit an untranslatable construct — report it."

## Review
- [x] Reviewed
