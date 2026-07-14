import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/lib/api-client";
import { useCursorPagination, type UseCursorPaginationResult } from "@/lib/cursor";
import { queryKeys } from "@/lib/query";
import type { TriggerInstance, TriggerInstancesPage, TriggerInstanceStatusResult } from "@/lib/api-types";

/** useTriggerInstances lists the selected org's trigger instances (Slice 2,
 * AC4), cursor-paginated. Disabled while no org is selected. */
export function useTriggerInstances(orgId: string | undefined): UseCursorPaginationResult<TriggerInstance> {
  return useCursorPagination<TriggerInstance>({
    queryKey: queryKeys.triggerInstances.list(orgId ?? ""),
    fetchPage: (cursor) => fetchTriggerInstancesPage(orgId as string, cursor),
    enabled: Boolean(orgId),
  });
}

function fetchTriggerInstancesPage(orgId: string, cursor: string | undefined): Promise<TriggerInstancesPage> {
  const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  return apiClient.get<TriggerInstancesPage>(`/organizations/${orgId}/trigger-instances${query}`);
}

/** useTriggerInstance reads one instance's full detail (Slice 2, AC4) for
 * the full-page detail view. */
export function useTriggerInstance(orgId: string | undefined, instanceId: string | undefined) {
  return useQuery({
    queryKey: queryKeys.triggerInstances.detail(orgId ?? "", instanceId ?? ""),
    queryFn: () => apiClient.get<TriggerInstance>(`/organizations/${orgId}/trigger-instances/${instanceId}`),
    enabled: Boolean(orgId) && Boolean(instanceId),
  });
}

export type TriggerInstanceToggleAction = "enable" | "disable";

/** useSetTriggerInstanceStatus enables/disables a trigger instance (Slice 2,
 * AC5) with an optimistic status flip on the cached detail (architecture
 * doc §2.4: "optimistic updates only for cheap toggles"), rolled back on
 * failure. */
export function useSetTriggerInstanceStatus(orgId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ instanceId, action }: { instanceId: string; action: TriggerInstanceToggleAction }) =>
      apiClient.post<TriggerInstanceStatusResult>(`/organizations/${orgId}/trigger-instances/${instanceId}/${action}`),

    onMutate: async ({ instanceId, action }) => {
      const detailKey = queryKeys.triggerInstances.detail(orgId, instanceId);
      await queryClient.cancelQueries({ queryKey: detailKey });
      const previous = queryClient.getQueryData<TriggerInstance>(detailKey);
      queryClient.setQueryData<TriggerInstance>(detailKey, (current) =>
        current ? { ...current, status: action === "enable" ? "ACTIVE" : "DISABLED" } : current,
      );
      return { previous, detailKey };
    },

    onError: (_error, _variables, context) => {
      if (context?.previous) {
        queryClient.setQueryData(context.detailKey, context.previous);
      }
    },

    onSettled: (_result, _error, { instanceId }) => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.triggerInstances.detail(orgId, instanceId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.triggerInstances.list(orgId) });
    },
  });
}

/** useDeleteTriggerInstance permanently removes a trigger instance (Slice 2,
 * AC6), guarded by a plain ConfirmDialog. */
export function useDeleteTriggerInstance(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (instanceId: string) => apiClient.delete<void>(`/organizations/${orgId}/trigger-instances/${instanceId}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.triggerInstances.list(orgId) });
    },
  });
}
