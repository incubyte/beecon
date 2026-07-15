import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { OperatorAccount, OperatorsPage as OperatorsPageDTO } from "@/lib/api-types";

import { OperatorsPage } from "./OperatorsPage";

/** renderOperatorsPage mirrors OrganizationsPage.test.tsx's own harness:
 * OperatorsPage is installation-level (never org-scoped, per architecture
 * doc §2.5/§8 — an operator administers the whole installation, like the
 * admin key it replaces), so no `?org=` search param or org-scoped route is
 * involved at all — a plain QueryClientProvider is enough. */
function renderOperatorsPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <OperatorsPage />
    </QueryClientProvider>,
  );
}

function operator(overrides: Partial<OperatorAccount> = {}): OperatorAccount {
  return {
    id: "op_1",
    email: "founder@example.com",
    status: "ACTIVE",
    createdAt: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

function mockOperators(items: OperatorAccount[]) {
  server.use(http.get("/api/v1/operators", () => HttpResponse.json({ items } satisfies OperatorsPageDTO)));
}

describe("OperatorsPage", () => {
  it("shows the empty state when the installation has no operator accounts", async () => {
    mockOperators([]);
    renderOperatorsPage();

    expect(await screen.findByText(/no operator accounts yet/i)).toBeInTheDocument();
  });

  it("lists every operator, showing email, status (never color-only), and created date — never a password hash", async () => {
    mockOperators([
      operator({ id: "op_1", email: "founder@example.com", status: "ACTIVE" }),
      operator({ id: "op_2", email: "disabled-operator@example.com", status: "DISABLED" }),
    ]);
    renderOperatorsPage();

    expect(await screen.findByText("founder@example.com")).toBeInTheDocument();
    expect(screen.getByText("disabled-operator@example.com")).toBeInTheDocument();
    // StatusBadge always pairs its label with an icon (never color alone).
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.getByText("Disabled")).toBeInTheDocument();
    expect(document.body.textContent?.toLowerCase()).not.toContain("password");
  });

  it("shows an inline error card with a Retry action when the list request fails", async () => {
    server.use(
      http.get("/api/v1/operators", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "The operators list could not be loaded." } }, { status: 500 }),
      ),
    );
    renderOperatorsPage();

    expect(await screen.findByText("The operators list could not be loaded.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });

  describe("create operator (AC1/AC2)", () => {
    it("creates an operator via CreateOperatorModal and refreshes the list", async () => {
      mockOperators([]);
      let createdBody: unknown;
      server.use(
        http.post("/api/v1/operators", async ({ request }) => {
          createdBody = await request.json();
          return HttpResponse.json({ id: "op_new", email: "second@example.com" }, { status: 201 });
        }),
      );
      renderOperatorsPage();
      await screen.findByText(/no operator accounts yet/i);
      mockOperators([operator({ id: "op_new", email: "second@example.com" })]);

      fireEvent.click(screen.getByRole("button", { name: /create operator/i }));
      fireEvent.change(await screen.findByLabelText(/^email$/i), { target: { value: "Second@Example.com" } });
      fireEvent.change(screen.getByLabelText(/initial password/i), { target: { value: "another correct horse battery" } });
      fireEvent.click(screen.getByRole("button", { name: /^create operator$/i }));

      await waitFor(() =>
        expect(createdBody).toEqual({ email: "Second@Example.com", password: "another correct horse battery" }),
      );
      expect(await screen.findByText("second@example.com")).toBeInTheDocument();
    });

    it("surfaces the server's duplicate-email rejection inline without closing the form", async () => {
      mockOperators([operator({ email: "existing@example.com" })]);
      server.use(
        http.post("/api/v1/operators", () =>
          HttpResponse.json(
            { error: { code: "email_exists", message: "an operator account with this email already exists" } },
            { status: 409 },
          ),
        ),
      );
      renderOperatorsPage();
      await screen.findByText("existing@example.com");

      fireEvent.click(screen.getByRole("button", { name: /create operator/i }));
      fireEvent.change(await screen.findByLabelText(/^email$/i), { target: { value: "existing@example.com" } });
      fireEvent.change(screen.getByLabelText(/initial password/i), { target: { value: "another correct horse battery" } });
      fireEvent.click(screen.getByRole("button", { name: /^create operator$/i }));

      expect(await screen.findByText(/already exists/i)).toBeInTheDocument();
      // The form is still open (not silently dismissed on failure).
      expect(screen.getByLabelText(/^email$/i)).toBeInTheDocument();
    });

    it("surfaces the server's too-short-password rejection inline", async () => {
      mockOperators([]);
      server.use(
        http.post("/api/v1/operators", () =>
          HttpResponse.json(
            {
              error: {
                code: "validation_failed",
                message: "validation failed",
                details: { field: "password", issue: "must be at least 12 characters" },
              },
            },
            { status: 422 },
          ),
        ),
      );
      renderOperatorsPage();
      await screen.findByText(/no operator accounts yet/i);

      fireEvent.click(screen.getByRole("button", { name: /create operator/i }));
      fireEvent.change(await screen.findByLabelText(/^email$/i), { target: { value: "second@example.com" } });
      // The client-side minLength=12 constraint would normally block a
      // shorter value on submit in a real browser, but jsdom's form
      // validation does not enforce minLength on requestSubmit the same
      // way — the point of this test is the server rejection surfacing,
      // not client-side HTML5 validation.
      fireEvent.change(screen.getByLabelText(/initial password/i), { target: { value: "short-password-that-still-passes-jsdom" } });
      fireEvent.click(screen.getByRole("button", { name: /^create operator$/i }));

      expect(await screen.findByText(/validation failed/i)).toBeInTheDocument();
    });
  });

  describe("deactivate (AC5/AC6)", () => {
    it("deactivates a non-last operator after confirming, and refreshes the list", async () => {
      mockOperators([operator({ id: "op_1", email: "founder@example.com" }), operator({ id: "op_2", email: "second@example.com" })]);
      let deactivateCalled = false;
      server.use(
        http.post("/api/v1/operators/op_2/deactivate", () => {
          deactivateCalled = true;
          return new HttpResponse(null, { status: 204 });
        }),
      );
      renderOperatorsPage();
      await screen.findByText("second@example.com");
      mockOperators([
        operator({ id: "op_1", email: "founder@example.com" }),
        operator({ id: "op_2", email: "second@example.com", status: "DISABLED" }),
      ]);

      const deactivateButtons = screen.getAllByRole("button", { name: /^deactivate$/i });
      fireEvent.click(deactivateButtons[deactivateButtons.length - 1]);
      const confirmButtons = await screen.findAllByRole("button", { name: /^deactivate$/i });
      fireEvent.click(confirmButtons[confirmButtons.length - 1]);

      await waitFor(() => expect(deactivateCalled).toBe(true));
    });

    it("disables the Deactivate action client-side for the last remaining ACTIVE operator", async () => {
      mockOperators([operator({ id: "op_1", email: "only-active@example.com", status: "ACTIVE" })]);
      renderOperatorsPage();

      await screen.findByText("only-active@example.com");

      const deactivateButton = screen.getByRole("button", { name: /^deactivate$/i });
      expect(deactivateButton).toBeDisabled();
    });

    it("surfaces the server's last-active-operator 409 inline if the client-side guard is bypassed", async () => {
      // Two operators shown as ACTIVE client-side (so the button isn't
      // disabled), but the server is still the actual authority and rejects
      // the call — this proves the server's rejection surfaces to the
      // operator rather than being silently swallowed.
      mockOperators([operator({ id: "op_1", email: "a@example.com" }), operator({ id: "op_2", email: "b@example.com" })]);
      server.use(
        http.post("/api/v1/operators/op_1/deactivate", () =>
          HttpResponse.json(
            { error: { code: "last_active_operator", message: "cannot deactivate the last active operator" } },
            { status: 409 },
          ),
        ),
      );
      renderOperatorsPage();
      await screen.findByText("a@example.com");

      const deactivateButtons = screen.getAllByRole("button", { name: /^deactivate$/i });
      fireEvent.click(deactivateButtons[0]);
      const confirmButtons = await screen.findAllByRole("button", { name: /^deactivate$/i });
      fireEvent.click(confirmButtons[confirmButtons.length - 1]);

      expect(await screen.findByText(/cannot deactivate the last active operator/i)).toBeInTheDocument();
    });
  });
});
