// @beecon/sdk/openai — turns Beecon catalog tools into OpenAI Responses API
// function-tools. This subpath never imports the `openai` package: the
// Responses API's flat function-tool shape is small enough to declare
// locally, so importing `.` (the core SDK) never pulls in `openai` even
// though the two ship from the same package (Phase 5 decisions).
import type { BeeconClient, Tool, ToolExecutionResult } from './types.js';

// OpenAIFunctionToolDef is the Responses API's flat shape — NOT the Chat
// Completions nested `{ type: 'function', function: {...} }` wrapper.
// `parameters` is the tool's `inputSchema` passed through verbatim (PD13):
// this adapter never re-derives a schema.
export interface OpenAIFunctionToolDef {
  type: 'function';
  name: string;
  description: string;
  parameters: Record<string, unknown>;
}

export interface ToOpenAIToolsOptions {
  userId: string;
  connectionId: string;
  /** Deprecated tools are excluded unless this is explicitly true. */
  includeDeprecated?: boolean;
}

export interface OpenAIToolCall {
  name: string;
  /** The model's tool-call arguments: a JSON string or an already-parsed object. */
  arguments: string | Record<string, unknown>;
}

export interface OpenAITools {
  toolDefs: OpenAIFunctionToolDef[];
  runToolCall(call: OpenAIToolCall): Promise<ToolExecutionResult>;
}

// UnknownToolCallError is a client-side adapter failure — the model named a
// tool outside the set toOpenAITools was built from. It is distinct from
// BeeconApiError (a platform-level HTTP failure) and from a
// `{ successful: false }` provider-level result.
export class UnknownToolCallError extends Error {
  readonly toolName: string;

  constructor(toolName: string) {
    super(`beecon: no tool named "${toolName}" in the set toOpenAITools was built from.`);
    this.name = 'UnknownToolCallError';
    this.toolName = toolName;
    Object.setPrototypeOf(this, UnknownToolCallError.prototype);
  }
}

// toOpenAITools converts one Tool (or a list) into OpenAI function-tool
// definitions and a runToolCall dispatcher, both scoped to
// { userId, connectionId } curried at build time (Phase 5 decisions): the
// model only ever supplies a tool's own arguments. runToolCall forwards to
// beecon.tools.execute, so a provider-level failure resolves as
// { successful: false, error } (PD6) while a platform-level failure
// (BeeconApiError/RateLimitedError, PD21) still rejects the returned
// promise. The input Tool objects are only read, never mutated.
export function toOpenAITools(
  beecon: BeeconClient,
  tools: Tool | Tool[],
  options: ToOpenAIToolsOptions,
): OpenAITools {
  const included = selectTools(tools, options.includeDeprecated ?? false);
  const toolDefs = included.map(toFunctionToolDef);
  const knownSlugs = new Set(included.map((tool) => tool.slug));

  const runToolCall = async (call: OpenAIToolCall): Promise<ToolExecutionResult> => {
    if (!knownSlugs.has(call.name)) {
      throw new UnknownToolCallError(call.name);
    }
    return beecon.tools.execute(call.name, {
      userId: options.userId,
      connectionId: options.connectionId,
      arguments: parseToolCallArguments(call.arguments),
    });
  };

  return { toolDefs, runToolCall };
}

function selectTools(tools: Tool | Tool[], includeDeprecated: boolean): Tool[] {
  const list = Array.isArray(tools) ? tools : [tools];
  return includeDeprecated ? list : list.filter((tool) => !tool.deprecated);
}

function toFunctionToolDef(tool: Tool): OpenAIFunctionToolDef {
  return {
    type: 'function',
    name: tool.slug,
    description: tool.description,
    parameters: tool.inputSchema,
  };
}

function parseToolCallArguments(args: string | Record<string, unknown>): Record<string, unknown> {
  return typeof args === 'string' ? JSON.parse(args) : args;
}
