import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/lib/api-client";
import { queryKeys } from "@/lib/query";
import type { Retention, RetentionUpdate } from "@/lib/api-types";

/** useRetention reads the selected org's own log/event retention windows
 * (Slice 7, AC1): a single settings object, mirroring useGovernance's own
 * "not every org-scoped read is a page" precedent. */
export function useRetention(orgId: string | undefined) {
  return useQuery({
    queryKey: queryKeys.retention.detail(orgId ?? ""),
    queryFn: () => apiClient.get<Retention>(`/organizations/${orgId}/retention`),
    enabled: Boolean(orgId),
  });
}

/** useUpdateRetention replaces the selected org's entire retention record
 * (Slice 7, AC1/AC4/AC5): a whole-object PUT, mirroring
 * useUpdateGovernance's own replace convention. */
export function useUpdateRetention(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (update: RetentionUpdate) => apiClient.put<Retention>(`/organizations/${orgId}/retention`, update),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.retention.detail(orgId) });
    },
  });
}
