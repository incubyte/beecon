import { useMutation, useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/lib/api-client";
import { queryKeys } from "@/lib/query";
import type { CreateIntegrationRequest, IntegrationSummary } from "@/lib/api-types";

/** useCreateIntegration registers an installation integration from a
 * provider definition (POST /api/v1/integrations): a provider slug plus the
 * OAuth client credentials. The clientSecret is write-once — sent here,
 * never returned in the IntegrationSummary response — so, unlike an issued
 * API key, there is no server-minted secret to reveal after creation. */
export function useCreateIntegration() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateIntegrationRequest) => apiClient.post<IntegrationSummary>("/integrations", input),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.integrations.list() });
    },
  });
}
