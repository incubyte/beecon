import { useId, useState, type FormEvent } from "react";

import { Modal } from "@/components/ui/Modal";
import { ApiError } from "@/lib/api-client";

import { useCreateUser } from "./api";

export interface CreateUserModalProps {
  orgId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** CreateUserModal is Slice 4's AC2: an operator creates an end-user in the
 * selected org from the console, reusing the org-scoped CreateUser
 * endpoint. A plain, freely-dismissable form — nothing here is a secret, so
 * Modal (not SecretOnceModal) is the right primitive. */
export function CreateUserModal({ orgId, open, onOpenChange }: CreateUserModalProps) {
  const createUser = useCreateUser(orgId);
  const [name, setName] = useState("");
  const [externalId, setExternalId] = useState("");
  const nameId = useId();
  const externalIdId = useId();

  function handleOpenChange(next: boolean) {
    if (!next) {
      setName("");
      setExternalId("");
      createUser.reset();
    }
    onOpenChange(next);
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    createUser.mutate(
      { name, externalId },
      { onSuccess: () => handleOpenChange(false) },
    );
  }

  return (
    <Modal open={open} onOpenChange={handleOpenChange} title="Create end-user" description="Add a new end-user to this organization.">
      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <label htmlFor={nameId} className="flex flex-col gap-1 text-sm text-text-secondary">
          Name
          <input
            id={nameId}
            type="text"
            value={name}
            onChange={(event) => setName(event.target.value)}
            required
            autoFocus
            className="min-h-11 rounded-md border border-border-strong bg-surface px-3 text-sm text-text focus-visible:border-primary"
          />
        </label>
        <label htmlFor={externalIdId} className="flex flex-col gap-1 text-sm text-text-secondary">
          External ID <span className="text-text-muted">(optional)</span>
          <input
            id={externalIdId}
            type="text"
            value={externalId}
            onChange={(event) => setExternalId(event.target.value)}
            className="min-h-11 rounded-md border border-border-strong bg-surface px-3 font-mono text-sm text-text focus-visible:border-primary"
          />
        </label>

        {createUser.isError ? <p className="text-sm text-error-text">{errorMessage(createUser.error)}</p> : null}

        <div className="mt-2 flex justify-end gap-2">
          <button
            type="button"
            onClick={() => handleOpenChange(false)}
            className="min-h-11 rounded-md border border-border-strong px-4 text-sm font-medium text-text transition-colors hover:bg-surface-muted cursor-pointer"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={!name.trim() || createUser.isPending}
            className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
          >
            {createUser.isPending ? "Creating…" : "Create user"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The user could not be created.";
  }
  return "The user could not be created.";
}
