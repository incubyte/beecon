import { describe, expect, it, vi } from 'vitest';
import { HttpClient } from '../src/http.js';
import { UsersResource } from '../src/resources/users.js';
import { asFetch, jsonResponse } from './support/responses.js';

function buildResource(fetchMock: ReturnType<typeof vi.fn>): UsersResource {
  const http = new HttpClient({
    apiKey: 'beecon_sk_test_key',
    baseUrl: 'https://api.example.com',
    fetchImpl: asFetch(fetchMock),
  });
  return new UsersResource(http);
}

describe('users.create', () => {
  it('POSTs the name and externalId to /api/v1/users with a bearer auth header', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(
        { id: 'user_abc', name: 'Ada Lovelace', externalId: 'app-user-42', createdAt: '2026-01-01T00:00:00Z' },
        201,
      ),
    );
    const users = buildResource(fetchMock);

    await users.create({ name: 'Ada Lovelace', externalId: 'app-user-42' });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/users');
    expect(init.method).toBe('POST');
    expect(init.headers.Authorization).toBe('Bearer beecon_sk_test_key');
    expect(JSON.parse(init.body)).toEqual({ name: 'Ada Lovelace', externalId: 'app-user-42' });
  });

  it('returns the created user id from the response body', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(
        { id: 'user_abc', name: 'Ada Lovelace', externalId: '', createdAt: '2026-01-01T00:00:00Z' },
        201,
      ),
    );
    const users = buildResource(fetchMock);

    const user = await users.create({ name: 'Ada Lovelace' });

    expect(user.id).toBe('user_abc');
  });

  it('omits externalId from the request body when not provided', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({ id: 'user_abc', name: 'Ada Lovelace', externalId: '', createdAt: '2026-01-01T00:00:00Z' }, 201),
    );
    const users = buildResource(fetchMock);

    await users.create({ name: 'Ada Lovelace' });

    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body)).toEqual({ name: 'Ada Lovelace' });
  });
});
