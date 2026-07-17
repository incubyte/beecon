import { describe, expect, it, vi } from 'vitest';
import { BeeconApiError, RateLimitedError } from '../src/errors.js';
import { MastraToolExecutionError, toMastraTools } from '../src/mastra.js';
import type { MastraCreateToolConfig } from '../src/mastra.js';
import type { BeeconClient, Tool, ToolExecutionResult } from '../src/types.js';

function buildTool(overrides: Partial<Tool> = {}): Tool {
  return {
    slug: 'outlook-list-messages',
    name: 'List messages',
    description: 'List messages in a folder.',
    inputSchema: { type: 'object', properties: { top: { type: 'number' } }, required: [] },
    outputSchema: { type: 'object', properties: {} },
    deprecated: false,
    provider: { slug: 'outlook', name: 'Outlook', logo: 'https://example.com/outlook.png' },
    ...overrides,
  };
}

// Only tools.execute is exercised by this adapter; the rest of BeeconClient
// is stubbed to satisfy the interface without being called.
function buildBeeconClient(executeMock: ReturnType<typeof vi.fn>): BeeconClient {
  return {
    users: { create: vi.fn() },
    integrations: { list: vi.fn(), getExpectedParams: vi.fn() },
    connections: {
      initiate: vi.fn(),
      get: vi.fn(),
      list: vi.fn(),
      disable: vi.fn(),
      delete: vi.fn(),
      reconnect: vi.fn(),
    },
    tools: { list: vi.fn(), get: vi.fn(), execute: executeMock },
    logs: { list: vi.fn() },
    userTokens: { create: vi.fn() },
    files: { upload: vi.fn() },
    triggers: {
      listDefinitions: vi.fn(),
      getDefinition: vi.fn(),
      create: vi.fn(),
      list: vi.fn(),
      get: vi.fn(),
      enable: vi.fn(),
      disable: vi.fn(),
      delete: vi.fn(),
    },
    webhookEndpoint: { set: vi.fn(), get: vi.fn(), rotateSecret: vi.fn(), sendTest: vi.fn() },
    events: { list: vi.fn(), redeliver: vi.fn() },
    webhooks: { verify: vi.fn() },
  };
}

function successResult(): ToolExecutionResult {
  return { successful: true, error: null, data: { ok: true } };
}

// A fake createTool: returns a distinctive marker object holding the config
// it was invoked with, so tests can assert both that createTool was actually
// invoked and that toMastraTools' return value IS createTool's return value
// (proving real createTool construction, not a plain object standing in for
// one).
function fakeCreateTool() {
  return vi.fn((config: MastraCreateToolConfig) => ({
    __mastraTool: true,
    config,
  }));
}

describe('toMastraTools — real createTool construction', () => {
  it('invokes the injected createTool function', () => {
    const createTool = fakeCreateTool();
    const beecon = buildBeeconClient(vi.fn());

    toMastraTools(beecon, buildTool(), { userId: 'user_1', connectionId: 'conn_1', createTool });

    expect(createTool).toHaveBeenCalledTimes(1);
  });

  it("returns createTool's own return value, not a plain object standing in for it", () => {
    const createTool = fakeCreateTool();
    const beecon = buildBeeconClient(vi.fn());

    const tools = toMastraTools(beecon, buildTool(), { userId: 'user_1', connectionId: 'conn_1', createTool });

    expect(tools[0]).toBe(createTool.mock.results[0]?.value);
  });

  it('invokes createTool once per tool when given a list', () => {
    const createTool = fakeCreateTool();
    const beecon = buildBeeconClient(vi.fn());
    const tools = [buildTool({ slug: 'a' }), buildTool({ slug: 'b' })];

    toMastraTools(beecon, tools, { userId: 'user_1', connectionId: 'conn_1', createTool });

    expect(createTool).toHaveBeenCalledTimes(2);
  });
});

describe('toMastraTools — createTool config shape', () => {
  it("sets id to the tool's slug", () => {
    const createTool = fakeCreateTool();
    const beecon = buildBeeconClient(vi.fn());
    const tool = buildTool({ slug: 'hubspot-create-contact' });

    toMastraTools(beecon, tool, { userId: 'user_1', connectionId: 'conn_1', createTool });

    expect(createTool.mock.calls[0][0].id).toBe('hubspot-create-contact');
  });

  it("sets description to the tool's description", () => {
    const createTool = fakeCreateTool();
    const beecon = buildBeeconClient(vi.fn());
    const tool = buildTool({ description: 'Create a contact in HubSpot.' });

    toMastraTools(beecon, tool, { userId: 'user_1', connectionId: 'conn_1', createTool });

    expect(createTool.mock.calls[0][0].description).toBe('Create a contact in HubSpot.');
  });

  it('passes inputSchema through as the same object reference, not a re-derived copy', () => {
    const createTool = fakeCreateTool();
    const beecon = buildBeeconClient(vi.fn());
    const tool = buildTool();

    toMastraTools(beecon, tool, { userId: 'user_1', connectionId: 'conn_1', createTool });

    expect(createTool.mock.calls[0][0].inputSchema).toBe(tool.inputSchema);
  });
});

describe('toMastraTools — deprecated-tool filtering', () => {
  it('excludes deprecated tools by default', () => {
    const createTool = fakeCreateTool();
    const beecon = buildBeeconClient(vi.fn());
    const tools = [buildTool({ slug: 'current' }), buildTool({ slug: 'old', deprecated: true })];

    toMastraTools(beecon, tools, { userId: 'user_1', connectionId: 'conn_1', createTool });

    expect(createTool).toHaveBeenCalledTimes(1);
    expect(createTool.mock.calls[0][0].id).toBe('current');
  });

  it('includes deprecated tools when includeDeprecated is true', () => {
    const createTool = fakeCreateTool();
    const beecon = buildBeeconClient(vi.fn());
    const tools = [buildTool({ slug: 'current' }), buildTool({ slug: 'old', deprecated: true })];

    toMastraTools(beecon, tools, {
      userId: 'user_1',
      connectionId: 'conn_1',
      includeDeprecated: true,
      createTool,
    });

    expect(createTool).toHaveBeenCalledTimes(2);
    expect(createTool.mock.calls.map((call) => call[0].id).sort()).toEqual(['current', 'old']);
  });
});

describe("execute — scoping and context-as-arguments forwarding", () => {
  it("forwards exactly the execute input's context (not the whole input) as arguments, alongside the curried userId/connectionId", async () => {
    const executeMock = vi.fn().mockResolvedValue(successResult());
    const beecon = buildBeeconClient(executeMock);
    const createTool = fakeCreateTool();
    const tools = toMastraTools(beecon, buildTool({ slug: 'outlook-list-messages' }), {
      userId: 'user_1',
      connectionId: 'conn_1',
      createTool,
    }) as Array<{ config: { execute: (input: { context: Record<string, unknown> }) => Promise<unknown> } }>;

    await tools[0].config.execute({ context: { top: 5 } });

    expect(executeMock).toHaveBeenCalledWith('outlook-list-messages', {
      userId: 'user_1',
      connectionId: 'conn_1',
      arguments: { top: 5 },
    });
  });

  it('resolves with the Beecon result data on a successful execution', async () => {
    const result: ToolExecutionResult = { successful: true, error: null, data: { messages: [] } };
    const executeMock = vi.fn().mockResolvedValue(result);
    const beecon = buildBeeconClient(executeMock);
    const createTool = fakeCreateTool();
    const tools = toMastraTools(beecon, buildTool(), {
      userId: 'user_1',
      connectionId: 'conn_1',
      createTool,
    }) as Array<{ config: { execute: (input: { context: Record<string, unknown> }) => Promise<unknown> } }>;

    await expect(tools[0].config.execute({ context: {} })).resolves.toEqual({ messages: [] });
  });
});

describe('execute — provider-level vs platform-level failures (PD6/PD21)', () => {
  it('throws MastraToolExecutionError carrying the provider error message when beecon.tools.execute resolves { successful: false }', async () => {
    const failure: ToolExecutionResult = {
      successful: false,
      error: { code: 'connection_not_active', message: 'Connection is not ACTIVE.' },
      data: null,
    };
    const executeMock = vi.fn().mockResolvedValue(failure);
    const beecon = buildBeeconClient(executeMock);
    const createTool = fakeCreateTool();
    const tools = toMastraTools(beecon, buildTool({ slug: 'outlook-list-messages' }), {
      userId: 'user_1',
      connectionId: 'conn_1',
      createTool,
    }) as Array<{ config: { execute: (input: { context: Record<string, unknown> }) => Promise<unknown> } }>;

    const pending = tools[0].config.execute({ context: {} });

    await expect(pending).rejects.toBeInstanceOf(MastraToolExecutionError);
    await expect(pending).rejects.toThrow('Connection is not ACTIVE.');
  });

  it('carries the provider error code and tool slug on the thrown MastraToolExecutionError', async () => {
    const failure: ToolExecutionResult = {
      successful: false,
      error: { code: 'connection_not_active', message: 'Connection is not ACTIVE.' },
      data: null,
    };
    const executeMock = vi.fn().mockResolvedValue(failure);
    const beecon = buildBeeconClient(executeMock);
    const createTool = fakeCreateTool();
    const tools = toMastraTools(beecon, buildTool({ slug: 'outlook-list-messages' }), {
      userId: 'user_1',
      connectionId: 'conn_1',
      createTool,
    }) as Array<{ config: { execute: (input: { context: Record<string, unknown> }) => Promise<unknown> } }>;

    try {
      await tools[0].config.execute({ context: {} });
      expect.unreachable('expected execute to throw');
    } catch (err) {
      expect(err).toBeInstanceOf(MastraToolExecutionError);
      const mastraError = err as MastraToolExecutionError;
      expect(mastraError.toolSlug).toBe('outlook-list-messages');
      expect(mastraError.code).toBe('connection_not_active');
    }
  });

  it('rejects with the same BeeconApiError instance when beecon.tools.execute rejects with one', async () => {
    const apiError = new BeeconApiError(401, { code: 'unauthorized', message: 'bad key' });
    const executeMock = vi.fn().mockRejectedValue(apiError);
    const beecon = buildBeeconClient(executeMock);
    const createTool = fakeCreateTool();
    const tools = toMastraTools(beecon, buildTool(), {
      userId: 'user_1',
      connectionId: 'conn_1',
      createTool,
    }) as Array<{ config: { execute: (input: { context: Record<string, unknown> }) => Promise<unknown> } }>;

    await expect(tools[0].config.execute({ context: {} })).rejects.toBe(apiError);
  });

  it('rejects with the same RateLimitedError instance when beecon.tools.execute rejects with one', async () => {
    const rateLimited = new RateLimitedError(30, { code: 'rate_limited', message: 'upstream rate limit exhausted' });
    const executeMock = vi.fn().mockRejectedValue(rateLimited);
    const beecon = buildBeeconClient(executeMock);
    const createTool = fakeCreateTool();
    const tools = toMastraTools(beecon, buildTool(), {
      userId: 'user_1',
      connectionId: 'conn_1',
      createTool,
    }) as Array<{ config: { execute: (input: { context: Record<string, unknown> }) => Promise<unknown> } }>;

    await expect(tools[0].config.execute({ context: {} })).rejects.toBe(rateLimited);
  });
});

describe('toMastraTools — non-mutation of input Tool objects', () => {
  it('does not mutate a single Tool passed in', () => {
    const createTool = fakeCreateTool();
    const beecon = buildBeeconClient(vi.fn());
    const tool = buildTool();
    const snapshot = structuredClone(tool);

    toMastraTools(beecon, tool, { userId: 'user_1', connectionId: 'conn_1', createTool });

    expect(tool).toEqual(snapshot);
  });

  it('does not mutate a list of Tools passed in', () => {
    const createTool = fakeCreateTool();
    const beecon = buildBeeconClient(vi.fn());
    const tools = [buildTool({ slug: 'a' }), buildTool({ slug: 'b', deprecated: true })];
    const snapshot = structuredClone(tools);

    toMastraTools(beecon, tools, {
      userId: 'user_1',
      connectionId: 'conn_1',
      includeDeprecated: true,
      createTool,
    });

    expect(tools).toEqual(snapshot);
  });
});

describe('toMastraTools — peer-absent confinement', () => {
  it('throws a clear install-or-inject error when @mastra/core is genuinely absent and no createTool was injected', () => {
    const beecon = buildBeeconClient(vi.fn());

    // No `createTool` option supplied: this exercises the lazy
    // createRequire('@mastra/core') fallback. @mastra/core is not installed
    // in this workspace, so the require throws and toMastraTools must
    // surface a clear, actionable error rather than crashing obscurely or
    // (worse) succeeding silently.
    expect(() => toMastraTools(beecon, buildTool(), { userId: 'user_1', connectionId: 'conn_1' })).toThrow(
      /@mastra\/core/,
    );
  });

  it('never throws at module-import time when @mastra/core is absent (the lazy path only fails when actually invoked)', async () => {
    // Re-importing the module (already imported above) must not itself throw
    // even though @mastra/core is absent — proving the require is deferred
    // to call time, not hoisted to import time.
    await expect(import('../src/mastra.js')).resolves.toBeTruthy();
  });
});

describe('mastra subpath packaging', () => {
  it('exposes a ./mastra subpath export', async () => {
    const { readFileSync } = await import('node:fs');
    const { fileURLToPath } = await import('node:url');
    const { dirname, resolve } = await import('node:path');
    const packageDir = resolve(dirname(fileURLToPath(import.meta.url)), '..');
    const pkg = JSON.parse(readFileSync(resolve(packageDir, 'package.json'), 'utf8')) as {
      exports?: Record<string, unknown>;
      peerDependenciesMeta?: Record<string, { optional?: boolean }>;
      dependencies?: Record<string, string>;
    };

    expect(pkg.exports).toHaveProperty('./mastra');
  });

  it('declares @mastra/core as an optional peer dependency', async () => {
    const { readFileSync } = await import('node:fs');
    const { fileURLToPath } = await import('node:url');
    const { dirname, resolve } = await import('node:path');
    const packageDir = resolve(dirname(fileURLToPath(import.meta.url)), '..');
    const pkg = JSON.parse(readFileSync(resolve(packageDir, 'package.json'), 'utf8')) as {
      peerDependenciesMeta?: Record<string, { optional?: boolean }>;
    };

    expect(pkg.peerDependenciesMeta?.['@mastra/core']?.optional).toBe(true);
  });

  it('keeps dependencies empty even with the mastra adapter present', async () => {
    const { readFileSync } = await import('node:fs');
    const { fileURLToPath } = await import('node:url');
    const { dirname, resolve } = await import('node:path');
    const packageDir = resolve(dirname(fileURLToPath(import.meta.url)), '..');
    const pkg = JSON.parse(readFileSync(resolve(packageDir, 'package.json'), 'utf8')) as {
      dependencies?: Record<string, string>;
    };

    expect(pkg.dependencies ?? {}).toEqual({});
  });
});
