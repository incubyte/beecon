import { inspect, format } from 'node:util';
import { describe, expect, it, vi } from 'vitest';
import { Beecon } from '../src/client.js';
import { MissingSigningSecretError } from '../src/errors.js';
import { HttpClient } from '../src/http.js';
import { UserTokensResource } from '../src/resources/userTokens.js';
import { WebhookEndpointResource } from '../src/resources/webhookEndpoint.js';
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

// AC (Slice 6) extends the same guarantee to the webhook endpoint signing
// secret (whsec_, PD27/PD31): WebhookEndpointResource is stateless (no
// constructor argument at all — the secret only ever exists transiently in
// a resolved promise's value), so the adversarial surface here is narrower
// than the API key's, but the returned secret must still never bleed into
// the Beecon client's own serialized/inspected/enumerated surface, and must
// never become an enumerable property of the resource itself.
const WEBHOOK_SECRET = 'whsec_do_not_leak_this_rotation_secret_ax91';

describe('webhook signing secret never leaks off the Beecon client or WebhookEndpointResource', () => {
  it('is absent from JSON.stringify(client) immediately after webhookEndpoint.set() resolves with the secret', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        id: 'wep_1',
        url: 'https://consumer.example.com/hooks',
        secret: WEBHOOK_SECRET,
        createdAt: '2026-01-01T00:00:00Z',
      }),
    );
    const client = new Beecon({
      apiKey: 'beecon_sk_test_key',
      baseUrl: 'https://api.example.com',
      fetch: asFetch(fetchMock),
    });

    await client.webhookEndpoint.set({ url: 'https://consumer.example.com/hooks' });

    expect(JSON.stringify(client)).not.toContain(WEBHOOK_SECRET);
  });

  it('is absent from util.inspect(client) immediately after webhookEndpoint.rotateSecret() resolves with a new secret', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ secret: WEBHOOK_SECRET }));
    const client = new Beecon({
      apiKey: 'beecon_sk_test_key',
      baseUrl: 'https://api.example.com',
      fetch: asFetch(fetchMock),
    });

    await client.webhookEndpoint.rotateSecret();

    expect(inspect(client)).not.toContain(WEBHOOK_SECRET);
  });

  it('is never assigned to any property of WebhookEndpointResource, even though set() must return it', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        id: 'wep_1',
        url: 'https://consumer.example.com/hooks',
        secret: WEBHOOK_SECRET,
        createdAt: '2026-01-01T00:00:00Z',
      }),
    );
    const http = new HttpClient({
      apiKey: 'beecon_sk_test_key',
      baseUrl: 'https://api.example.com',
      fetchImpl: asFetch(fetchMock),
    });
    const endpoint = new WebhookEndpointResource(http);

    const result = await endpoint.set({ url: 'https://consumer.example.com/hooks' });

    // Unlike UserTokensResource (which holds a signing secret directly and
    // so needs a true #private field plus its own toJSON/inspect
    // overrides), WebhookEndpointResource legitimately holds an `http`
    // dependency field — the resource is stateless with respect to the
    // whsec_ secret itself, which only ever lives in this resolved
    // promise's value. So the guarantee under test is narrower and more
    // direct: the secret string never becomes a value reachable from the
    // resource's own enumerable properties, not that the resource has zero
    // properties.
    expect(result.secret).toBe(WEBHOOK_SECRET);
    for (const value of Object.values(endpoint)) {
      expect(JSON.stringify(value)).not.toContain(WEBHOOK_SECRET);
    }
    expect(JSON.stringify(endpoint)).not.toContain(WEBHOOK_SECRET);
    expect(inspect(endpoint)).not.toContain(WEBHOOK_SECRET);
  });
});
