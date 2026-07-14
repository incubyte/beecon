import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/lib/api-client";
import { useCursorPagination, type UseCursorPaginationResult } from "@/lib/cursor";
import { queryKeys } from "@/lib/query";
import type { Connection, ConnectionsPage, ConnectionStatusResult, InitiatedConnection } from "@/lib/api-types";

/** useConnections lists the selected org's connections (Slice 2, AC1) via
 * the AdminOrgScope console mount, cursor-paginated. Disabled while no org
 * is selected — the top-bar org switcher's `?org=` is the single source of
 * truth (architecture doc §2.4). */
export function useConnections(orgId: string | undefined): UseCursorPaginationResult<Connection> {
  return useCursorPagination<Connection>({
    queryKey: queryKeys.connections.list(orgId ?? ""),
    fetchPage: (cursor) => fetchConnectionsPage(orgId as string, cursor),
    enabled: Boolean(orgId),
  });
}

function fetchConnectionsPage(orgId: string, cursor: string | undefined): Promise<ConnectionsPage> {
  const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  return apiClient.get<ConnectionsPage>(`/organizations/${orgId}/connections${query}`);
}

/** useConnection reads one connection's full detail (Slice 2, AC2) for the
 * drawer — disabled until a row has actually been selected. */
export function useConnection(orgId: string | undefined, connectionId: string | undefined) {
  return useQuery({
    queryKey: queryKeys.connections.detail(orgId ?? "", connectionId ?? ""),
    queryFn: () => apiClient.get<Connection>(`/organizations/${orgId}/connections/${connectionId}`),
    enabled: Boolean(orgId) && Boolean(connectionId),
  });
}

/** useDisableConnection transitions a connection to DISCONNECTED and
 * refreshes the list so the new status badge shows up without a manual
 * reload. */
export function useDisableConnection(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (connectionId: string) =>
      apiClient.post<ConnectionStatusResult>(`/organizations/${orgId}/connections/${connectionId}/disable`),
    onSuccess: (_result, connectionId) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.connections.list(orgId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.connections.detail(orgId, connectionId) });
    },
  });
}

/** useDeleteConnection permanently removes a connection (guarded by
 * TypeToConfirm in the drawer, DESIGN.md §7). */
export function useDeleteConnection(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (connectionId: string) => apiClient.delete<void>(`/organizations/${orgId}/connections/${connectionId}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.connections.list(orgId) });
    },
  });
}

/** useReconnectConnection starts a fresh connect-page handshake under the
 * connection's own immutable id (PD19), returning the redirectUrl the
 * operator hands to the end user. */
export function useReconnectConnection(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ connectionId, redirectUri }: { connectionId: string; redirectUri: string }) =>
      apiClient.post<InitiatedConnection>(`/organizations/${orgId}/connections/${connectionId}/reconnect`, { redirectUri }),
    onSuccess: (_result, variables) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.connections.list(orgId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.connections.detail(orgId, variables.connectionId) });
    },
  });
}
