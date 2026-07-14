import { AlertTriangle } from "lucide-react";
import type { ColumnDef } from "@tanstack/react-table";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { Timestamp } from "@/components/ui/Timestamp";
import type { RotatedWebhookEndpointSecret, WebhookEndpoint } from "@/lib/api-types";

import { eventTypeLabel } from "./eventTypes";
import { WebhookEndpointActionsCell } from "./WebhookEndpointActionsCell";

/** buildWebhookEndpointColumns is Slice 8's endpoints table shape (AC1):
 * URL, event-type filter, status (incl. DISABLED_AUTO), consecutive
 * failures, created date, and per-row edit/enable-disable/rotate/delete
 * actions — never a secret. A factory (not a static column array, mirroring
 * buildApiKeyColumns) because the actions column needs orgId and the
 * rotate/edit callbacks. */
export function buildWebhookEndpointColumns(
  orgId: string,
  onRotated: (rotated: RotatedWebhookEndpointSecret) => void,
  onEdit: (endpoint: WebhookEndpoint) => void,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
): ColumnDef<WebhookEndpoint, any>[] {
  return [
    {
      accessorKey: "id",
      header: "ID",
      cell: ({ row }) => <CopyIdChip id={row.original.id} />,
    },
    {
      accessorKey: "url",
      header: "URL",
      cell: ({ row }) => <span className="break-all text-text">{row.original.url}</span>,
    },
    {
      id: "eventTypes",
      header: "Event types",
      cell: ({ row }) => <EventTypesCell eventTypes={row.original.eventTypes} />,
    },
    {
      accessorKey: "status",
      header: "Status",
      cell: ({ row }) => <StatusBadge taxonomy="endpoint" status={row.original.status} />,
    },
    {
      accessorKey: "consecutiveFailures",
      header: "Consecutive failures",
      cell: ({ row }) => <ConsecutiveFailuresCell count={row.original.consecutiveFailures} />,
    },
    {
      accessorKey: "createdAt",
      header: "Created",
      cell: ({ row }) => <Timestamp iso={row.original.createdAt} />,
    },
    {
      id: "actions",
      header: "Actions",
      cell: ({ row }) => (
        <WebhookEndpointActionsCell orgId={orgId} endpoint={row.original} onRotated={onRotated} onEdit={onEdit} />
      ),
    },
  ];
}

function EventTypesCell({ eventTypes }: { eventTypes: string[] | null }) {
  if (eventTypes === null) {
    return <span className="text-sm text-text-secondary">All types</span>;
  }
  return (
    <div className="flex flex-wrap gap-1">
      {eventTypes.map((eventType) => (
        <span
          key={eventType}
          className="inline-flex items-center rounded-pill bg-neutral-bg px-2 py-0.5 text-xs text-neutral-text"
        >
          {eventTypeLabel(eventType)}
        </span>
      ))}
    </div>
  );
}

function ConsecutiveFailuresCell({ count }: { count: number }) {
  if (count === 0) {
    return <span className="text-text-secondary">0</span>;
  }
  return (
    <span className="inline-flex items-center gap-1 text-warning-text">
      <AlertTriangle className="size-3.5 shrink-0" aria-hidden="true" />
      {count}
    </span>
  );
}
