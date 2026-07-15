import { AlertTriangle, Lock } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { apiClient } from "@/lib/api-client";
import { queryKeys } from "@/lib/query";

/**
 * LoginScreen is the Phase 5 Slice 1 login card (PD49/PD55, DESIGN.md §5):
 * replaces the Phase 4 admin-key GateScreen. Email + password POST to
 * `/api/v1/auth/login`; on success the server sets the session + CSRF
 * cookies and this component invalidates the `auth.me` probe so
 * routes/__root.tsx mounts the shell on the next render — the SPA itself
 * never stores a credential anywhere in JS memory. A failed attempt shows a
 * single generic inline error (icon + text, never color-only) without
 * clearing the typed email, and never navigates away from this screen. The
 * SSO slot (PD53, a later sub-phase) is deliberately left empty, not a
 * placeholder button.
 */
export function LoginScreen() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const queryClient = useQueryClient();
  const emailId = useId();
  const passwordId = useId();
  const errorId = useId();

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!email.trim() || !password) {
      setError("Enter your email and password.");
      return;
    }
    setIsSubmitting(true);
    setError(null);
    try {
      await apiClient.post("/auth/login", { email: email.trim(), password });
      setPassword("");
      await queryClient.invalidateQueries({ queryKey: queryKeys.auth.me() });
    } catch {
      setPassword("");
      setError("Invalid email or password.");
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-bg px-4">
      <div className="w-full max-w-[400px] rounded-lg border border-border bg-surface p-8 shadow-md">
        <div className="mb-6 flex flex-col items-center gap-2 text-center">
          <span className="rounded-md bg-primary/10 p-2 text-primary">
            <Lock className="size-6" aria-hidden="true" />
          </span>
          <h1 className="text-xl font-semibold text-text">Beecon Admin</h1>
          <p className="text-sm text-text-secondary">Sign in to open the console.</p>
        </div>

        <form onSubmit={handleSubmit} noValidate>
          <label htmlFor={emailId} className="mb-1.5 block text-sm font-medium text-text">
            Email
          </label>
          <input
            id={emailId}
            type="email"
            autoComplete="username"
            autoFocus
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? errorId : undefined}
            className="mb-4 min-h-11 w-full rounded-md border border-border-strong bg-surface px-3 text-base text-text focus-visible:border-primary"
            placeholder="you@example.com"
          />

          <label htmlFor={passwordId} className="mb-1.5 block text-sm font-medium text-text">
            Password
          </label>
          <input
            id={passwordId}
            type="password"
            autoComplete="current-password"
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

        <p className="mt-6 text-xs text-text-muted">
          Lost access? Ask another operator to change your password once you're
          signed in, or see README.md for the break-glass recovery path.
        </p>
      </div>
    </div>
  );
}
