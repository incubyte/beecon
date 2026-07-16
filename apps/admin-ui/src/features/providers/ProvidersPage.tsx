import { useNavigate } from "@tanstack/react-router";
import { Check } from "lucide-react";
import { useState } from "react";

import { CreateIntegrationModal } from "@/features/integrations/CreateIntegrationModal";
import { DataTable } from "@/components/ui/DataTable";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { SkeletonRows } from "@/components/ui/SkeletonRows";
import { ApiError } from "@/lib/api-client";
import type { IntegrationSummary, ProviderDefinitionSummary } from "@/lib/api-types";

import { useProviderDefinitions } from "./api";
import { providerDefinitionColumns } from "./columns";

/** ProvidersPage is Slice 6's Catalog > Providers surface (AC1): every
 * provider definition this installation has loaded, cursor-paginated,
 * installation-wide and never governance-filtered (AC7) — an operator sees
 * the real installed estate, not any organization's filtered catalog.
 * Opening a row navigates to the full-page bundle detail (AC2). */
export function ProvidersPage() {
  const navigate = useNavigate();
  const { items, isLoading, isError, error, hasMore, isLoadingMore, loadMore, refetch } = useProviderDefinitions();
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [createdIntegration, setCreatedIntegration] = useState<IntegrationSummary | null>(null);

  function openProvider(provider: ProviderDefinitionSummary) {
    void navigate({ to: "/providers/$slug", params: { slug: provider.slug }, search: (prev) => prev });
  }

  function openCreateIntegration() {
    setCreatedIntegration(null);
    setIsCreateOpen(true);
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-text">Providers</h1>
          <p className="text-sm text-text-secondary">
            Every provider definition loaded by this installation — the real installed estate, not an organization's
            filtered view.
          </p>
        </div>
        <button
          type="button"
          onClick={openCreateIntegration}
          className="min-h-11 shrink-0 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover cursor-pointer"
        >
          Create integration
        </button>
      </div>

      {createdIntegration ? (
        <div
          role="status"
          className="flex items-center gap-2 rounded-md border border-border bg-success-bg px-4 py-3 text-sm text-success-text"
        >
          <Check className="size-4 shrink-0" aria-hidden="true" />
          <span>
            Integration “{createdIntegration.name}” created from {createdIntegration.providerSlug}.
          </span>
        </div>
      ) : null}

      {isError ? (
        <ErrorCard message={errorMessage(error)} onRetry={refetch} />
      ) : (
        <>
          <DataTable
            caption="Provider definitions"
            columns={providerDefinitionColumns}
            data={items}
            isLoading={isLoading}
            onRowClick={openProvider}
            loadingRows={<SkeletonRows columns={providerDefinitionColumns.length} />}
            emptyState={
              <EmptyState
                title="No provider definitions loaded"
                description="Provider definitions bundled with this installation will appear here."
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

      <CreateIntegrationModal open={isCreateOpen} onOpenChange={setIsCreateOpen} onCreated={setCreatedIntegration} />
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The provider definitions list could not be loaded.";
  }
  return "The provider definitions list could not be loaded.";
}
