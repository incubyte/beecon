import { useSearch } from "@tanstack/react-router";

import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { ApiError } from "@/lib/api-client";

import { useRetention } from "./api";
import { RetentionFieldSection } from "./RetentionFieldSection";
import { useRetentionForm } from "./useRetentionForm";

/** minRetentionDays mirrors organizations.MinRetentionDays
 * (server/internal/organizations/types.go) — the platform-wide floor a
 * custom, non-zero retention window must clear (Slice 7, AC5). 0 (unlimited)
 * is a separate radio option below and is never subject to this floor. */
const minRetentionDays = 1;

/** RetentionPage is Slice 7's GOVERN > Settings > Retention surface: per-org
 * log/event retention windows, each an inherit-default/custom/unlimited
 * choice (AC1/AC4/AC5). Retention is org-scoped, so every read/write is
 * disabled until an organization is selected — mirroring GovernancePage's
 * own shape exactly, since both editors replace one shared org_governance
 * settings row (FD8). */
export function RetentionPage() {
  const search = useSearch({ from: "__root__" });
  const orgId = search.org;

  const retentionQuery = useRetention(orgId);
  const form = useRetentionForm(orgId ?? "", retentionQuery.data);

  if (!orgId) {
    return (
      <EmptyState
        title="Select an organization"
        description="Choose an organization from the top-bar switcher to manage its retention windows."
      />
    );
  }

  if (retentionQuery.isError) {
    return <ErrorCard message={errorMessage(retentionQuery.error)} onRetry={() => void retentionQuery.refetch()} />;
  }

  if (retentionQuery.isLoading || !form.form) {
    return <p className="text-sm text-text-secondary">Loading retention settings…</p>;
  }

  const installationDefaultDays = retentionQuery.data?.installationDefaultDays ?? minRetentionDays;
  const state = form.form;

  return (
    <div className="flex flex-col gap-4 pb-20">
      <div>
        <h1 className="text-2xl font-semibold text-text">Retention</h1>
        <p className="text-sm text-text-secondary">
          Control how long this organization's logs and events are kept before the purge worker removes them (Slice
          7).
        </p>
      </div>

      <div className="flex flex-col gap-4">
        <RetentionFieldSection
          legend="Logs"
          description="How long redacted event-log entries are kept before the purge worker hard-deletes them."
          installationDefaultDays={installationDefaultDays}
          minDays={minRetentionDays}
          state={state.logs}
          onModeChange={form.setLogsMode}
          onDaysChange={form.setLogsDays}
        />

        <RetentionFieldSection
          legend="Events"
          description="How long delivered, failed, or no-endpoint outbox events are kept. Pending or in-flight events are never purged, regardless of age or this window."
          installationDefaultDays={installationDefaultDays}
          minDays={minRetentionDays}
          state={state.events}
          onModeChange={form.setEventsMode}
          onDaysChange={form.setEventsDays}
        />
      </div>

      <div className="sticky bottom-0 z-10 -mx-6 flex items-center justify-between gap-4 border-t border-border bg-surface px-6 py-3">
        <div className="text-sm">
          {form.isError ? <span className="text-error-text">{errorMessage(form.error)}</span> : null}
        </div>
        <button
          type="button"
          onClick={form.save}
          disabled={form.isSaving}
          className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
        >
          {form.isSaving ? "Saving…" : "Save changes"}
        </button>
      </div>
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "Retention settings could not be loaded.";
  }
  return "Retention settings could not be loaded.";
}
