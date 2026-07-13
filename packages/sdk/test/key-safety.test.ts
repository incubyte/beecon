import { inspect, format } from 'node:util';
import { describe, expect, it, vi } from 'vitest';
import { Beecon } from '../src/client.js';
import { MissingSigningSecretError } from '../src/errors.js';
import { HttpClient } from '../src/http.js';
import { UserTokensResource } from '../src/resources/userTokens.js';
import { asFetch, jsonResponse } from './support/responses.js';

// AC9 is high risk: a leaked API key is a full account compromise. These
// tests are adversarial — they try every common way a value can escape an
// object (serialization, inspection, console formatting, enumeration, and
// error surfaces) and assert the raw key shows up in none of them.
const SECRET_KEY = 'beecon_sk_do_not_leak_this_9f3a7c2e';

describe('API key never leaks off the Beecon client', () => {
  it('is absent from JSON.stringify(client), which surfaces only baseUrl', () => {
    const client = new Beecon({ apiKey: SECRET_KEY, baseUrl: 'https://api.example.com' });

    const serialized = JSON.stringify(client);

    expect(serialized).not.toContain(SECRET_KEY);
    expect(JSON.parse(serialized)).toEqual({ baseUrl: 'https://api.example.com' });
  });

  it('is absent from util.inspect(client)', () => {
    const client = new Beecon({ apiKey: SECRET_KEY, baseUrl: 'https://api.example.com' });

    expect(inspect(client)).not.toContain(SECRET_KEY);
  });

  it('is absent from console.log-style formatting of the client', () => {
    const client = new Beecon({ apiKey: SECRET_KEY, baseUrl: 'https://api.example.com' });
    const logSpy = vi.spyOn(console, 'log').mockImplementation(() => {});

    console.log(client);

    const printed = format(...(logSpy.mock.calls[0] as unknown[]));
    expect(printed).not.toContain(SECRET_KEY);
    logSpy.mockRestore();
  });

  it('is absent from Object.keys/Object.entries enumeration of the client', () => {
    const client = new Beecon({ apiKey: SECRET_KEY, baseUrl: 'https://api.example.com' });

    const keys = Object.keys(client);
    const entries = Object.entries(client);

    expect(keys).not.toContain('apiKey');
    expect(entries.some(([, value]) => value === SECRET_KEY)).toBe(false);
    for (const [, value] of entries) {
      expect(JSON.stringify(value)).not.toContain(SECRET_KEY);
    }
  });

  it('is absent from JSON.stringify, util.inspect, and enumeration of the underlying HttpClient', () => {
    const http = new HttpClient({ apiKey: SECRET_KEY, baseUrl: 'https://api.example.com' });

    expect(JSON.stringify(http)).not.toContain(SECRET_KEY);
    expect(inspect(http)).not.toContain(SECRET_KEY);
    expect(Object.keys(http)).toHaveLength(0);
  });

  it('is absent from a BeeconApiError thrown by a failed request: message, stack, string form, and JSON', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(jsonResponse({ error: { code: 'unauthorized', message: 'invalid API key' } }, 401));
    const http = new HttpClient({
      apiKey: SECRET_KEY,
      baseUrl: 'https://api.example.com',
      fetchImpl: asFetch(fetchMock),
    });

    let caught: unknown;
    try {
      await http.get('/api/v1/integrations');
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(Error);
    const error = caught as Error;
    expect(error.message).not.toContain(SECRET_KEY);
    expect(error.stack ?? '').not.toContain(SECRET_KEY);
    expect(String(error)).not.toContain(SECRET_KEY);
    expect(JSON.stringify(error)).not.toContain(SECRET_KEY);
  });

  it('sends the API key correctly as the Authorization header — the key reaches the wire, and only the wire', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse([]));
    const client = new Beecon({
      apiKey: SECRET_KEY,
      baseUrl: 'https://api.example.com',
      fetch: asFetch(fetchMock),
    });

    await client.integrations.list();

    const [, init] = fetchMock.mock.calls[0];
    expect(init.headers.Authorization).toBe(`Bearer ${SECRET_KEY}`);
  });
});

// AC9 also covers the user-token signing secret (PD20), which the SDK holds
// entirely client-side for local minting — same adversarial coverage as the
// API key above, on the Beecon client (constructed with signingSecret) and
// directly on UserTokensResource.
const SIGNING_SECRET = 'usertoken-signing-secret-do-not-leak-4b2e91';

describe('signing secret never leaks off the Beecon client or UserTokensResource', () => {
  it('is absent from JSON.stringify(client), which surfaces only baseUrl', () => {
    const client = new Beecon({
      apiKey: 'beecon_sk_test_key',
      baseUrl: 'https://api.example.com',
      signingSecret: { id: 'usk_1', secret: SIGNING_SECRET },
    });

    const serialized = JSON.stringify(client);

    expect(serialized).not.toContain(SIGNING_SECRET);
    expect(JSON.parse(serialized)).toEqual({ baseUrl: 'https://api.example.com' });
  });

  it('is absent from util.inspect(client)', () => {
    const client = new Beecon({
      apiKey: 'beecon_sk_test_key',
      baseUrl: 'https://api.example.com',
      signingSecret: { id: 'usk_1', secret: SIGNING_SECRET },
    });

    expect(inspect(client)).not.toContain(SIGNING_SECRET);
  });

  it('is absent from console.log-style formatting of the client', () => {
    const client = new Beecon({
      apiKey: 'beecon_sk_test_key',
      baseUrl: 'https://api.example.com',
      signingSecret: { id: 'usk_1', secret: SIGNING_SECRET },
    });
    const logSpy = vi.spyOn(console, 'log').mockImplementation(() => {});

    console.log(client);

    const printed = format(...(logSpy.mock.calls[0] as unknown[]));
    expect(printed).not.toContain(SIGNING_SECRET);
    logSpy.mockRestore();
  });

  it('is absent from Object.keys/Object.entries enumeration of the client', () => {
    const client = new Beecon({
      apiKey: 'beecon_sk_test_key',
      baseUrl: 'https://api.example.com',
      signingSecret: { id: 'usk_1', secret: SIGNING_SECRET },
    });

    const entries = Object.entries(client);
    for (const [, value] of entries) {
      expect(JSON.stringify(value)).not.toContain(SIGNING_SECRET);
    }
  });

  it('is absent from JSON.stringify, util.inspect, and enumeration of the underlying UserTokensResource', () => {
    const userTokens = new UserTokensResource({ id: 'usk_1', secret: SIGNING_SECRET });

    expect(JSON.stringify(userTokens)).not.toContain(SIGNING_SECRET);
    expect(inspect(userTokens)).not.toContain(SIGNING_SECRET);
    expect(Object.keys(userTokens)).toHaveLength(0);
  });

  it('is absent from a MissingSigningSecretError message, stack, string form, and JSON when unconfigured', () => {
    const userTokens = new UserTokensResource();

    let caught: unknown;
    try {
      userTokens.create({ userId: 'user_1' });
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(MissingSigningSecretError);
    const error = caught as Error;
    expect(error.message).not.toContain(SIGNING_SECRET);
    expect(error.stack ?? '').not.toContain(SIGNING_SECRET);
    expect(String(error)).not.toContain(SIGNING_SECRET);
    expect(JSON.stringify(error)).not.toContain(SIGNING_SECRET);
  });

  it('is never assigned to an enumerable property of a configured UserTokensResource, even though it must be used to mint', () => {
    const userTokens = new UserTokensResource({ id: 'usk_1', secret: SIGNING_SECRET });

    const result = userTokens.create({ userId: 'user_1' });

    // The secret is used to compute the HMAC signature, so the *token
    // itself* legitimately contains the signature bytes derived from it —
    // but never the raw secret string.
    expect(result.token).not.toContain(SIGNING_SECRET);
    expect(Object.keys(userTokens)).toHaveLength(0);
  });
});
