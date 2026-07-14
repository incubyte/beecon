import type { ColumnDef } from "@tanstack/react-table";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import type { CatalogTool } from "@/lib/api-types";

/** toolColumns is Slice 6's Catalog > Tools table shape (AC3): slug
 * (click-to-copy, AC5), name, owning provider, and a deprecated flag.
 * Opening a row shows the tool's input/output JSON-Schema in the drawer
 * (AC4) — scan-heavy, cross-provider browsing gets a drawer rather than a
 * full page (DESIGN.md §0#4). */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const toolColumns: ColumnDef<CatalogTool, any>[] = [
  {
    accessorKey: "slug",
    header: "Slug",
    cell: ({ row }) => <CopyIdChip id={row.original.slug} />,
  },
  {
    accessorKey: "name",
    header: "Name",
    cell: ({ row }) => <span className="text-text">{row.original.name}</span>,
  },
  {
    accessorKey: "providerName",
    header: "Provider",
    cell: ({ row }) => <span className="text-text-secondary">{row.original.providerName}</span>,
  },
  {
    accessorKey: "deprecated",
    header: "Deprecated",
    cell: ({ row }) =>
      row.original.deprecated ? (
        <span className="inline-flex items-center rounded-pill bg-warning-bg px-2.5 py-1 text-xs font-medium text-warning-text">
          Deprecated
        </span>
      ) : (
        <span className="text-text-muted">—</span>
      ),
  },
];
