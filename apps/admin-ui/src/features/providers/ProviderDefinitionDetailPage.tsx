import { Link, useParams } from "@tanstack/react-router";
import { Check } from "lucide-react";
import { useState } from "react";

import { CodeViewer } from "@/components/ui/CodeViewer";
import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { DataTable } from "@/components/ui/DataTable";
import { DetailRow } from "@/components/ui/DetailRow";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { SkeletonRows } from "@/components/ui/SkeletonRows";
import { CreateIntegrationModal } from "@/features/integrations/CreateIntegrationModal";
import { integrationColumns } from "@/features/integrations/columns";
import { ApiError } from "@/lib/api-client";
import type { IntegrationSummary } from "@/lib/api-types";

import { useProviderDefinition, useProviderIntegrations } from "./api";

/** ProviderDefinitionDetailPage is Slice 6's config-heavy full-page detail
 * (AC2): a provider definition's full versioned Bundle rendered in a
 * collapsible, copyable mono JSON/YAML viewer — a full page rather than a
 * drawer, per DESIGN.md §0#4's confirmed drill pattern for config-heavy
 * surfaces (the same pattern Trigger Instances uses). Read-only over the
 * bundle itself; the "add integration" action (moved here from the
 * installation-wide Providers list) and the Integrations section below let
 * an operator see and grow this provider's installation-level integrations
 * without leaving the page. */
export function ProviderDefinitionDetailPage() {
  const { slug } = useParams({ from: "/providers/$slug" });
  const { data: detail, isLoading, isError, error, refetch } = useProviderDefinition(slug);
  const {
    items: integrations,
    isLoading: isLoadingIntegrations,
    isError: isIntegrationsError,
    error: integrationsError,
    refetch: refetchIntegrations,
  } = useProviderIntegrations(slug);
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [createdIntegration, setCreatedIntegration] = useState<IntegrationSummary | null>(null);

  if (isLoading) {
    return <p className="text-sm text-text-secondary">Loading…</p>;
  }

  if (isError || !detail) {
    return <ErrorCard message={errorMessage(error)} onRetry={refetch} />;
  }

  function handleCreated(integration: IntegrationSummary) {
    setCreatedIntegration(integration);
    refetchIntegrations();
  }

  return (
    <div className="mx-auto flex max-w-[1040px] flex-col gap-6">
      <div>
        <Link to="/providers" search={(prev) => prev} className="text-sm text-text-secondary transition-colors hover:text-text">
          ← Providers
        </Link>
        <div className="mt-2 flex flex-wrap items-start justify-between gap-3">
          <div className="flex flex-wrap items-center gap-3">
            {detail.bundle.logo ? (
              <img src={detail.bundle.logo} alt="" className="size-6 shrink-0 rounded-sm" aria-hidden="true" />
            ) : null}
            <h1 className="text-2xl font-semibold text-text">{detail.name}</h1>
          </div>
          <button
            type="button"
            onClick={() => setIsCreateOpen(true)}
            className="min-h-11 shrink-0 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover cursor-pointer"
          >
            Add integration
          </button>
        </div>
        <div className="mt-2">
          <CopyIdChip id={detail.slug} />
        </div>
      </div>

      {createdIntegration ? (
        <div
          role="status"
          className="flex items-center gap-2 rounded-md border border-border bg-success-bg px-4 py-3 text-sm text-success-text"
        >
          <Check className="size-4 shrink-0" aria-hidden="true" />
          <span>Integration “{createdIntegration.name}” created.</span>
        </div>
      ) : null}

      <section className="rounded-lg border border-border bg-surface p-4">
        <h2 className="mb-3 text-sm font-semibold text-text">Overview</h2>
        <dl className="grid grid-cols-1 gap-4 sm:grid-cols-4">
          <DetailRow label="Auth scheme">
            <span className="text-text">{detail.bundle.authScheme}</span>
          </DetailRow>
          <DetailRow label="Format version">
            <span className="font-mono text-sm text-text">{detail.formatVersion}</span>
          </DetailRow>
          <DetailRow label="Tools">
            <span className="font-mono text-sm text-text">{detail.bundle.tools.length}</span>
          </DetailRow>
          <DetailRow label="Triggers">
            <span className="font-mono text-sm text-text">{detail.bundle.triggers.length}</span>
          </DetailRow>
        </dl>
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-sm font-semibold text-text">Integrations ({integrations.length})</h2>
        {isIntegrationsError ? (
          <ErrorCard message={integrationsErrorMessage(integrationsError)} onRetry={refetchIntegrations} />
        ) : (
          <DataTable
            caption={`${detail.name} integrations`}
            columns={integrationColumns}
            data={integrations}
            isLoading={isLoadingIntegrations}
            loadingRows={<SkeletonRows columns={integrationColumns.length} />}
            emptyState={
              <EmptyState
                title="No integrations yet"
                description="Add an integration to register this provider's OAuth client credentials for this installation."
              />
            }
          />
        )}
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="text-sm font-semibold text-text">Definition bundle</h2>
        {detail.bundle.tools.length === 0 && detail.bundle.triggers.length === 0 ? (
          <EmptyState title="No tools or triggers declared" description="This provider declares no tools or triggers." />
        ) : null}
        <CodeViewer label={`${detail.name} bundle`} value={JSON.stringify(detail.bundle)} />
      </section>

      <CreateIntegrationModal
        open={isCreateOpen}
        onOpenChange={setIsCreateOpen}
        onCreated={handleCreated}
        providerSlug={detail.slug}
        providerName={detail.name}
      />
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "This provider definition could not be loaded.";
  }
  return "This provider definition could not be loaded.";
}

function integrationsErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "This provider's integrations could not be loaded.";
  }
  return "This provider's integrations could not be loaded.";
}
