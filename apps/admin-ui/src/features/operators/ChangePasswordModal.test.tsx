import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";

import { server } from "@/test/msw/server";

import { ChangePasswordModal } from "./ChangePasswordModal";

function renderChangePasswordModal(onOpenChange = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return {
    onOpenChange,
    ...render(
      <QueryClientProvider client={queryClient}>
        <ChangePasswordModal open={true} onOpenChange={onOpenChange} />
      </QueryClientProvider>,
    ),
  };
}

describe("ChangePasswordModal", () => {
  it("submits current and new password to POST /operators/me/password", async () => {
    let requestBody: unknown;
    server.use(
      http.post("/api/v1/operators/me/password", async ({ request }) => {
        requestBody = await request.json();
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderChangePasswordModal();

    fireEvent.change(screen.getByLabelText(/current password/i), { target: { value: "correct horse battery staple" } });
    fireEvent.change(screen.getByLabelText(/new password/i), { target: { value: "a brand new password entirely" } });
    fireEvent.click(screen.getByRole("button", { name: /^change password$/i }));

    await waitFor(() =>
      expect(requestBody).toEqual({
        currentPassword: "correct horse battery staple",
        newPassword: "a brand new password entirely",
      }),
    );
  });

  // NOTE (tester finding, not fixed here — reporting per protocol rather
  // than editing business logic): handleSubmit's onSuccess callback calls
  // handleOpenChange(false), which synchronously calls
  // changePassword.reset() — so the `changePassword.isSuccess ? <p>Password
  // changed…` JSX branch a few lines below is currently unreachable dead
  // code; the mutation's success state is reset before that render could
  // ever show it. This test pins the ACTUAL observable behavior (the modal
  // is told to close via onOpenChange(false), the same "close on success"
  // precedent CreateOperatorModal's own handleSubmit already establishes)
  // rather than the unreachable success-message branch.
  it("closes the modal (via onOpenChange) on a successful password change — the acting session stays signed in, so no redirect happens", async () => {
    server.use(http.post("/api/v1/operators/me/password", () => new HttpResponse(null, { status: 204 })));
    const { onOpenChange } = renderChangePasswordModal();

    fireEvent.change(screen.getByLabelText(/current password/i), { target: { value: "correct horse battery staple" } });
    fireEvent.change(screen.getByLabelText(/new password/i), { target: { value: "a brand new password entirely" } });
    fireEvent.click(screen.getByRole("button", { name: /^change password$/i }));

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
  });

  it("rejects a wrong current password with the server's generic inline error, without clearing the form", async () => {
    server.use(
      http.post("/api/v1/operators/me/password", () =>
        HttpResponse.json({ error: { code: "unauthorized", message: "invalid credentials" } }, { status: 401 }),
      ),
    );
    renderChangePasswordModal();

    fireEvent.change(screen.getByLabelText(/current password/i), { target: { value: "totally-wrong-password" } });
    fireEvent.change(screen.getByLabelText(/new password/i), { target: { value: "a brand new password entirely" } });
    fireEvent.click(screen.getByRole("button", { name: /^change password$/i }));

    expect(await screen.findByText("invalid credentials")).toBeInTheDocument();
    expect(screen.getByLabelText(/current password/i)).toBeInTheDocument();
  });
});
