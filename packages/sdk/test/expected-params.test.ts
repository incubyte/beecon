import { describe, expect, it, vi } from 'vitest';
import { HttpClient } from '../src/http.js';
import { IntegrationsResource } from '../src/resources/integrations.js';
import { asFetch, jsonResponse } from './support/responses.js';

function buildResource(fetchMock: ReturnType<typeof vi.fn>): IntegrationsResource {
  const http = new HttpClient({
    apiKey: 'beecon_sk_test_key',
    baseUrl: 'https://api.example.com',
    fetchImpl: asFetch(fetchMock),
  });
  return new IntegrationsResource(http);
}

describe('integrations.getExpectedParams', () => {
  it('GETs /api/v1/integrations/{integrationId}/expected-params', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ providerName: 'Hubspot', fields: [] }));
    const integrations = buildResource(fetchMock);

    await integrations.getExpectedParams('int_1');

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/integrations/int_1/expected-params',
      expect.objectContaining({ method: 'GET' }),
    );
  });

  it('returns the provider name and each field typed with required/secret flags', async () => {
    const expectedParams = {
      providerName: 'Hubspot',
      fields: [
        {
          name: 'apiKey',
          displayName: 'API Key',
          description: 'Your private-app access token.',
          required: true,
          secret: true,
        },
        {
          name: 'subdomain',
          displayName: 'Subdomain',
          description: 'Your Hubspot account subdomain.',
          required: false,
          secret: false,
        },
      ],
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(expectedParams));
    const integrations = buildResource(fetchMock);

    const result = await integrations.getExpectedParams('int_1');

    expect(result).toEqual(expectedParams);
    expect(result.fields[0].required).toBe(true);
    expect(result.fields[0].secret).toBe(true);
    expect(result.fields[1].required).toBe(false);
    expect(result.fields[1].secret).toBe(false);
  });

  it('URL-encodes the integration id', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ providerName: 'Hubspot', fields: [] }));
    const integrations = buildResource(fetchMock);

    await integrations.getExpectedParams('int/weird id');

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/integrations/int%2Fweird%20id/expected-params');
  });
});
