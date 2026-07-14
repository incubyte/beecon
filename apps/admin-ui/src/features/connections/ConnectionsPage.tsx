import { useSearch } from "@tanstack/react-router";
import { useMemo, useState } from "react";

import { DataTable } from "@/components/ui/DataTable";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { FilterBar } from "@/components/ui/FilterBar";
import { FilterChip } from "@/components/ui/FilterChip";
import { SkeletonRows } from "@/components/ui/SkeletonRows";
import { ApiError } from "@/lib/api-client";
import type { Connection, ConnectionStatus } from "@/lib/api-types";

import { useConnections } from "./api";
import { connectionColumns } from "./columns";
import { ConnectionDrawer } from "./ConnectionDrawer";

const STATUS_OPTIONS: ConnectionStatus[] = ["ACTIVE", "INITIATED", "EXPIRED", "DISCONNECTED"];

/**
 * ConnectionsPage is Slice 2's Operate > Connections surface: the selected
 * org's connections, cursor-paginated (AC1), filterable by status and
 * integration with removable chips (AC3), a row opens the detail drawer
 * (AC2). Status/integration filtering runs client-side over the fetched
 * pages — the connections List endpoint (Phase 1-3) takes no such filter
 * params today (see lib/query.ts).
 */
export function ConnectionsPage() {
  const search = useSearch({ from: "__root__" });
  const orgId = search.org;
  const { items, isLoading, isError, error, hasMore, isLoadingMore, loadMore, refetch } = useConnections(orgId);

  const [statusFilter, setStatusFilter] = useState("");
  const [integrationFilter, setIntegrationFilter] = useState("");
  const [selectedConnectionId, setSelectedConnectionId] = useState<string | null>(null);

  const integrationOptions = useMemo(
    () => Array.from(new Set(items.map((connection) => connection.providerSlug))).sort(),
    [items],
  );

  const filteredItems = useMemo(
    () =>
      items.filter(
        (connection) =>
          (!statusFilter || connection.status === statusFilter) &&
          (!integrationFilter || connection.providerSlug === integrationFilter),
      ),
    [items, statusFilter, integrationFilter],
  );

  if (!orgId) {
    return (
      <EmptyState
        title="Select an organization"
        description="Choose an organization from the top-bar switcher to see its connections."
      />
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold text-text">Connections</h1>
        <p className="text-sm text-text-secondary">Every connection for the selected organization.</p>
      </div>

      <FilterBar>
        <label className="flex items-center gap-2 text-sm text-text-secondary">
          Status
          <select
            value={statusFilter}
            onChange={(event) => setStatusFilter(event.target.value)}
            className="min-h-11 rounded-md border border-border-strong bg-surface px-2 text-sm text-text"
          >
            <option value="">All statuses</option>
            {STATUS_OPTIONS.map((status) => (
              <option key={status} value={status}>
                {status}
              </option>
            ))}
          </select>
        </label>
        <label className="flex items-center gap-2 text-sm text-text-secondary">
          Integration
          <select
            value={integrationFilter}
            onChange={(event) => setIntegrationFilter(event.target.value)}
            className="min-h-11 rounded-md border border-border-strong bg-surface px-2 text-sm text-text"
          >
            <option value="">All integrations</option>
            {integrationOptions.map((provider) => (
              <option key={provider} value={provider}>
                {provider}
              </option>
            ))}
          </select>
        </label>
        {statusFilter ? <FilterChip label={`Status: ${statusFilter}`} onRemove={() => setStatusFilter("")} /> : null}
        {integrationFilter ? (
          <FilterChip label={`Integration: ${integrationFilter}`} onRemove={() => setIntegrationFilter("")} />
        ) : null}
      </FilterBar>

      {isError ? (
        <ErrorCard message={errorMessage(error)} onRetry={refetch} />
      ) : (
        <>
          <DataTable
            caption="Connections"
            columns={connectionColumns}
            data={filteredItems}
            isLoading={isLoading}
            onRowClick={(connection: Connection) => setSelectedConnectionId(connection.id)}
            loadingRows={<SkeletonRows columns={connectionColumns.length} />}
            emptyState={
              <EmptyState
                title="No connections yet"
                description="Connections created through the API will appear here."
              />
            }
          />

          {hasMore ? (
            <button
              type="button"
              onClick={loadMore}
              disabled={isLoadingMore}
              className="min-h-11 self-start rounded-md border border-border-strong px-4 text-sm font-medium text-text transition-colors hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
            >
              {isLoadingMore ? "Loading…" : "Load more"}
            </button>
          ) : null}
        </>
      )}

      <ConnectionDrawer orgId={orgId} connectionId={selectedConnectionId} onClose={() => setSelectedConnectionId(null)} />
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The connections list could not be loaded.";
  }
  return "The connections list could not be loaded.";
}
