import { QueryClient } from "@tanstack/react-query";

/** The app's single QueryClient (§2.4). Retries are off — an ApiError from
 * a bad or missing admin key should surface immediately, not after silent
 * background retries. */
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: false,
      refetchOnWindowFocus: false,
    },
  },
});

/**
 * queryKeys is the org-scoped key factory (§2.4): every org-bound key
 * embeds the selected organization's id under an `"org"` prefix, so
 * switching the top-bar org switcher changes the query key, which
 * re-fetches and re-scopes the view for free — an operator's own query
 * cache never returns another org's data for the same key shape.
 * Installation-wide keys (organizations itself) omit it. Status/integration
 * filtering (Slice 2, connections) is applied client-side over one fetched
 * list rather than embedded in the key, matching the read surface the
 * connections List endpoint actually exposes today (userId/cursor/limit
 * only) — no filter query params to key on.
 */
export const queryKeys = {
  organizations: {
    list: (cursor?: string) => ["organizations", "list", cursor ?? null] as const,
  },
  connections: {
    list: (orgId: string) => ["org", orgId, "connections", "list"] as const,
    detail: (orgId: string, connectionId: string) => ["org", orgId, "connections", "detail", connectionId] as const,
  },
  triggerInstances: {
    list: (orgId: string) => ["org", orgId, "trigger-instances", "list"] as const,
    detail: (orgId: string, instanceId: string) => ["org", orgId, "trigger-instances", "detail", instanceId] as const,
  },
  logs: {
    // Slice 3: connectionId/userId/toolSlug/from/to are server-side filters
    // (logging.QueryParams) embedded in the key so changing any of them
    // re-fetches, matching every other org-scoped list's own key shape.
    list: (orgId: string, filters: LogsFilterKey) => ["org", orgId, "logs", "list", filters] as const,
    attempts: (orgId: string, eventId: string) => ["org", orgId, "logs", "attempts", eventId] as const,
  },
  events: {
    list: (orgId: string, filters: EventsFilterKey) => ["org", orgId, "events", "list", filters] as const,
  },
  dashboard: {
    metrics: () => ["dashboard", "metrics"] as const,
  },
  users: {
    list: (orgId: string) => ["org", orgId, "users", "list"] as const,
  },
  apiKeys: {
    list: (orgId: string) => ["org", orgId, "api-keys", "list"] as const,
  },
  governance: {
    detail: (orgId: string) => ["org", orgId, "governance", "detail"] as const,
    catalog: (orgId: string) => ["org", orgId, "governance", "catalog"] as const,
  },
  retention: {
    detail: (orgId: string) => ["org", orgId, "retention", "detail"] as const,
  },
  webhookEndpoints: {
    list: (orgId: string) => ["org", orgId, "webhook-endpoints", "list"] as const,
  },
  // Slice 6: installation-wide, never org-scoped (§3.1/AC7) — no "org"
  // prefix, matching organizations.list's own installation-wide key shape.
  providerDefinitions: {
    list: () => ["provider-definitions", "list"] as const,
    detail: (slug: string) => ["provider-definitions", "detail", slug] as const,
    bundles: () => ["provider-definitions", "bundles"] as const,
  },
};

/** LogsFilterKey is the subset of logging.QueryParams the console's filter
 * bar sends server-side (Slice 3, AC1): connectionId/userId/toolSlug/from/to. */
export interface LogsFilterKey {
  connectionId: string;
  userId: string;
  toolSlug: string;
  from: string;
  to: string;
}

/** EventsFilterKey is the subset of delivery.ListEventsParams the console's
 * filter bar sends server-side (Slice 3): type/deliveryStatus. */
export interface EventsFilterKey {
  type: string;
  deliveryStatus: string;
}
