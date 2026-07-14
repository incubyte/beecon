import { describe, expect, it, vi } from 'vitest';
import { HttpClient } from '../src/http.js';
import { TriggersResource } from '../src/resources/triggers.js';
import { asFetch, jsonResponse, noContentResponse } from './support/responses.js';

function buildResource(fetchMock: ReturnType<typeof vi.fn>): TriggersResource {
  const http = new HttpClient({
    apiKey: 'beecon_sk_test_key',
    baseUrl: 'https://api.example.com',
    fetchImpl: asFetch(fetchMock),
  });
  return new TriggersResource(http);
}

describe('triggers.listDefinitions', () => {
  it('GETs /api/v1/trigger-definitions with providerSlug, integrationId, cursor, and limit as query params', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [] }));
    const triggers = buildResource(fetchMock);

    await triggers.listDefinitions({
      providerSlug: 'outlook',
      integrationId: 'int_1',
      cursor: 'cur_1',
      limit: 10,
    });

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(url);
    expect(parsed.pathname).toBe('/api/v1/trigger-definitions');
    expect(init.method).toBe('GET');
    expect(parsed.searchParams.get('providerSlug')).toBe('outlook');
    expect(parsed.searchParams.get('integrationId')).toBe('int_1');
    expect(parsed.searchParams.get('cursor')).toBe('cur_1');
    expect(parsed.searchParams.get('limit')).toBe('10');
  });

  it('omits providerSlug, integrationId, cursor, and limit from the query string when not provided', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [] }));
    const triggers = buildResource(fetchMock);

    await triggers.listDefinitions();

    const [url] = fetchMock.mock.calls[0];
    expect([...new URL(url).searchParams.keys()]).toEqual([]);
  });

  it('returns the page of trigger definitions plus nextCursor', async () => {
    const definition = {
      slug: 'outlook-message-received',
      name: 'New message received',
      description: 'Fires when a new message arrives',
      configSchema: { type: 'object' },
      payloadSchema: { type: 'object' },
      ingestion: 'poll',
      provider: { slug: 'outlook', name: 'Outlook', logo: 'https://x/outlook.png' },
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse({ items: [definition], nextCursor: 'cur_2' }));
    const triggers = buildResource(fetchMock);

    const page = await triggers.listDefinitions({ providerSlug: 'outlook' });

    expect(page.items).toEqual([definition]);
    expect(page.nextCursor).toBe('cur_2');
  });
});

describe('triggers.getDefinition', () => {
  it('GETs /api/v1/trigger-definitions/{slug} and returns the config and payload schemas', async () => {
    const definition = {
      slug: 'hubspot-contact-created',
      name: 'Contact created',
      description: 'Fires when a contact is created',
      configSchema: { type: 'object' },
      payloadSchema: { type: 'object', properties: { id: { type: 'string' } } },
      ingestion: 'poll',
      provider: { slug: 'hubspot', name: 'HubSpot', logo: 'https://x/hubspot.png' },
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(definition));
    const triggers = buildResource(fetchMock);

    const result = await triggers.getDefinition('hubspot-contact-created');

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/trigger-definitions/hubspot-contact-created',
      expect.objectContaining({ method: 'GET' }),
    );
    expect(result).toEqual(definition);
  });

  it('URL-encodes the slug', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        slug: 'x',
        name: 'x',
        description: 'x',
        configSchema: {},
        payloadSchema: {},
        ingestion: 'poll',
        provider: { slug: 'x', name: 'x', logo: 'x' },
      }),
    );
    const triggers = buildResource(fetchMock);

    await triggers.getDefinition('weird slug/x');

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/trigger-definitions/weird%20slug%2Fx');
  });
});

describe('triggers.create', () => {
  it('POSTs connectionId and config, mapping the input slug onto the wire field triggerSlug', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ id: 'trg_1', status: 'ACTIVE' }, 201));
    const triggers = buildResource(fetchMock);

    const result = await triggers.create({
      connectionId: 'conn_1',
      slug: 'outlook-message-received',
      config: { folderId: 'Inbox' },
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/trigger-instances');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({
      connectionId: 'conn_1',
      triggerSlug: 'outlook-message-received',
      config: { folderId: 'Inbox' },
    });
    expect(result).toEqual({ id: 'trg_1', status: 'ACTIVE' });
  });
});

describe('triggers.list', () => {
  it('GETs /api/v1/trigger-instances with connectionId, userId, cursor, and limit as query params', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [] }));
    const triggers = buildResource(fetchMock);

    await triggers.list({ connectionId: 'conn_1', userId: 'user_1', cursor: 'cur_1', limit: 5 });

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(url);
    expect(parsed.pathname).toBe('/api/v1/trigger-instances');
    expect(init.method).toBe('GET');
    expect(parsed.searchParams.get('connectionId')).toBe('conn_1');
    expect(parsed.searchParams.get('userId')).toBe('user_1');
    expect(parsed.searchParams.get('cursor')).toBe('cur_1');
    expect(parsed.searchParams.get('limit')).toBe('5');
  });

  it('returns the page of trigger instances plus nextCursor', async () => {
    const instance = {
      id: 'trg_1',
      status: 'ACTIVE',
      connectionId: 'conn_1',
      triggerSlug: 'outlook-message-received',
      config: { folderId: 'Inbox' },
      userId: 'user_1',
      createdAt: '2026-01-01T00:00:00Z',
    };
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse({ items: [instance], nextCursor: 'cur_2' }));
    const triggers = buildResource(fetchMock);

    const page = await triggers.list({ connectionId: 'conn_1' });

    expect(page.items).toEqual([instance]);
    expect(page.nextCursor).toBe('cur_2');
  });
});

describe('triggers.get', () => {
  it('GETs /api/v1/trigger-instances/{id}', async () => {
    const instance = {
      id: 'trg_1',
      status: 'ACTIVE',
      connectionId: 'conn_1',
      triggerSlug: 'outlook-message-received',
      config: {},
      userId: 'user_1',
      createdAt: '2026-01-01T00:00:00Z',
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(instance));
    const triggers = buildResource(fetchMock);

    const result = await triggers.get('trg_1');

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/trigger-instances/trg_1',
      expect.objectContaining({ method: 'GET' }),
    );
    expect(result).toEqual(instance);
  });
});

describe('triggers.enable', () => {
  it('POSTs to /api/v1/trigger-instances/{id}/enable and returns the new status', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ id: 'trg_1', status: 'ACTIVE' }));
    const triggers = buildResource(fetchMock);

    const result = await triggers.enable('trg_1');

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/trigger-instances/trg_1/enable',
      expect.objectContaining({ method: 'POST' }),
    );
    expect(result).toEqual({ id: 'trg_1', status: 'ACTIVE' });
  });
});

describe('triggers.disable', () => {
  it('POSTs to /api/v1/trigger-instances/{id}/disable and returns the new status', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ id: 'trg_1', status: 'DISABLED' }));
    const triggers = buildResource(fetchMock);

    const result = await triggers.disable('trg_1');

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/trigger-instances/trg_1/disable',
      expect.objectContaining({ method: 'POST' }),
    );
    expect(result).toEqual({ id: 'trg_1', status: 'DISABLED' });
  });
});

describe('triggers.delete', () => {
  it('sends a DELETE request to /api/v1/trigger-instances/{id}', async () => {
    const fetchMock = vi.fn().mockResolvedValue(noContentResponse());
    const triggers = buildResource(fetchMock);

    await triggers.delete('trg_1');

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/trigger-instances/trg_1',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });

  it('URL-encodes the instance id', async () => {
    const fetchMock = vi.fn().mockResolvedValue(noContentResponse());
    const triggers = buildResource(fetchMock);

    await triggers.delete('trg/weird id');

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/trigger-instances/trg%2Fweird%20id');
  });
});
