import { describe, expect, it, vi } from 'vitest';
import { HttpClient } from '../src/http.js';
import { LogsResource } from '../src/resources/logs.js';
import { asFetch, jsonResponse } from './support/responses.js';

function buildResource(fetchMock: ReturnType<typeof vi.fn>): LogsResource {
  const http = new HttpClient({
    apiKey: 'beecon_sk_test_key',
    baseUrl: 'https://api.example.com',
    fetchImpl: asFetch(fetchMock),
  });
  return new LogsResource(http);
}

describe('logs.list', () => {
  it('builds query params from every filter field', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ entries: [] }));
    const logs = buildResource(fetchMock);

    await logs.list({
      connectionId: 'conn_1',
      userId: 'user_1',
      toolSlug: 'outlook-list-messages',
      from: '2026-01-01T00:00:00Z',
      to: '2026-01-02T00:00:00Z',
      cursor: 'cur_1',
      limit: 50,
    });

    const [url] = fetchMock.mock.calls[0];
    const parsed = new URL(url);
    expect(parsed.pathname).toBe('/api/v1/logs');
    expect(parsed.searchParams.get('connectionId')).toBe('conn_1');
    expect(parsed.searchParams.get('userId')).toBe('user_1');
    expect(parsed.searchParams.get('toolSlug')).toBe('outlook-list-messages');
    expect(parsed.searchParams.get('from')).toBe('2026-01-01T00:00:00Z');
    expect(parsed.searchParams.get('to')).toBe('2026-01-02T00:00:00Z');
    expect(parsed.searchParams.get('cursor')).toBe('cur_1');
    expect(parsed.searchParams.get('limit')).toBe('50');
  });

  it('omits undefined filter values from the query string instead of the literal string "undefined"', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ entries: [] }));
    const logs = buildResource(fetchMock);

    await logs.list({ connectionId: 'conn_1' });

    const [url] = fetchMock.mock.calls[0];
    expect(url).not.toContain('undefined');
    const parsed = new URL(url);
    expect([...parsed.searchParams.keys()]).toEqual(['connectionId']);
  });

  it('defaults to no filters when called without arguments, still with no "undefined" leaking into the URL', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ entries: [] }));
    const logs = buildResource(fetchMock);

    await logs.list();

    const [url] = fetchMock.mock.calls[0];
    expect(url).not.toContain('undefined');
    const parsed = new URL(url);
    expect([...parsed.searchParams.keys()]).toEqual([]);
  });

  it('returns entries and nextCursor from the response', async () => {
    const page = {
      entries: [
        {
          id: 'log_1',
          organizationId: 'org_1',
          kind: 'tool_execution',
          status: 200,
          durationMs: 42,
          requestBody: '{}',
          responseBody: '{}',
          createdAt: '2026-01-01T00:00:00Z',
        },
      ],
      nextCursor: 'cur_2',
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(page));
    const logs = buildResource(fetchMock);

    const result = await logs.list();

    expect(result).toEqual(page);
  });
});
