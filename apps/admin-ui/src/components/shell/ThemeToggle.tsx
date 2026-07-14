import * as Switch from "@radix-ui/react-switch";
import { Moon, Sun } from "lucide-react";
import { useEffect, useState } from "react";

const STORAGE_KEY = "beecon-admin-theme";

type Theme = "light" | "dark";

function systemPrefersDark(): boolean {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function readStoredTheme(): Theme {
  const stored = window.localStorage.getItem(STORAGE_KEY);
  if (stored === "light" || stored === "dark") {
    return stored;
  }
  return systemPrefersDark() ? "dark" : "light";
}

function applyTheme(theme: Theme) {
  document.documentElement.setAttribute("data-theme", theme);
}

/**
 * ThemeToggle honors light/dark (Slice 1, AC6). Theme choice is not a
 * secret, so persisting it in localStorage is fine and does not touch
 * PD39's in-memory-only rule, which applies solely to the admin key
 * (architecture doc §2.6).
 */
export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(() => (typeof window === "undefined" ? "light" : readStoredTheme()));

  useEffect(() => {
    applyTheme(theme);
    window.localStorage.setItem(STORAGE_KEY, theme);
  }, [theme]);

  const isDark = theme === "dark";

  return (
    <label className="flex min-h-11 cursor-pointer items-center gap-2 px-1 text-text-secondary">
      <Sun className="size-4" aria-hidden="true" />
      <Switch.Root
        checked={isDark}
        onCheckedChange={(checked) => setTheme(checked ? "dark" : "light")}
        aria-label="Toggle dark theme"
        className="relative h-6 w-10 rounded-pill bg-surface-muted transition-colors data-[state=checked]:bg-primary"
      >
        <Switch.Thumb className="block size-4.5 translate-x-0.5 rounded-full bg-surface shadow-sm transition-transform duration-150 will-change-transform motion-reduce:transition-none data-[state=checked]:translate-x-4.5" />
      </Switch.Root>
      <Moon className="size-4" aria-hidden="true" />
    </label>
  );
}
