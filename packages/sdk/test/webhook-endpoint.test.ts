import { describe, expect, it, vi } from 'vitest';
import { HttpClient } from '../src/http.js';
import { WebhookEndpointResource } from '../src/resources/webhookEndpoint.js';
import { asFetch, jsonResponse } from './support/responses.js';

function buildResource(fetchMock: ReturnType<typeof vi.fn>): WebhookEndpointResource {
  const http = new HttpClient({
    apiKey: 'beecon_sk_test_key',
    baseUrl: 'https://api.example.com',
    fetchImpl: asFetch(fetchMock),
  });
  return new WebhookEndpointResource(http);
}

describe('webhookEndpoint.set', () => {
  // API Shape: "PUT /api/v1/webhook-endpoint (org) { url } -> { id, url,
  // secret (creation only), createdAt }" — and the server only registers a
  // PUT route (server/internal/app/router.go: r.Put("/",
  // deliveryHandler.SetEndpoint)), no POST handler exists on this path.
  it('PUTs { url } to /api/v1/webhook-endpoint and returns the secret exactly once on creation', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        id: 'wep_1',
        url: 'https://consumer.example.com/hooks',
        secret: 'whsec_abc123',
        createdAt: '2026-01-01T00:00:00Z',
      }),
    );
    const endpoint = buildResource(fetchMock);

    const result = await endpoint.set({ url: 'https://consumer.example.com/hooks' });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/webhook-endpoint');
    expect(init.method).toBe('PUT');
    expect(JSON.parse(init.body)).toEqual({ url: 'https://consumer.example.com/hooks' });
    expect(result.secret).toBe('whsec_abc123');
  });
});

describe('webhookEndpoint.get', () => {
  it('GETs /api/v1/webhook-endpoint and returns only a secret prefix, never the full secret', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        id: 'wep_1',
        url: 'https://consumer.example.com/hooks',
        secretPrefix: 'whsec_ab',
        createdAt: '2026-01-01T00:00:00Z',
      }),
    );
    const endpoint = buildResource(fetchMock);

    const result = await endpoint.get();

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/webhook-endpoint',
      expect.objectContaining({ method: 'GET' }),
    );
    expect(result.secretPrefix).toBe('whsec_ab');
    expect(Object.keys(result)).not.toContain('secret');
  });
});

describe('webhookEndpoint.rotateSecret', () => {
  it('POSTs an overlapHours body to /api/v1/webhook-endpoint/rotate-secret and returns the new secret', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ secret: 'whsec_new123' }));
    const endpoint = buildResource(fetchMock);

    const result = await endpoint.rotateSecret({ overlapHours: 12 });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/webhook-endpoint/rotate-secret');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({ overlapHours: 12 });
    expect(result.secret).toBe('whsec_new123');
  });

  it('omits overlapHours from the body when not provided, leaving the server default', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ secret: 'whsec_new123' }));
    const endpoint = buildResource(fetchMock);

    await endpoint.rotateSecret();

    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body)).toEqual({});
  });
});

describe('webhookEndpoint.sendTest', () => {
  it('POSTs to /api/v1/webhook-endpoint/test with no body', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 202 }));
    const endpoint = buildResource(fetchMock);

    await endpoint.sendTest();

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.example.com/api/v1/webhook-endpoint/test');
    expect(init.method).toBe('POST');
    expect(init.body).toBeUndefined();
  });
});
