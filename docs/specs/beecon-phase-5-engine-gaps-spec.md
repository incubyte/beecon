# Spec: Beecon Phase 5 — Template Engine Gaps (composable mapping values + tool-input defaults)

## Overview
Close two consistency gaps in `execution`'s declarative template engine so a tool's HTTP request is built the same way path and poll mappings already work. **Gap A** makes query/header/body mapping values support embedded/multi-token interpolation (e.g. `"receivedDateTime gt {input.since}"`, `"{input.first} {input.last}"`), replacing today's whole-token-only rule and killing the silent-literal footgun where a partial-token value is currently sent with its braces intact. **Gap C** applies a tool's `inputSchema` JSON-Schema `default`s to missing arguments before validation and request-building, mirroring what `mergeConfigWithSchemaDefaults` already does for trigger config.

This is consistency, not new logic — no conditionals, no functions, no new grammar tokens. It changes how every tool's request is built, so backward compatibility and no-silent-wrong-behavior are the core concerns.

**De-risking fact:** no current tool definition (Outlook, HubSpot) uses a partial-token mapping value — every existing query/header/body value is either a whole token (`"{input.top}"`) or a token-free literal. Gap A therefore changes **zero live request behavior**; it enables a new capability and closes a latent footgun. The only embedded-token mappings that exist today live in the trigger *poll* mappings, which already use `RenderPollTemplate` (find-anywhere) and are out of scope.

## Slice 1: Composable mapping values (Gap A — kill the silent-literal footgun)
The walking skeleton: unify tool query/header/body value rendering onto find-anywhere interpolation so an embedded token is substituted in place instead of the value being sent verbatim. `execution` owns this (`template.go` renderer + `facade.go` callers `buildToolQuery`/`buildToolHeaders`/`buildToolBody`).

- [x] A query mapping value that embeds a token inside a larger literal (e.g. `"receivedDateTime gt {input.since}"`) sends the token substituted in place, not the literal `{input.since}` with braces intact.
- [x] A header mapping value that embeds a token is likewise substituted in place, not sent verbatim.
- [x] A body mapping value that embeds a token is substituted in place before the JSON body is encoded.
- [x] A mapping value with multiple tokens (e.g. `"{input.first} {input.last}"`) sends every token substituted, with the surrounding literal text preserved.
- [x] An embedded `{params.x}` token resolves against the connection's param bag the same way an embedded `{input.x}` token resolves against arguments.
- [x] A body mapping value with a dotted key (e.g. `properties.email`) and an embedded/multi-token value builds the nested JSON object with the value interpolated.
- [x] An embedded/multi-token value with any referenced input/param absent fails the tool call as an invalid-arguments tool-level failure naming the missing token, and the provider is never called.
- [x] A whole-token value (`"{input.select}"`) whose input is supplied sends the substituted value; whose input is absent drops that query param / header entirely (and omits the body key) — unchanged from today.
- [x] A token-free literal value (e.g. `"application/json"`) is sent verbatim — unchanged from today.
- [x] Substituted query/header/body values are sent raw, not URL-escaped — unchanged from today.

**Test anchors:** `internal/execution/template_test.go` (`RenderMappedValue` cases — existing whole-token/literal/absent tests must stay green; new embedded/multi-token/embedded-missing cases added). `internal/execution/facade_test.go` (`Execute` request-building assertions on `Query`/`Headers`/`Body`). Mirror the embedded-token pattern already pinned by `TestRenderPollTemplate_SubstitutesAWatermarkTokenEmbeddedInsideALargerLiteral`.

## Slice 2: Tool-input defaults (Gap C)
Apply a tool's `inputSchema` `default`s to missing arguments before validation and request-building, mirroring `execution/poll.go`'s `mergeConfigWithSchemaDefaults`. `execution` owns the merge (it is the one place a tool's arguments are evaluated); `catalog` continues to own the schema.

- [x] A tool argument whose schema declares a `default` (e.g. `userId` with `default: me`) is filled with that default when the caller omits it, and the provider receives the defaulted value.
- [x] An explicitly-supplied argument is never overridden by its schema default — defaults fill only absent keys.
- [x] An explicitly-supplied `null` or empty string counts as present, so its default does not fill it.
- [x] Defaults are merged before argument validation, so a tool call that omits a required-but-defaulted argument validates and runs instead of failing validation.
- [x] Defaults are merged before path/query/body building, so a default can fill a path token (e.g. path `/users/{input.userId}` with `userId` default `me` renders `/users/me`).
- [x] Only top-level `inputSchema` property defaults are applied; a nested object property's `default` is not filled.
- [x] A tool whose `inputSchema` declares no defaults (or declares no schema at all) builds an identical request to today — no argument is invented.

**Test anchors:** `internal/execution/validate_test.go` / `facade_test.go` (`Execute` with a defaulted-arg schema); precedent to mirror is `poll.go`'s `mergeConfigWithSchemaDefaults` and its `folderId`-defaults-to-`Inbox` behavior.

## Backward Compatibility (cross-cutting — first-class)
- [x] Every existing test under `internal/execution` passes unchanged (no test edits to accommodate a behavior change for whole-token or literal values).
- [x] The Outlook definition (`outlook-list-messages`, `outlook-get-message`) builds a byte-identical request (URL, query, headers, body) before and after both slices.
- [x] The HubSpot definition (`hubspot-create-contact`, `hubspot-list-contacts`, `hubspot-upload-file`) builds a byte-identical request before and after both slices.
- [x] Trigger poll mappings (`RenderPollTemplate`, `buildPollRequest`) are untouched and behave identically.

## Mapping value grammar (contract)
Indicative — the value grammar after Slice 1, per token source:

```
mapping value := literal text with zero or more {input.NAME} / {params.NAME} tokens embedded anywhere
  "{input.select}"                      whole token   → substitute, or drop key if absent
  "application/json"                    no token      → sent verbatim
  "receivedDateTime gt {input.since}"   embedded      → substitute in place; error if a token is absent
  "{input.first} {input.last}"          multi-token   → substitute all; error if any token is absent
```

Token regex (unchanged): `\{(input|params)\.([A-Za-z0-9_]+)\}`. Query/header/body substitutions are raw (not URL-escaped); path substitutions remain URL-escaped.

## Out of Scope
- Conditionals, functions, formatters, or any new grammar in template values — this is consistency only.
- Trigger poll mapping rendering (already find-anywhere via `RenderPollTemplate`).
- Nested/deep `inputSchema` default application — top-level properties only.
- URL-escaping changes for query/header/body values.
- Changing `RenderPath`'s existing behavior (path already interpolates anywhere and errors on a missing token).
- Any change to the `catalog` definition format, YAML shape, or `Mapping`/`Tool.InputSchema` types — this strand is rendering + defaults in `execution` only.

## Technical Context
- **Module boundary:** `execution` owns rendering (`template.go`, `facade.go`, `poll.go`) and tool-input default application; `catalog` owns the `Mapping` and `Tool.InputSchema` schema. No boundary change (`execution` already depends on `catalog`).
- **Patterns to follow:**
  - Gap A mirrors `RenderPollTemplate` / `RenderPath` find-anywhere interpolation. The renderer used by `buildToolQuery`/`buildToolHeaders`/`buildToolBody` (`RenderMappedValue`) is unified onto find-anywhere; the whole-token drop is preserved for the exactly-one-token case.
  - Gap C mirrors `poll.go`'s `mergeConfigWithSchemaDefaults(config, configSchema)` (top-level `properties[].default`, fills only absent keys via `if _, exists`). DRY: prefer reusing/generalizing that existing helper (both live in package `execution`) over a second copy.
  - Validation is the shared `internal/schema.Validate`; `Execute` calls `validateArguments(tool.InputSchema, arguments)` — the default merge must happen in `Execute` before that call and before `callProvider` builds the request.
- **Key integration points:** `Facade.Execute` (defaults merge site), `callProvider` (must receive the merged arguments so URL/query/body/headers all see defaults), `RenderMappedValue` + its three `buildTool*` callers.
- **Risk level:** MODERATE — changes how every tool's request is built; backward compatibility and no-silent-wrong-behavior are the guardrails (see the Backward Compatibility block).

## Review
- [x] Reviewed
