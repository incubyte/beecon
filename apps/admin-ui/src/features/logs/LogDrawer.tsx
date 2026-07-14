import { CodeViewer } from "@/components/ui/CodeViewer";
import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { DetailRow } from "@/components/ui/DetailRow";
import { Drawer } from "@/components/ui/Drawer";
import { HttpStatusBadge } from "@/components/ui/HttpStatusBadge";
import { Timestamp } from "@/components/ui/Timestamp";
import type { LogEntry } from "@/lib/api-types";

const KIND_LABELS: Record<string, string> = {
  tool_execution: "Tool execution",
  oauth_token_exchange: "OAuth token exchange",
  webhook_delivery: "Webhook delivery",
  trigger_poll: "Trigger poll",
};

export interface LogDrawerProps {
  entry: LogEntry | null;
  onClose: () => void;
}

/** LogDrawer is Slice 3's right-side detail panel for one log entry (AC1):
 * ids, kind, status, duration, and the already-redacted request/response
 * bodies rendered through CodeViewer, whose textual `[REDACTED]` marker is
 * exactly what logging.Redact wrote at record time (AC6 — nothing here
 * un-redacts or reformats a secret). There is no GET-by-id log endpoint
 * (logging only exposes List), so this takes the already-fetched row
 * directly rather than querying by id. */
export function LogDrawer({ entry, onClose }: LogDrawerProps) {
  return (
    <Drawer
      open={entry !== null}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
      title="Log entry"
      description={entry ? <CopyIdChip id={entry.id} /> : undefined}
    >
      {entry ? (
        <div className="flex flex-col gap-5">
          <dl className="flex flex-col gap-4">
            <DetailRow label="Kind">
              <span className="text-text">{KIND_LABELS[entry.kind] ?? entry.kind}</span>
            </DetailRow>
            <DetailRow label="Status">
              <HttpStatusBadge status={entry.status} size="md" />
            </DetailRow>
            <DetailRow label="Duration">
              <span className="font-mono text-sm text-text">{entry.durationMs} ms</span>
            </DetailRow>
            {entry.connectionId ? (
              <DetailRow label="Connection">
                <CopyIdChip id={entry.connectionId} />
              </DetailRow>
            ) : null}
            {entry.userId ? (
              <DetailRow label="User">
                <CopyIdChip id={entry.userId} />
              </DetailRow>
            ) : null}
            {entry.toolSlug ? (
              <DetailRow label="Tool">
                <span className="text-text">{entry.toolSlug}</span>
              </DetailRow>
            ) : null}
            {entry.eventId ? (
              <DetailRow label="Delivery event">
                <span className="flex items-center gap-2">
                  <CopyIdChip id={entry.eventId} />
                  <span className="text-xs text-text-muted">attempt {entry.attempt}</span>
                </span>
              </DetailRow>
            ) : null}
            <DetailRow label="Rate limited">
              <span className="text-text">{entry.rateLimited ? "Yes" : "No"}</span>
            </DetailRow>
            <DetailRow label="Created">
              <Timestamp iso={entry.createdAt} />
            </DetailRow>
          </dl>

          <CodeViewer label="Request body" value={entry.requestBody} />
          <CodeViewer label="Response body" value={entry.responseBody} />
        </div>
      ) : null}
    </Drawer>
  );
}
