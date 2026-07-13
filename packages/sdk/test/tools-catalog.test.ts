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

const catalogTool = {
  slug: 'hubspot-list-contacts',
  name: 'List contacts',
  description: 'Lists contacts in the CRM.',
  inputSchema: { type: 'object', properties: { pageSize: { type: 'number' } } },
  outputSchema: { type: 'object', properties: { results: { type: 'array' } } },
  deprecated: false,
  provider: { slug: 'hubspot', name: 'Hubspot', logo: 'https://x/hubspot.png' },
};

describe('tools.list', () => {
  it('sends integrationId, providerSlug, cursor, and limit as query params', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [catalogTool] }));
    const tools = buildResource(fetchMock);

    await tools.list({
      integrationId: 'int_1',
      providerSlug: 'hubspot',
      cursor: 'cur_1',
      limit: 20,
    });

    const [url, init] = fetchMock.mock.calls[0];
    const parsed = new URL(url);
    expect(parsed.pathname).toBe('/api/v1/tools');
    expect(init.method).toBe('GET');
    expect(parsed.searchParams.get('integrationId')).toBe('int_1');
    expect(parsed.searchParams.get('providerSlug')).toBe('hubspot');
    expect(parsed.searchParams.get('cursor')).toBe('cur_1');
    expect(parsed.searchParams.get('limit')).toBe('20');
  });

  it('sends includeDeprecated=true as a query param when requested', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [] }));
    const tools = buildResource(fetchMock);

    await tools.list({ providerSlug: 'hubspot', includeDeprecated: true });

    const [url] = fetchMock.mock.calls[0];
    expect(new URL(url).searchParams.get('includeDeprecated')).toBe('true');
  });

  it('sends includeDeprecated=false as an explicit query param rather than omitting it', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [] }));
    const tools = buildResource(fetchMock);

    await tools.list({ providerSlug: 'hubspot', includeDeprecated: false });

    const [url] = fetchMock.mock.calls[0];
    expect(new URL(url).searchParams.get('includeDeprecated')).toBe('false');
  });

  it('omits includeDeprecated, cursor, and limit from the query string when not provided', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [] }));
    const tools = buildResource(fetchMock);

    await tools.list({ providerSlug: 'hubspot' });

    const [url] = fetchMock.mock.calls[0];
    expect(url).not.toContain('undefined');
    const keys = [...new URL(url).searchParams.keys()];
    expect(keys).not.toContain('includeDeprecated');
    expect(keys).not.toContain('cursor');
    expect(keys).not.toContain('limit');
  });

  it('defaults to no filters at all when called without arguments', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [] }));
    const tools = buildResource(fetchMock);

    await tools.list();

    const [url] = fetchMock.mock.calls[0];
    expect([...new URL(url).searchParams.keys()]).toEqual([]);
  });

  it('returns typed tools carrying input and output JSON Schemas', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [catalogTool] }));
    const tools = buildResource(fetchMock);

    const page = await tools.list({ providerSlug: 'hubspot' });

    expect(page.items[0].inputSchema).toEqual(catalogTool.inputSchema);
    expect(page.items[0].outputSchema).toEqual(catalogTool.outputSchema);
    expect(page.items[0].provider).toEqual(catalogTool.provider);
  });

  it('propagates nextCursor from the response envelope for pagination', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [catalogTool], nextCursor: 'cur_2' }));
    const tools = buildResource(fetchMock);

    const page = await tools.list({ providerSlug: 'hubspot' });

    expect(page.nextCursor).toBe('cur_2');
  });

  it('leaves nextCursor undefined when the response carries no further page', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [catalogTool] }));
    const tools = buildResource(fetchMock);

    const page = await tools.list({ providerSlug: 'hubspot' });

    expect(page.nextCursor).toBeUndefined();
  });
});

describe('tools.get', () => {
  it('GETs the tool by slug', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(catalogTool));
    const tools = buildResource(fetchMock);

    await tools.get('hubspot-list-contacts');

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/tools/hubspot-list-contacts',
      expect.objectContaining({ method: 'GET' }),
    );
  });

  it('returns the tool detail typed with schemas and deprecation flag', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(catalogTool));
    const tools = buildResource(fetchMock);

    const detail = await tools.get('hubspot-list-contacts');

    expect(detail).toEqual(catalogTool);
    expect(detail.deprecated).toBe(false);
  });

  it('URL-encodes special characters in the slug', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(catalogTool));
    const tools = buildResource(fetchMock);

    await tools.get('weird/slug name');

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/tools/weird%2Fslug%20name');
  });
});
