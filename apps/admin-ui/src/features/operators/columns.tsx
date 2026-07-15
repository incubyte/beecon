import type { ColumnDef } from "@tanstack/react-table";

import { StatusBadge } from "@/components/ui/StatusBadge";
import { Timestamp } from "@/components/ui/Timestamp";
import type { OperatorAccount } from "@/lib/api-types";

import { OperatorActionsCell } from "./OperatorActionsCell";

/** buildOperatorColumns is Slice 4's operators table shape (AC3): email,
 * status (ACTIVE/DISABLED, never color-only), created date, and a per-row
 * deactivate action — never a password hash. A factory (not a static column
 * array), mirroring buildApiKeyColumns' own precedent, since the actions
 * column needs the operator id and the total active count for the
 * last-active-operator guard (AC6) to disable the button client-side too,
 * even though the server is the actual source of truth for that rejection. */
export function buildOperatorColumns(
  activeCount: number,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
): ColumnDef<OperatorAccount, any>[] {
  return [
    {
      accessorKey: "email",
      header: "Email",
      cell: ({ row }) => <span className="font-medium text-text">{row.original.email}</span>,
    },
    {
      accessorKey: "status",
      header: "Status",
      cell: ({ row }) => <StatusBadge taxonomy="operator" status={row.original.status} />,
    },
    {
      accessorKey: "createdAt",
      header: "Created",
      cell: ({ row }) => <Timestamp iso={row.original.createdAt} />,
    },
    {
      id: "actions",
      header: "Actions",
      cell: ({ row }) => <OperatorActionsCell operator={row.original} activeCount={activeCount} />,
    },
  ];
}
