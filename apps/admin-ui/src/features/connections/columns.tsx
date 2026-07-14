import type { ColumnDef } from "@tanstack/react-table";

import { CopyIdChip } from "@/components/ui/CopyIdChip";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { Timestamp } from "@/components/ui/Timestamp";
import type { Connection } from "@/lib/api-types";

/** connectionColumns is Slice 2's Connections table shape (AC1/AC2): a
 * status badge, the provider/integration, the account metadata the OAuth
 * callback captured (or an em dash before that has happened), and a
 * relative-with-absolute-hover created timestamp. */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export const connectionColumns: ColumnDef<Connection, any>[] = [
  {
    accessorKey: "id",
    header: "ID",
    cell: ({ row }) => <CopyIdChip id={row.original.id} />,
  },
  {
    accessorKey: "status",
    header: "Status",
    cell: ({ row }) => <StatusBadge taxonomy="connection" status={row.original.status} />,
  },
  {
    accessorKey: "providerSlug",
    header: "Integration",
    cell: ({ row }) => <span className="font-medium text-text">{row.original.providerSlug}</span>,
  },
  {
    id: "account",
    header: "Account",
    cell: ({ row }) => <AccountCell connection={row.original} />,
  },
  {
    accessorKey: "createdAt",
    header: "Created",
    cell: ({ row }) => <Timestamp iso={row.original.createdAt} />,
  },
];

function AccountCell({ connection }: { connection: Connection }) {
  if (!connection.account) {
    return <span className="text-text-muted">—</span>;
  }
  return <span className="text-text">{connection.account.displayName || connection.account.email}</span>;
}
