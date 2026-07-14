/** KNOWN_EVENT_TYPES mirrors delivery.KnownEventTypes
 * (server/internal/delivery/types.go, PD45) — the fixed vocabulary an
 * endpoint's optional event-type filter is validated against. Shares its
 * three literal values with EventsPage's own EVENT_TYPES (Slice 3) rather
 * than importing a shared constant — the same "define locally" precedent
 * that file already established for this exact list. */
export const KNOWN_EVENT_TYPES = ["trigger.event", "connection.expired", "webhook.test"] as const;

export type KnownEventType = (typeof KNOWN_EVENT_TYPES)[number];

const EVENT_TYPE_LABELS: Record<KnownEventType, string> = {
  "trigger.event": "Trigger event",
  "connection.expired": "Connection expired",
  "webhook.test": "Webhook test",
};

export function eventTypeLabel(eventType: string): string {
  return EVENT_TYPE_LABELS[eventType as KnownEventType] ?? eventType;
}
