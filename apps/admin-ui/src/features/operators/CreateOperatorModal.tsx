import { useId, useState, type FormEvent } from "react";

import { Modal } from "@/components/ui/Modal";
import { ApiError } from "@/lib/api-client";

import { useCreateOperator } from "./api";

export interface CreateOperatorModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** CreateOperatorModal is Slice 4's AC1: a logged-in operator creates
 * another operator account with an email and an initial password the
 * creator itself chooses — unlike issuing an API key, the account holder
 * (not the server) sets the credential, so a plain, freely-dismissable
 * Modal is the right primitive, not SecretOnceModal. */
export function CreateOperatorModal({ open, onOpenChange }: CreateOperatorModalProps) {
  const createOperator = useCreateOperator();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const emailId = useId();
  const passwordId = useId();

  function handleOpenChange(next: boolean) {
    if (!next) {
      setEmail("");
      setPassword("");
      createOperator.reset();
    }
    onOpenChange(next);
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    createOperator.mutate(
      { email, password },
      { onSuccess: () => handleOpenChange(false) },
    );
  }

  return (
    <Modal
      open={open}
      onOpenChange={handleOpenChange}
      title="Create operator"
      description="Add another operator account to this installation."
    >
      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <label htmlFor={emailId} className="flex flex-col gap-1 text-sm text-text-secondary">
          Email
          <input
            id={emailId}
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            required
            autoFocus
            className="min-h-11 rounded-md border border-border-strong bg-surface px-3 text-sm text-text focus-visible:border-primary"
          />
        </label>
        <label htmlFor={passwordId} className="flex flex-col gap-1 text-sm text-text-secondary">
          Initial password
          <input
            id={passwordId}
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            required
            minLength={12}
            autoComplete="new-password"
            className="min-h-11 rounded-md border border-border-strong bg-surface px-3 text-sm text-text focus-visible:border-primary"
          />
        </label>

        {createOperator.isError ? <p className="text-sm text-error-text">{errorMessage(createOperator.error)}</p> : null}

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
            disabled={!email.trim() || !password || createOperator.isPending}
            className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
          >
            {createOperator.isPending ? "Creating…" : "Create operator"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The operator could not be created.";
  }
  return "The operator could not be created.";
}
