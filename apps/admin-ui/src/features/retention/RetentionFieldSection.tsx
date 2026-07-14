import { useId } from "react";

import type { RetentionFieldState, RetentionMode } from "./useRetentionForm";

export interface RetentionFieldSectionProps {
  legend: string;
  description: string;
  installationDefaultDays: number;
  minDays: number;
  state: RetentionFieldState;
  onModeChange: (mode: RetentionMode) => void;
  onDaysChange: (days: number) => void;
}

/** RetentionFieldSection is one entity kind's (logs or events) retention
 * editor (Slice 7, AC1/AC4/AC5): a three-way radio choice — inherit the
 * installation default, a custom day count, or unlimited (never purge) —
 * with the custom day input only shown (and only editable) when "Custom" is
 * selected. Each option's consequence is spelled out in its own label text
 * rather than relying on the radio's position alone, and the "0 = unlimited"
 * / "inherit default (N)" semantics are both named explicitly per DESIGN.md's
 * request that this be shown clearly. */
export function RetentionFieldSection({
  legend,
  description,
  installationDefaultDays,
  minDays,
  state,
  onModeChange,
  onDaysChange,
}: RetentionFieldSectionProps) {
  const groupId = useId();
  const daysInputId = useId();

  return (
    <fieldset className="flex flex-col gap-3 rounded-lg border border-border bg-surface p-4">
      <legend className="px-1 text-sm font-semibold text-text">{legend}</legend>
      <p className="text-sm text-text-secondary">{description}</p>

      <label className="flex min-h-11 cursor-pointer items-center gap-3 text-sm text-text">
        <input
          type="radio"
          name={groupId}
          checked={state.mode === "inherit"}
          onChange={() => onModeChange("inherit")}
          className="size-4 cursor-pointer"
        />
        <span>
          Inherit installation default (<span className="font-mono">{installationDefaultDays}</span> days)
        </span>
      </label>

      <label className="flex min-h-11 cursor-pointer items-center gap-3 text-sm text-text">
        <input
          type="radio"
          name={groupId}
          checked={state.mode === "custom"}
          onChange={() => onModeChange("custom")}
          className="size-4 cursor-pointer"
        />
        <span>Custom window</span>
      </label>
      {state.mode === "custom" ? (
        <label htmlFor={daysInputId} className="ml-7 flex max-w-xs flex-col gap-1 text-xs text-text-secondary">
          Days to keep
          <input
            id={daysInputId}
            type="number"
            min={minDays}
            value={state.days}
            onChange={(event) => onDaysChange(Number(event.target.value) || minDays)}
            className="min-h-11 rounded-md border border-border-strong bg-surface px-3 font-mono text-sm text-text focus-visible:border-primary"
          />
        </label>
      ) : null}

      <label className="flex min-h-11 cursor-pointer items-center gap-3 text-sm text-text">
        <input
          type="radio"
          name={groupId}
          checked={state.mode === "unlimited"}
          onChange={() => onModeChange("unlimited")}
          className="size-4 cursor-pointer"
        />
        <span>Unlimited (0 = never purge, disables purging for this organization)</span>
      </label>
    </fieldset>
  );
}
