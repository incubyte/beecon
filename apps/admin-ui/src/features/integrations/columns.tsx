import type { ColumnDef } from "@tanstack/react-table";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import type { IntegrationSummary } from "@/lib/api-types";

/** integrationColumns is the provider detail page's Integrations section
 * table shape: id (click-to-copy) and the summary fields the DTO actually
 * carries — provider name and auth scheme. The client secret never appears
 * here or in any other API response after creation. */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const integrationColumns: ColumnDef<IntegrationSummary, any>[] = [
  {
    accessorKey: "id",
    header: "ID",
    cell: ({ row }) => <CopyIdChip id={row.original.id} />,
  },
  {
    accessorKey: "name",
    header: "Provider",
    cell: ({ row }) => <span className="text-text">{row.original.name}</span>,
  },
  {
    accessorKey: "authScheme",
    header: "Auth scheme",
    cell: ({ row }) => <span className="text-text-secondary">{row.original.authScheme}</span>,
  },
];
