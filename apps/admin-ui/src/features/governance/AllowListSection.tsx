import * as Switch from "@radix-ui/react-switch";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { EmptyState } from "@/components/ui/EmptyState";
import type { IntegrationVisibility } from "@/lib/api-types";

export interface AllowListSectionProps {
  integrations: IntegrationVisibility[];
  allowListEnabled: boolean;
  allowList: string[];
  onToggleEnabled: (enabled: boolean) => void;
  onToggleMember: (integrationId: string, allowed: boolean) => void;
}

/** AllowListSection is the governance editor's allow-list tab (Slice 5,
 * AC2/AC3): a top-level switch chooses between "inherit the full catalog"
 * (AllowList null, PD42's continuity default) and "restrict to an
 * allow-list" — only when restricting does the per-integration checkbox
 * list become the primary action; the "inherit all" state stays a single,
 * always-visible sentence rather than a wall of disabled checkboxes. */
export function AllowListSection({
  integrations,
  allowListEnabled,
  allowList,
  onToggleEnabled,
  onToggleMember,
}: AllowListSectionProps) {
  return (
    <div className="flex flex-col gap-4">
      <label className="flex min-h-11 cursor-pointer items-center gap-3 rounded-lg border border-border bg-surface px-4 py-3">
        <Switch.Root
          checked={allowListEnabled}
          onCheckedChange={onToggleEnabled}
          aria-label="Restrict this organization to an allow-list"
          className="relative h-6 w-10 shrink-0 rounded-pill bg-surface-muted transition-colors data-[state=checked]:bg-primary"
        >
          <Switch.Thumb className="block size-4.5 translate-x-0.5 rounded-full bg-surface shadow-sm transition-transform duration-150 will-change-transform motion-reduce:transition-none data-[state=checked]:translate-x-4.5" />
        </Switch.Root>
        <span className="text-sm text-text">
          <span className="block font-medium">Restrict to an allow-list</span>
          <span className="block text-text-secondary">
            {allowListEnabled
              ? "Only the integrations checked below are visible to this organization."
              : "Off: this organization sees the full installation catalog (today's default, unchanged until you turn this on)."}
          </span>
        </span>
      </label>

      {allowListEnabled ? (
        integrations.length === 0 ? (
          <EmptyState title="No integrations yet" description="Create an integration before curating this org's allow-list." />
        ) : (
          <ul className="flex flex-col divide-y divide-border rounded-lg border border-border bg-surface">
            {integrations.map((integration) => (
              <li key={integration.id} className="flex items-center justify-between gap-4 px-4 py-3">
                <label className="flex min-h-11 flex-1 cursor-pointer items-center gap-3 text-sm text-text">
                  <input
                    type="checkbox"
                    checked={allowList.includes(integration.id)}
                    onChange={(event) => onToggleMember(integration.id, event.target.checked)}
                    className="size-4 cursor-pointer"
                  />
                  <span>
                    <span className="block font-medium">{integration.name}</span>
                    <span className="block text-xs text-text-secondary">{integration.providerSlug}</span>
                  </span>
                </label>
                <CopyIdChip id={integration.id} />
              </li>
            ))}
          </ul>
        )
      ) : null}
    </div>
  );
}
