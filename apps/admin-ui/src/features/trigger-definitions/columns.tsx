import type { ColumnDef } from "@tanstack/react-table";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import type { CatalogTriggerDefinition } from "@/lib/api-types";

/** triggerDefinitionColumns is Slice 6's Catalog > Trigger Definitions table
 * shape (AC3): slug (click-to-copy, AC5), name, owning provider, and
 * ingestion mode. Opening a row shows the config schema, payload schema,
 * and ingestion mode in the drawer (AC4). */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const triggerDefinitionColumns: ColumnDef<CatalogTriggerDefinition, any>[] = [
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
    accessorKey: "ingestion",
    header: "Ingestion",
    cell: ({ row }) => <span className="font-mono text-[13px] text-text">{row.original.ingestion}</span>,
  },
];
