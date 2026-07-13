import { describe, expect, it, vi } from 'vitest';
import { BeeconApiError, RateLimitedError } from '../src/errors.js';
import { HttpClient } from '../src/http.js';
import { asFetch, jsonResponse, noContentResponse, rawResponse } from './support/responses.js';

function rateLimitedResponse(retryAfterHeader?: string): Response {
  const body = JSON.stringify({ error: { code: 'rate_limited', message: 'upstream rate limit exhausted' } });
  const headers: Record<string, string> = { 'content-type': 'application/json' };
  if (retryAfterHeader !== undefined) {
    headers['Retry-After'] = retryAfterHeader;
  }
  return new Response(body, { status: 429, headers });
}

function buildClient(fetchMock: ReturnType<typeof vi.fn>): HttpClient {
  return new HttpClient({
    apiKey: 'beecon_sk_test_key',
    baseUrl: 'https://api.example.com',
    fetchImpl: asFetch(fetchMock),
  });
}

describe('platform HTTP error handling', () => {
  it('throws a BeeconApiError carrying status, code, and message from an error envelope', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse({ error: { code: 'not_found', message: 'connection not found' } }, 404));
    const http = buildClient(fetchMock);

    await expect(http.get('/api/v1/connections/conn_missing')).rejects.toMatchObject({
      status: 404,
      code: 'not_found',
      message: 'connection not found',
    });
  });

  it('throws a BeeconApiError that is an instance of BeeconApiError', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse({ error: { code: 'unauthorized', message: 'bad key' } }, 401));
    const http = buildClient(fetchMock);

    await expect(http.get('/api/v1/integrations')).rejects.toBeInstanceOf(BeeconApiError);
  });

  it('passes through the details field from the error envelope when present', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse(
        { error: { code: 'validation_failed', message: 'invalid input', details: { field: 'name' } } },
        422,
      ),
    );
    const http = buildClient(fetchMock);

    await expect(http.post('/api/v1/users', {})).rejects.toMatchObject({
      details: { field: 'name' },
    });
  });

  it('falls back to code "unknown_error" when the response body has no error envelope', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ oops: 'no envelope here' }, 500));
    const http = buildClient(fetchMock);

    await expect(http.get('/api/v1/integrations')).rejects.toMatchObject({
      status: 500,
      code: 'unknown_error',
    });
  });

  it('falls back to code "unknown_error" without crashing when the error body is malformed JSON', async () => {
    const fetchMock = vi.fn().mockResolvedValue(rawResponse('{not valid json', 500));
    const http = buildClient(fetchMock);

    await expect(http.get('/api/v1/integrations')).rejects.toMatchObject({
      status: 500,
      code: 'unknown_error',
    });
  });

  it('resolves a 204 No Content response to undefined', async () => {
    const fetchMock = vi.fn().mockResolvedValue(noContentResponse());
    const http = buildClient(fetchMock);

    await expect(http.post('/api/v1/connections/conn_1/disable')).resolves.toBeUndefined();
  });

  it('resolves to undefined without crashing when a 2xx response body is malformed JSON', async () => {
    const fetchMock = vi.fn().mockResolvedValue(rawResponse('{not valid json', 200));
    const http = buildClient(fetchMock);

    await expect(http.get('/api/v1/integrations')).resolves.toBeUndefined();
  });
});

describe('429 rate-limit responses', () => {
  it('throws a RateLimitedError (not a plain BeeconApiError) on HTTP 429', async () => {
    const fetchMock = vi.fn().mockResolvedValue(rateLimitedResponse('30'));
    const http = buildClient(fetchMock);

    await expect(http.get('/api/v1/tools/hubspot-list-contacts/execute')).rejects.toBeInstanceOf(
      RateLimitedError,
    );
  });

  it('is still catchable as a BeeconApiError, since RateLimitedError extends it', async () => {
    const fetchMock = vi.fn().mockResolvedValue(rateLimitedResponse('30'));
    const http = buildClient(fetchMock);

    await expect(http.get('/api/v1/tools/hubspot-list-contacts/execute')).rejects.toBeInstanceOf(
      BeeconApiError,
    );
  });

  it('parses retryAfter in whole seconds from the Retry-After header', async () => {
    const fetchMock = vi.fn().mockResolvedValue(rateLimitedResponse('42'));
    const http = buildClient(fetchMock);

    await expect(http.get('/api/v1/tools/hubspot-list-contacts/execute')).rejects.toMatchObject({
      retryAfter: 42,
      status: 429,
      code: 'rate_limited',
    });
  });

  it('defaults retryAfter to 0 when the Retry-After header is absent', async () => {
    const fetchMock = vi.fn().mockResolvedValue(rateLimitedResponse());
    const http = buildClient(fetchMock);

    await expect(http.get('/api/v1/tools/hubspot-list-contacts/execute')).rejects.toMatchObject({
      retryAfter: 0,
    });
  });

  it('defaults retryAfter to 0 when the Retry-After header is not a valid number', async () => {
    const fetchMock = vi.fn().mockResolvedValue(rateLimitedResponse('not-a-number'));
    const http = buildClient(fetchMock);

    await expect(http.get('/api/v1/tools/hubspot-list-contacts/execute')).rejects.toMatchObject({
      retryAfter: 0,
    });
  });

  it('defaults retryAfter to 0 rather than a negative number when the Retry-After header is negative', async () => {
    const fetchMock = vi.fn().mockResolvedValue(rateLimitedResponse('-5'));
    const http = buildClient(fetchMock);

    await expect(http.get('/api/v1/tools/hubspot-list-contacts/execute')).rejects.toMatchObject({
      retryAfter: 0,
    });
  });
});
