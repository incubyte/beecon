// This file backs the OpenAI sample in ../README.md. It is compiled by
// `npm run typecheck` (see ../tsconfig.examples.json) so the sample provably
// type-checks against this package's real, built types — it is never
// published (only "dist" ships, see package.json's "files").
//
// It imports from this package's own src/ rather than "@beecon/sdk/openai"
// because the file lives inside the package it is demonstrating; a real
// consumer writes the subpath imports shown in the README instead.
//
// It also does not import the `openai` package itself (an optional peer
// dependency this package's devDependencies deliberately omit, to keep this
// package's own zero-runtime-dependency install footprint honest).
// `OpenAIResponsesClient` below is a minimal structural stand-in for the
// slice of `new OpenAI().responses` this sample calls — a real consumer
// passes the actual `openai` client instead, which satisfies this same
// shape.
import { Beecon, type BeeconClient } from '../src/index.js';
import { toOpenAITools } from '../src/openai.js';

interface OpenAIFunctionCallOutput {
  type: 'function_call';
  name: string;
  arguments: string;
}

interface OpenAIResponsesResult {
  output: OpenAIFunctionCallOutput[];
}

interface OpenAIResponsesClient {
  responses: {
    create(input: {
      model: string;
      input: string;
      tools: unknown[];
    }): Promise<OpenAIResponsesResult>;
  };
}

export async function askModelToListRecentMessages(
  beecon: BeeconClient,
  openai: OpenAIResponsesClient,
  userId: string,
  connectionId: string,
): Promise<void> {
  const catalog = (await beecon.tools.list({ providerSlug: 'outlook' })).items;
  const { toolDefs, runToolCall } = toOpenAITools(beecon, catalog, { userId, connectionId });

  const response = await openai.responses.create({
    model: 'gpt-4.1',
    input: 'List the 5 most recent messages in my inbox.',
    tools: toolDefs,
  });

  for (const output of response.output) {
    if (output.type !== 'function_call') continue;
    const result = await runToolCall({ name: output.name, arguments: output.arguments });
    console.log(result.successful ? result.data : result.error);
  }
}

export function buildScopedBeecon(): BeeconClient {
  return new Beecon({
    apiKey: process.env.BEECON_API_KEY!,
    baseUrl: process.env.BEECON_BASE_URL!,
  });
}
