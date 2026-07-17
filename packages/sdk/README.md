# @beecon/sdk

TypeScript SDK for the [Beecon](https://github.com/incubyte/beecon) API — a
self-hosted alternative to Membrane and Composio for integrating your app
with third-party providers (OAuth connections, tool execution, triggers, and
webhook events).

Zero runtime dependencies. ESM only, Node 18+.

## Install

```bash
npm install @beecon/sdk
```

## Quickstart

```ts
import { Beecon, type BeeconClient } from '@beecon/sdk';

const beecon: BeeconClient = new Beecon({
  apiKey: process.env.BEECON_API_KEY!, // beecon_sk_...
  baseUrl: process.env.BEECON_BASE_URL!, // e.g. https://beecon.example.com
});

const integrations = await beecon.integrations.list();
```

Type your own code against the `BeeconClient` interface (not the `Beecon`
class) so tests can substitute a `vi.fn()`-built double — every method
`BeeconClient` declares must be present, or `satisfies BeeconClient` fails to
compile.

For the full walkthrough — connecting a user, executing a tool, paging
results, uploading files, and receiving/verifying webhooks — see
[docs/quickstart.md](https://github.com/incubyte/beecon/blob/main/docs/quickstart.md).

## AI-framework tool adapters

Beecon tools convert into the shapes OpenAI's Responses API and Mastra's
`createTool` expect, scoped to one user + connection up front so the model
only ever supplies a tool's own arguments. Both adapters are opt-in subpath
imports — importing `@beecon/sdk` itself never loads `openai` or
`@mastra/core`.

### `@beecon/sdk/openai` — OpenAI Responses API

Install `openai` yourself — it's an optional peer dependency, not something
`@beecon/sdk` installs for you:

```bash
npm install openai
```

```ts
import OpenAI from 'openai';
import { Beecon, type BeeconClient } from '@beecon/sdk';
import { toOpenAITools } from '@beecon/sdk/openai';

const beecon: BeeconClient = new Beecon({
  apiKey: process.env.BEECON_API_KEY!,
  baseUrl: process.env.BEECON_BASE_URL!,
});
const openai = new OpenAI();

const catalog = (await beecon.tools.list({ providerSlug: 'outlook' })).items;

// userId/connectionId are curried here — the model never sees them, only
// its own tool arguments.
const { toolDefs, runToolCall } = toOpenAITools(beecon, catalog, { userId, connectionId });
// toolDefs[i] -> { type: 'function', name: slug, description, parameters: inputSchema }
// — the Responses API's flat shape, not Chat Completions' nested `function` wrapper.

const response = await openai.responses.create({
  model: 'gpt-4.1',
  input: 'List the 5 most recent messages in my inbox.',
  tools: toolDefs,
});

for (const output of response.output) {
  if (output.type !== 'function_call') continue;

  // runToolCall never throws on a provider-level tool failure — it resolves
  // { successful: false, error } so you can feed the result back to the
  // model. A platform-level failure (bad key, unknown tool, non-ACTIVE
  // connection, rate limit) still rejects with BeeconApiError/RateLimitedError.
  const result = await runToolCall({ name: output.name, arguments: output.arguments });
  console.log(result.successful ? result.data : result.error);
}
```

A runnable, type-checked version of this sample lives at
[`examples/openai.ts`](./examples/openai.ts).

### `@beecon/sdk/mastra` — registering tools on a Mastra agent

Install `@mastra/core` yourself — it's an optional peer dependency:

```bash
npm install @mastra/core
```

```ts
import { Agent } from '@mastra/core/agent';
import { Beecon, type BeeconClient } from '@beecon/sdk';
import { toMastraTools } from '@beecon/sdk/mastra';

const beecon: BeeconClient = new Beecon({
  apiKey: process.env.BEECON_API_KEY!,
  baseUrl: process.env.BEECON_BASE_URL!,
});

const catalog = (await beecon.tools.list({ providerSlug: 'outlook' })).items;

// userId/connectionId are curried here, same as the OpenAI adapter.
// toMastraTools lazily requires @mastra/core's own createTool the first time
// it runs, so you don't have to pass it yourself.
const mastraTools = toMastraTools(beecon, catalog, { userId, connectionId });
// each -> createTool({ id: slug, description, inputSchema, execute })

const agent = new Agent({
  name: 'inbox-assistant',
  instructions: 'Help the user manage their inbox.',
  model: /* your model */,
  tools: Object.fromEntries(catalog.map((tool, i) => [tool.slug, mastraTools[i]])),
});
```

On success a Mastra tool's `execute` returns the Beecon result's `data`
directly. A provider-level failure (`{ successful: false }`) throws
`MastraToolExecutionError` carrying `result.error.message` — Mastra tool
executions signal failure by throwing, so this is never a silently-swallowed
empty result. A platform-level `BeeconApiError`/`RateLimitedError` propagates
out of `execute` unchanged.

A runnable, type-checked version of this sample lives at
[`examples/mastra.ts`](./examples/mastra.ts).

### Verifying these samples compile

Both samples above are backed by real source files under
[`examples/`](./examples) that import from this package's built types and are
type-checked by `npm run typecheck` (see `tsconfig.examples.json`) — they are
never bundled into the published tarball (only `dist` ships; see `files` in
`package.json`).

## Migrating from Membrane?

The per-operation Membrane → Beecon migration guide is **not** part of this
package's docs — it belongs to the separate importer strand. Watch for it
there instead of expecting it here.

## License

MIT
