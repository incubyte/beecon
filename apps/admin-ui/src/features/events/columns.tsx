import type { ColumnDef } from "@tanstack/react-table";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { Timestamp } from "@/components/ui/Timestamp";
import type { DeliveryEvent } from "@/lib/api-types";

/** eventColumns is Slice 3's Events & Delivery table shape (AC2): the
 * event's own id, its type, a delivery-status pill (color+icon+label,
 * never color-only), attempt count, and created/last-attempt timestamps. */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const eventColumns: ColumnDef<DeliveryEvent, any>[] = [
  {
    accessorKey: "id",
    header: "ID",
    cell: ({ row }) => <CopyIdChip id={row.original.id} />,
  },
  {
    accessorKey: "type",
    header: "Type",
    cell: ({ row }) => <span className="font-mono text-xs text-text">{row.original.type}</span>,
  },
  {
    accessorKey: "deliveryStatus",
    header: "Status",
    cell: ({ row }) => <StatusBadge taxonomy="event" status={row.original.deliveryStatus} />,
  },
  {
    accessorKey: "attempts",
    header: "Attempts",
    cell: ({ row }) => <span className="text-text">{row.original.attempts}</span>,
  },
  {
    accessorKey: "createdAt",
    header: "Created",
    cell: ({ row }) => <Timestamp iso={row.original.createdAt} />,
  },
  {
    id: "lastAttemptAt",
    header: "Last attempt",
    cell: ({ row }) => (row.original.lastAttemptAt ? <Timestamp iso={row.original.lastAttemptAt} /> : <span className="text-text-muted">—</span>),
  },
];
