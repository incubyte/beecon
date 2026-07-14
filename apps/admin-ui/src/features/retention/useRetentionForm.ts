import { useEffect, useState } from "react";

import type { Retention, RetentionUpdate } from "@/lib/api-types";

import { useUpdateRetention } from "./api";

/** RetentionMode is one field's (logs or events) three mutually-exclusive
 * states (Slice 7, AC1/AC4): "inherit" mirrors the server's own null
 * (inherit the installation default); "unlimited" mirrors the server's own
 * 0 (never purge this org's rows for this entity kind, regardless of age);
 * "custom" is a positive day count this org overrides the installation
 * default with. */
export type RetentionMode = "inherit" | "unlimited" | "custom";

/** RetentionFieldState is one field's (logs or events) local, editable
 * mirror of the fetched Retention. */
export interface RetentionFieldState {
  mode: RetentionMode;
  days: number;
}

export interface RetentionFormState {
  logs: RetentionFieldState;
  events: RetentionFieldState;
}

function modeFor(days: number | null): RetentionMode {
  if (days === null) return "inherit";
  if (days === 0) return "unlimited";
  return "custom";
}

function fieldStateFor(days: number | null, installationDefaultDays: number): RetentionFieldState {
  return {
    mode: modeFor(days),
    days: days !== null && days > 0 ? days : installationDefaultDays,
  };
}

function toFormState(retention: Retention): RetentionFormState {
  return {
    logs: fieldStateFor(retention.logDays, retention.installationDefaultDays),
    events: fieldStateFor(retention.eventDays, retention.installationDefaultDays),
  };
}

function fieldToDaysValue(field: RetentionFieldState): number | null {
  if (field.mode === "inherit") return null;
  if (field.mode === "unlimited") return 0;
  return field.days;
}

function toUpdate(state: RetentionFormState): RetentionUpdate {
  return {
    logDays: fieldToDaysValue(state.logs),
    eventDays: fieldToDaysValue(state.events),
  };
}

/** useRetentionForm holds the retention editor's local draft, seeded from
 * the fetched Retention and re-seeded whenever a fresh fetch (or a
 * successful save) supplies a new one, plus the per-field (logs/events)
 * setters the page's two sections mutate through — mirroring
 * useGovernanceForm's own shape. */
export function useRetentionForm(orgId: string, retention: Retention | undefined) {
  const updateRetention = useUpdateRetention(orgId);
  const [form, setForm] = useState<RetentionFormState | null>(null);

  useEffect(() => {
    if (retention) {
      setForm(toFormState(retention));
    }
  }, [retention]);

  function setLogsMode(mode: RetentionMode) {
    setForm((prev) => (prev ? { ...prev, logs: { ...prev.logs, mode } } : prev));
  }

  function setLogsDays(days: number) {
    setForm((prev) => (prev ? { ...prev, logs: { ...prev.logs, days: Math.max(1, days) } } : prev));
  }

  function setEventsMode(mode: RetentionMode) {
    setForm((prev) => (prev ? { ...prev, events: { ...prev.events, mode } } : prev));
  }

  function setEventsDays(days: number) {
    setForm((prev) => (prev ? { ...prev, events: { ...prev.events, days: Math.max(1, days) } } : prev));
  }

  function save() {
    if (!form) return;
    updateRetention.mutate(toUpdate(form));
  }

  return {
    form,
    setLogsMode,
    setLogsDays,
    setEventsMode,
    setEventsDays,
    save,
    isSaving: updateRetention.isPending,
    isError: updateRetention.isError,
    error: updateRetention.error,
  };
}
