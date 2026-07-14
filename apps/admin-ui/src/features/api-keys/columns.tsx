import type { ColumnDef } from "@tanstack/react-table";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { Timestamp } from "@/components/ui/Timestamp";
import type { ApiKeyListing, RotatedApiKey } from "@/lib/api-types";

import { ApiKeyActionsCell } from "./ApiKeyActionsCell";
import { deriveApiKeyStatus } from "./status";

/** buildApiKeyColumns is Slice 4's API-keys table shape (AC3): id, prefix,
 * scope, rotation/revocation status, created date, and per-row rotate/revoke
 * actions — never a secret. A factory (not a static column array, unlike
 * every other feature's columns.tsx) because the actions column needs
 * orgId and the "just rotated" callback the actions cell's mutations
 * depend on. */
export function buildApiKeyColumns(
  orgId: string,
  onRotated: (rotated: RotatedApiKey) => void,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
): ColumnDef<ApiKeyListing, any>[] {
  return [
    {
      accessorKey: "id",
      header: "ID",
      cell: ({ row }) => <CopyIdChip id={row.original.id} />,
    },
    {
      accessorKey: "prefix",
      header: "Prefix",
      cell: ({ row }) => <span className="font-mono text-[13px] text-text-secondary">{row.original.prefix}</span>,
    },
    {
      accessorKey: "scope",
      header: "Scope",
      cell: ({ row }) => <StatusBadge taxonomy="apiKeyScope" status={row.original.scope} />,
    },
    {
      id: "status",
      header: "Status",
      cell: ({ row }) => <StatusBadge taxonomy="apiKey" status={deriveApiKeyStatus(row.original)} />,
    },
    {
      accessorKey: "createdAt",
      header: "Created",
      cell: ({ row }) => <Timestamp iso={row.original.createdAt} />,
    },
    {
      id: "actions",
      header: "Actions",
      cell: ({ row }) => <ApiKeyActionsCell orgId={orgId} apiKey={row.original} onRotated={onRotated} />,
    },
  ];
}
