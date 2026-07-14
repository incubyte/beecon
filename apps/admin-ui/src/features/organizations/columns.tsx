import type { ColumnDef } from "@tanstack/react-table";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { formatDate } from "@/lib/format";
import type { Organization } from "@/lib/api-types";

/** organizationColumns is Slice 1's Organizations table shape (AC7): a
 * mono, click-to-copy id and the created date. Name is shown too — it is
 * already part of every Organization DTO and free real estate next to the
 * id, not a separate surface to build. */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const organizationColumns: ColumnDef<Organization, any>[] = [
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
    accessorKey: "createdAt",
    header: "Created",
    cell: ({ row }) => <span className="text-text-secondary">{formatDate(row.original.createdAt)}</span>,
  },
];
