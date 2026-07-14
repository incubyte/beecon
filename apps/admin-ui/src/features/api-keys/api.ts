import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/lib/api-client";
import { queryKeys } from "@/lib/query";
import type { ApiKeyListing, ApiKeyScope, IssuedApiKey, RotatedApiKey } from "@/lib/api-types";

/** useApiKeys lists the selected org's API keys (Slice 4, AC3). Unlike every
 * other org-scoped list in this console, GET .../api-keys is not
 * cursor-paginated — it returns a flat array (access/driving/httpapi's List
 * handler) — so this is a plain useQuery, not useCursorPagination. */
export function useApiKeys(orgId: string | undefined) {
  return useQuery({
    queryKey: queryKeys.apiKeys.list(orgId ?? ""),
    queryFn: () => apiClient.get<ApiKeyListing[]>(`/organizations/${orgId}/api-keys`),
    enabled: Boolean(orgId),
  });
}

/** useIssueApiKey creates a new key with the chosen scope (AC4); the
 * response's full secret must be shown to the operator exactly once by the
 * caller (SecretOnceModal) and never persisted client-side beyond that. */
export function useIssueApiKey(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (scope: ApiKeyScope) => apiClient.post<IssuedApiKey>(`/organizations/${orgId}/api-keys`, { scope }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.apiKeys.list(orgId) });
    },
  });
}

/** useRotateApiKey mints a fresh secret for an existing key (AC6): the new
 * secret is shown exactly once; the outgoing secret keeps authenticating
 * until overlapExpiresAt (PD23). */
export function useRotateApiKey(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (keyId: string) => apiClient.post<RotatedApiKey>(`/organizations/${orgId}/api-keys/${keyId}/rotate`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.apiKeys.list(orgId) });
    },
  });
}

/** useRevokeApiKey permanently revokes a key (AC6, guarded by TypeToConfirm
 * — the highest-risk destructive action in this feature area). */
export function useRevokeApiKey(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (keyId: string) => apiClient.delete<void>(`/organizations/${orgId}/api-keys/${keyId}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.apiKeys.list(orgId) });
    },
  });
}
