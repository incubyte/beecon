import * as Tabs from "@radix-ui/react-tabs";
import { useSearch } from "@tanstack/react-router";

import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { ApiError } from "@/lib/api-client";

import { useGovernance, useIntegrationsWithVisibility } from "./api";
import { AllowListSection } from "./AllowListSection";
import { OnboardingSection } from "./OnboardingSection";
import { useGovernanceForm } from "./useGovernanceForm";
import { VisibilitySection } from "./VisibilitySection";

const tabTriggerClass =
  "min-h-11 rounded-md px-3 text-sm font-medium text-text-secondary transition-colors hover:bg-surface-muted hover:text-text data-[state=active]:bg-primary/10 data-[state=active]:text-primary cursor-pointer";

/** GovernancePage is Slice 5's GOVERN > Governance surface: the core-risk
 * seam's operator editor. Progressive disclosure over tabs (AC9, DESIGN.md
 * §1#5) — allow-list, per-integration visibility, and onboarding each get
 * their own section rather than one mega-form — while the Save action bar
 * stays visible across every tab (destructive/primary actions are never
 * hidden). Governance is org-scoped, so every read/write is disabled until
 * an organization is selected. */
export function GovernancePage() {
  const search = useSearch({ from: "__root__" });
  const orgId = search.org;

  const governanceQuery = useGovernance(orgId);
  const catalogQuery = useIntegrationsWithVisibility(orgId);
  const form = useGovernanceForm(orgId ?? "", governanceQuery.data);

  if (!orgId) {
    return (
      <EmptyState
        title="Select an organization"
        description="Choose an organization from the top-bar switcher to manage its governance."
      />
    );
  }

  // isError is checked BEFORE the loading/!form.form guard below: when
  // governanceQuery fails, its data stays undefined, so useGovernanceForm
  // never populates form.form — a loading check that also waits on
  // `!form.form` would then never clear, stranding the page on "Loading
  // governance…" forever with the ErrorCard's Retry unreachable. Checking
  // isError first guarantees a failed query always reaches the ErrorCard.
  if (governanceQuery.isError || catalogQuery.isError) {
    return (
      <ErrorCard
        message={errorMessage(governanceQuery.error ?? catalogQuery.error)}
        onRetry={() => {
          void governanceQuery.refetch();
          void catalogQuery.refetch();
        }}
      />
    );
  }

  if (governanceQuery.isLoading || catalogQuery.isLoading || !form.form) {
    return <p className="text-sm text-text-secondary">Loading governance…</p>;
  }

  const integrations = catalogQuery.data ?? [];
  const state = form.form;

  return (
    <div className="flex flex-col gap-4 pb-20">
      <div>
        <h1 className="text-2xl font-semibold text-text">Governance</h1>
        <p className="text-sm text-text-secondary">
          Control which integrations this organization can see and connect to (Slice 5).
        </p>
      </div>

      <Tabs.Root defaultValue="allow-list" className="flex flex-col gap-4">
        <Tabs.List aria-label="Governance sections" className="flex gap-1 border-b border-border">
          <Tabs.Trigger value="allow-list" className={tabTriggerClass}>
            Allow-list
          </Tabs.Trigger>
          <Tabs.Trigger value="visibility" className={tabTriggerClass}>
            Visibility
          </Tabs.Trigger>
          <Tabs.Trigger value="onboarding" className={tabTriggerClass}>
            Onboarding
          </Tabs.Trigger>
        </Tabs.List>

        <Tabs.Content value="allow-list">
          <AllowListSection
            integrations={integrations}
            allowListEnabled={state.allowListEnabled}
            allowList={state.allowList}
            onToggleEnabled={form.setAllowListEnabled}
            onToggleMember={form.setAllowListMember}
          />
        </Tabs.Content>

        <Tabs.Content value="visibility">
          <VisibilitySection integrations={integrations} hidden={state.hidden} onToggleHidden={form.setHidden} />
        </Tabs.Content>

        <Tabs.Content value="onboarding">
          <OnboardingSection
            integrations={integrations}
            featured={state.featured}
            cap={state.cap}
            onAdd={form.addFeatured}
            onRemove={form.removeFeatured}
            onMove={form.moveFeatured}
            onCapChange={form.setCap}
          />
        </Tabs.Content>
      </Tabs.Root>

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
    return error.message || "Governance could not be loaded.";
  }
  return "Governance could not be loaded.";
}
