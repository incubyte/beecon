import { useSearch } from "@tanstack/react-router";
import { useState } from "react";

import { DataTable } from "@/components/ui/DataTable";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { SkeletonRows } from "@/components/ui/SkeletonRows";
import { ApiError } from "@/lib/api-client";

import { useUsers } from "./api";
import { userColumns } from "./columns";
import { CreateUserModal } from "./CreateUserModal";

/** UsersPage is Slice 4's Administer > Users surface: the selected org's
 * end-users, cursor-paginated (AC1), with a "Create user" action (AC2) —
 * mirrors OrganizationsPage/ConnectionsPage's own list-page shape. */
export function UsersPage() {
  const search = useSearch({ from: "__root__" });
  const orgId = search.org;
  const { items, isLoading, isError, error, hasMore, isLoadingMore, loadMore, refetch } = useUsers(orgId);
  const [isCreateOpen, setIsCreateOpen] = useState(false);

  if (!orgId) {
    return (
      <EmptyState
        title="Select an organization"
        description="Choose an organization from the top-bar switcher to see its end-users."
      />
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-text">Users</h1>
          <p className="text-sm text-text-secondary">The selected organization's end-users.</p>
        </div>
        <button
          type="button"
          onClick={() => setIsCreateOpen(true)}
          className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover cursor-pointer"
        >
          Create user
        </button>
      </div>

      {isError ? (
        <ErrorCard message={errorMessage(error)} onRetry={refetch} />
      ) : (
        <>
          <DataTable
            caption="Users"
            columns={userColumns}
            data={items}
            isLoading={isLoading}
            loadingRows={<SkeletonRows columns={userColumns.length} />}
            emptyState={
              <EmptyState
                title="No end-users yet"
                description="Create the first end-user, or wait for the organization's own server to create one."
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

      <CreateUserModal orgId={orgId} open={isCreateOpen} onOpenChange={setIsCreateOpen} />
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The users list could not be loaded.";
  }
  return "The users list could not be loaded.";
}
