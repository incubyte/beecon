// @beecon/sdk/agent — typed helpers over the existing `triggers` sub-API and
// `webhooks.verify`, for consumers who need trigger instances and inbound
// webhook events rather than model-callable tools (Phase 5 decisions, Slice
// 5). Triggers and connection-lifecycle events are inbound — they don't map
// onto the LLM function-tool model the way actions do (see
// openai.ts/mastra.ts) — so this subpath is not another toXTools: it's thin
// typed convenience over what BeeconClient already exposes, plus a
// handler-map dispatch over the existing verifier. `verify` is imported from
// ../webhooks.js unchanged — no second Standard Webhooks implementation
// exists anywhere in this package (interop constraint, PD27).
import { verify } from './webhooks.js';
import type {
  BeeconClient,
  ConnectionExpiredEventData,
  CreatedTriggerInstance,
  TriggerEventData,
  VerifyWebhookInput,
  WebhookEvent,
  WebhookTestEventData,
} from './types.js';

// ScopedCreateTriggerInstanceInput is `triggers.create`'s input minus the
// connectionId, which `triggersForConnection` binds once instead of the
// caller repeating it on every call.
export interface ScopedCreateTriggerInstanceInput {
  slug: string;
  config: Record<string, unknown>;
}

export interface ScopedTriggers {
  create(input: ScopedCreateTriggerInstanceInput): Promise<CreatedTriggerInstance>;
}

// triggersForConnection binds one connectionId so a consumer creating
// several trigger instances for the same connection never repeats it. It is
// a thin curried wrapper over `beecon.triggers.create` — not a builder, since
// no AC calls for one (YAGNI).
export function triggersForConnection(beecon: BeeconClient, connectionId: string): ScopedTriggers {
  return {
    create(input: ScopedCreateTriggerInstanceInput): Promise<CreatedTriggerInstance> {
      return beecon.triggers.create({ connectionId, slug: input.slug, config: input.config });
    },
  };
}

// WebhookEventHandlers is the handler-map onWebhookEvent dispatches a
// verified WebhookEvent through: one handler per PD32 event type, each
// receiving that event's own typed `data` — so the caller never narrows the
// untyped union itself.
export interface WebhookEventHandlers {
  'trigger.event'(data: TriggerEventData): void;
  'connection.expired'(data: ConnectionExpiredEventData): void;
  'webhook.test'(data: WebhookTestEventData): void;
}

// onWebhookEvent verifies an incoming webhook delivery and routes it to the
// matching handler. Verification is delegated to `webhooks.verify` unchanged
// (no second verifier — PD27 interop preserved): a bad signature or a stale
// timestamp throws WebhookVerificationError before any handler runs, and is
// never swallowed or converted into a handled event. On success it returns
// the verified WebhookEvent — `id` included — so the caller can deduplicate
// on `event.id` itself; onWebhookEvent never dedupes on the caller's behalf.
export function onWebhookEvent(input: VerifyWebhookInput, handlers: WebhookEventHandlers): WebhookEvent {
  const event = verify(input);
  dispatchEvent(event, handlers);
  return event;
}

function dispatchEvent(event: WebhookEvent, handlers: WebhookEventHandlers): void {
  switch (event.type) {
    case 'trigger.event':
      handlers['trigger.event'](event.data);
      return;
    case 'connection.expired':
      handlers['connection.expired'](event.data);
      return;
    case 'webhook.test':
      handlers['webhook.test'](event.data);
      return;
  }
}
