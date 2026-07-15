import * as Dialog from "@radix-ui/react-dialog";
import { AlertTriangle, Lock } from "lucide-react";
import { useEffect, useId, useState, type FormEvent } from "react";

import { apiClient } from "@/lib/api-client";
import { cachedOperatorEmail, resolveSessionExpiredMidWork, useReauthRequired, useSignOut } from "@/lib/auth";

/**
 * ReauthModal is the Phase 5 Slice 5 mid-session re-authenticate experience
 * (DESIGN.md §5): when a console API call returns 401 while the operator was
 * already authenticated, api-client.ts flags `useReauthRequired()` true
 * instead of hard-bouncing to LoginScreen — AppShell renders this modal as
 * an overlay on top of whatever page was already mounted, so in-progress
 * work (the current route, unsubmitted form fields underneath the overlay)
 * is preserved rather than lost to a full remount. The operator's identity
 * is already known from the last successful `/auth/me` probe
 * (cachedOperatorEmail), so this only asks for the password — a full
 * email+password form is the fallback for the unlikely case no identity was
 * ever cached.
 *
 * Deliberately non-dismissible (Esc, overlay click, and the close affordance
 * are all suppressed, mirroring SecretOnceModal's own technique): the
 * operator either re-authenticates to resume, or explicitly signs out — both
 * paths clear the flag, one back to the same page, one to LoginScreen. There
 * is no third "just close it" option, since the underlying session really is
 * gone and every other console call will keep failing until one of those two
 * things happens.
 */
export function ReauthModal() {
  const open = useReauthRequired();
  const knownEmail = cachedOperatorEmail();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const signOut = useSignOut();
  const emailId = useId();
  const passwordId = useId();
  const errorId = useId();

  useEffect(() => {
    if (open) {
      setPassword("");
      setError(null);
    }
  }, [open]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const loginEmail = knownEmail ?? email.trim();
    if (!loginEmail || !password) {
      setError("Enter your email and password.");
      return;
    }
    setIsSubmitting(true);
    setError(null);
    try {
      await apiClient.post("/auth/login", { email: loginEmail, password });
      setPassword("");
      resolveSessionExpiredMidWork();
    } catch {
      setPassword("");
      setError("Invalid email or password.");
    } finally {
      setIsSubmitting(false);
    }
  }

  function preventDismiss(event: Event) {
    event.preventDefault();
  }

  return (
    <Dialog.Root open={open} onOpenChange={() => undefined}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 motion-safe:transition-opacity motion-safe:duration-200" />
        <Dialog.Content
          onEscapeKeyDown={preventDismiss}
          onPointerDownOutside={preventDismiss}
          onInteractOutside={preventDismiss}
          className="fixed top-1/2 left-1/2 z-50 w-full max-w-[400px] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-surface p-6 shadow-lg focus:outline-none motion-safe:transition-all motion-safe:duration-200"
        >
          <div className="mb-4 flex flex-col items-center gap-2 text-center">
            <span className="rounded-md bg-primary/10 p-2 text-primary">
              <Lock className="size-6" aria-hidden="true" />
            </span>
            <Dialog.Title className="text-lg font-semibold text-text">Session expired</Dialog.Title>
            <Dialog.Description className="text-sm text-text-secondary">
              Sign in again to keep going — your work on this page is still here.
            </Dialog.Description>
          </div>

          <form onSubmit={handleSubmit} noValidate>
            {knownEmail ? (
              <p className="mb-4 text-sm text-text-secondary">
                Signed in as <span className="font-medium text-text">{knownEmail}</span>
              </p>
            ) : (
              <>
                <label htmlFor={emailId} className="mb-1.5 block text-sm font-medium text-text">
                  Email
                </label>
                <input
                  id={emailId}
                  type="email"
                  autoComplete="username"
                  value={email}
                  onChange={(event) => setEmail(event.target.value)}
                  aria-invalid={error ? true : undefined}
                  aria-describedby={error ? errorId : undefined}
                  className="mb-4 min-h-11 w-full rounded-md border border-border-strong bg-surface px-3 text-base text-text focus-visible:border-primary"
                  placeholder="you@example.com"
                />
              </>
            )}

            <label htmlFor={passwordId} className="mb-1.5 block text-sm font-medium text-text">
              Password
            </label>
            <input
              id={passwordId}
              type="password"
              autoComplete="current-password"
              autoFocus
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? errorId : undefined}
              className="min-h-11 w-full rounded-md border border-border-strong bg-surface px-3 text-base text-text focus-visible:border-primary"
              placeholder="••••••••••••"
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
              disabled={isSubmitting}
              className="mt-4 min-h-11 w-full rounded-md bg-primary px-4 text-sm font-semibold text-primary-fg transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:opacity-60 cursor-pointer"
            >
              {isSubmitting ? "Signing in…" : "Sign in"}
            </button>
          </form>

          <button
            type="button"
            onClick={signOut}
            className="mt-4 min-h-11 w-full rounded-md text-sm text-text-secondary transition-colors hover:bg-surface-muted hover:text-text cursor-pointer"
          >
            Sign out instead
          </button>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
