import { createHmac } from 'node:crypto';
import { describe, expect, it } from 'vitest';
import { WebhookVerificationError } from '../src/errors.js';
import { verify } from '../src/webhooks.js';
import type { ConnectionExpiredEvent, TriggerEvent, VerifyWebhookHeaders, WebhookEvent } from '../src/types.js';

// sign/headersFor independently reproduce the Standard Webhooks (PD27)
// construction webhooks.verify is documented to check (see the comment
// above signedContent in ../src/webhooks.ts): strip "whsec_", base64-decode
// the remainder into the raw HMAC key, sign "{id}.{timestamp}.{raw body}"
// with HMAC-SHA256, base64-encode, prefix "v1,". This helper never imports
// the production verify/signing internals — it recomputes the construction
// from scratch, mirroring the pattern in test/user-token-minting.test.ts, so
// the assertions below aren't circular against the code they're checking.
function sign(id: string, timestampSeconds: number, payload: string, secret: string): string {
  const key = Buffer.from(secret.replace(/^whsec_/, ''), 'base64');
  const content = `${id}.${timestampSeconds}.${payload}`;
  const digest = createHmac('sha256', key).update(content, 'utf8').digest('base64');
  return `v1,${digest}`;
}

function headersFor(id: string, timestampSeconds: number, signature: string): Record<string, string> {
  return {
    'webhook-id': id,
    'webhook-timestamp': String(timestampSeconds),
    'webhook-signature': signature,
  };
}

// makeSecret builds a syntactically valid whsec_ secret (a base64-decodable
// value after the prefix) from a readable seed, so every test below can name
// its own secret without hand-encoding base64.
function makeSecret(seed: string): string {
  return `whsec_${Buffer.from(seed, 'utf8').toString('base64')}`;
}

function captureError(fn: () => unknown): WebhookVerificationError {
  try {
    fn();
  } catch (err) {
    return err as WebhookVerificationError;
  }
  throw new Error('expected verify() to throw, but it returned successfully');
}

// --- Golden-delivery interop (section 4 of the architecture doc) ---------
//
// This exact vector is committed in server/internal/delivery/signing_test.go
// (goldenEventID/goldenTimestampUnix/goldenBody/goldenSecret/goldenSignature)
// and was produced by the real Go signer, cross-checked externally in both
// Node and Go. It is embedded verbatim here — a shared misreading of the
// Standard Webhooks construction (wrong key derivation, wrong content
// ordering, wrong header names) cannot pass both suites at once.
const GOLDEN_EVENT_ID = 'evt_golden00000000000000001';
const GOLDEN_TIMESTAMP = 1700000000;
const GOLDEN_BODY = '{"test":"data"}';
const GOLDEN_SECRET = 'whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw6XYz1Q9F8mI=';
const GOLDEN_SIGNATURE = 'v1,VTFU5mQgmJpE/NnJF4dLKTpfyU53iqJXnn77YvZ9QDw=';

describe('webhooks.verify — golden-delivery interop (Go signer -> TS verifier)', () => {
  it('accepts the exact committed delivery the Go signer produced and returns its parsed body', () => {
    const now = new Date(GOLDEN_TIMESTAMP * 1000); // within the +/-5min tolerance of the vector's own timestamp
    const headers = headersFor(GOLDEN_EVENT_ID, GOLDEN_TIMESTAMP, GOLDEN_SIGNATURE);

    // The golden body ({"test":"data"}) is a raw signing fixture, not a
    // PD32 envelope with an `id`/`type`/`data` shape — verify() does not
    // validate envelope structure, only the signature and timestamp, and
    // hands back JSON.parse(payload) as-is. So the meaningful assertion
    // here is "the signature is accepted and the exact body round-trips",
    // not "the returned union member is one of trigger.event/etc."
    const result = verify({ payload: GOLDEN_BODY, headers, secret: GOLDEN_SECRET, now });

    expect(result).toEqual({ test: 'data' });
  });

  it('accepts the same golden delivery when headers are given as a WHATWG Headers instance', () => {
    const now = new Date(GOLDEN_TIMESTAMP * 1000);
    const headers = new Headers();
    headers.set('webhook-id', GOLDEN_EVENT_ID);
    headers.set('webhook-timestamp', String(GOLDEN_TIMESTAMP));
    headers.set('webhook-signature', GOLDEN_SIGNATURE);

    const result = verify({ payload: GOLDEN_BODY, headers, secret: GOLDEN_SECRET, now });

    expect(result).toEqual({ test: 'data' });
  });

  it('rejects the golden signature against a differently-formatted (but semantically equal) body', () => {
    const now = new Date(GOLDEN_TIMESTAMP * 1000);
    const headers = headersFor(GOLDEN_EVENT_ID, GOLDEN_TIMESTAMP, GOLDEN_SIGNATURE);

    // Guards the exact byte-for-byte requirement the quickstart calls out
    // (a body-parser that re-serializes JSON breaks verification): the
    // signature was computed over GOLDEN_BODY's exact bytes, so even a
    // whitespace-only reformatting of the same JSON value must fail.
    const differentlyFormatted = '{ "test": "data" }';
    const error = captureError(() =>
      verify({ payload: differentlyFormatted, headers, secret: GOLDEN_SECRET, now }),
    );

    expect(error).toBeInstanceOf(WebhookVerificationError);
    expect(error.reason).toBe('tampered');
  });
});

// --- Tamper / malformed-header / timestamp matrix -------------------------

const MATRIX_SECRET = makeSecret('matrix-fixture-secret-material');
const MATRIX_ID = 'evt_matrix00000000000000001';
const MATRIX_TIMESTAMP = 1700000000;
const MATRIX_NOW = new Date(MATRIX_TIMESTAMP * 1000);
const MATRIX_PAYLOAD = JSON.stringify({
  id: MATRIX_ID,
  type: 'webhook.test',
  createdAt: MATRIX_NOW.toISOString(),
  data: {},
});
const MATRIX_SIGNATURE = sign(MATRIX_ID, MATRIX_TIMESTAMP, MATRIX_PAYLOAD, MATRIX_SECRET);

describe('webhooks.verify — tamper, malformed-header, and timestamp matrix', () => {
  it('throws malformed-header when the webhook-id header is missing', () => {
    const headers: VerifyWebhookHeaders = {
      'webhook-timestamp': String(MATRIX_TIMESTAMP),
      'webhook-signature': MATRIX_SIGNATURE,
    };

    const error = captureError(() =>
      verify({ payload: MATRIX_PAYLOAD, headers, secret: MATRIX_SECRET, now: MATRIX_NOW }),
    );

    expect(error).toBeInstanceOf(WebhookVerificationError);
    expect(error.reason).toBe('malformed-header');
  });

  it('throws malformed-header when the webhook-timestamp header is missing', () => {
    const headers: VerifyWebhookHeaders = {
      'webhook-id': MATRIX_ID,
      'webhook-signature': MATRIX_SIGNATURE,
    };

    const error = captureError(() =>
      verify({ payload: MATRIX_PAYLOAD, headers, secret: MATRIX_SECRET, now: MATRIX_NOW }),
    );

    expect(error).toBeInstanceOf(WebhookVerificationError);
    expect(error.reason).toBe('malformed-header');
  });

  it('throws malformed-header when the webhook-signature header is missing', () => {
    const headers: VerifyWebhookHeaders = {
      'webhook-id': MATRIX_ID,
      'webhook-timestamp': String(MATRIX_TIMESTAMP),
    };

    const error = captureError(() =>
      verify({ payload: MATRIX_PAYLOAD, headers, secret: MATRIX_SECRET, now: MATRIX_NOW }),
    );

    expect(error).toBeInstanceOf(WebhookVerificationError);
    expect(error.reason).toBe('malformed-header');
  });

  it('throws malformed-header when webhook-timestamp is not a unix-seconds integer', () => {
    const headers = headersFor(MATRIX_ID, MATRIX_TIMESTAMP, MATRIX_SIGNATURE);
    headers['webhook-timestamp'] = 'not-a-number';

    const error = captureError(() =>
      verify({ payload: MATRIX_PAYLOAD, headers, secret: MATRIX_SECRET, now: MATRIX_NOW }),
    );

    expect(error).toBeInstanceOf(WebhookVerificationError);
    expect(error.reason).toBe('malformed-header');
  });

  it('throws signature when webhook-signature carries no recognized "v1," entry', () => {
    const headers = headersFor(MATRIX_ID, MATRIX_TIMESTAMP, 'v0,not-a-real-scheme==');

    const error = captureError(() =>
      verify({ payload: MATRIX_PAYLOAD, headers, secret: MATRIX_SECRET, now: MATRIX_NOW }),
    );

    expect(error).toBeInstanceOf(WebhookVerificationError);
    expect(error.reason).toBe('signature');
  });

  it('throws tampered when the payload was altered after signing', () => {
    const headers = headersFor(MATRIX_ID, MATRIX_TIMESTAMP, MATRIX_SIGNATURE);
    const tamperedPayload = JSON.stringify({
      id: MATRIX_ID,
      type: 'webhook.test',
      createdAt: MATRIX_NOW.toISOString(),
      data: { injected: true },
    });

    const error = captureError(() =>
      verify({ payload: tamperedPayload, headers, secret: MATRIX_SECRET, now: MATRIX_NOW }),
    );

    expect(error).toBeInstanceOf(WebhookVerificationError);
    expect(error.reason).toBe('tampered');
  });

  it('throws tampered when verified against the wrong secret', () => {
    const headers = headersFor(MATRIX_ID, MATRIX_TIMESTAMP, MATRIX_SIGNATURE);
    const wrongSecret = makeSecret('a-different-secret-entirely');

    const error = captureError(() =>
      verify({ payload: MATRIX_PAYLOAD, headers, secret: wrongSecret, now: MATRIX_NOW }),
    );

    expect(error).toBeInstanceOf(WebhookVerificationError);
    expect(error.reason).toBe('tampered');
  });

  it('throws timestamp when webhook-timestamp is more than 5 minutes in the past', () => {
    const headers = headersFor(MATRIX_ID, MATRIX_TIMESTAMP, MATRIX_SIGNATURE);
    const farFuture = new Date((MATRIX_TIMESTAMP + 301) * 1000); // timestamp is 301s in "the past" relative to now

    const error = captureError(() =>
      verify({ payload: MATRIX_PAYLOAD, headers, secret: MATRIX_SECRET, now: farFuture }),
    );

    expect(error).toBeInstanceOf(WebhookVerificationError);
    expect(error.reason).toBe('timestamp');
  });

  it('throws timestamp when webhook-timestamp is more than 5 minutes in the future', () => {
    const headers = headersFor(MATRIX_ID, MATRIX_TIMESTAMP, MATRIX_SIGNATURE);
    const farPast = new Date((MATRIX_TIMESTAMP - 301) * 1000); // timestamp is 301s in "the future" relative to now

    const error = captureError(() =>
      verify({ payload: MATRIX_PAYLOAD, headers, secret: MATRIX_SECRET, now: farPast }),
    );

    expect(error).toBeInstanceOf(WebhookVerificationError);
    expect(error.reason).toBe('timestamp');
  });

  it('accepts a delivery exactly at the 5-minute tolerance boundary', () => {
    const headers = headersFor(MATRIX_ID, MATRIX_TIMESTAMP, MATRIX_SIGNATURE);
    const atBoundary = new Date((MATRIX_TIMESTAMP + 300) * 1000);

    expect(() =>
      verify({ payload: MATRIX_PAYLOAD, headers, secret: MATRIX_SECRET, now: atBoundary }),
    ).not.toThrow();
  });
});

// --- Secret rotation overlap (PD31) ---------------------------------------

describe('webhooks.verify — secret rotation overlap (PD31)', () => {
  const oldSecret = makeSecret('rotation-old-secret-material');
  const newSecret = makeSecret('rotation-new-secret-material');
  const unrelatedSecret = makeSecret('rotation-unrelated-secret-material');
  const id = 'evt_rotation000000000000001';
  const timestamp = 1700000000;
  const now = new Date(timestamp * 1000);
  const payload = JSON.stringify({ id, type: 'webhook.test', createdAt: now.toISOString(), data: {} });

  it('accepts a delivery signed with the old secret when the consumer verifies with [old, new]', () => {
    const headers = headersFor(id, timestamp, sign(id, timestamp, payload, oldSecret));

    const result = verify({ payload, headers, secrets: [oldSecret, newSecret], now }) as { type: string };

    expect(result.type).toBe('webhook.test');
  });

  it('accepts a delivery signed with the new secret when the consumer verifies with [old, new]', () => {
    const headers = headersFor(id, timestamp, sign(id, timestamp, payload, newSecret));

    const result = verify({ payload, headers, secrets: [oldSecret, newSecret], now }) as { type: string };

    expect(result.type).toBe('webhook.test');
  });

  it('rejects a delivery signed with a secret outside the configured rotation set', () => {
    const headers = headersFor(id, timestamp, sign(id, timestamp, payload, unrelatedSecret));

    const error = captureError(() => verify({ payload, headers, secrets: [oldSecret, newSecret], now }));

    expect(error).toBeInstanceOf(WebhookVerificationError);
    expect(error.reason).toBe('tampered');
  });

  it('still accepts a single-secret consumer (no rotation in progress) via the `secret` field', () => {
    const headers = headersFor(id, timestamp, sign(id, timestamp, payload, oldSecret));

    expect(() => verify({ payload, headers, secret: oldSecret, now })).not.toThrow();
  });
});

// --- Typed event union (PD32) ---------------------------------------------

describe('webhooks.verify — typed event union (PD32)', () => {
  const secret = makeSecret('typed-union-fixture-secret-material');
  const timestamp = 1700000000;
  const now = new Date(timestamp * 1000);

  function deliver(body: { id: string } & Record<string, unknown>): WebhookEvent {
    const payload = JSON.stringify(body);
    const headers = headersFor(body.id, timestamp, sign(body.id, timestamp, payload, secret));
    return verify({ payload, headers, secret, now });
  }

  it('parses a trigger.event body into the TriggerEvent shape with its full data payload', () => {
    const event = deliver({
      id: 'evt_trigger001',
      type: 'trigger.event',
      createdAt: now.toISOString(),
      data: {
        triggerInstanceId: 'trg_1',
        triggerSlug: 'outlook-message-received',
        connectionId: 'conn_1',
        userId: 'user_1',
        payload: { subject: 'hi' },
      },
    });

    expect(event.type).toBe('trigger.event');
    const triggerEvent = event as TriggerEvent;
    expect(triggerEvent.data).toEqual({
      triggerInstanceId: 'trg_1',
      triggerSlug: 'outlook-message-received',
      connectionId: 'conn_1',
      userId: 'user_1',
      payload: { subject: 'hi' },
    });
  });

  it('parses a connection.expired body into the ConnectionExpiredEvent shape with its full data payload', () => {
    const event = deliver({
      id: 'evt_expired001',
      type: 'connection.expired',
      createdAt: now.toISOString(),
      data: {
        connectionId: 'conn_1',
        userId: 'user_1',
        integrationId: 'int_1',
        providerSlug: 'outlook',
        reason: 'invalid_grant',
      },
    });

    expect(event.type).toBe('connection.expired');
    const expiredEvent = event as ConnectionExpiredEvent;
    expect(expiredEvent.data).toEqual({
      connectionId: 'conn_1',
      userId: 'user_1',
      integrationId: 'int_1',
      providerSlug: 'outlook',
      reason: 'invalid_grant',
    });
  });

  it('parses a webhook.test body into the WebhookTestEvent shape with empty data', () => {
    const event = deliver({ id: 'evt_test001', type: 'webhook.test', createdAt: now.toISOString(), data: {} });

    expect(event.type).toBe('webhook.test');
    expect(event.data).toEqual({});
  });
});

// --- Secret never leaks through a verification failure --------------------

describe('webhooks.verify — the secret never appears in a verification failure', () => {
  const secret = makeSecret('never-log-this-secret-material');
  const id = 'evt_leak_check_001';
  const timestamp = 1700000000;
  const now = new Date(timestamp * 1000);
  const payload = JSON.stringify({ id, type: 'webhook.test', createdAt: now.toISOString(), data: {} });
  const signature = sign(id, timestamp, payload, secret);

  it('never includes the secret in a tampered-payload error (message, stack, JSON)', () => {
    const headers = headersFor(id, timestamp, signature);
    const tamperedPayload = JSON.stringify({
      id,
      type: 'webhook.test',
      createdAt: now.toISOString(),
      data: { extra: true },
    });

    const error = captureError(() => verify({ payload: tamperedPayload, headers, secret, now }));

    expect(error.message).not.toContain(secret);
    expect(error.stack ?? '').not.toContain(secret);
    expect(String(error)).not.toContain(secret);
    expect(JSON.stringify(error)).not.toContain(secret);
  });

  it('never includes either secret in a wrong-secret error', () => {
    const headers = headersFor(id, timestamp, signature);
    const wrongSecret = makeSecret('a-different-secret-material');

    const error = captureError(() => verify({ payload, headers, secret: wrongSecret, now }));

    expect(error.message).not.toContain(secret);
    expect(error.message).not.toContain(wrongSecret);
    expect(JSON.stringify(error)).not.toContain(secret);
    expect(JSON.stringify(error)).not.toContain(wrongSecret);
  });

  it('never includes the secret in a malformed-header error', () => {
    const headers: VerifyWebhookHeaders = { 'webhook-timestamp': String(timestamp), 'webhook-signature': signature };

    const error = captureError(() => verify({ payload, headers, secret, now }));

    expect(error.message).not.toContain(secret);
    expect(JSON.stringify(error)).not.toContain(secret);
  });

  it('never includes the secret in a timestamp-tolerance error', () => {
    const headers = headersFor(id, timestamp, signature);
    const wayLater = new Date((timestamp + 10_000) * 1000);

    const error = captureError(() => verify({ payload, headers, secret, now: wayLater }));

    expect(error.message).not.toContain(secret);
    expect(JSON.stringify(error)).not.toContain(secret);
  });
});
