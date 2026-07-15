import { QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";

import { queryClient, queryKeys } from "@/lib/query";
import { markSessionExpiredMidWork } from "@/lib/session-state";
import { server } from "@/test/msw/server";

import { ReauthModal } from "./ReauthModal";

/**
 * ReauthModal is mounted permanently by AppShell and renders nothing (Radix's
 * own empty Portal) until useReauthRequired() is true — so these tests drive
 * that flag directly through the real singleton queryClient (the one
 * lib/session-state.ts always writes to; see auth.test.tsx's own
 * singletonWrapper rationale for why a fresh per-test QueryClient would not
 * observe markSessionExpiredMidWork at all) rather than rendering the whole
 * app just to provoke a 401.
 */
function renderReauthModal() {
  return render(
    <QueryClientProvider client={queryClient}>
      <ReauthModal />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  queryClient.setQueryData(queryKeys.auth.reauthRequired(), false);
  queryClient.removeQueries({ queryKey: queryKeys.auth.me() });
});

describe("ReauthModal", () => {
  it("renders nothing while the session has not expired mid-work", () => {
    renderReauthModal();

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("renders the re-authenticate dialog once the session is flagged expired mid-work", () => {
    markSessionExpiredMidWork();

    renderReauthModal();

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByText(/session expired/i)).toBeInTheDocument();
  });

  it("shows the cached operator's email (from the last successful auth.me probe) instead of an email field", () => {
    queryClient.setQueryData(queryKeys.auth.me(), { id: "op_1", email: "operator@example.com" });
    markSessionExpiredMidWork();

    renderReauthModal();

    expect(screen.getByText("operator@example.com")).toBeInTheDocument();
    expect(screen.queryByLabelText(/email/i)).not.toBeInTheDocument();
  });

  it("falls back to an email field when no operator email was ever cached", () => {
    markSessionExpiredMidWork();

    renderReauthModal();

    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
  });

  // --- Non-dismissible (DESIGN.md §5, FD-I): Esc and an overlay click must
  // never bypass the modal — the operator either re-authenticates or signs
  // out, there is no third "just close it" escape hatch. ---

  it("pressing Escape does not dismiss the modal", () => {
    markSessionExpiredMidWork();
    renderReauthModal();

    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape", code: "Escape" });

    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("clicking outside the dialog (the overlay) does not dismiss the modal", () => {
    markSessionExpiredMidWork();
    renderReauthModal();

    const overlay = document.querySelector(".fixed.inset-0");
    expect(overlay).not.toBeNull();
    fireEvent.pointerDown(overlay as Element);
    fireEvent.click(overlay as Element);

    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  // --- A11y (FD-I): the password field takes focus automatically (a focus
  // trap starting point), and a failed attempt renders an icon+text error,
  // never color-only. ---

  it("puts initial focus on the password field", () => {
    markSessionExpiredMidWork();

    renderReauthModal();

    expect(document.activeElement).toBe(screen.getByLabelText(/password/i));
  });

  it("shows an inline icon+text error (never color-only) for a wrong password", async () => {
    queryClient.setQueryData(queryKeys.auth.me(), { id: "op_1", email: "operator@example.com" });
    markSessionExpiredMidWork();
    server.use(http.post("/api/v1/auth/login", () => new HttpResponse(null, { status: 401 })));
    renderReauthModal();

    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "wrong-password" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/invalid/i);
    expect(alert.querySelector("svg")).not.toBeNull();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("shows the same generic message for a 429 lockout response — no existence leak", async () => {
    queryClient.setQueryData(queryKeys.auth.me(), { id: "op_1", email: "operator@example.com" });
    markSessionExpiredMidWork();
    server.use(http.post("/api/v1/auth/login", () => new HttpResponse(null, { status: 429 })));
    renderReauthModal();

    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "whatever" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/invalid email or password/i);
  });

  // --- Successful re-authentication resumes in place. ---

  it("clears the reauthRequired flag and closes the modal on a successful re-authentication", async () => {
    queryClient.setQueryData(queryKeys.auth.me(), { id: "op_1", email: "operator@example.com" });
    markSessionExpiredMidWork();
    server.use(http.post("/api/v1/auth/login", () => new HttpResponse(null, { status: 204 })));
    renderReauthModal();

    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "correct horse battery staple" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(queryClient.getQueryData(queryKeys.auth.reauthRequired())).toBe(false);
  });

  it("refetches active queries on a successful re-authentication, so the underlying page resumes with fresh data", async () => {
    queryClient.setQueryData(queryKeys.auth.me(), { id: "op_1", email: "operator@example.com" });
    markSessionExpiredMidWork();
    server.use(http.post("/api/v1/auth/login", () => new HttpResponse(null, { status: 204 })));
    const refetchSpy = vi.spyOn(queryClient, "refetchQueries");
    renderReauthModal();

    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "correct horse battery staple" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() => expect(refetchSpy).toHaveBeenCalledWith({ type: "active" }));
    refetchSpy.mockRestore();
  });

  // --- The sign-out escape hatch. ---

  it("posts to /auth/logout when 'Sign out instead' is clicked", async () => {
    markSessionExpiredMidWork();
    let logoutCalled = false;
    server.use(
      http.post("/api/v1/auth/logout", () => {
        logoutCalled = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderReauthModal();

    fireEvent.click(screen.getByRole("button", { name: /sign out instead/i }));

    await waitFor(() => expect(logoutCalled).toBe(true));
  });

  it("clears the reauthRequired flag when signing out instead of re-authenticating", async () => {
    markSessionExpiredMidWork();
    server.use(http.post("/api/v1/auth/logout", () => new HttpResponse(null, { status: 204 })));
    renderReauthModal();

    fireEvent.click(screen.getByRole("button", { name: /sign out instead/i }));

    await waitFor(() => expect(queryClient.getQueryData(queryKeys.auth.reauthRequired())).toBe(false));
  });
});
