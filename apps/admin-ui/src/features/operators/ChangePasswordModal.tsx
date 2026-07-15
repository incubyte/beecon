import { useId, useState, type FormEvent } from "react";

import { Modal } from "@/components/ui/Modal";
import { ApiError } from "@/lib/api-client";

import { useChangeMyPassword } from "./api";

export interface ChangePasswordModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** ChangePasswordModal is Slice 4's AC4 (and closes the carried-forward
 * Slice 2 AC4): an operator changes their own password after presenting
 * their current one. A wrong current password surfaces as an inline error
 * (the server's generic "invalid credentials", the same verdict Login
 * itself uses) without closing the form. On success, every one of this
 * operator's OTHER sessions was just revoked server-side — the one making
 * this very request stays alive, so nothing here needs to redirect to
 * LoginScreen or invalidate the session probe. */
export function ChangePasswordModal({ open, onOpenChange }: ChangePasswordModalProps) {
  const changePassword = useChangeMyPassword();
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const currentPasswordId = useId();
  const newPasswordId = useId();

  function handleOpenChange(next: boolean) {
    if (!next) {
      setCurrentPassword("");
      setNewPassword("");
      changePassword.reset();
    }
    onOpenChange(next);
  }

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    changePassword.mutate(
      { currentPassword, newPassword },
      { onSuccess: () => handleOpenChange(false) },
    );
  }

  return (
    <Modal
      open={open}
      onOpenChange={handleOpenChange}
      title="Change your password"
      description="Every other session of yours is signed out once this succeeds — this one stays signed in."
    >
      <form onSubmit={handleSubmit} className="flex flex-col gap-4">
        <label htmlFor={currentPasswordId} className="flex flex-col gap-1 text-sm text-text-secondary">
          Current password
          <input
            id={currentPasswordId}
            type="password"
            value={currentPassword}
            onChange={(event) => setCurrentPassword(event.target.value)}
            required
            autoFocus
            autoComplete="current-password"
            className="min-h-11 rounded-md border border-border-strong bg-surface px-3 text-sm text-text focus-visible:border-primary"
          />
        </label>
        <label htmlFor={newPasswordId} className="flex flex-col gap-1 text-sm text-text-secondary">
          New password
          <input
            id={newPasswordId}
            type="password"
            value={newPassword}
            onChange={(event) => setNewPassword(event.target.value)}
            required
            minLength={12}
            autoComplete="new-password"
            className="min-h-11 rounded-md border border-border-strong bg-surface px-3 text-sm text-text focus-visible:border-primary"
          />
        </label>

        {changePassword.isError ? (
          <p className="text-sm text-error-text">{errorMessage(changePassword.error)}</p>
        ) : null}

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
            disabled={!currentPassword || !newPassword || changePassword.isPending}
            className="min-h-11 rounded-md bg-primary px-4 text-sm font-medium text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
          >
            {changePassword.isPending ? "Changing…" : "Change password"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.message || "The password could not be changed.";
  }
  return "The password could not be changed.";
}
