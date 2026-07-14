import { apiClient } from "@/lib/api-client";
import { useCursorPagination, type UseCursorPaginationResult } from "@/lib/cursor";
import { queryKeys } from "@/lib/query";
import type { LogEntry, LogsPage, Page } from "@/lib/api-types";

/** LogsFilters is the console's log-explorer filter bar shape (Slice 3,
 * AC1): every field is sent server-side as a logging.QueryParams filter
 * (connectionId/userId/toolSlug/from/to, from/to as RFC3339 strings) —
 * unlike Connections' client-side status/integration filters (Slice 2), the
 * logs Query endpoint already supports these dimensions natively. */
export interface LogsFilters {
  connectionId: string;
  userId: string;
  toolSlug: string;
  from: string;
  to: string;
}

export const emptyLogsFilters: LogsFilters = { connectionId: "", userId: "", toolSlug: "", from: "", to: "" };

/** useLogs lists the selected org's log entries (AC1), cursor-paginated,
 * re-fetching whenever any filter field changes (the filters object is part
 * of the query key). Disabled while no org is selected. */
export function useLogs(orgId: string | undefined, filters: LogsFilters): UseCursorPaginationResult<LogEntry> {
  return useCursorPagination<LogEntry>({
    queryKey: queryKeys.logs.list(orgId ?? "", filters),
    fetchPage: (cursor) => fetchLogsPage(orgId as string, filters, cursor),
    enabled: Boolean(orgId),
  });
}

async function fetchLogsPage(orgId: string, filters: LogsFilters, cursor: string | undefined): Promise<Page<LogEntry>> {
  const query = buildLogsQuery(filters, cursor);
  const page = await apiClient.get<LogsPage>(`/organizations/${orgId}/logs${query}`);
  return { items: page.entries, nextCursor: page.nextCursor };
}

function buildLogsQuery(filters: LogsFilters, cursor: string | undefined): string {
  const params = new URLSearchParams();
  if (filters.connectionId) params.set("connectionId", filters.connectionId);
  if (filters.userId) params.set("userId", filters.userId);
  if (filters.toolSlug) params.set("toolSlug", filters.toolSlug);
  if (filters.from) params.set("from", filters.from);
  if (filters.to) params.set("to", filters.to);
  if (cursor) params.set("cursor", cursor);
  const query = params.toString();
  return query ? `?${query}` : "";
}
