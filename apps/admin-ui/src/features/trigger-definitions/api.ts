import { useMemo } from "react";

import { useProviderDefinitionBundles } from "@/features/providers/api";
import type { CatalogTriggerDefinition } from "@/lib/api-types";

/** useCatalogTriggerDefinitions flattens every loaded provider definition's
 * triggers into one cross-provider list (Slice 6, AC3), tagged with each
 * trigger's owning provider identity — installation-wide and never
 * governance-filtered (AC7), built entirely from ListProviderDefinitions/
 * ProviderDefinitionDetail. Sorted by slug for a stable, scannable order. */
export function useCatalogTriggerDefinitions() {
  const { bundles, isLoading, isError, error, refetch } = useProviderDefinitionBundles();

  const triggers = useMemo<CatalogTriggerDefinition[]>(() => {
    const flattened = bundles.flatMap((definition) =>
      definition.bundle.triggers.map((trigger) => ({
        ...trigger,
        providerSlug: definition.slug,
        providerName: definition.name,
      })),
    );
    return flattened.sort((a, b) => a.slug.localeCompare(b.slug));
  }, [bundles]);

  return { triggers, isLoading, isError, error, refetch };
}
