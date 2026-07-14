import { useNavigate, useSearch } from "@tanstack/react-router";

import { DataTable } from "@/components/ui/DataTable";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { SkeletonRows } from "@/components/ui/SkeletonRows";
import { ApiError } from "@/lib/api-client";
import type { TriggerInstance } from "@/lib/api-types";

import { useTriggerInstances } from "./api";
import { triggerInstanceColumns } from "./columns";

/** TriggerInstancesPage is Slice 2's Operate > Trigger Instances list
 * (AC4): the selected org's instances, cursor-paginated; opening a row
 * navigates to the full-page detail view (config-heavy surfaces get a full
 * page, not a drawer — DESIGN.md §0#4). */
export function TriggerInstancesPage() {
  const search = useSearch({ from: "__root__" });
  const orgId = search.org;
  const navigate = useNavigate();
  const { items, isLoading, isError, error, hasMore, isLoadingMore, loadMore, refetch } = useTriggerInstances(orgId);

  function openInstance(instance: TriggerInstance) {
    void navigate({ to: "/trigger-instances/$trgId", params: { trgId: instance.id }, search: (prev) => prev });
  }

  if (!orgId) {
    return (
      <EmptyState
        title="Select an organization"
        description="Choose an organization from the top-bar switcher to see its trigger instances."
      />
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold text-text">Trigger Instances</h1>
        <p className="text-sm text-text-secondary">Every trigger instance for the selected organization.</p>
      </div>

      {isError ? (
        <ErrorCard message={errorMessage(error)} onRetry={refetch} />
      ) : (
        <>
          <DataTable
            caption="Trigger instances"
            columns={triggerInstanceColumns}
            data={items}
            isLoading={isLoading}
            onRowClick={openInstance}
            loadingRows={<SkeletonRows columns={triggerInstanceColumns.length} />}
            emptyState={
              <EmptyState
                title="No trigger instances yet"
                description="Trigger instances created through the API will appear here."
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
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The trigger instances list could not be loaded.";
  }
  return "The trigger instances list could not be loaded.";
}
