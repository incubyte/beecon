import { useState } from "react";

import { DataTable } from "@/components/ui/DataTable";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorCard } from "@/components/ui/ErrorCard";
import { SkeletonRows } from "@/components/ui/SkeletonRows";
import { ApiError } from "@/lib/api-client";

import { useOperators } from "./api";
import { buildOperatorColumns } from "./columns";
import { CreateOperatorModal } from "./CreateOperatorModal";

/** OperatorsPage is Slice 4's Administer > Operators surface: every
 * operator account in the installation (AC3) — flat, installation-wide,
 * like OrganizationsPage's own list — with a "Create operator" action
 * (AC1/AC2) and per-row deactivate (AC5/AC6). */
export function OperatorsPage() {
  const { data: operators, isLoading, isError, error, refetch } = useOperators();
  const [isCreateOpen, setIsCreateOpen] = useState(false);

  const items = operators ?? [];
  const activeCount = items.filter((operator) => operator.status === "ACTIVE").length;
  const columns = buildOperatorColumns(activeCount);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-text">Operators</h1>
          <p className="text-sm text-text-secondary">Every operator account in this installation.</p>
        </div>
        <button
          type="button"
          onClick={() => setIsCreateOpen(true)}
          className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover cursor-pointer"
        >
          Create operator
        </button>
      </div>

      {isError ? (
        <ErrorCard message={errorMessage(error)} onRetry={refetch} />
      ) : (
        <DataTable
          caption="Operators"
          columns={columns}
          data={items}
          isLoading={isLoading}
          loadingRows={<SkeletonRows columns={columns.length} />}
          emptyState={
            <EmptyState
              title="No operator accounts yet"
              description="Operator accounts are created here or via the break-glass bootstrap path."
            />
          }
        />
      )}

      <CreateOperatorModal open={isCreateOpen} onOpenChange={setIsCreateOpen} />
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The operators list could not be loaded.";
  }
  return "The operators list could not be loaded.";
}
