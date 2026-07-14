import { useMutation, useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/lib/api-client";
import { queryKeys } from "@/lib/query";
import type { ConfigDocument, ConfigImportApplyResult, ConfigImportMode, ConfigImportPlan } from "@/lib/api-types";

/** useExportConfig fetches the selected org's current config document on
 * demand (Slice 9, AC1/AC2): a click-triggered mutation rather than a
 * cached useQuery, since the operator always wants the export current as
 * of the moment they click "Download", not a stale cached copy. The
 * response never contains a secret/credential/connection/user-token/
 * provider-definition — there is no field on ConfigDocument for any of
 * them. */
export function useExportConfig(orgId: string) {
  return useMutation({
    mutationFn: () => apiClient.get<ConfigDocument>(`/organizations/${orgId}/config/export`),
  });
}

export interface ImportConfigInput {
  document: ConfigDocument;
  mode: ConfigImportMode;
}

function importPath(orgId: string, dryRun: boolean, mode: ConfigImportMode): string {
  return `/organizations/${orgId}/config/import?dryRun=${dryRun}&mode=${mode}`;
}

/** useDryRunImport always calls with dryRun=true (Slice 9, AC3): validates
 * the document and computes the diff/plan it would apply, plus any
 * unknown-integration-id warnings, without writing anything. */
export function useDryRunImport(orgId: string) {
  return useMutation({
    mutationFn: ({ document, mode }: ImportConfigInput) =>
      apiClient.post<ConfigImportPlan>(importPath(orgId, true, mode), document),
  });
}

/** useApplyImport calls with dryRun=false (Slice 9, AC4/AC5/AC7): on
 * success it invalidates every query area an import can change —
 * governance, retention, and the webhook endpoints list — so the console's
 * other GOVERN pages reflect the import immediately rather than showing a
 * stale cached view. */
export function useApplyImport(orgId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ document, mode }: ImportConfigInput) =>
      apiClient.post<ConfigImportApplyResult>(importPath(orgId, false, mode), document),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.governance.detail(orgId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.retention.detail(orgId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.webhookEndpoints.list(orgId) });
    },
  });
}
