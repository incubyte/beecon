import type { ColumnDef } from "@tanstack/react-table";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { HttpStatusBadge } from "@/components/ui/HttpStatusBadge";
import { Timestamp } from "@/components/ui/Timestamp";
import type { LogEntry } from "@/lib/api-types";

const KIND_LABELS: Record<string, string> = {
  tool_execution: "Tool execution",
  oauth_token_exchange: "OAuth token exchange",
  webhook_delivery: "Webhook delivery",
  trigger_poll: "Trigger poll",
};

/** logColumns is Slice 3's log-explorer table shape (AC1): kind, the
 * connection/user/tool the entry belongs to (whichever ids are present),
 * a status pill (never color-only, AC6), duration, and a relative-with-
 * absolute-hover timestamp. */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const logColumns: ColumnDef<LogEntry, any>[] = [
  {
    accessorKey: "kind",
    header: "Kind",
    cell: ({ row }) => <span className="text-text">{KIND_LABELS[row.original.kind] ?? row.original.kind}</span>,
  },
  {
    id: "source",
    header: "Source",
    cell: ({ row }) => <SourceCell entry={row.original} />,
  },
  {
    accessorKey: "status",
    header: "Status",
    cell: ({ row }) => <HttpStatusBadge status={row.original.status} />,
  },
  {
    accessorKey: "durationMs",
    header: "Duration",
    cell: ({ row }) => <span className="font-mono text-xs text-text-secondary">{row.original.durationMs} ms</span>,
  },
  {
    accessorKey: "createdAt",
    header: "Time",
    cell: ({ row }) => <Timestamp iso={row.original.createdAt} />,
  },
];

function SourceCell({ entry }: { entry: LogEntry }) {
  if (entry.connectionId) {
    return <CopyIdChip id={entry.connectionId} />;
  }
  if (entry.userId) {
    return <CopyIdChip id={entry.userId} />;
  }
  return <span className="text-text-muted">—</span>;
}
