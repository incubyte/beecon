import { CodeViewer } from "@/components/ui/CodeViewer";
import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { DetailRow } from "@/components/ui/DetailRow";
import { Drawer } from "@/components/ui/Drawer";
import type { CatalogTool } from "@/lib/api-types";

export interface ToolDrawerProps {
  tool: CatalogTool | null;
  onClose: () => void;
}

/** ToolDrawer is Slice 6's right-side detail panel for one tool (AC4): its
 * input and output JSON-Schema rendered through CodeViewer, the same mono
 * viewer the log/provider-definition surfaces use, so a schema's structure
 * stays legible in grayscale (AC6). */
export function ToolDrawer({ tool, onClose }: ToolDrawerProps) {
  return (
    <Drawer
      open={tool !== null}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
      title="Tool detail"
      description={tool ? <CopyIdChip id={tool.slug} /> : undefined}
    >
      {tool ? (
        <div className="flex flex-col gap-5">
          <dl className="flex flex-col gap-4">
            <DetailRow label="Name">
              <span className="text-text">{tool.name}</span>
            </DetailRow>
            <DetailRow label="Provider">
              <span className="text-text">{tool.providerName}</span>
            </DetailRow>
            {tool.description ? (
              <DetailRow label="Description">
                <span className="text-text-secondary">{tool.description}</span>
              </DetailRow>
            ) : null}
            <DetailRow label="Deprecated">
              <span className="text-text">{tool.deprecated ? "Yes" : "No"}</span>
            </DetailRow>
          </dl>

          <CodeViewer label="Input schema" value={JSON.stringify(tool.inputSchema)} />
          <CodeViewer label="Output schema" value={JSON.stringify(tool.outputSchema)} />
        </div>
      ) : null}
    </Drawer>
  );
}
