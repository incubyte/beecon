import * as Switch from "@radix-ui/react-switch";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { EmptyState } from "@/components/ui/EmptyState";
import { StatusBadge } from "@/components/ui/StatusBadge";
import type { IntegrationVisibility } from "@/lib/api-types";

export interface VisibilitySectionProps {
  integrations: IntegrationVisibility[];
  hidden: string[];
  onToggleHidden: (integrationId: string, hidden: boolean) => void;
}

/** VisibilitySection is the governance editor's per-integration visibility
 * tab (Slice 5, AC1/AC4): the installation catalog with each integration's
 * effective visibility (VISIBLE/HIDDEN/NOT_ALLOWED, color+icon+label —
 * never color-only), plus a "Hidden" switch for one-off exclusions that
 * don't require maintaining a whole allow-list. The badge reflects the
 * server's last-saved state; the switch reflects the unsaved local draft —
 * they can differ until Save is pressed, same as every other field here. */
export function VisibilitySection({ integrations, hidden, onToggleHidden }: VisibilitySectionProps) {
  if (integrations.length === 0) {
    return <EmptyState title="No integrations yet" description="Create an integration to manage its per-org visibility." />;
  }

  return (
    <table className="w-full border-collapse overflow-hidden rounded-lg border border-border bg-surface text-sm">
      <caption className="sr-only">Integration visibility for this organization</caption>
      <thead>
        <tr className="border-b border-border text-left text-xs font-medium tracking-wide text-text-muted uppercase">
          <th scope="col" className="px-4 py-3">
            Integration
          </th>
          <th scope="col" className="px-4 py-3">
            Effective visibility
          </th>
          <th scope="col" className="px-4 py-3">
            Hidden
          </th>
        </tr>
      </thead>
      <tbody className="divide-y divide-border">
        {integrations.map((integration) => (
          <tr key={integration.id}>
            <td className="px-4 py-3">
              <div className="font-medium text-text">{integration.name}</div>
              <div className="mt-1 flex items-center gap-2 text-xs text-text-secondary">
                <span>{integration.providerSlug}</span>
                <CopyIdChip id={integration.id} />
              </div>
            </td>
            <td className="px-4 py-3">
              <StatusBadge taxonomy="integrationVisibility" status={integration.visibility} />
            </td>
            <td className="px-4 py-3">
              <Switch.Root
                checked={hidden.includes(integration.id)}
                onCheckedChange={(checked) => onToggleHidden(integration.id, checked)}
                aria-label={`Hide ${integration.name} for this organization`}
                className="relative h-6 w-10 rounded-pill bg-surface-muted transition-colors data-[state=checked]:bg-warning-solid"
              >
                <Switch.Thumb className="block size-4.5 translate-x-0.5 rounded-full bg-surface shadow-sm transition-transform duration-150 will-change-transform motion-reduce:transition-none data-[state=checked]:translate-x-4.5" />
              </Switch.Root>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
