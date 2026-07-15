import { KeyRound, LogOut } from "lucide-react";
import { useState } from "react";

import { useSignOut } from "@/lib/auth";
import { ChangePasswordModal } from "@/features/operators/ChangePasswordModal";

import { CommandPalette } from "./CommandPalette";
import { OrgSwitcher } from "./OrgSwitcher";
import { ThemeToggle } from "./ThemeToggle";

/** TopBar renders the slim bar DESIGN.md §5 specifies: wordmark, the
 * organization switcher, a command-palette trigger, the theme toggle,
 * "Change password" (Phase 5 Slice 4, AC4 — the operator/session menu slot
 * DESIGN.md §5 reserves), and sign-out. */
export function TopBar() {
  const signOut = useSignOut();
  const [isChangePasswordOpen, setIsChangePasswordOpen] = useState(false);

  return (
    <header className="flex h-14 shrink-0 items-center gap-3 border-b border-border bg-surface px-4">
      <span className="text-sm font-semibold tracking-tight text-text">Beecon</span>
      <OrgSwitcher />
      <div className="flex-1" />
      <CommandPalette />
      <ThemeToggle />
      <button
        type="button"
        onClick={() => setIsChangePasswordOpen(true)}
        aria-label="Change password"
        title="Change password"
        className="flex min-h-11 min-w-11 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface-muted hover:text-text cursor-pointer"
      >
        <KeyRound className="size-4" aria-hidden="true" />
      </button>
      <button
        type="button"
        onClick={signOut}
        aria-label="Sign out"
        title="Sign out"
        className="flex min-h-11 min-w-11 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface-muted hover:text-text cursor-pointer"
      >
        <LogOut className="size-4" aria-hidden="true" />
      </button>

      <ChangePasswordModal open={isChangePasswordOpen} onOpenChange={setIsChangePasswordOpen} />
    </header>
  );
}
