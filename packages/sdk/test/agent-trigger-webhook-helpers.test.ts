import { createHmac } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { describe, expect, it, vi } from 'vitest';
import { onWebhookEvent, triggersForConnection, type WebhookEventHandlers } from '../src/agent.js';
import { WebhookVerificationError } from '../src/errors.js';
import type { BeeconClient, CreatedTriggerInstance, TriggerEventData, ConnectionExpiredEventData } from '../src/types.js';

// sign/headersFor/makeSecret independently reproduce the Standard Webhooks
// (PD27) construction the shared verifier checks — see the fuller comment in
// test/webhook-verify.test.ts. Reimplementing them here (rather than
// importing production internals) keeps these assertions non-circular: they
// exercise onWebhookEvent's use of the real webhooks.verify, not a mock of
// it.
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

function makeSecret(seed: string): string {
  return `whsec_${Buffer.from(seed, 'utf8').toString('base64')}`;
}

function spyHandlers(): WebhookEventHandlers {
  return {
    'trigger.event': vi.fn(),
    'connection.expired': vi.fn(),
    'webhook.test': vi.fn(),
  };
}

// Only triggers.create is exercised by triggersForConnection; the rest of
// BeeconClient is stubbed to satisfy the interface without being called
// (same double-building convention as test/openai-tool-adapter.test.ts).
function buildBeeconClient(createMock: ReturnType<typeof vi.fn>): BeeconClient {
  return {
    users: { create: vi.fn() },
    integrations: { list: vi.fn(), getExpectedParams: vi.fn() },
    connections: {
      initiate: vi.fn(),
      get: vi.fn(),
      list: vi.fn(),
      disable: vi.fn(),
      delete: vi.fn(),
      reconnect: vi.fn(),
    },
    tools: { list: vi.fn(), get: vi.fn(), execute: vi.fn() },
    logs: { list: vi.fn() },
    userTokens: { create: vi.fn() },
    files: { upload: vi.fn() },
    triggers: {
      listDefinitions: vi.fn(),
      getDefinition: vi.fn(),
      create: createMock,
      list: vi.fn(),
      get: vi.fn(),
      enable: vi.fn(),
      disable: vi.fn(),
      delete: vi.fn(),
    },
    webhookEndpoint: { set: vi.fn(), get: vi.fn(), rotateSecret: vi.fn(), sendTest: vi.fn() },
    events: { list: vi.fn(), redeliver: vi.fn() },
    webhooks: { verify: vi.fn() },
  };
}

// --- triggersForConnection --------------------------------------------------

describe('triggersForConnection — binds a connectionId once', () => {
  it('forwards create({slug, config}) to beecon.triggers.create with the bound connectionId merged in', async () => {
    const created: CreatedTriggerInstance = { id: 'trg_1', status: 'ACTIVE' };
    const createMock = vi.fn().mockResolvedValue(created);
    const beecon = buildBeeconClient(createMock);

    const scoped = triggersForConnection(beecon, 'conn_1');
    const result = await scoped.create({ slug: 'outlook-message-received', config: { folderId: 'Inbox' } });

    expect(createMock).toHaveBeenCalledWith({
      connectionId: 'conn_1',
      slug: 'outlook-message-received',
      config: { folderId: 'Inbox' },
    });
    expect(result).toEqual(created);
  });

  it('reuses the same bound connectionId across multiple create calls without the caller repeating it', async () => {
    const createMock = vi.fn().mockResolvedValue({ id: 'trg_2', status: 'ACTIVE' });
    const beecon = buildBeeconClient(createMock);
    const scoped = triggersForConnection(beecon, 'conn_42');

    await scoped.create({ slug: 'a-trigger', config: {} });
    await scoped.create({ slug: 'b-trigger', config: { x: 1 } });

    expect(createMock).toHaveBeenNthCalledWith(1, { connectionId: 'conn_42', slug: 'a-trigger', config: {} });
    expect(createMock).toHaveBeenNthCalledWith(2, { connectionId: 'conn_42', slug: 'b-trigger', config: { x: 1 } });
  });
});

// --- onWebhookEvent — verification failure ---------------------------------

describe('onWebhookEvent — verification failure', () => {
  const secret = makeSecret('agent-verify-failure-secret-material');
  const id = 'evt_agent_verify_fail_001';
  const timestamp = 1700000000;
  const now = new Date(timestamp * 1000);
  const payload = JSON.stringify({ id, type: 'webhook.test', createdAt: now.toISOString(), data: {} });

  it('throws WebhookVerificationError and calls no handler when the signature does not match', () => {
    const wrongSecret = makeSecret('a-completely-different-secret');
    const headers = headersFor(id, timestamp, sign(id, timestamp, payload, wrongSecret));
    const handlers = spyHandlers();

    expect(() => onWebhookEvent({ payload, headers, secret, now }, handlers)).toThrow(WebhookVerificationError);
    expect(handlers['trigger.event']).not.toHaveBeenCalled();
    expect(handlers['connection.expired']).not.toHaveBeenCalled();
    expect(handlers['webhook.test']).not.toHaveBeenCalled();
  });

  it('throws WebhookVerificationError and calls no handler when webhook-timestamp is outside the tolerance window', () => {
    const headers = headersFor(id, timestamp, sign(id, timestamp, payload, secret));
    const farFuture = new Date((timestamp + 301) * 1000);
    const handlers = spyHandlers();

    expect(() => onWebhookEvent({ payload, headers, secret, now: farFuture }, handlers)).toThrow(
      WebhookVerificationError,
    );
    expect(handlers['trigger.event']).not.toHaveBeenCalled();
    expect(handlers['connection.expired']).not.toHaveBeenCalled();
    expect(handlers['webhook.test']).not.toHaveBeenCalled();
  });

  it('throws WebhookVerificationError and calls no handler when a required signature header is missing', () => {
    const headers = { 'webhook-timestamp': String(timestamp), 'webhook-signature': sign(id, timestamp, payload, secret) };
    const handlers = spyHandlers();

    expect(() => onWebhookEvent({ payload, headers, secret, now }, handlers)).toThrow(WebhookVerificationError);
    expect(handlers['trigger.event']).not.toHaveBeenCalled();
    expect(handlers['connection.expired']).not.toHaveBeenCalled();
    expect(handlers['webhook.test']).not.toHaveBeenCalled();
  });
});

// --- onWebhookEvent — typed dispatch ----------------------------------------

describe('onWebhookEvent — typed dispatch', () => {
  const secret = makeSecret('agent-dispatch-secret-material');
  const timestamp = 1700000000;
  const now = new Date(timestamp * 1000);

  function deliver(body: { id: string } & Record<string, unknown>): { payload: string; headers: Record<string, string> } {
    const payload = JSON.stringify(body);
    const headers = headersFor(body.id, timestamp, sign(body.id, timestamp, payload, secret));
    return { payload, headers };
  }

  it('invokes only the trigger.event handler, with its typed TriggerEventData, when a trigger.event is delivered', () => {
    const data: TriggerEventData = {
      triggerInstanceId: 'trg_1',
      triggerSlug: 'outlook-message-received',
      connectionId: 'conn_1',
      userId: 'user_1',
      payload: { subject: 'hi' },
    };
    const { payload, headers } = deliver({
      id: 'evt_trigger_dispatch_1',
      type: 'trigger.event',
      createdAt: now.toISOString(),
      data,
    });
    const handlers = spyHandlers();

    onWebhookEvent({ payload, headers, secret, now }, handlers);

    expect(handlers['trigger.event']).toHaveBeenCalledTimes(1);
    expect(handlers['trigger.event']).toHaveBeenCalledWith(data);
    expect(handlers['connection.expired']).not.toHaveBeenCalled();
    expect(handlers['webhook.test']).not.toHaveBeenCalled();
  });

  it('invokes only the connection.expired handler, with its typed ConnectionExpiredEventData, when a connection.expired event is delivered', () => {
    const data: ConnectionExpiredEventData = {
      connectionId: 'conn_1',
      userId: 'user_1',
      integrationId: 'int_1',
      providerSlug: 'outlook',
      reason: 'invalid_grant',
    };
    const { payload, headers } = deliver({
      id: 'evt_expired_dispatch_1',
      type: 'connection.expired',
      createdAt: now.toISOString(),
      data,
    });
    const handlers = spyHandlers();

    onWebhookEvent({ payload, headers, secret, now }, handlers);

    expect(handlers['connection.expired']).toHaveBeenCalledTimes(1);
    expect(handlers['connection.expired']).toHaveBeenCalledWith(data);
    expect(handlers['trigger.event']).not.toHaveBeenCalled();
    expect(handlers['webhook.test']).not.toHaveBeenCalled();
  });

  it('invokes only the webhook.test handler, with its empty typed data, when a webhook.test event is delivered', () => {
    const { payload, headers } = deliver({ id: 'evt_test_dispatch_1', type: 'webhook.test', createdAt: now.toISOString(), data: {} });
    const handlers = spyHandlers();

    onWebhookEvent({ payload, headers, secret, now }, handlers);

    expect(handlers['webhook.test']).toHaveBeenCalledTimes(1);
    expect(handlers['webhook.test']).toHaveBeenCalledWith({});
    expect(handlers['trigger.event']).not.toHaveBeenCalled();
    expect(handlers['connection.expired']).not.toHaveBeenCalled();
  });
});

// --- onWebhookEvent — idempotency id exposed --------------------------------

describe('onWebhookEvent — idempotency id exposed for caller-side dedupe', () => {
  it('returns the full verified WebhookEvent whose id matches the delivered event id', () => {
    const secret = makeSecret('agent-idempotency-secret-material');
    const timestamp = 1700000000;
    const now = new Date(timestamp * 1000);
    const body = {
      id: 'evt_dedupe_check_001',
      type: 'webhook.test' as const,
      createdAt: now.toISOString(),
      data: {},
    };
    const payload = JSON.stringify(body);
    const headers = headersFor(body.id, timestamp, sign(body.id, timestamp, payload, secret));

    const event = onWebhookEvent({ payload, headers, secret, now }, spyHandlers());

    expect(event.id).toBe('evt_dedupe_check_001');
  });
});

// --- onWebhookEvent — no second verifier (golden-vector interop reuse) -----
//
// GOLDEN_* below is the exact vector committed in server/internal/delivery
// /signing_test.go and reused verbatim in test/webhook-verify.test.ts. It was
// produced by the real Go signer. Running it through onWebhookEvent (rather
// than webhooks.verify directly) proves the agent helper delegates to the
// same verification, not a second reimplementation.
const GOLDEN_EVENT_ID = 'evt_golden00000000000000001';
const GOLDEN_TIMESTAMP = 1700000000;
const GOLDEN_BODY = '{"test":"data"}';
const GOLDEN_SECRET = 'whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw6XYz1Q9F8mI=';
const GOLDEN_SIGNATURE = 'v1,VTFU5mQgmJpE/NnJF4dLKTpfyU53iqJXnn77YvZ9QDw=';

describe('onWebhookEvent — reuses webhooks.verify exactly (no second verifier)', () => {
  it('accepts the exact committed Go-signed golden delivery that webhooks.verify itself accepts', () => {
    const now = new Date(GOLDEN_TIMESTAMP * 1000);
    const headers = headersFor(GOLDEN_EVENT_ID, GOLDEN_TIMESTAMP, GOLDEN_SIGNATURE);

    expect(() =>
      onWebhookEvent({ payload: GOLDEN_BODY, headers, secret: GOLDEN_SECRET, now }, spyHandlers()),
    ).not.toThrow();
  });

  it('rejects the same golden signature replayed against a differently-formatted body, exactly as webhooks.verify does', () => {
    const now = new Date(GOLDEN_TIMESTAMP * 1000);
    const headers = headersFor(GOLDEN_EVENT_ID, GOLDEN_TIMESTAMP, GOLDEN_SIGNATURE);
    const differentlyFormatted = '{ "test": "data" }';

    let caught: unknown;
    try {
      onWebhookEvent({ payload: differentlyFormatted, headers, secret: GOLDEN_SECRET, now }, spyHandlers());
    } catch (err) {
      caught = err;
    }

    expect(caught).toBeInstanceOf(WebhookVerificationError);
    expect((caught as WebhookVerificationError).reason).toBe('tampered');
  });

  it('source-level: agent.ts delegates to ./webhooks.js verify and defines no signature/HMAC logic of its own', () => {
    const agentSourcePath = resolve(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'agent.ts');
    const source = readFileSync(agentSourcePath, 'utf8');

    expect(source).toMatch(/import\s*\{\s*verify\s*\}\s*from\s*['"]\.\/webhooks\.js['"]/);
    expect(source).not.toMatch(/createHmac|timingSafeEqual|node:crypto/);
  });
});
