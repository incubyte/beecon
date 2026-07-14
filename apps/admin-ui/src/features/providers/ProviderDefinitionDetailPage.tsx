import { Link, useParams } from "@tanstack/react-router";

import { CodeViewer } from "@/components/ui/CodeViewer";
import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { DetailRow } from "@/components/ui/DetailRow";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { ApiError } from "@/lib/api-client";

import { useProviderDefinition } from "./api";

/** ProviderDefinitionDetailPage is Slice 6's config-heavy full-page detail
 * (AC2): a provider definition's full versioned Bundle rendered in a
 * collapsible, copyable mono JSON/YAML viewer — a full page rather than a
 * drawer, per DESIGN.md §0#4's confirmed drill pattern for config-heavy
 * surfaces (the same pattern Trigger Instances uses). Read-only: there is no
 * write path over provider definitions this phase. */
export function ProviderDefinitionDetailPage() {
  const { slug } = useParams({ from: "/providers/$slug" });
  const { data: detail, isLoading, isError, error, refetch } = useProviderDefinition(slug);

  if (isLoading) {
    return <p className="text-sm text-text-secondary">Loading…</p>;
  }

  if (isError || !detail) {
    return <ErrorCard message={errorMessage(error)} onRetry={refetch} />;
  }

  return (
    <div className="mx-auto flex max-w-[1040px] flex-col gap-6">
      <div>
        <Link to="/providers" search={(prev) => prev} className="text-sm text-text-secondary transition-colors hover:text-text">
          ← Providers
        </Link>
        <div className="mt-2 flex flex-wrap items-center gap-3">
          {detail.bundle.logo ? (
            <img src={detail.bundle.logo} alt="" className="size-6 shrink-0 rounded-sm" aria-hidden="true" />
          ) : null}
          <h1 className="text-2xl font-semibold text-text">{detail.name}</h1>
        </div>
        <div className="mt-2">
          <CopyIdChip id={detail.slug} />
        </div>
      </div>

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
        <h2 className="text-sm font-semibold text-text">Definition bundle</h2>
        {detail.bundle.tools.length === 0 && detail.bundle.triggers.length === 0 ? (
          <EmptyState title="No tools or triggers declared" description="This provider declares no tools or triggers." />
        ) : null}
        <CodeViewer label={`${detail.name} bundle`} value={JSON.stringify(detail.bundle)} />
      </section>
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "This provider definition could not be loaded.";
  }
  return "This provider definition could not be loaded.";
}
