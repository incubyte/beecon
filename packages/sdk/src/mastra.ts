// @beecon/sdk/mastra — turns Beecon catalog tools into Mastra tools built via
// `@mastra/core`'s `createTool`. `@mastra/core` is an optional peer
// dependency (Phase 5 decisions): this subpath never imports it at the
// module top level, so importing `.` (the core SDK) or even this subpath
// never pulls in `@mastra/core`'s runtime. The `createTool` function itself
// is resolved lazily — from `options.createTool` when the caller supplies
// one, otherwise from a lazily `require`d `@mastra/core` the first time
// `toMastraTools` actually runs.
import { createRequire } from 'node:module';
import type { BeeconClient, Tool, ToolExecutionResult } from './types.js';

// MastraToolExecuteInput mirrors the shape createTool()'s `execute` is
// called with: Mastra parses the model's arguments against `inputSchema`
// and hands the result back as `context`.
export interface MastraToolExecuteInput {
  context: Record<string, unknown>;
}

// MastraCreateToolConfig mirrors the subset of `@mastra/core`'s
// `createTool()` config this adapter relies on. `inputSchema` is the tool's
// `inputSchema` passed through verbatim (PD13) — Mastra accepts any
// Standard-JSON-Schema input, so this adapter never re-derives one.
export interface MastraCreateToolConfig {
  id: string;
  description: string;
  inputSchema: Record<string, unknown>;
  execute: (input: MastraToolExecuteInput) => Promise<unknown>;
}

// CreateToolFn is `@mastra/core`'s `createTool` — a real Mastra tool is only
// ever what this function returns; a plain object shaped like a Mastra tool
// is not enough (Mastra silently fails to execute it).
export type CreateToolFn = (config: MastraCreateToolConfig) => unknown;

export interface ToMastraToolsOptions {
  userId: string;
  connectionId: string;
  /** Deprecated tools are excluded unless this is explicitly true. */
  includeDeprecated?: boolean;
  /**
   * The `createTool` function from `@mastra/core`. Optional: when omitted,
   * `toMastraTools` lazily requires `@mastra/core` itself, so most consumers
   * never need to pass this — it exists as a seam for callers whose module
   * loader can't resolve the optional peer synchronously, and for testing
   * without installing `@mastra/core`.
   */
  createTool?: CreateToolFn;
}

// MastraToolExecutionError is how a provider-level tool failure
// (`{ successful: false, error }`, PD6) surfaces to Mastra: a thrown error
// carrying the Beecon error's message, never a silently-swallowed empty
// result. A platform-level failure (BeeconApiError/RateLimitedError, PD21)
// is not this error — it propagates out of `execute` unchanged.
export class MastraToolExecutionError extends Error {
  readonly toolSlug: string;
  readonly code: string;

  constructor(toolSlug: string, code: string, message: string) {
    super(message);
    this.name = 'MastraToolExecutionError';
    this.toolSlug = toolSlug;
    this.code = code;
    Object.setPrototypeOf(this, MastraToolExecutionError.prototype);
  }
}

// toMastraTools converts one Tool (or a list) into real Mastra tools built
// via createTool(), both scoped to { userId, connectionId } curried at build
// time (Phase 5 decisions): the model only ever supplies a tool's own
// arguments. Each tool's execute forwards to beecon.tools.execute, so a
// provider-level failure throws MastraToolExecutionError (PD6) while a
// platform-level failure (BeeconApiError/RateLimitedError, PD21) still
// rejects unchanged. The input Tool objects are only read, never mutated.
export function toMastraTools(
  beecon: BeeconClient,
  tools: Tool | Tool[],
  options: ToMastraToolsOptions,
): unknown[] {
  const createTool = options.createTool ?? loadCreateTool();
  const included = selectTools(tools, options.includeDeprecated ?? false);
  return included.map((tool) => buildMastraTool(tool, beecon, options, createTool));
}

function buildMastraTool(
  tool: Tool,
  beecon: BeeconClient,
  options: ToMastraToolsOptions,
  createTool: CreateToolFn,
): unknown {
  return createTool({
    id: tool.slug,
    description: tool.description,
    inputSchema: tool.inputSchema,
    execute: ({ context }) => runTool(tool.slug, context, beecon, options),
  });
}

async function runTool(
  slug: string,
  context: Record<string, unknown>,
  beecon: BeeconClient,
  options: ToMastraToolsOptions,
): Promise<unknown> {
  const result = await beecon.tools.execute(slug, {
    userId: options.userId,
    connectionId: options.connectionId,
    arguments: context,
  });
  return toMastraExecuteResult(slug, result);
}

function toMastraExecuteResult(slug: string, result: ToolExecutionResult): unknown {
  if (!result.successful) {
    const error = result.error ?? { code: 'unknown', message: 'beecon: tool execution failed.' };
    throw new MastraToolExecutionError(slug, error.code, error.message);
  }
  return result.data;
}

function selectTools(tools: Tool | Tool[], includeDeprecated: boolean): Tool[] {
  const list = Array.isArray(tools) ? tools : [tools];
  return includeDeprecated ? list : list.filter((tool) => !tool.deprecated);
}

function loadCreateTool(): CreateToolFn {
  const require = createRequire(import.meta.url);
  try {
    const mastraCore = require('@mastra/core') as { createTool: CreateToolFn };
    return mastraCore.createTool;
  } catch (cause) {
    throw new Error(
      'beecon: toMastraTools needs "@mastra/core" installed (it is an optional peer ' +
        'dependency) — install it, or pass options.createTool explicitly.',
      { cause },
    );
  }
}
