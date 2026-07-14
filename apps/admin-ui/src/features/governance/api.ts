import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/lib/api-client";
import { queryKeys } from "@/lib/query";
import type { Governance, GovernanceUpdate, IntegrationVisibility } from "@/lib/api-types";

/** useGovernance reads the selected org's governance settings (Slice 5,
 * AC1/AC2/AC4/AC7): allow-list, hidden set, and onboarding featured/cap.
 * Unlike the cursor-paginated lists elsewhere in this console, governance is
 * a single settings object — a plain useQuery, mirroring useApiKeys' own
 * "not every org-scoped read is a page" precedent. */
export function useGovernance(orgId: string | undefined) {
  return useQuery({
    queryKey: queryKeys.governance.detail(orgId ?? ""),
    queryFn: () => apiClient.get<Governance>(`/organizations/${orgId}/governance`),
    enabled: Boolean(orgId),
  });
}

/** useIntegrationsWithVisibility reads every installation integration,
 * unfiltered, each annotated with its effective visibility for the selected
 * org (Slice 5, AC1) — the operator's governance view, distinct from the
 * org-facing (already-filtered) consumer catalog. */
export function useIntegrationsWithVisibility(orgId: string | undefined) {
  return useQuery({
    queryKey: queryKeys.governance.catalog(orgId ?? ""),
    queryFn: () => apiClient.get<IntegrationVisibility[]>(`/organizations/${orgId}/governance/catalog`),
    enabled: Boolean(orgId),
  });
}

/** useUpdateGovernance replaces the selected org's entire governance record
 * (Slice 5, AC2/AC4/AC7): a whole-object PUT, mirroring
 * UpdateAllowedRedirectURIs' own replace convention. Invalidates both the
 * governance detail and the unfiltered catalog view so the effective
 * visibility column reflects the change immediately. */
export function useUpdateGovernance(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (update: GovernanceUpdate) => apiClient.put<Governance>(`/organizations/${orgId}/governance`, update),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.governance.detail(orgId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.governance.catalog(orgId) });
    },
  });
}
