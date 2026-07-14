import type { ColumnDef } from "@tanstack/react-table";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { Timestamp } from "@/components/ui/Timestamp";
import type { EndUser } from "@/lib/api-types";

/** userColumns is Slice 4's end-users table shape (AC1): a mono,
 * click-to-copy id, name, the consumer's own optional externalId, and a
 * relative-with-hover created timestamp — mirrors organizationColumns'/
 * connectionColumns' own shape. */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const userColumns: ColumnDef<EndUser, any>[] = [
  {
    accessorKey: "id",
    header: "ID",
    cell: ({ row }) => <CopyIdChip id={row.original.id} />,
  },
  {
    accessorKey: "name",
    header: "Name",
    cell: ({ row }) => <span className="font-medium text-text">{row.original.name}</span>,
  },
  {
    accessorKey: "externalId",
    header: "External ID",
    cell: ({ row }) =>
      row.original.externalId ? (
        <span className="font-mono text-[13px] text-text-secondary">{row.original.externalId}</span>
      ) : (
        <span className="text-text-muted">—</span>
      ),
  },
  {
    accessorKey: "createdAt",
    header: "Created",
    cell: ({ row }) => <Timestamp iso={row.original.createdAt} />,
  },
];
