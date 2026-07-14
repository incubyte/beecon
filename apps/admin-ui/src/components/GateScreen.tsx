import { AlertTriangle, KeyRound } from "lucide-react";
import { useId, useState, type FormEvent } from "react";

import { verifyAdminKey } from "@/lib/api-client";
import { setAdminKey } from "@/lib/auth";

/**
 * GateScreen is the PD39 admin-key gate (Slice 1, AC2/AC5): a centered card
 * prompting for the installation admin key before the shell mounts. On
 * submit it pre-flights the key against GET /admin/verify (FD3) — a valid
 * key stores it in memory (never localStorage/sessionStorage/a cookie) and
 * the shell takes over on the next render; a rejected key shows an inline
 * error (icon + text, never color-only) with the input preserved and no
 * navigation (this component itself never unmounts on failure).
 */
export function GateScreen() {
  const [key, setKey] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [isVerifying, setIsVerifying] = useState(false);
  const inputId = useId();
  const errorId = useId();

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!key.trim()) {
      setError("Enter the installation admin key.");
      return;
    }
    setIsVerifying(true);
    setError(null);
    const isValid = await verifyAdminKey(key.trim());
    setIsVerifying(false);
    if (!isValid) {
      setError("That admin key was rejected. Check the value and try again.");
      return;
    }
    setAdminKey(key.trim());
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-bg px-4">
      <div className="w-full max-w-[400px] rounded-lg border border-border bg-surface p-8 shadow-md">
        <div className="mb-6 flex flex-col items-center gap-2 text-center">
          <span className="rounded-md bg-primary/10 p-2 text-primary">
            <KeyRound className="size-6" aria-hidden="true" />
          </span>
          <h1 className="text-xl font-semibold text-text">Beecon Admin</h1>
          <p className="text-sm text-text-secondary">Enter the installation admin key to open the console.</p>
        </div>

        <form onSubmit={handleSubmit} noValidate>
          <label htmlFor={inputId} className="mb-1.5 block text-sm font-medium text-text">
            Admin key
          </label>
          <input
            id={inputId}
            type="password"
            autoComplete="off"
            autoFocus
            value={key}
            onChange={(event) => setKey(event.target.value)}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? errorId : undefined}
            className="min-h-11 w-full rounded-md border border-border-strong bg-surface px-3 text-base text-text focus-visible:border-primary"
            placeholder="beecon_admin_..."
          />

          {error ? (
            <div
              id={errorId}
              role="alert"
              className="mt-3 flex items-center gap-2 rounded-md border border-error-solid/30 bg-error-bg px-3 py-2 text-sm text-error-text"
            >
              <AlertTriangle className="size-4 shrink-0" aria-hidden="true" />
              <span>{error}</span>
            </div>
          ) : null}

          <button
            type="submit"
            disabled={isVerifying}
            className="mt-4 min-h-11 w-full rounded-md bg-primary px-4 text-sm font-semibold text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
          >
            {isVerifying ? "Verifying…" : "Open console"}
          </button>
        </form>

        <p className="mt-6 text-xs text-text-muted">
          This console authenticates with a single shared installation-wide key — there are no
          per-operator accounts yet. The key is held in this tab's memory only and is forgotten on
          reload. Intended for a trusted-operator, network-restricted deployment.
        </p>
      </div>
    </div>
  );
}
