import { useEffect, useState } from "react";

import type { Governance, GovernanceUpdate } from "@/lib/api-types";

import { useUpdateGovernance } from "./api";

/** GovernanceFormState is the governance editor's local, editable mirror of
 * the fetched Governance (Slice 5): the whole-object PUT convention
 * (mirrors UpdateAllowedRedirectURIs) means every section edits this one
 * local draft, and a single Save action bar persists all of it at once. */
export interface GovernanceFormState {
  allowListEnabled: boolean;
  allowList: string[];
  hidden: string[];
  featured: string[];
  cap: number;
}

function toFormState(governance: Governance): GovernanceFormState {
  return {
    allowListEnabled: governance.allowList !== null,
    allowList: governance.allowList ?? [],
    hidden: governance.hidden,
    featured: governance.onboarding.featured,
    cap: governance.onboarding.cap,
  };
}

function toUpdate(state: GovernanceFormState): GovernanceUpdate {
  return {
    allowList: state.allowListEnabled ? state.allowList : null,
    hidden: state.hidden,
    onboarding: { featured: state.featured, cap: state.cap },
  };
}

function toggleMembership(list: string[], id: string, member: boolean): string[] {
  if (member) {
    return list.includes(id) ? list : [...list, id];
  }
  return list.filter((existing) => existing !== id);
}

/** useGovernanceForm holds the governance editor's local draft, seeded from
 * the fetched Governance and re-seeded whenever a fresh fetch (or a
 * successful save) supplies a new one, plus the section-scoped setters each
 * tab (allow-list, visibility, onboarding) mutates through. */
export function useGovernanceForm(orgId: string, governance: Governance | undefined) {
  const updateGovernance = useUpdateGovernance(orgId);
  const [form, setForm] = useState<GovernanceFormState | null>(null);

  useEffect(() => {
    if (governance) {
      setForm(toFormState(governance));
    }
  }, [governance]);

  function setAllowListEnabled(enabled: boolean) {
    setForm((prev) => (prev ? { ...prev, allowListEnabled: enabled } : prev));
  }

  function setAllowListMember(integrationId: string, allowed: boolean) {
    setForm((prev) => (prev ? { ...prev, allowList: toggleMembership(prev.allowList, integrationId, allowed) } : prev));
  }

  function setHidden(integrationId: string, hidden: boolean) {
    setForm((prev) => (prev ? { ...prev, hidden: toggleMembership(prev.hidden, integrationId, hidden) } : prev));
  }

  function addFeatured(integrationId: string) {
    setForm((prev) => {
      if (!prev || prev.featured.includes(integrationId) || prev.featured.length >= prev.cap) {
        return prev;
      }
      return { ...prev, featured: [...prev.featured, integrationId] };
    });
  }

  function removeFeatured(integrationId: string) {
    setForm((prev) => (prev ? { ...prev, featured: prev.featured.filter((id) => id !== integrationId) } : prev));
  }

  function moveFeatured(integrationId: string, direction: -1 | 1) {
    setForm((prev) => {
      if (!prev) return prev;
      const index = prev.featured.indexOf(integrationId);
      const targetIndex = index + direction;
      if (index < 0 || targetIndex < 0 || targetIndex >= prev.featured.length) {
        return prev;
      }
      const next = [...prev.featured];
      [next[index], next[targetIndex]] = [next[targetIndex], next[index]];
      return { ...prev, featured: next };
    });
  }

  function setCap(cap: number) {
    setForm((prev) => (prev ? { ...prev, cap: Math.max(1, cap) } : prev));
  }

  function save() {
    if (!form) return;
    updateGovernance.mutate(toUpdate(form));
  }

  return {
    form,
    setAllowListEnabled,
    setAllowListMember,
    setHidden,
    addFeatured,
    removeFeatured,
    moveFeatured,
    setCap,
    save,
    isSaving: updateGovernance.isPending,
    isError: updateGovernance.isError,
    error: updateGovernance.error,
  };
}
