// This file backs the Mastra sample in ../README.md. It is compiled by
// `npm run typecheck` (see ../tsconfig.examples.json) so the sample provably
// type-checks against this package's real, built types — it is never
// published (only "dist" ships, see package.json's "files").
//
// It imports from this package's own src/ rather than "@beecon/sdk/mastra"
// because the file lives inside the package it is demonstrating; a real
// consumer writes the subpath import shown in the README instead.
//
// It also does not import the `@mastra/core` package itself (an optional
// peer dependency this package's devDependencies deliberately omit). Instead
// it uses the same `options.createTool` seam `toMastraTools` exposes for
// callers who can't resolve the optional peer synchronously — a real
// consumer omits `createTool` entirely and lets `toMastraTools` lazily
// `require('@mastra/core')` for them.
import type { BeeconClient, Tool } from '../src/types.js';
import { toMastraTools, type CreateToolFn, type MastraCreateToolConfig } from '../src/mastra.js';

interface MastraAgentLike {
  tools: Record<string, unknown>;
}

const stubCreateTool: CreateToolFn = (config: MastraCreateToolConfig) => config;

export function registerBeeconToolsOnAgent(
  agent: MastraAgentLike,
  beecon: BeeconClient,
  catalog: Tool[],
  userId: string,
  connectionId: string,
  createTool: CreateToolFn = stubCreateTool,
): void {
  const mastraTools = toMastraTools(beecon, catalog, { userId, connectionId, createTool });

  catalog.forEach((tool, index) => {
    agent.tools[tool.slug] = mastraTools[index];
  });
}
