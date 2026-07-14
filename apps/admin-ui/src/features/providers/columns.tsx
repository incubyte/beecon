import type { ColumnDef } from "@tanstack/react-table";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import type { ProviderDefinitionSummary } from "@/lib/api-types";

/** providerDefinitionColumns is Slice 6's Catalog > Providers table shape
 * (AC1): name, slug (click-to-copy, AC5), auth scheme, and tool/trigger
 * counts so an operator can scan the installed estate without opening every
 * provider's full detail. Opening a row navigates to the full-page detail
 * view (config-heavy surfaces get a full page, DESIGN.md §0#4). */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const providerDefinitionColumns: ColumnDef<ProviderDefinitionSummary, any>[] = [
  {
    accessorKey: "name",
    header: "Name",
    cell: ({ row }) => (
      <div className="flex items-center gap-2">
        {row.original.logo ? (
          <img src={row.original.logo} alt="" className="size-5 shrink-0 rounded-sm" aria-hidden="true" />
        ) : null}
        <span className="font-medium text-text">{row.original.name}</span>
      </div>
    ),
  },
  {
    accessorKey: "slug",
    header: "Slug",
    cell: ({ row }) => <CopyIdChip id={row.original.slug} />,
  },
  {
    accessorKey: "authScheme",
    header: "Auth scheme",
    cell: ({ row }) => <span className="text-text-secondary">{row.original.authScheme}</span>,
  },
  {
    accessorKey: "toolCount",
    header: "Tools",
    cell: ({ row }) => <span className="font-mono text-[13px] text-text">{row.original.toolCount}</span>,
  },
  {
    accessorKey: "triggerCount",
    header: "Triggers",
    cell: ({ row }) => <span className="font-mono text-[13px] text-text">{row.original.triggerCount}</span>,
  },
];
