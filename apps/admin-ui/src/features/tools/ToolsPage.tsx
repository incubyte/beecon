import { useMemo, useState } from "react";

import { DataTable } from "@/components/ui/DataTable";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { FilterBar } from "@/components/ui/FilterBar";
import { FilterChip } from "@/components/ui/FilterChip";
import { SkeletonRows } from "@/components/ui/SkeletonRows";
import { ApiError } from "@/lib/api-client";
import type { CatalogTool } from "@/lib/api-types";

import { useCatalogTools } from "./api";
import { toolColumns } from "./columns";
import { ToolDrawer } from "./ToolDrawer";

/** ToolsPage is Slice 6's Catalog > Tools surface (AC3): every tool across
 * every loaded provider definition, filterable by provider, installation-
 * wide and never governance-filtered (AC7). A row opens the schema drawer
 * (AC4). */
export function ToolsPage() {
  const { tools, isLoading, isError, error, refetch } = useCatalogTools();
  const [providerFilter, setProviderFilter] = useState("");
  const [selectedTool, setSelectedTool] = useState<CatalogTool | null>(null);

  const providerOptions = useMemo(
    () => Array.from(new Set(tools.map((tool) => tool.providerSlug))).sort(),
    [tools],
  );

  const filteredTools = useMemo(
    () => tools.filter((tool) => !providerFilter || tool.providerSlug === providerFilter),
    [tools, providerFilter],
  );

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold text-text">Tools</h1>
        <p className="text-sm text-text-secondary">Every tool across every loaded provider definition.</p>
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
          caption="Tools"
          columns={toolColumns}
          data={filteredTools}
          isLoading={isLoading}
          onRowClick={setSelectedTool}
          loadingRows={<SkeletonRows columns={toolColumns.length} />}
          emptyState={
            <EmptyState title="No tools declared" description="Tools declared by loaded provider definitions will appear here." />
          }
        />
      )}

      <ToolDrawer tool={selectedTool} onClose={() => setSelectedTool(null)} />
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The tools catalog could not be loaded.";
  }
  return "The tools catalog could not be loaded.";
}
