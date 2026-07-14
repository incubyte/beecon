import { ChevronDown, ChevronUp, X } from "lucide-react";
import { useId } from "react";

import { EmptyState } from "@/components/ui/EmptyState";
import type { IntegrationVisibility } from "@/lib/api-types";

export interface OnboardingSectionProps {
  integrations: IntegrationVisibility[];
  featured: string[];
  cap: number;
  onAdd: (integrationId: string) => void;
  onRemove: (integrationId: string) => void;
  onMove: (integrationId: string, direction: -1 | 1) => void;
  onCapChange: (cap: number) => void;
}

/** OnboardingSection is the governance editor's onboarding tab (Slice 5,
 * AC7, PD43): an ordered "featured" integration list capped at a
 * configurable count (default 8) — the subset the consumer catalog's
 * `?featured=true` filter returns, in exactly this order. Reordering uses
 * up/down buttons (keyboard-operable, no drag-and-drop dependency) rather
 * than pointer-only drag handles. */
export function OnboardingSection({ integrations, featured, cap, onAdd, onRemove, onMove, onCapChange }: OnboardingSectionProps) {
  const capId = useId();
  const byId = new Map(integrations.map((integration) => [integration.id, integration]));
  const candidates = integrations.filter((integration) => !featured.includes(integration.id));
  const atCap = featured.length >= cap;

  return (
    <div className="flex flex-col gap-4">
      <label htmlFor={capId} className="flex max-w-xs flex-col gap-1 text-sm text-text-secondary">
        Featured cap
        <input
          id={capId}
          type="number"
          min={1}
          value={cap}
          onChange={(event) => onCapChange(Number(event.target.value) || 1)}
          className="min-h-11 rounded-md border border-border-strong bg-surface px-3 font-mono text-sm text-text focus-visible:border-primary"
        />
        <span className="text-xs text-text-muted">
          {featured.length} of {cap} featured slots used.
        </span>
      </label>

      {featured.length === 0 ? (
        <EmptyState
          title="No featured integrations yet"
          description="Without a featured list, onboarding falls back to the first visible integrations."
        />
      ) : (
        <ol className="flex flex-col divide-y divide-border rounded-lg border border-border bg-surface">
          {featured.map((integrationId, index) => {
            const integration = byId.get(integrationId);
            return (
              <li key={integrationId} className="flex items-center justify-between gap-4 px-4 py-3">
                <span className="text-sm font-medium text-text">
                  {index + 1}. {integration?.name ?? integrationId}
                </span>
                <span className="flex items-center gap-1">
                  <button
                    type="button"
                    onClick={() => onMove(integrationId, -1)}
                    disabled={index === 0}
                    aria-label={`Move ${integration?.name ?? integrationId} up`}
                    className="flex min-h-11 min-w-11 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface-muted hover:text-text disabled:cursor-not-allowed disabled:opacity-40 cursor-pointer"
                  >
                    <ChevronUp className="size-4" aria-hidden="true" />
                  </button>
                  <button
                    type="button"
                    onClick={() => onMove(integrationId, 1)}
                    disabled={index === featured.length - 1}
                    aria-label={`Move ${integration?.name ?? integrationId} down`}
                    className="flex min-h-11 min-w-11 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface-muted hover:text-text disabled:cursor-not-allowed disabled:opacity-40 cursor-pointer"
                  >
                    <ChevronDown className="size-4" aria-hidden="true" />
                  </button>
                  <button
                    type="button"
                    onClick={() => onRemove(integrationId)}
                    aria-label={`Remove ${integration?.name ?? integrationId} from featured`}
                    className="flex min-h-11 min-w-11 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-error-bg hover:text-error-text cursor-pointer"
                  >
                    <X className="size-4" aria-hidden="true" />
                  </button>
                </span>
              </li>
            );
          })}
        </ol>
      )}

      {candidates.length > 0 ? (
        <label className="flex max-w-sm flex-col gap-1 text-sm text-text-secondary">
          Add to featured
          <select
            value=""
            disabled={atCap}
            onChange={(event) => {
              if (event.target.value) {
                onAdd(event.target.value);
              }
            }}
            className="min-h-11 rounded-md border border-border-strong bg-surface px-3 text-sm text-text focus-visible:border-primary disabled:cursor-not-allowed disabled:opacity-60"
          >
            <option value="">{atCap ? "Featured cap reached" : "Choose an integration…"}</option>
            {candidates.map((integration) => (
              <option key={integration.id} value={integration.id}>
                {integration.name}
              </option>
            ))}
          </select>
        </label>
      ) : null}
    </div>
  );
}
