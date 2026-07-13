import { describe, expect, it, vi } from 'vitest';
import { HttpClient } from '../src/http.js';
import { ToolsResource } from '../src/resources/tools.js';
import { asFetch, jsonResponse } from './support/responses.js';

function buildResource(fetchMock: ReturnType<typeof vi.fn>): ToolsResource {
  const http = new HttpClient({
    apiKey: 'beecon_sk_test_key',
    baseUrl: 'https://api.example.com',
    fetchImpl: asFetch(fetchMock),
  });
  return new ToolsResource(http);
}

describe('tools.execute', () => {
  it('POSTs userId/connectionId/arguments to /api/v1/tools/{slug}/execute', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ successful: true, error: null, data: { messages: [] } }));
    const tools = buildResource(fetchMock);

    await tools.execute('outlook-list-messages', {
      userId: 'user_1',
      connectionId: 'conn_1',
      arguments: { top: 10 },
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/tools/outlook-list-messages/execute');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({
      userId: 'user_1',
      connectionId: 'conn_1',
      arguments: { top: 10 },
    });
  });

  it('returns a typed successful result with data on success', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse({ successful: true, error: null, data: { messages: [{ id: 'm1' }] } }));
    const tools = buildResource(fetchMock);

    const result = await tools.execute('outlook-list-messages', {
      userId: 'user_1',
      connectionId: 'conn_1',
      arguments: {},
    });

    expect(result).toEqual({ successful: true, error: null, data: { messages: [{ id: 'm1' }] } });
  });

  it('returns successful:false as a resolved value rather than throwing, on a 200 tool-level failure', async () => {
    const failureBody = {
      successful: false,
      error: { code: 'connection_not_active', message: 'Connection is not ACTIVE.' },
      data: null,
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(failureBody, 200));
    const tools = buildResource(fetchMock);

    const result = await tools.execute('outlook-list-messages', {
      userId: 'user_1',
      connectionId: 'conn_1',
      arguments: {},
    });

    expect(result).toEqual(failureBody);
  });

  it('carries nextCursor on the execution result for a paginated tool', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({ successful: true, error: null, data: { results: [] }, nextCursor: 'cur_2' }),
    );
    const tools = buildResource(fetchMock);

    const result = await tools.execute('hubspot-list-contacts', {
      userId: 'user_1',
      connectionId: 'conn_1',
      arguments: { pageSize: 50 },
    });

    expect(result.nextCursor).toBe('cur_2');
  });

  it('leaves nextCursor undefined for a non-paginated tool result', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ successful: true, error: null, data: {} }));
    const tools = buildResource(fetchMock);

    const result = await tools.execute('outlook-get-message', {
      userId: 'user_1',
      connectionId: 'conn_1',
      arguments: {},
    });

    expect(result.nextCursor).toBeUndefined();
  });

  it('URL-encodes special characters in the tool slug', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ successful: true, error: null, data: null }));
    const tools = buildResource(fetchMock);

    await tools.execute('weird/slug name', { userId: 'user_1', connectionId: 'conn_1', arguments: {} });

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/tools/weird%2Fslug%20name/execute');
  });
});
