import { useQuery } from "@tanstack/react-query";

import { apiClient } from "@/lib/api-client";
import { useCursorPagination, type UseCursorPaginationResult } from "@/lib/cursor";
import { queryKeys } from "@/lib/query";
import type {
  IntegrationSummary,
  IntegrationSummaryList,
  ProviderDefinitionDetail,
  ProviderDefinitionSummary,
  ProviderDefinitionsPage,
} from "@/lib/api-types";

/** useProviderDefinitions lists every provider definition this installation
 * has loaded (Slice 6, AC1), cursor-paginated, via the new admin-guarded
 * GET /api/v1/provider-definitions — installation-wide, no orgId in the
 * path, and never governance-filtered (AC7). */
export function useProviderDefinitions(): UseCursorPaginationResult<ProviderDefinitionSummary> {
  return useCursorPagination<ProviderDefinitionSummary>({
    queryKey: queryKeys.providerDefinitions.list(),
    fetchPage: fetchProviderDefinitionsPage,
  });
}

function fetchProviderDefinitionsPage(cursor: string | undefined): Promise<ProviderDefinitionsPage> {
  const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  return apiClient.get<ProviderDefinitionsPage>(`/provider-definitions${query}`);
}

/** useProviderDefinition reads one provider's full versioned Bundle (Slice
 * 6, AC2) for the full-page detail view's mono JSON/YAML viewer. */
export function useProviderDefinition(slug: string | undefined) {
  return useQuery({
    queryKey: queryKeys.providerDefinitions.detail(slug ?? ""),
    queryFn: () => apiClient.get<ProviderDefinitionDetail>(`/provider-definitions/${slug}`),
    enabled: Boolean(slug),
  });
}

/** useProviderIntegrations reads the installation-level integrations created
 * against one provider (this slice's new console read): the provider
 * detail page's Integrations section, via the console-guarded GET
 * /provider-definitions/{slug}/integrations — installation-wide and never
 * governance-filtered, mirroring useProviderDefinition's own posture. Low
 * cardinality, so no cursor pagination. */
export function useProviderIntegrations(slug: string | undefined) {
  const query = useQuery({
    queryKey: queryKeys.providerDefinitions.integrations(slug ?? ""),
    queryFn: () => apiClient.get<IntegrationSummaryList>(`/provider-definitions/${slug}/integrations`),
    enabled: Boolean(slug),
  });
  return {
    items: query.data?.items ?? ([] as IntegrationSummary[]),
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error,
    refetch: () => void query.refetch(),
  };
}

async function fetchAllProviderDefinitionSlugs(): Promise<string[]> {
  const slugs: string[] = [];
  let cursor: string | undefined;
  do {
    const page = await fetchProviderDefinitionsPage(cursor);
    slugs.push(...page.items.map((item) => item.slug));
    cursor = page.nextCursor;
  } while (cursor);
  return slugs;
}

async function fetchAllProviderDefinitionBundles(): Promise<ProviderDefinitionDetail[]> {
  const slugs = await fetchAllProviderDefinitionSlugs();
  return Promise.all(slugs.map((slug) => apiClient.get<ProviderDefinitionDetail>(`/provider-definitions/${slug}`)));
}

/**
 * useProviderDefinitionBundles fetches every loaded provider definition's
 * full Bundle (Slice 6, AC3/AC4): the Tools and Trigger Definitions catalog
 * pages derive their cross-provider, filterable-by-provider rows from this
 * one shared read instead of duplicating the "list slugs, then fetch each
 * detail" dance — a provider definition's Bundle already carries every tool
 * and trigger it declares, so no separate tools/trigger-definitions catalog
 * endpoint is needed (installation-wide, unfiltered by any org's governance,
 * AC7, since it is built only from ListProviderDefinitions/
 * ProviderDefinitionDetail).
 */
export function useProviderDefinitionBundles() {
  const query = useQuery({
    queryKey: queryKeys.providerDefinitions.bundles(),
    queryFn: fetchAllProviderDefinitionBundles,
  });
  return {
    bundles: query.data ?? [],
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error,
    refetch: () => void query.refetch(),
  };
}
