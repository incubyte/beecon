import { describe, expect, it, vi } from 'vitest';
import { HttpClient } from '../src/http.js';
import { ConnectionsResource } from '../src/resources/connections.js';
import { asFetch, jsonResponse } from './support/responses.js';

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
