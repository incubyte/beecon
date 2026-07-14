import { describe, expect, it, vi } from 'vitest';
import { HttpClient } from '../src/http.js';
import { EventsResource } from '../src/resources/events.js';
import { asFetch, jsonResponse } from './support/responses.js';

function buildResource(fetchMock: ReturnType<typeof vi.fn>): EventsResource {
  const http = new HttpClient({
    apiKey: 'beecon_sk_test_key',
    baseUrl: 'https://api.example.com',
    fetchImpl: asFetch(fetchMock),
  });
  return new EventsResource(http);
}

describe('events.list', () => {
  it('GETs /api/v1/events with type, deliveryStatus, cursor, and limit as query params', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [] }));
    const events = buildResource(fetchMock);

    await events.list({ type: 'trigger.event', deliveryStatus: 'FAILED', cursor: 'cur_1', limit: 25 });

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(url);
    expect(parsed.pathname).toBe('/api/v1/events');
    expect(init.method).toBe('GET');
    expect(parsed.searchParams.get('type')).toBe('trigger.event');
    expect(parsed.searchParams.get('deliveryStatus')).toBe('FAILED');
    expect(parsed.searchParams.get('cursor')).toBe('cur_1');
    expect(parsed.searchParams.get('limit')).toBe('25');
  });

  it('omits type, deliveryStatus, cursor, and limit from the query string when not provided', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [] }));
    const events = buildResource(fetchMock);

    await events.list();

    const [url] = fetchMock.mock.calls[0];
    expect([...new URL(url).searchParams.keys()]).toEqual([]);
  });

  it('returns the page of outbox events plus nextCursor, without exposing the event body', async () => {
    const event = {
      id: 'evt_1',
      type: 'trigger.event',
      createdAt: '2026-01-01T00:00:00Z',
      deliveryStatus: 'DELIVERED',
      attempts: 1,
      lastAttemptAt: '2026-01-01T00:00:01Z',
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [event], nextCursor: 'cur_2' }));
    const events = buildResource(fetchMock);

    const page = await events.list();

    expect(page.items).toEqual([event]);
    expect(page.nextCursor).toBe('cur_2');
    expect(Object.keys(page.items[0])).not.toContain('data');
  });
});

describe('events.redeliver', () => {
  it('POSTs to /api/v1/events/{id}/redeliver with no body', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 202 }));
    const events = buildResource(fetchMock);

    await events.redeliver('evt_1');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/events/evt_1/redeliver');
    expect(init.method).toBe('POST');
    expect(init.body).toBeUndefined();
  });

  it('URL-encodes the event id', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 202 }));
    const events = buildResource(fetchMock);

    await events.redeliver('evt/weird id');

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/events/evt%2Fweird%20id/redeliver');
  });
});
