import { describe, expect, it, vi } from 'vitest';
import { HttpClient } from '../src/http.js';
import { ConnectionsResource } from '../src/resources/connections.js';
import { asFetch, jsonResponse, noContentResponse } from './support/responses.js';

function buildResource(fetchMock: ReturnType<typeof vi.fn>): ConnectionsResource {
  const http = new HttpClient({
    apiKey: 'beecon_sk_test_key',
    baseUrl: 'https://api.example.com',
    fetchImpl: asFetch(fetchMock),
  });
  return new ConnectionsResource(http);
}

describe('connections.initiate', () => {
  it('POSTs userId, integrationId, and redirectUri and returns id/status/redirectUrl', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({ id: 'conn_1', status: 'INITIATED', redirectUrl: 'https://beecon.example.com/connect/tok' }, 201),
    );
    const connections = buildResource(fetchMock);

    const result = await connections.initiate({
      userId: 'user_1',
      integrationId: 'int_1',
      redirectUri: 'https://app.example.com/connected',
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/connections/initiate');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({
      userId: 'user_1',
      integrationId: 'int_1',
      redirectUri: 'https://app.example.com/connected',
    });
    expect(result).toEqual({
      id: 'conn_1',
      status: 'INITIATED',
      redirectUrl: 'https://beecon.example.com/connect/tok',
    });
  });
});

describe('connections.get', () => {
  it('GETs the connection by id and returns status plus account metadata', async () => {
    const wireConnection = {
      id: 'conn_1',
      status: 'ACTIVE',
      providerSlug: 'outlook',
      userId: 'user_1',
      createdAt: '2026-01-01T00:00:00Z',
      account: { email: 'ada@example.com', displayName: 'Ada Lovelace' },
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(wireConnection));
    const connections = buildResource(fetchMock);

    const result = await connections.get('conn_1');

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/connections/conn_1',
      expect.objectContaining({ method: 'GET' }),
    );
    expect(result.status).toBe('ACTIVE');
    expect(result.account).toEqual({ email: 'ada@example.com', displayName: 'Ada Lovelace' });
  });

  it('never carries token fields on the returned connection object', async () => {
    const wireConnection = {
      id: 'conn_1',
      status: 'ACTIVE',
      providerSlug: 'outlook',
      userId: 'user_1',
      createdAt: '2026-01-01T00:00:00Z',
      account: { email: 'ada@example.com', displayName: 'Ada Lovelace' },
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(wireConnection));
    const connections = buildResource(fetchMock);

    const result = await connections.get('conn_1');

    const keys = Object.keys(result);
    for (const forbidden of ['accessToken', 'refreshToken', 'token', 'access_token', 'refresh_token']) {
      expect(keys).not.toContain(forbidden);
    }
    expect(keys.sort()).toEqual(
      ['account', 'createdAt', 'id', 'providerSlug', 'status', 'userId'].sort(),
    );
  });

  it('URL-encodes the connection id', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        id: 'conn_1',
        status: 'ACTIVE',
        providerSlug: 'outlook',
        userId: 'user_1',
        createdAt: '2026-01-01T00:00:00Z',
      }),
    );
    const connections = buildResource(fetchMock);

    await connections.get('conn/weird id');

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/connections/conn%2Fweird%20id');
  });
});

describe('connections.list', () => {
  it('GETs /api/v1/connections with userId, cursor, and limit as query params', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [] }));
    const connections = buildResource(fetchMock);

    await connections.list({ userId: 'user_1', cursor: 'cur_1', limit: 20 });

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(url);
    expect(parsed.pathname).toBe('/api/v1/connections');
    expect(init.method).toBe('GET');
    expect(parsed.searchParams.get('userId')).toBe('user_1');
    expect(parsed.searchParams.get('cursor')).toBe('cur_1');
    expect(parsed.searchParams.get('limit')).toBe('20');
  });

  it('omits userId, cursor, and limit from the query string when not provided', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [] }));
    const connections = buildResource(fetchMock);

    await connections.list();

    const [url] = fetchMock.mock.calls[0];
    expect(url).not.toContain('undefined');
    expect([...new URL(url).searchParams.keys()]).toEqual([]);
  });

  it('returns the page of connections plus nextCursor for pagination', async () => {
    const wireConnection = {
      id: 'conn_1',
      status: 'ACTIVE',
      providerSlug: 'outlook',
      userId: 'user_1',
      createdAt: '2026-01-01T00:00:00Z',
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [wireConnection], nextCursor: 'cur_2' }));
    const connections = buildResource(fetchMock);

    const page = await connections.list({ userId: 'user_1' });

    expect(page.items).toEqual([wireConnection]);
    expect(page.nextCursor).toBe('cur_2');
  });
});

describe('connections.disable', () => {
  it('POSTs to /api/v1/connections/{id}/disable and returns the new status', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ id: 'conn_1', status: 'DISCONNECTED' }));
    const connections = buildResource(fetchMock);

    const result = await connections.disable('conn_1');

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/connections/conn_1/disable',
      expect.objectContaining({ method: 'POST' }),
    );
    expect(result).toEqual({ id: 'conn_1', status: 'DISCONNECTED' });
  });
});

describe('connections.delete', () => {
  it('sends a DELETE request to /api/v1/connections/{id}', async () => {
    const fetchMock = vi.fn().mockResolvedValue(noContentResponse());
    const connections = buildResource(fetchMock);

    await connections.delete('conn_1');

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/connections/conn_1',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });
});

describe('connections.reconnect', () => {
  it('POSTs redirectUri to /api/v1/connections/{id}/reconnect', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({ id: 'conn_1', status: 'INITIATED', redirectUrl: 'https://beecon.example.com/connect/tok2' }),
    );
    const connections = buildResource(fetchMock);

    await connections.reconnect('conn_1', { redirectUri: 'https://app.example.com/connected' });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/connections/conn_1/reconnect');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({ redirectUri: 'https://app.example.com/connected' });
  });

  it('returns the same connection id back with a fresh redirectUrl', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({ id: 'conn_1', status: 'INITIATED', redirectUrl: 'https://beecon.example.com/connect/fresh-tok' }),
    );
    const connections = buildResource(fetchMock);

    const result = await connections.reconnect('conn_1', { redirectUri: 'https://app.example.com/connected' });

    expect(result.id).toBe('conn_1');
    expect(result.redirectUrl).toBe('https://beecon.example.com/connect/fresh-tok');
  });

  it('URL-encodes the connection id', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({ id: 'conn_1', status: 'INITIATED', redirectUrl: 'https://x' }),
    );
    const connections = buildResource(fetchMock);

    await connections.reconnect('conn/weird id', { redirectUri: 'https://app.example.com/connected' });

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/connections/conn%2Fweird%20id/reconnect');
  });
});
