import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/lib/api-client";
import { queryKeys } from "@/lib/query";
import type { CreatedOperator, OperatorsPage } from "@/lib/api-types";

/** useOperators lists every operator account in the installation (Phase 5
 * Slice 4, AC3): flat and installation-wide — GET /api/v1/operators returns
 * a `{ items: [...] }` envelope (not cursor-paginated), so this reads
 * `.items` out of a plain useQuery, the same "not paginated" shape
 * useApiKeys' own doc comment already establishes for a different flat
 * list. */
export function useOperators() {
  return useQuery({
    queryKey: queryKeys.operators.list(),
    queryFn: async () => (await apiClient.get<OperatorsPage>("/operators")).items,
  });
}

/** useCreateOperator creates another ACTIVE operator account from the
 * console (AC1): the creator sets the initial password directly, so —
 * unlike issuing an API key — there is no server-generated secret to show
 * once. */
export function useCreateOperator() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: { email: string; password: string }) =>
      apiClient.post<CreatedOperator>("/operators", input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.operators.list() });
    },
  });
}

/** useDeactivateOperator disables another operator account (AC5): the
 * server itself rejects deactivating the last remaining ACTIVE operator
 * with a 409 (AC6) — surfaced by the caller as an ApiError, not silently
 * retried or swallowed. */
export function useDeactivateOperator() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (operatorId: string) => apiClient.post<void>(`/operators/${operatorId}/deactivate`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.operators.list() });
    },
  });
}

/** useChangeMyPassword changes the signed-in operator's own password (AC4):
 * on success the server has already revoked every one of this operator's
 * OTHER sessions while keeping the one making this very call alive (the
 * carried-forward Slice 2 AC4 keep-current semantics) — nothing else for
 * the console to invalidate or re-fetch here. */
export function useChangeMyPassword() {
  return useMutation({
    mutationFn: (input: { currentPassword: string; newPassword: string }) =>
      apiClient.post<void>("/operators/me/password", input),
  });
}
