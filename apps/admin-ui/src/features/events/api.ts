import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/lib/api-client";
import { useCursorPagination, type UseCursorPaginationResult } from "@/lib/cursor";
import { queryKeys } from "@/lib/query";
import type { DeliveryEvent, EventsPage, LogEntry, LogsPage } from "@/lib/api-types";

/** EventsFilters is the console's Events & Delivery filter bar shape
 * (Slice 3): both fields are sent server-side as delivery.ListEventsParams
 * filters (type/deliveryStatus). */
export interface EventsFilters {
  type: string;
  deliveryStatus: string;
}

export const emptyEventsFilters: EventsFilters = { type: "", deliveryStatus: "" };

/** useEvents lists the selected org's outbox events (AC2), cursor-paginated,
 * re-fetching whenever a filter changes. Disabled while no org is selected. */
export function useEvents(orgId: string | undefined, filters: EventsFilters): UseCursorPaginationResult<DeliveryEvent> {
  return useCursorPagination<DeliveryEvent>({
    queryKey: queryKeys.events.list(orgId ?? "", filters),
    fetchPage: (cursor) => fetchEventsPage(orgId as string, filters, cursor),
    enabled: Boolean(orgId),
  });
}

function fetchEventsPage(orgId: string, filters: EventsFilters, cursor: string | undefined): Promise<EventsPage> {
  const query = buildEventsQuery(filters, cursor);
  return apiClient.get<EventsPage>(`/organizations/${orgId}/events${query}`);
}

function buildEventsQuery(filters: EventsFilters, cursor: string | undefined): string {
  const params = new URLSearchParams();
  if (filters.type) params.set("type", filters.type);
  if (filters.deliveryStatus) params.set("deliveryStatus", filters.deliveryStatus);
  if (cursor) params.set("cursor", cursor);
  const query = params.toString();
  return query ? `?${query}` : "";
}

/**
 * useEventDeliveryAttempts sources one event's per-attempt history (attempt
 * number, response status, duration — AC2) via the org's logs Query
 * endpoint (architecture doc: "Admin UI consumes the existing redacted
 * Query" — logging's read surface is unchanged this slice, so there is no
 * dedicated per-event attempts endpoint). It fetches one page at the max
 * page size and filters client-side by eventId — the same pattern
 * Connections' status/integration filters already established (Slice 2)
 * for a dimension the backend doesn't filter on directly — every
 * KindWebhookDelivery entry for an event carries that event's own id and
 * its 1-indexed attempt number.
 */
export function useEventDeliveryAttempts(orgId: string | undefined, eventId: string | undefined) {
  return useQuery({
    queryKey: queryKeys.logs.attempts(orgId ?? "", eventId ?? ""),
    queryFn: () => fetchEventDeliveryAttempts(orgId as string, eventId as string),
    enabled: Boolean(orgId) && Boolean(eventId),
  });
}

async function fetchEventDeliveryAttempts(orgId: string, eventId: string): Promise<LogEntry[]> {
  const page = await apiClient.get<LogsPage>(`/organizations/${orgId}/logs?limit=200`);
  return page.entries.filter((entry) => entry.eventId === eventId).sort((a, b) => (a.attempt ?? 0) - (b.attempt ?? 0));
}

/** useRedeliverEvent re-queues a FAILED (or NO_ENDPOINT) event for another
 * attempt (AC3): 202 Accepted — the dispatcher loop performs the actual
 * delivery asynchronously, so this only invalidates the list and the
 * per-attempt history so a subsequent refetch picks up whatever the
 * background worker has processed by then. */
export function useRedeliverEvent(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (eventId: string) => apiClient.post<void>(`/organizations/${orgId}/events/${eventId}/redeliver`),
    onSuccess: (_result, eventId) => {
      // A prefix key (rather than one exact filters value) so every active
      // filter combination's cached page is invalidated, not just the
      // default/empty one.
      void queryClient.invalidateQueries({ queryKey: ["org", orgId, "events", "list"] });
      void queryClient.invalidateQueries({ queryKey: queryKeys.logs.attempts(orgId, eventId) });
    },
  });
}
