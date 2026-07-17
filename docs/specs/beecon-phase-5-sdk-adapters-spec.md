# Spec: Beecon Phase 5 — SDK polish (npm publish + AI-framework tool adapters)

## Overview

Make `@beecon/sdk` a first-class dependency for consumers: publish it to public npm (so
Rolai stops hand-maintaining an HTTP client and a mirrored type file) and add subpath
adapters that turn a Beecon tool into an OpenAI function-tool and a Mastra tool, plus
typed helpers so triggers and webhook events are just as easy to consume. This is what
lets Rolai hand Beecon tools straight to an LLM/agent runtime, the way it uses
`@composio/mastra` today. The core SDK stays zero-runtime-dependency; every adapter is an
opt-in subpath import with optional peer dependencies.

## Decisions (settled with akshay, 2026-07-13)

- **Package name:** `@beecon/sdk` (unchanged; requires owning the `@beecon` npm scope).
- **Registry:** public npm (`registry.npmjs.org`).
- **Release:** manual `npm publish` by a maintainer — no CI-publish or changesets in this
  strand — but publish must be gated on a green build + full test run.
- **Version line:** stays `0.x` (no stability promise; breaking changes allowed on minor).
- **Adapter surface:** subpath entrypoints `@beecon/sdk/openai` and `@beecon/sdk/mastra`
  (functions `toOpenAITools(...)` / `toMastraTools(...)`); triggers/webhooks helpers land
  under a third subpath `@beecon/sdk/agent` (see Slice 5). No methods added to the client.
- **Adapter dependencies:** `openai` and `@mastra/core` are optional `peerDependencies`
  (`peerDependenciesMeta.*.optional = true`); the core SDK keeps zero runtime deps.
- **Execution binding:** `userId` + `connectionId` are curried at build time —
  `toOpenAITools(beecon, { userId, connectionId })` returns already-scoped tools whose
  execute forwards to `beecon.tools.execute(slug, { userId, connectionId, arguments })`.
- **OpenAI shape:** Responses API **flat** shape — `{ type: 'function', name, description,
  parameters }`. NOT the Chat Completions nested `{ type: 'function', function: {...} }`
  shape.
- **Adapter scope:** tools AND triggers/webhooks (Slice 5 is in scope).
- **Docs scope:** adapter usage docs + README + install only. The Membrane→Beecon
  per-operation migration guide is out of this strand (it belongs to the importer strand).

---

## Slice 1 — The SDK is a real, installable package (walking skeleton)

The thinnest end-to-end win: a consumer runs `npm install @beecon/sdk` from public npm and
gets the current SDK surface with native types, instead of a hand-mirrored file.

- [x] A consumer can install `@beecon/sdk` from public npm and import `Beecon`,
      `BeeconClient`, and the exported error classes with no build step of their own.
- [x] The installed package resolves types for every existing sub-API (users,
      integrations, connections, tools, logs, userTokens, files, triggers,
      webhookEndpoint, events, webhooks) — `import type { BeeconClient } from '@beecon/sdk'`
      type-checks against the same surface as the source.
- [x] The published tarball contains only build output and declaration files — no `src`,
      no tests, no `node_modules` — verifiable via `npm pack --dry-run`.
- [x] Importing the package in an ESM Node 18+ project works (matches the current
      `"type": "module"`, NodeNext, `.js`-extension build).
- [x] `npm install @beecon/sdk` adds zero transitive runtime dependencies.
- [x] The package declares its license, repository URL, and a `README` that renders on the
      npm package page.
- [x] The first published release is a `0.x` version.
- [x] `npm publish` is preceded by a build and the full vitest suite, and aborts if either
      fails (e.g. a `prepublishOnly` gate) — a broken build never reaches the registry.
- [x] An already-published version cannot be republished — re-running publish with an
      unchanged version fails loudly rather than silently succeeding.

## Slice 2 — A Beecon tool becomes an OpenAI function-tool

A consumer imports `toOpenAITools` from `@beecon/sdk/openai`, converts catalog tools into
OpenAI Responses-API function-tools scoped to one user+connection, and hands them to a
model; when the model calls one, execution routes through `beecon.tools.execute`.

- [x] A consumer can import `toOpenAITools` from `@beecon/sdk/openai` and turn a `Tool`
      (or a list of tools) into OpenAI function-tool definitions.
- [x] Each generated definition has the flat Responses-API shape
      `{ type: 'function', name, description, parameters }` — not the Chat Completions
      nested `function` wrapper.
- [x] Each definition's `name` is the tool's `slug` and its `description` is the tool's
      `description`.
- [x] Each definition's `parameters` is the tool's `inputSchema` passed through verbatim
      (the accurate JSON Schema Beecon ships, PD13) — not a re-derived schema.
- [x] `toOpenAITools(beecon, { userId, connectionId })` returns tools already scoped to
      that user and connection, so the model calls a tool with only the tool's own
      arguments.
- [x] The consumer gets a way to run a model tool-call: given the model's chosen tool name
      and JSON arguments, it invokes `beecon.tools.execute(slug, { userId, connectionId,
      arguments })` and returns the `{ successful, error, data }` result unchanged.
- [x] A provider-level tool failure comes back as a `{ successful: false, error }` value
      the consumer can feed to the model as the tool result — never a thrown exception
      (preserves PD6).
- [x] A platform-level failure (unknown tool, bad key, non-ACTIVE connection, rate limit)
      still throws `BeeconApiError`/`RateLimitedError` — the adapter does not swallow it.
- [x] Shows a clear error when a tool-call names a tool that is not in the set the consumer
      built the adapter from.
- [x] A deprecated tool is excluded by default and included only when the consumer opts in.
- [x] The adapter does not mutate the `Tool` objects passed in.

## Slice 3 — A Beecon tool becomes a Mastra tool

The same capability for `@mastra/core`'s `createTool`, so a consumer can register Beecon
tools on a Mastra agent (parity with `@composio/mastra`).

- [x] A consumer can import `toMastraTools` from `@beecon/sdk/mastra` and turn a `Tool`
      (or a list of tools) into Mastra tools.
- [x] Each Mastra tool is built via `createTool()` (not a plain object, which Mastra
      silently fails to execute).
- [x] Each Mastra tool's `id` is the tool's `slug` and its `description` is the tool's
      `description`.
- [x] Each Mastra tool's `inputSchema` is the tool's JSON-Schema `inputSchema` passed
      through in a form `createTool` accepts (Mastra takes any Standard-JSON-Schema
      input) — not a re-derived schema.
- [x] `toMastraTools(beecon, { userId, connectionId })` returns tools already scoped, and
      each tool's `execute` forwards Mastra's parsed `context` as `arguments` to
      `beecon.tools.execute(slug, { userId, connectionId, arguments })`.
- [x] On a successful execution the Mastra tool returns the Beecon result's `data`.
- [x] A provider-level failure (`successful: false`) surfaces as a Mastra tool error
      carrying `result.error.message` — never a silently-swallowed empty result.
- [x] A platform-level `BeeconApiError`/`RateLimitedError` propagates out of the Mastra
      `execute` rather than being converted to a fake success.
- [x] `@mastra/core` is declared an optional peer dependency and is absent from the core
      SDK's install footprint.

## Slice 4 — Docs make the adapters and install obvious

- [x] The README (published with the package) shows `npm install @beecon/sdk` and a
      minimal construct-the-client example.
- [x] Docs show, end-to-end, importing `toOpenAITools` from `@beecon/sdk/openai`, passing
      the definitions to an OpenAI Responses-API call, and running the model's tool-call
      through Beecon.
- [x] Docs show, end-to-end, importing `toMastraTools` from `@beecon/sdk/mastra` and
      registering the tools on a Mastra agent.
- [x] Docs state that the consumer installs `openai` / `@mastra/core` themselves (optional
      peer deps) and which subpath needs which.
- [x] Every adapter code sample compiles against the published types (verified by a
      compile check, not hand-written prose).
- [x] The Membrane→Beecon migration guide is explicitly noted as out of this strand, with
      a pointer to the importer strand — not half-documented here.

## Slice 5 — Triggers and webhook events reach the consumer through typed helpers

Triggers and connection-lifecycle events do not fit the LLM function-tool model the way
actions do — they are inbound events, not model-callable functions. This slice gives the
consumer typed helpers over the existing `triggers` / `webhookEndpoint` / `events`
sub-APIs and `webhooks.verify`, exposed from a third subpath `@beecon/sdk/agent`, so that
consuming Beecon events is as ergonomic as consuming its tools — without forcing events
into function-tool form.

- [x] A consumer can import the trigger/webhook helpers from a dedicated subpath
      (`@beecon/sdk/agent`) separate from the `openai`/`mastra` subpaths.
- [x] A consumer can create a trigger instance for a scoped connection through a helper
      that forwards to `beecon.triggers.create` with the bound `connectionId`, without
      re-passing the connection each call.
- [x] The consumer gets a typed dispatch helper over a verified `WebhookEvent`: given the
      raw payload + headers + secret, it verifies via `webhooks.verify` and routes to a
      per-event-type handler (`trigger.event`, `connection.expired`, `webhook.test`) with
      the event's typed `data`.
- [x] A signature/timestamp verification failure surfaces as `WebhookVerificationError`
      (from the existing verifier) — the helper does not swallow it or convert it to a
      handled event.
- [x] The dispatch helper exposes the event's idempotency `id` to the caller so the
      consumer can deduplicate — it does not silently drop or dedupe on the caller's
      behalf.
- [x] The helpers add no dependency on `openai` or `@mastra/core` and reuse the existing
      `webhooks.verify` (no second verifier implementation).

> Open design note (Slice 5): the exact ergonomics of the trigger/webhook helper —
> whether create-trigger is a thin curried wrapper vs a richer builder, and whether
> dispatch is a `switch`-style router vs a handler-map — is the one real decision left in
> this strand. The TDD planner should propose the smallest shape that satisfies the ACs
> above (YAGNI: no builder unless an AC needs it) and flag it for akshay in the plan.

---

## API Shape (indicative)

```ts
// OpenAI (Responses API flat shape) — @beecon/sdk/openai
import { toOpenAITools } from '@beecon/sdk/openai';

const catalog = (await beecon.tools.list({ providerSlug: 'outlook' })).items;
const { toolDefs, runToolCall } = toOpenAITools(beecon, catalog, { userId, connectionId });
// toolDefs[i] -> { type: 'function', name: slug, description, parameters: inputSchema }
// runToolCall({ name, arguments }) -> ToolExecutionResult   (never throws on provider error)

// Mastra — @beecon/sdk/mastra
import { toMastraTools } from '@beecon/sdk/mastra';
const mastraTools = toMastraTools(beecon, catalog, { userId, connectionId });
// each -> createTool({ id: slug, description, inputSchema, execute })

// Triggers / webhook events — @beecon/sdk/agent
import { onWebhookEvent } from '@beecon/sdk/agent';
const event = onWebhookEvent({ payload, headers, secret }, {
  'trigger.event': (data) => { /* data is TriggerEventData */ },
  'connection.expired': (data) => { /* ... */ },
  'webhook.test': () => {},
});
// throws WebhookVerificationError on bad signature; returns the verified WebhookEvent
```

## Out of Scope

- Any AI agent runtime / agent loop — Beecon emits tool shapes and event helpers; the
  agent lives in the consumer (per discovery Out of Scope).
- Adapters for framework tool shapes beyond OpenAI and Mastra (Anthropic, LangChain,
  Vercel AI SDK, etc.).
- The Chat Completions nested tool shape — only the Responses API flat shape is emitted.
- CI-driven publishing, changesets, and any automated release pipeline — publish is manual
  this strand.
- The Membrane-export → Beecon definition importer and the per-operation SDK migration
  guide — separate Phase 5 "Migration aids" strand.
- The registry service and service-bus delivery adapter — separate Phase 5 strands.
- Changing the wire contract or any existing sub-API surface — this strand packages and
  adapts what already exists; it adds no new server endpoints.
- Rolai's own router rewrite (`integration-routing.service.ts`) — Rolai-side adoption work
  downstream of this strand.

## Technical Context

- **Patterns to follow:** existing `packages/sdk` conventions — `BeeconClient` interface
  is the public surface (consumers type against it, mock with `vi.fn()`); NodeNext ESM with
  `.js` import extensions; `tsc`-based build to `dist`; vitest suite; API key never lands
  on an enumerable property. Adapters are new files/subpaths built and exported the same
  way.
- **Zero-runtime-dependency core is a hard constraint:** `openai` and `@mastra/core` are
  optional `peerDependencies` only; adapters import their types, never bundle their
  runtime. `npm install @beecon/sdk` must still pull zero transitive runtime deps.
- **Subpath exports:** `package.json` `exports` must add `./openai`, `./mastra`, and
  `./agent` entries (types + import) alongside the existing `.` — each with its own build
  output; a consumer importing only `.` must not trigger loading of `openai`/`@mastra/core`.
- **Result contract is load-bearing:** `tools.execute` returns
  `{ successful, error, data, nextCursor? }` and never throws on provider errors (PD6);
  only platform failures throw `BeeconApiError`/`RateLimitedError` (PD21). Both action
  adapters preserve this distinction (Slices 2 & 3).
- **Tool schema source of truth:** `Tool.inputSchema` (PD13 accurate JSON Schema) is passed
  through verbatim; adapters do not re-derive it.
- **Boundaries:** `packages/sdk/` depends on the API contract only (`.claude/BOUNDARIES.md`);
  adapters live inside this package and add no dependency on any server module.
- **Interop unaffected:** the golden-vector Go↔TS webhooks-verifier interop constraint is
  untouched — Slice 5 reuses the existing `webhooks.verify`, adding no second verifier.
- **Tests are anchorable:** every AC is expressible as a vitest test — shape assertions on
  generated defs, execute-routing with a `vi.fn()` client double, `npm pack` contents,
  subpath-resolution/type checks, and verification-failure paths.
- **Risk level:** MODERATE — public published surface and a new opt-in dependency boundary;
  no credential-security or data-isolation surface in this strand.

---

- [x] Reviewed
