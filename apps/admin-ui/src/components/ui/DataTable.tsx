import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from "@tanstack/react-table";
import type { KeyboardEvent, ReactNode } from "react";

export interface DataTableProps<T> {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- react-table's
  // own docs use `any` here: a column array is necessarily heterogeneous in
  // its per-column TValue (id column renders a chip, createdAt renders text).
  columns: ColumnDef<T, any>[];
  data: T[];
  isLoading?: boolean;
  loadingRows?: ReactNode;
  emptyState?: ReactNode;
  caption?: string;
  /** When set, every row becomes clickable (mouse) and operable (Enter/
   * Space, DESIGN.md §9's keyboard mandate) — used by list pages whose
   * detail view is a drawer or, this slice, the org-scoping action. */
  onRowClick?: (row: T) => void;
}

/**
 * DataTable renders @tanstack/react-table's headless sort/visibility logic
 * as a real semantic <table> with <th scope="col">, a sticky header, and
 * row hover (DESIGN.md §7/§9) — table *logic*, not a styled grid. Empty and
 * loading states are first-class: loadingRows renders in place of data rows
 * (see SkeletonRows), and emptyState renders once, below the (still
 * visible) header, when there is no data and nothing is loading.
 */
export function DataTable<T>({
  columns,
  data,
  isLoading,
  loadingRows,
  emptyState,
  caption,
  onRowClick,
}: DataTableProps<T>) {
  const table = useReactTable({ data, columns, getCoreRowModel: getCoreRowModel() });
  const showEmptyState = !isLoading && data.length === 0 && emptyState;

  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <table className="w-full border-collapse text-left text-sm">
        {caption ? <caption className="sr-only">{caption}</caption> : null}
        <thead className="sticky top-0 z-10 bg-surface-muted">
          {table.getHeaderGroups().map((headerGroup) => (
            <tr key={headerGroup.id}>
              {headerGroup.headers.map((header) => (
                <th
                  key={header.id}
                  scope="col"
                  className="border-b border-border px-4 py-3 text-xs font-medium tracking-wide text-text-muted uppercase"
                >
                  {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody>
          {isLoading
            ? loadingRows
            : table.getRowModel().rows.map((row) => (
                <tr
                  key={row.id}
                  className={`border-b border-border last:border-b-0 hover:bg-surface-muted/60 ${
                    onRowClick ? "cursor-pointer" : ""
                  }`}
                  {...rowInteractionProps(onRowClick, row.original)}
                >
                  {row.getVisibleCells().map((cell) => (
                    <td key={cell.id} className="px-4 py-3 align-middle">
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  ))}
                </tr>
              ))}
        </tbody>
      </table>
      {showEmptyState ? <div className="p-4">{emptyState}</div> : null}
    </div>
  );
}

function rowInteractionProps<T>(onRowClick: DataTableProps<T>["onRowClick"], original: T) {
  if (!onRowClick) {
    return {};
  }
  return {
    tabIndex: 0,
    onClick: () => onRowClick(original),
    onKeyDown: (event: KeyboardEvent<HTMLTableRowElement>) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        onRowClick(original);
      }
    },
  };
}
