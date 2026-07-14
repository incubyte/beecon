import { useMemo } from "react";

import { useProviderDefinitionBundles } from "@/features/providers/api";
import type { CatalogTool } from "@/lib/api-types";

/** useCatalogTools flattens every loaded provider definition's tools into
 * one cross-provider list (Slice 6, AC3), tagged with each tool's owning
 * provider identity — installation-wide and never governance-filtered
 * (AC7), since it is built entirely from ListProviderDefinitions/
 * ProviderDefinitionDetail. Sorted by slug for a stable, scannable order. */
export function useCatalogTools() {
  const { bundles, isLoading, isError, error, refetch } = useProviderDefinitionBundles();

  const tools = useMemo<CatalogTool[]>(() => {
    const flattened = bundles.flatMap((definition) =>
      definition.bundle.tools.map((tool) => ({
        ...tool,
        providerSlug: definition.slug,
        providerName: definition.name,
      })),
    );
    return flattened.sort((a, b) => a.slug.localeCompare(b.slug));
  }, [bundles]);

  return { tools, isLoading, isError, error, refetch };
}
