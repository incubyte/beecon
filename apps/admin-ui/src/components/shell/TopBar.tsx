import { LogOut } from "lucide-react";

import { clearAdminKey } from "@/lib/auth";

import { CommandPalette } from "./CommandPalette";
import { OrgSwitcher } from "./OrgSwitcher";
import { ThemeToggle } from "./ThemeToggle";

/** TopBar renders the slim bar DESIGN.md §5 specifies: wordmark, the
 * organization switcher, a command-palette trigger, the theme toggle, and
 * sign-out — which clears the in-memory admin key (Slice 1, AC6). */
export function TopBar() {
  return (
    <header className="flex h-14 shrink-0 items-center gap-3 border-b border-border bg-surface px-4">
      <span className="text-sm font-semibold tracking-tight text-text">Beecon</span>
      <OrgSwitcher />
      <div className="flex-1" />
      <CommandPalette />
      <ThemeToggle />
      <button
        type="button"
        onClick={clearAdminKey}
        aria-label="Sign out"
        title="Sign out"
        className="flex min-h-11 min-w-11 items-center justify-center rounded-md text-text-secondary transition-colors hover:bg-surface-muted hover:text-text cursor-pointer"
      >
        <LogOut className="size-4" aria-hidden="true" />
      </button>
    </header>
  );
}
