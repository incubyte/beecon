import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { ApiError } from "@/lib/api-client";
import type { OperatorAccount } from "@/lib/api-types";

import { useDeactivateOperator } from "./api";

export interface OperatorActionsCellProps {
  operator: OperatorAccount;
  activeCount: number;
}

/** OperatorActionsCell is Slice 4's per-row deactivate action (AC5): guarded
 * by ConfirmDialog (no reactivate path exists yet, but deactivating is not
 * the irreversible data loss TypeToConfirm's own precedent — revoking an API
 * key — represents, so the plain confirm/cancel dialog is the right
 * primitive here). Disabled outright once already DISABLED, or when this
 * operator is the installation's last remaining ACTIVE one (AC6) — the
 * server is still the authority (it rejects with 409 regardless), but
 * disabling the button client-side avoids a pointless round trip and names
 * the reason up front. */
export function OperatorActionsCell({ operator, activeCount }: OperatorActionsCellProps) {
  const deactivateOperator = useDeactivateOperator();
  const isDisabled = operator.status === "DISABLED";
  const isLastActive = operator.status === "ACTIVE" && activeCount <= 1;

  return (
    <div className="flex flex-col items-start gap-1">
      <ConfirmDialog
        trigger={
          <button
            type="button"
            disabled={isDisabled || isLastActive}
            title={isLastActive ? "Cannot deactivate the last active operator" : undefined}
            className="min-h-11 rounded-md border border-error-solid/40 px-3 text-sm font-medium text-error-text transition-colors hover:bg-error-solid/10 disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
          >
            Deactivate
          </button>
        }
        title="Deactivate this operator?"
        description={`${operator.email} will no longer be able to log in. This does not delete their account.`}
        confirmLabel="Deactivate"
        onConfirm={() => deactivateOperator.mutate(operator.id)}
        isConfirming={deactivateOperator.isPending}
      />
      {deactivateOperator.isError ? (
        <p className="text-xs text-error-text">{errorMessage(deactivateOperator.error)}</p>
      ) : null}
    </div>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The operator could not be deactivated.";
  }
  return "The operator could not be deactivated.";
}
