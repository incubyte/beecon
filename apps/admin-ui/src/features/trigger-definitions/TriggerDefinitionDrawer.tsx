import { CodeViewer } from "@/components/ui/CodeViewer";
import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { DetailRow } from "@/components/ui/DetailRow";
import { Drawer } from "@/components/ui/Drawer";
import type { CatalogTriggerDefinition } from "@/lib/api-types";

export interface TriggerDefinitionDrawerProps {
  trigger: CatalogTriggerDefinition | null;
  onClose: () => void;
}

/** TriggerDefinitionDrawer is Slice 6's right-side detail panel for one
 * trigger definition (AC4): ingestion mode plus its config schema and
 * payload schema, each rendered through CodeViewer so structure stays
 * legible in grayscale (AC6). */
export function TriggerDefinitionDrawer({ trigger, onClose }: TriggerDefinitionDrawerProps) {
  return (
    <Drawer
      open={trigger !== null}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
      title="Trigger definition detail"
      description={trigger ? <CopyIdChip id={trigger.slug} /> : undefined}
    >
      {trigger ? (
        <div className="flex flex-col gap-5">
          <dl className="flex flex-col gap-4">
            <DetailRow label="Name">
              <span className="text-text">{trigger.name}</span>
            </DetailRow>
            <DetailRow label="Provider">
              <span className="text-text">{trigger.providerName}</span>
            </DetailRow>
            {trigger.description ? (
              <DetailRow label="Description">
                <span className="text-text-secondary">{trigger.description}</span>
              </DetailRow>
            ) : null}
            <DetailRow label="Ingestion mode">
              <span className="font-mono text-sm text-text">{trigger.ingestion}</span>
            </DetailRow>
            <DetailRow label="Poll interval">
              <span className="font-mono text-sm text-text">{trigger.pollIntervalSeconds}s</span>
            </DetailRow>
          </dl>

          <CodeViewer label="Config schema" value={JSON.stringify(trigger.configSchema)} />
          <CodeViewer label="Payload schema" value={JSON.stringify(trigger.payloadSchema)} />
        </div>
      ) : null}
    </Drawer>
  );
}
