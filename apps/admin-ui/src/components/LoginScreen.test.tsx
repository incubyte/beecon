import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";

import { server } from "@/test/msw/server";

import { LoginScreen } from "./LoginScreen";

function renderLoginScreen() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <LoginScreen />
    </QueryClientProvider>,
  );
}

function fillAndSubmit(email: string, password: string) {
  fireEvent.change(screen.getByLabelText(/email/i), { target: { value: email } });
  fireEvent.change(screen.getByLabelText(/password/i), { target: { value: password } });
  fireEvent.click(screen.getByRole("button", { name: /sign in/i }));
}

describe("LoginScreen", () => {
  it("renders no SSO option — the SSO slot is reserved empty until a later sub-phase", () => {
    renderLoginScreen();

    expect(screen.queryByRole("button", { name: /sso|single sign|google|microsoft/i })).not.toBeInTheDocument();
  });

  it("shows an inline icon+text error and preserves the typed email when the credentials are wrong", async () => {
    server.use(http.post("/api/v1/auth/login", () => new HttpResponse(null, { status: 401 })));
    renderLoginScreen();

    fillAndSubmit("operator@example.com", "wrong-password");

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/invalid/i);
    // Icon, not color-only (WCAG 1.4.1): the alert renders a decorative SVG
    // icon alongside the text, not just styled text.
    expect(alert.querySelector("svg")).not.toBeNull();
    expect(screen.getByLabelText(/email/i)).toHaveValue("operator@example.com");
  });

  it("clears the typed password (never resubmits or displays it) after a failed attempt", async () => {
    server.use(http.post("/api/v1/auth/login", () => new HttpResponse(null, { status: 401 })));
    renderLoginScreen();

    fillAndSubmit("operator@example.com", "wrong-password");

    await screen.findByRole("alert");
    expect(screen.getByLabelText(/password/i)).toHaveValue("");
  });

  it("never navigates away from the login screen after a failed attempt — the form is still present", async () => {
    server.use(http.post("/api/v1/auth/login", () => new HttpResponse(null, { status: 401 })));
    renderLoginScreen();

    fillAndSubmit("operator@example.com", "wrong-password");

    await screen.findByRole("alert");
    expect(screen.getByRole("button", { name: /sign in/i })).toBeInTheDocument();
    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
  });

  it("posts the trimmed email and password to /auth/login and invalidates the session probe on success", async () => {
    let capturedBody: unknown;
    server.use(
      http.post("/api/v1/auth/login", async ({ request }) => {
        capturedBody = await request.json();
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderLoginScreen();

    fillAndSubmit("  operator@example.com  ", "correct horse battery staple");

    await waitFor(() => expect(capturedBody).toEqual({ email: "operator@example.com", password: "correct horse battery staple" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
  });

  it("shows a generic message (no existence leak) for a 429 lockout response, the same as any other failure", async () => {
    server.use(http.post("/api/v1/auth/login", () => new HttpResponse(null, { status: 429 })));
    renderLoginScreen();

    fillAndSubmit("operator@example.com", "wrong-password");

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/invalid email or password/i);
  });

  it("shows a client-side validation message rather than submitting when a field is empty", () => {
    renderLoginScreen();

    fireEvent.change(screen.getByLabelText(/email/i), { target: { value: "operator@example.com" } });
    fireEvent.click(screen.getByRole("button", { name: /sign in/i }));

    expect(screen.getByRole("alert")).toHaveTextContent(/enter your email and password/i);
  });

  // --- Adversarial: the SPA must hold no credential of any kind in browser
  // storage, mirroring SecretOnceModal.test.tsx's own guard for the retired
  // PD39 admin-key store. ---

  it("never writes the email or password to localStorage or sessionStorage while submitting (success or failure)", async () => {
    const localSetItem = vi.spyOn(window.localStorage, "setItem");
    const sessionSetItem = vi.spyOn(window.sessionStorage, "setItem");
    server.use(http.post("/api/v1/auth/login", () => new HttpResponse(null, { status: 401 })));
    renderLoginScreen();

    fillAndSubmit("operator@example.com", "correct horse battery staple");
    await screen.findByRole("alert");

    for (const call of localSetItem.mock.calls) {
      expect(call.join(" ")).not.toContain("correct horse battery staple");
      expect(call.join(" ")).not.toContain("operator@example.com");
    }
    for (const call of sessionSetItem.mock.calls) {
      expect(call.join(" ")).not.toContain("correct horse battery staple");
      expect(call.join(" ")).not.toContain("operator@example.com");
    }
  });

  it("sends no Authorization header on the login request — no credential is held anywhere except the submitted form fields", async () => {
    let capturedAuthHeader: string | null = "not-yet-captured";
    server.use(
      http.post("/api/v1/auth/login", ({ request }) => {
        capturedAuthHeader = request.headers.get("Authorization");
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderLoginScreen();

    fillAndSubmit("operator@example.com", "correct horse battery staple");

    await waitFor(() => expect(capturedAuthHeader).toBeNull());
  });
});
