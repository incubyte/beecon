import { useMemo, useState } from "react";

import { DataTable } from "@/components/ui/DataTable";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { FilterBar } from "@/components/ui/FilterBar";
import { FilterChip } from "@/components/ui/FilterChip";
import { SkeletonRows } from "@/components/ui/SkeletonRows";
import { ApiError } from "@/lib/api-client";
import type { CatalogTriggerDefinition } from "@/lib/api-types";

import { useCatalogTriggerDefinitions } from "./api";
import { triggerDefinitionColumns } from "./columns";
import { TriggerDefinitionDrawer } from "./TriggerDefinitionDrawer";

/** TriggerDefinitionsPage is Slice 6's Catalog > Trigger Definitions surface
 * (AC3): every trigger definition across every loaded provider, filterable
 * by provider, installation-wide and never governance-filtered (AC7). A row
 * opens the schema/ingestion-mode drawer (AC4). */
export function TriggerDefinitionsPage() {
  const { triggers, isLoading, isError, error, refetch } = useCatalogTriggerDefinitions();
  const [providerFilter, setProviderFilter] = useState("");
  const [selectedTrigger, setSelectedTrigger] = useState<CatalogTriggerDefinition | null>(null);

  const providerOptions = useMemo(
    () => Array.from(new Set(triggers.map((trigger) => trigger.providerSlug))).sort(),
    [triggers],
  );

  const filteredTriggers = useMemo(
    () => triggers.filter((trigger) => !providerFilter || trigger.providerSlug === providerFilter),
    [triggers, providerFilter],
  );

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold text-text">Trigger Definitions</h1>
        <p className="text-sm text-text-secondary">Every trigger definition across every loaded provider definition.</p>
      </div>

      <FilterBar>
        <label className="flex items-center gap-2 text-sm text-text-secondary">
          Provider
          <select
            value={providerFilter}
            onChange={(event) => setProviderFilter(event.target.value)}
            className="min-h-11 rounded-md border border-border-strong bg-surface px-2 text-sm text-text"
          >
            <option value="">All providers</option>
            {providerOptions.map((provider) => (
              <option key={provider} value={provider}>
                {provider}
              </option>
            ))}
          </select>
        </label>
        {providerFilter ? (
          <FilterChip label={`Provider: ${providerFilter}`} onRemove={() => setProviderFilter("")} />
        ) : null}
      </FilterBar>

      {isError ? (
        <ErrorCard message={errorMessage(error)} onRetry={refetch} />
      ) : (
        <DataTable
          caption="Trigger definitions"
          columns={triggerDefinitionColumns}
          data={filteredTriggers}
          isLoading={isLoading}
          onRowClick={setSelectedTrigger}
          loadingRows={<SkeletonRows columns={triggerDefinitionColumns.length} />}
          emptyState={
            <EmptyState
              title="No trigger definitions declared"
              description="Trigger definitions declared by loaded provider definitions will appear here."
            />
          }
        />
      )}

      <TriggerDefinitionDrawer trigger={selectedTrigger} onClose={() => setSelectedTrigger(null)} />
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The trigger definitions catalog could not be loaded.";
  }
  return "The trigger definitions catalog could not be loaded.";
}
