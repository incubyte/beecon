import type { ColumnDef } from "@tanstack/react-table";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { Timestamp } from "@/components/ui/Timestamp";
import type { TriggerInstance } from "@/lib/api-types";

/** triggerInstanceColumns is Slice 2's Trigger Instances table shape
 * (AC4): status badge, trigger slug, the bound connection's id, and a
 * relative-with-absolute-hover created timestamp. Opening a row navigates
 * to the full-page detail view (config-heavy surfaces get a full page per
 * DESIGN.md §0#4, not a drawer). */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const triggerInstanceColumns: ColumnDef<TriggerInstance, any>[] = [
  {
    accessorKey: "id",
    header: "ID",
    cell: ({ row }) => <CopyIdChip id={row.original.id} />,
  },
  {
    accessorKey: "status",
    header: "Status",
    cell: ({ row }) => <StatusBadge taxonomy="triggerInstance" status={row.original.status} />,
  },
  {
    accessorKey: "triggerSlug",
    header: "Trigger",
    cell: ({ row }) => <span className="font-mono text-[13px] text-text">{row.original.triggerSlug}</span>,
  },
  {
    accessorKey: "connectionId",
    header: "Connection",
    cell: ({ row }) => <CopyIdChip id={row.original.connectionId} />,
  },
  {
    accessorKey: "createdAt",
    header: "Created",
    cell: ({ row }) => <Timestamp iso={row.original.createdAt} />,
  },
];
