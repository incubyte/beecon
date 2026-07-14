import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import type { ReactNode } from "react";

import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { Timestamp } from "@/components/ui/Timestamp";
import { ApiError } from "@/lib/api-client";

import { useDeleteTriggerInstance, useSetTriggerInstanceStatus, useTriggerInstance } from "./api";

/** TriggerInstanceDetailPage is Slice 2's config-heavy full-page detail
 * (AC4: status, trigger slug, bound connection, and config) — a full page
 * rather than a drawer, per DESIGN.md §0#4's confirmed drill pattern for
 * config-heavy surfaces. Enable/disable (AC5) and delete (AC6, plain
 * ConfirmDialog) live here as the page's primary actions. */
export function TriggerInstanceDetailPage() {
  const { trgId } = useParams({ from: "/trigger-instances/$trgId" });
  const search = useSearch({ from: "__root__" });
  const orgId = search.org;
  const navigate = useNavigate();

  const { data: instance, isLoading, isError, error, refetch } = useTriggerInstance(orgId, trgId);
  const setStatus = useSetTriggerInstanceStatus(orgId ?? "");
  const deleteInstance = useDeleteTriggerInstance(orgId ?? "");

  if (!orgId) {
    return (
      <EmptyState
        title="Select an organization"
        description="Choose an organization from the top-bar switcher to see its trigger instances."
      />
    );
  }

  if (isLoading) {
    return <p className="text-sm text-text-secondary">Loading…</p>;
  }

  if (isError || !instance) {
    return <ErrorCard message={errorMessage(error)} onRetry={refetch} />;
  }

  function handleToggle() {
    setStatus.mutate({ instanceId: instance!.id, action: instance!.status === "ACTIVE" ? "disable" : "enable" });
  }

  function handleDelete() {
    deleteInstance.mutate(instance!.id, {
      onSuccess: () => void navigate({ to: "/trigger-instances", search: (prev) => prev }),
    });
  }

  return (
    <div className="mx-auto flex max-w-[1040px] flex-col gap-6">
      <div>
        <Link
          to="/trigger-instances"
          search={(prev) => prev}
          className="text-sm text-text-secondary transition-colors hover:text-text"
        >
          ← Trigger instances
        </Link>
        <div className="mt-2 flex flex-wrap items-center gap-3">
          <h1 className="text-2xl font-semibold text-text">{instance.triggerSlug}</h1>
          <StatusBadge taxonomy="triggerInstance" status={instance.status} />
        </div>
        <div className="mt-2">
          <CopyIdChip id={instance.id} />
        </div>
      </div>

      <section className="rounded-lg border border-border bg-surface p-4">
        <h2 className="mb-3 text-sm font-semibold text-text">Overview</h2>
        <dl className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <DetailRow label="Connection">
            <CopyIdChip id={instance.connectionId} />
          </DetailRow>
          <DetailRow label="User">
            <CopyIdChip id={instance.userId} />
          </DetailRow>
          <DetailRow label="Created">
            <Timestamp iso={instance.createdAt} />
          </DetailRow>
        </dl>
      </section>

      <section className="rounded-lg border border-border bg-surface p-4">
        <h2 className="mb-3 text-sm font-semibold text-text">Config</h2>
        <pre className="overflow-x-auto rounded-md bg-surface-muted p-3 font-mono text-xs text-text">
          {JSON.stringify(instance.config, null, 2)}
        </pre>
      </section>

      <section className="flex items-center gap-3">
        <button
          type="button"
          onClick={handleToggle}
          disabled={setStatus.isPending}
          className="min-h-11 rounded-md border border-border-strong px-4 text-sm font-medium text-text transition-colors hover:bg-surface-muted disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
        >
          {instance.status === "ACTIVE" ? "Disable" : "Enable"}
        </button>

        <ConfirmDialog
          trigger={
            <button
              type="button"
              className="min-h-11 rounded-md border border-error-solid/40 px-4 text-sm font-medium text-error-text transition-colors hover:bg-error-solid/10 cursor-pointer"
            >
              Delete trigger instance
            </button>
          }
          title="Delete this trigger instance?"
          description="This permanently removes the trigger instance. This cannot be undone."
          confirmLabel="Delete trigger instance"
          onConfirm={handleDelete}
          isConfirming={deleteInstance.isPending}
        />
      </section>
    </div>
  );
}

function DetailRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-xs font-medium tracking-wide text-text-muted uppercase">{label}</dt>
      <dd className="text-sm">{children}</dd>
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The trigger instance could not be loaded.";
  }
  return "The trigger instance could not be loaded.";
}
