import { useId } from "react";

import { eventTypeLabel, KNOWN_EVENT_TYPES } from "./eventTypes";

export interface EventTypeFilterEditorProps {
  /** null = match every event type (PD45's continuity-preserving default);
   * a non-null array restricts fan-out to exactly those types. */
  value: string[] | null;
  onChange: (value: string[] | null) => void;
}

/** EventTypeFilterEditor is Slice 8's per-endpoint event-type filter editor
 * (AC3): an "all event types" toggle plus one checkbox per known type,
 * shared by the create and edit flows so both build the identical
 * `eventTypes: string[] | null` shape the API expects. */
export function EventTypeFilterEditor({ value, onChange }: EventTypeFilterEditorProps) {
  const groupId = useId();
  const isFiltered = value !== null;

  function handleToggleFiltered(filtered: boolean) {
    onChange(filtered ? [] : null);
  }

  function handleToggleType(eventType: string, checked: boolean) {
    const current = value ?? [];
    const next = checked ? [...current, eventType] : current.filter((t) => t !== eventType);
    onChange(next);
  }

  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="text-sm font-medium text-text">Event types</legend>

      <label className="flex items-center gap-2 text-sm text-text">
        <input
          type="radio"
          name={`${groupId}-mode`}
          checked={!isFiltered}
          onChange={() => handleToggleFiltered(false)}
          className="size-4 shrink-0 cursor-pointer"
        />
        All event types
      </label>
      <label className="flex items-center gap-2 text-sm text-text">
        <input
          type="radio"
          name={`${groupId}-mode`}
          checked={isFiltered}
          onChange={() => handleToggleFiltered(true)}
          className="size-4 shrink-0 cursor-pointer"
        />
        Only specific event types
      </label>

      {isFiltered ? (
        <div className="ml-6 flex flex-col gap-1.5 border-l border-border pl-3">
          {KNOWN_EVENT_TYPES.map((eventType) => (
            <label key={eventType} className="flex items-center gap-2 text-sm text-text-secondary">
              <input
                type="checkbox"
                checked={(value ?? []).includes(eventType)}
                onChange={(event) => handleToggleType(eventType, event.target.checked)}
                className="size-4 shrink-0 cursor-pointer rounded border-border-strong"
              />
              {eventTypeLabel(eventType)}
            </label>
          ))}
          {value !== null && value.length === 0 ? (
            <p className="text-xs text-warning-text">Select at least one event type, or switch back to "All event types".</p>
          ) : null}
        </div>
      ) : null}
    </fieldset>
  );
}
