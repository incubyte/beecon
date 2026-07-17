import { describe, expect, it, vi } from 'vitest';
import { BeeconApiError, RateLimitedError } from '../src/errors.js';
import { toOpenAITools, UnknownToolCallError } from '../src/openai.js';
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

describe('toOpenAITools — generated tool-def shape', () => {
  it('returns exactly the four flat Responses-API keys, with no nested function wrapper', () => {
    const beecon = buildBeeconClient(vi.fn());
    const { toolDefs } = toOpenAITools(beecon, buildTool(), { userId: 'user_1', connectionId: 'conn_1' });

    expect(Object.keys(toolDefs[0]).sort()).toEqual(['description', 'name', 'parameters', 'type'].sort());
    expect(toolDefs[0]).not.toHaveProperty('function');
  });

  it('sets type to "function"', () => {
    const beecon = buildBeeconClient(vi.fn());
    const { toolDefs } = toOpenAITools(beecon, buildTool(), { userId: 'user_1', connectionId: 'conn_1' });

    expect(toolDefs[0].type).toBe('function');
  });

  it("sets name to the tool's slug", () => {
    const beecon = buildBeeconClient(vi.fn());
    const tool = buildTool({ slug: 'hubspot-create-contact' });
    const { toolDefs } = toOpenAITools(beecon, tool, { userId: 'user_1', connectionId: 'conn_1' });

    expect(toolDefs[0].name).toBe('hubspot-create-contact');
  });

  it("sets description to the tool's description", () => {
    const beecon = buildBeeconClient(vi.fn());
    const tool = buildTool({ description: 'Create a contact in HubSpot.' });
    const { toolDefs } = toOpenAITools(beecon, tool, { userId: 'user_1', connectionId: 'conn_1' });

    expect(toolDefs[0].description).toBe('Create a contact in HubSpot.');
  });

  it('passes inputSchema through as the same object reference, not a re-derived copy', () => {
    const beecon = buildBeeconClient(vi.fn());
    const tool = buildTool();
    const { toolDefs } = toOpenAITools(beecon, tool, { userId: 'user_1', connectionId: 'conn_1' });

    expect(toolDefs[0].parameters).toBe(tool.inputSchema);
  });

  it('accepts a list of tools and returns one def per tool', () => {
    const beecon = buildBeeconClient(vi.fn());
    const tools = [buildTool({ slug: 'a' }), buildTool({ slug: 'b' })];
    const { toolDefs } = toOpenAITools(beecon, tools, { userId: 'user_1', connectionId: 'conn_1' });

    expect(toolDefs.map((def) => def.name)).toEqual(['a', 'b']);
  });
});

describe('toOpenAITools — deprecated-tool filtering', () => {
  it('excludes deprecated tools by default', () => {
    const beecon = buildBeeconClient(vi.fn());
    const tools = [buildTool({ slug: 'current' }), buildTool({ slug: 'old', deprecated: true })];
    const { toolDefs } = toOpenAITools(beecon, tools, { userId: 'user_1', connectionId: 'conn_1' });

    expect(toolDefs.map((def) => def.name)).toEqual(['current']);
  });

  it('includes deprecated tools when includeDeprecated is true', () => {
    const beecon = buildBeeconClient(vi.fn());
    const tools = [buildTool({ slug: 'current' }), buildTool({ slug: 'old', deprecated: true })];
    const { toolDefs } = toOpenAITools(beecon, tools, {
      userId: 'user_1',
      connectionId: 'conn_1',
      includeDeprecated: true,
    });

    expect(toolDefs.map((def) => def.name).sort()).toEqual(['current', 'old']);
  });
});

describe('runToolCall — scoping and argument forwarding', () => {
  it('calls beecon.tools.execute with the slug and the curried userId/connectionId plus only the call\'s own arguments', async () => {
    const executeMock = vi.fn().mockResolvedValue(successResult());
    const beecon = buildBeeconClient(executeMock);
    const { runToolCall } = toOpenAITools(beecon, buildTool({ slug: 'outlook-list-messages' }), {
      userId: 'user_1',
      connectionId: 'conn_1',
    });

    await runToolCall({ name: 'outlook-list-messages', arguments: { top: 5 } });

    expect(executeMock).toHaveBeenCalledWith('outlook-list-messages', {
      userId: 'user_1',
      connectionId: 'conn_1',
      arguments: { top: 5 },
    });
  });

  it('parses JSON-string arguments before forwarding them', async () => {
    const executeMock = vi.fn().mockResolvedValue(successResult());
    const beecon = buildBeeconClient(executeMock);
    const { runToolCall } = toOpenAITools(beecon, buildTool(), { userId: 'user_1', connectionId: 'conn_1' });

    await runToolCall({ name: 'outlook-list-messages', arguments: '{"top":10}' });

    expect(executeMock).toHaveBeenCalledWith('outlook-list-messages', {
      userId: 'user_1',
      connectionId: 'conn_1',
      arguments: { top: 10 },
    });
  });

  it('forwards a pre-parsed object argument unchanged', async () => {
    const executeMock = vi.fn().mockResolvedValue(successResult());
    const beecon = buildBeeconClient(executeMock);
    const { runToolCall } = toOpenAITools(beecon, buildTool(), { userId: 'user_1', connectionId: 'conn_1' });

    await runToolCall({ name: 'outlook-list-messages', arguments: { top: 10 } });

    expect(executeMock).toHaveBeenCalledWith('outlook-list-messages', {
      userId: 'user_1',
      connectionId: 'conn_1',
      arguments: { top: 10 },
    });
  });

  it('resolves with the exact result returned by beecon.tools.execute on success', async () => {
    const result = successResult();
    const executeMock = vi.fn().mockResolvedValue(result);
    const beecon = buildBeeconClient(executeMock);
    const { runToolCall } = toOpenAITools(beecon, buildTool(), { userId: 'user_1', connectionId: 'conn_1' });

    await expect(runToolCall({ name: 'outlook-list-messages', arguments: {} })).resolves.toBe(result);
  });
});

describe('runToolCall — provider-level vs platform-level failures (PD6/PD21)', () => {
  it('resolves (never throws) when beecon.tools.execute resolves a { successful: false, error } provider failure', async () => {
    const failure: ToolExecutionResult = {
      successful: false,
      error: { code: 'connection_not_active', message: 'Connection is not ACTIVE.' },
      data: null,
    };
    const executeMock = vi.fn().mockResolvedValue(failure);
    const beecon = buildBeeconClient(executeMock);
    const { runToolCall } = toOpenAITools(beecon, buildTool(), { userId: 'user_1', connectionId: 'conn_1' });

    await expect(runToolCall({ name: 'outlook-list-messages', arguments: {} })).resolves.toEqual(failure);
  });

  it('rejects with the same BeeconApiError instance when beecon.tools.execute rejects with one', async () => {
    const apiError = new BeeconApiError(401, { code: 'unauthorized', message: 'bad key' });
    const executeMock = vi.fn().mockRejectedValue(apiError);
    const beecon = buildBeeconClient(executeMock);
    const { runToolCall } = toOpenAITools(beecon, buildTool(), { userId: 'user_1', connectionId: 'conn_1' });

    await expect(runToolCall({ name: 'outlook-list-messages', arguments: {} })).rejects.toBe(apiError);
  });

  it('rejects with the same RateLimitedError instance when beecon.tools.execute rejects with one', async () => {
    const rateLimited = new RateLimitedError(30, { code: 'rate_limited', message: 'upstream rate limit exhausted' });
    const executeMock = vi.fn().mockRejectedValue(rateLimited);
    const beecon = buildBeeconClient(executeMock);
    const { runToolCall } = toOpenAITools(beecon, buildTool(), { userId: 'user_1', connectionId: 'conn_1' });

    await expect(runToolCall({ name: 'outlook-list-messages', arguments: {} })).rejects.toBe(rateLimited);
  });
});

describe('runToolCall — unknown tool-call name', () => {
  it('rejects with UnknownToolCallError (a rejected promise, not a sync throw) when the name is not in the built set', async () => {
    const executeMock = vi.fn();
    const beecon = buildBeeconClient(executeMock);
    const { runToolCall } = toOpenAITools(beecon, buildTool({ slug: 'outlook-list-messages' }), {
      userId: 'user_1',
      connectionId: 'conn_1',
    });

    const pending = runToolCall({ name: 'not-in-set', arguments: {} });

    await expect(pending).rejects.toBeInstanceOf(UnknownToolCallError);
    expect(executeMock).not.toHaveBeenCalled();
  });

  it('rejects with UnknownToolCallError for a tool that exists on the client catalog but was excluded from this build (built-set scoping, not global lookup)', async () => {
    const executeMock = vi.fn();
    const beecon = buildBeeconClient(executeMock);
    // Only "outlook-list-messages" is built into this adapter instance, even
    // though "outlook-get-message" might exist elsewhere in the catalog.
    const { runToolCall } = toOpenAITools(beecon, buildTool({ slug: 'outlook-list-messages' }), {
      userId: 'user_1',
      connectionId: 'conn_1',
    });

    await expect(runToolCall({ name: 'outlook-get-message', arguments: {} })).rejects.toBeInstanceOf(
      UnknownToolCallError,
    );
    expect(executeMock).not.toHaveBeenCalled();
  });

  it('rejects with UnknownToolCallError for a deprecated tool excluded by default, even if its slug is otherwise valid', async () => {
    const executeMock = vi.fn();
    const beecon = buildBeeconClient(executeMock);
    const tools = [buildTool({ slug: 'current' }), buildTool({ slug: 'old', deprecated: true })];
    const { runToolCall } = toOpenAITools(beecon, tools, { userId: 'user_1', connectionId: 'conn_1' });

    await expect(runToolCall({ name: 'old', arguments: {} })).rejects.toBeInstanceOf(UnknownToolCallError);
  });
});

describe('toOpenAITools — non-mutation of input Tool objects', () => {
  it('does not mutate a single Tool passed in', () => {
    const beecon = buildBeeconClient(vi.fn());
    const tool = buildTool();
    const snapshot = structuredClone(tool);

    toOpenAITools(beecon, tool, { userId: 'user_1', connectionId: 'conn_1' });

    expect(tool).toEqual(snapshot);
  });

  it('does not mutate a list of Tools passed in', () => {
    const beecon = buildBeeconClient(vi.fn());
    const tools = [buildTool({ slug: 'a' }), buildTool({ slug: 'b', deprecated: true })];
    const snapshot = structuredClone(tools);

    toOpenAITools(beecon, tools, { userId: 'user_1', connectionId: 'conn_1', includeDeprecated: true });

    expect(tools).toEqual(snapshot);
  });
});
