import { useSearch } from "@tanstack/react-router";
import { useMemo, useState } from "react";

import { DataTable } from "@/components/ui/DataTable";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { FilterBar } from "@/components/ui/FilterBar";
import { FilterChip } from "@/components/ui/FilterChip";
import { SkeletonRows } from "@/components/ui/SkeletonRows";
import { ApiError } from "@/lib/api-client";
import type { LogEntry } from "@/lib/api-types";

import { emptyLogsFilters, useLogs, type LogsFilters } from "./api";
import { logColumns } from "./columns";
import { LogDrawer } from "./LogDrawer";

/** LogsPage is Slice 3's OBSERVE > Logs surface (AC1/AC6/AC7): the selected
 * org's redacted log entries, filterable by connection/user/tool/date-range
 * (each an individually-removable chip), cursor-paginated (load-more, never
 * numbered pages — AC7), a row opens the detail drawer. */
export function LogsPage() {
  const search = useSearch({ from: "__root__" });
  const orgId = search.org;

  const [filters, setFilters] = useState<LogsFilters>(emptyLogsFilters);
  const [fromLocal, setFromLocal] = useState("");
  const [toLocal, setToLocal] = useState("");
  const [selectedEntry, setSelectedEntry] = useState<LogEntry | null>(null);

  const { items, isLoading, isError, error, hasMore, isLoadingMore, loadMore, refetch } = useLogs(orgId, filters);

  const activeChips = useMemo(() => buildActiveChips(filters), [filters]);

  function updateFilter(field: keyof LogsFilters, value: string) {
    setFilters((current) => ({ ...current, [field]: value }));
  }

  function clearFilter(field: keyof LogsFilters) {
    setFilters((current) => ({ ...current, [field]: "" }));
    if (field === "from") {
      setFromLocal("");
    }
    if (field === "to") {
      setToLocal("");
    }
  }

  if (!orgId) {
    return (
      <EmptyState
        title="Select an organization"
        description="Choose an organization from the top-bar switcher to see its logs."
      />
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold text-text">Logs</h1>
        <p className="text-sm text-text-secondary">Every redacted provider exchange for the selected organization.</p>
      </div>

      <FilterBar>
        <TextFilter label="Connection ID" value={filters.connectionId} onChange={(value) => updateFilter("connectionId", value)} />
        <TextFilter label="User ID" value={filters.userId} onChange={(value) => updateFilter("userId", value)} />
        <TextFilter label="Tool slug" value={filters.toolSlug} onChange={(value) => updateFilter("toolSlug", value)} />
        <label className="flex items-center gap-2 text-sm text-text-secondary">
          From
          <input
            type="datetime-local"
            value={fromLocal}
            onChange={(event) => {
              setFromLocal(event.target.value);
              updateFilter("from", toIsoOrEmpty(event.target.value));
            }}
            className="min-h-11 rounded-md border border-border-strong bg-surface px-2 text-sm text-text"
          />
        </label>
        <label className="flex items-center gap-2 text-sm text-text-secondary">
          To
          <input
            type="datetime-local"
            value={toLocal}
            onChange={(event) => {
              setToLocal(event.target.value);
              updateFilter("to", toIsoOrEmpty(event.target.value));
            }}
            className="min-h-11 rounded-md border border-border-strong bg-surface px-2 text-sm text-text"
          />
        </label>
        {activeChips.map((chip) => (
          <FilterChip key={chip.field} label={chip.label} onRemove={() => clearFilter(chip.field)} />
        ))}
      </FilterBar>

      {isError ? (
        <ErrorCard message={errorMessage(error)} onRetry={refetch} />
      ) : (
        <>
          <DataTable
            caption="Log entries"
            columns={logColumns}
            data={items}
            isLoading={isLoading}
            onRowClick={setSelectedEntry}
            loadingRows={<SkeletonRows columns={logColumns.length} />}
            emptyState={
              <EmptyState title="No log entries yet" description="Provider exchanges recorded by the API will appear here." />
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

      <LogDrawer entry={selectedEntry} onClose={() => setSelectedEntry(null)} />
    </div>
  );
}

function TextFilter({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label className="flex items-center gap-2 text-sm text-text-secondary">
      {label}
      <input
        type="text"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="min-h-11 w-40 rounded-md border border-border-strong bg-surface px-2 text-sm text-text"
      />
    </label>
  );
}

interface ActiveChip {
  field: keyof LogsFilters;
  label: string;
}

function buildActiveChips(filters: LogsFilters): ActiveChip[] {
  const chips: ActiveChip[] = [];
  if (filters.connectionId) chips.push({ field: "connectionId", label: `Connection: ${filters.connectionId}` });
  if (filters.userId) chips.push({ field: "userId", label: `User: ${filters.userId}` });
  if (filters.toolSlug) chips.push({ field: "toolSlug", label: `Tool: ${filters.toolSlug}` });
  if (filters.from) chips.push({ field: "from", label: `From: ${new Date(filters.from).toLocaleString()}` });
  if (filters.to) chips.push({ field: "to", label: `To: ${new Date(filters.to).toLocaleString()}` });
  return chips;
}

function toIsoOrEmpty(localValue: string): string {
  if (!localValue) {
    return "";
  }
  const parsed = new Date(localValue);
  return Number.isNaN(parsed.getTime()) ? "" : parsed.toISOString();
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The logs list could not be loaded.";
  }
  return "The logs list could not be loaded.";
}
