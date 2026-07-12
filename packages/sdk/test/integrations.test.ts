import { describe, expect, it, vi } from 'vitest';
import { HttpClient } from '../src/http.js';
import { IntegrationsResource } from '../src/resources/integrations.js';
import { asFetch, jsonResponse } from './support/responses.js';

describe('integrations.list', () => {
  it('GETs /api/v1/integrations with a bearer auth header and returns the array', async () => {
    const catalog = [
      { id: 'int_1', providerSlug: 'outlook', name: 'Outlook', logo: 'https://x/logo.png', authScheme: 'oauth2' },
    ];
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(catalog));
    const http = new HttpClient({
      apiKey: 'beecon_sk_test_key',
      baseUrl: 'https://api.example.com',
      fetchImpl: asFetch(fetchMock),
    });
    const integrations = new IntegrationsResource(http);

    const result = await integrations.list();

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/integrations',
      expect.objectContaining({
        method: 'GET',
        headers: expect.objectContaining({ Authorization: 'Bearer beecon_sk_test_key' }),
      }),
    );
    expect(result).toEqual(catalog);
  });
});
