import { useNavigate } from "@tanstack/react-router";

import { DataTable } from "@/components/ui/DataTable";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { SkeletonRows } from "@/components/ui/SkeletonRows";
import { ApiError } from "@/lib/api-client";
import type { Organization } from "@/lib/api-types";

import { useOrganizations } from "./api";
import { organizationColumns } from "./columns";

/** OrganizationsPage is Slice 1's headline surface (AC7): every
 * organization in the installation, cursor-paginated, id (mono,
 * click-to-copy) and created date. Selecting a row sets `?org=` (the same
 * scoping action the top-bar org switcher performs), which later slices'
 * org-scoped views read. */
export function OrganizationsPage() {
  const navigate = useNavigate();
  const { items, isLoading, isError, error, hasMore, isLoadingMore, loadMore, refetch } = useOrganizations();

  function selectOrg(org: Organization) {
    void navigate({ to: ".", search: (prev) => ({ ...prev, org: org.id }) });
  }

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold text-text">Organizations</h1>
        <p className="text-sm text-text-secondary">Every organization in this installation.</p>
      </div>

      {isError ? (
        <ErrorCard message={errorMessage(error)} onRetry={refetch} />
      ) : (
        <>
          <DataTable
            caption="Organizations"
            columns={organizationColumns}
            data={items}
            isLoading={isLoading}
            onRowClick={selectOrg}
            loadingRows={<SkeletonRows columns={organizationColumns.length} />}
            emptyState={
              <EmptyState
                title="No organizations yet"
                description="Organizations created via the API will appear here."
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
    return error.message || "The organizations list could not be loaded.";
  }
  return "The organizations list could not be loaded.";
}
