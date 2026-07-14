import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { CreatedWebhookEndpoint, RotatedWebhookEndpointSecret, UpdatedWebhookEndpoint, WebhookEndpoint } from "@/lib/api-types";

import { WebhookEndpointsPage } from "./WebhookEndpointsPage";

/** renderWebhookEndpointsPage mirrors ApiKeysPage.test.tsx's own harness:
 * WebhookEndpointsPage reads the selected org from `?org=` via
 * useSearch({ from: "__root__" }). */
function renderWebhookEndpointsPage(initialPath = "/?org=org_1") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({
    validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
  });
  const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: WebhookEndpointsPage });
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute]),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

function endpoint(overrides: Partial<WebhookEndpoint> = {}): WebhookEndpoint {
  return {
    id: "wep_1",
    url: "https://example.com/hook",
    eventTypes: null,
    status: "ENABLED",
    consecutiveFailures: 0,
    secretPrefix: "whsec_abcdefgh",
    createdAt: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

function mockWebhookEndpointsList(items: WebhookEndpoint[]) {
  server.use(http.get("/api/v1/organizations/org_1/webhook-endpoints", () => HttpResponse.json({ items })));
}

/** mockStatefulWebhookEndpointsList backs GET .../webhook-endpoints with a
 * mutable in-memory list so a mutation's onSuccess invalidateQueries refetch
 * observes the effect of the mutation handler that ran just before it —
 * required for any test that both mutates an endpoint and then asserts the
 * list reflects the change, since a static mock would keep serving the
 * original snapshot forever. */
function mockStatefulWebhookEndpointsList(initial: WebhookEndpoint[]) {
  const state = [...initial];
  server.use(http.get("/api/v1/organizations/org_1/webhook-endpoints", () => HttpResponse.json({ items: state })));
  return {
    replace(id: string, next: Partial<WebhookEndpoint>) {
      const index = state.findIndex((item) => item.id === id);
      if (index !== -1) {
        state[index] = { ...state[index], ...next };
      }
    },
  };
}

describe("WebhookEndpointsPage", () => {
  it("shows a 'select an organization' state and never fetches when no org is selected", async () => {
    let requested = false;
    server.use(
      http.get("/api/v1/organizations/:orgId/webhook-endpoints", () => {
        requested = true;
        return HttpResponse.json({ items: [] });
      }),
    );
    renderWebhookEndpointsPage("/");

    expect(await screen.findByText(/select an organization/i)).toBeInTheDocument();
    expect(requested).toBe(false);
  });

  it("shows the empty state when the org has no endpoints", async () => {
    mockWebhookEndpointsList([]);
    renderWebhookEndpointsPage();

    expect(await screen.findByText(/no webhook endpoints yet/i)).toBeInTheDocument();
  });

  it("renders URL, 'All types' for a nil filter, status, and consecutive-failure count — never a secret anywhere in the table", async () => {
    mockWebhookEndpointsList([endpoint({ id: "wep_all_types" })]);
    renderWebhookEndpointsPage();

    await screen.findByText("https://example.com/hook");
    const table = screen.getByRole("table");
    expect(within(table).getByText(/all types/i)).toBeInTheDocument();
    expect(within(table).getByText("Enabled")).toBeInTheDocument();
    expect(within(table).getByText("0")).toBeInTheDocument();
    // Adversarial: the table must never render anything resembling a full
    // secret (only secretPrefix ever reaches this row), guarding against a
    // future regression that serializes one in.
    expect(within(table).queryByText(/whsec_the-full-secret/i)).not.toBeInTheDocument();
  });

  it("renders a named event-type filter as its own labeled chip, not 'All types'", async () => {
    mockWebhookEndpointsList([endpoint({ id: "wep_filtered", eventTypes: ["connection.expired"] })]);
    renderWebhookEndpointsPage();

    await screen.findByText(/connection expired/i);
    expect(screen.queryByText(/all types/i)).not.toBeInTheDocument();
  });

  it("shows a DISABLED_AUTO endpoint's status as 'Auto-disabled', visually distinct from an operator's own 'Disabled'", async () => {
    mockWebhookEndpointsList([
      endpoint({ id: "wep_operator_disabled", status: "DISABLED" }),
      endpoint({ id: "wep_auto_disabled", status: "DISABLED_AUTO" }),
    ]);
    renderWebhookEndpointsPage();

    await screen.findByText("Disabled");
    expect(screen.getByText("Auto-disabled")).toBeInTheDocument();
  });

  it("a nonzero consecutive-failure count renders with a warning treatment, not silently as plain text", async () => {
    mockWebhookEndpointsList([endpoint({ id: "wep_failing", consecutiveFailures: 3 })]);
    renderWebhookEndpointsPage();

    const count = await screen.findByText("3");
    expect(count.className).toContain("text-warning-text");
  });

  it("registering an endpoint opens SecretOnceModal exactly once with the returned secret", async () => {
    mockWebhookEndpointsList([]);
    server.use(
      http.post("/api/v1/organizations/org_1/webhook-endpoints", () =>
        HttpResponse.json(
          { id: "wep_new", url: "https://example.com/new-hook", eventTypes: null, secret: "whsec_freshly-issued" } satisfies CreatedWebhookEndpoint,
          { status: 201 },
        ),
      ),
    );
    renderWebhookEndpointsPage();
    await screen.findByText(/no webhook endpoints yet/i);

    fireEvent.click(screen.getByRole("button", { name: /register endpoint/i }));
    const urlInput = await screen.findByPlaceholderText(/https:\/\/example\.com\/webhooks\/beecon/i);
    fireEvent.change(urlInput, { target: { value: "https://example.com/new-hook" } });
    fireEvent.click(screen.getByRole("button", { name: /^register endpoint$/i }));

    expect(await screen.findByText("whsec_freshly-issued")).toBeInTheDocument();
    // Exactly one SecretOnceModal instance is showing the secret — a second,
    // stray render would duplicate the text node.
    expect(screen.getAllByText("whsec_freshly-issued")).toHaveLength(1);
  });

  it("registering beyond the configured cap surfaces the backend's cap-naming validation error inline, without opening SecretOnceModal", async () => {
    mockWebhookEndpointsList([]);
    server.use(
      http.post("/api/v1/organizations/org_1/webhook-endpoints", () =>
        HttpResponse.json(
          { error: { code: "validation_failed", message: "organization already has the maximum of 5 webhook endpoints" } },
          { status: 422 },
        ),
      ),
    );
    renderWebhookEndpointsPage();
    await screen.findByText(/no webhook endpoints yet/i);

    fireEvent.click(screen.getByRole("button", { name: /register endpoint/i }));
    const urlInput = await screen.findByPlaceholderText(/https:\/\/example\.com\/webhooks\/beecon/i);
    fireEvent.change(urlInput, { target: { value: "https://example.com/one-too-many" } });
    fireEvent.click(screen.getByRole("button", { name: /^register endpoint$/i }));

    expect(await screen.findByText(/maximum of 5 webhook endpoints/i)).toBeInTheDocument();
    expect(screen.queryByText(/you will not be able to see this again/i)).not.toBeInTheDocument();
  });

  it("rotating an endpoint's secret opens SecretOnceModal with the newly rotated secret", async () => {
    mockWebhookEndpointsList([endpoint({ id: "wep_1" })]);
    server.use(
      http.post("/api/v1/organizations/org_1/webhook-endpoints/wep_1/rotate-secret", () =>
        HttpResponse.json({ secret: "whsec_rotated-secret" } satisfies RotatedWebhookEndpointSecret),
      ),
    );
    renderWebhookEndpointsPage();

    fireEvent.click(await screen.findByRole("button", { name: /rotate secret/i }));

    expect(await screen.findByText("whsec_rotated-secret")).toBeInTheDocument();
  });

  it("disabling an ENABLED endpoint flips its status to Disabled", async () => {
    const list = mockStatefulWebhookEndpointsList([endpoint({ id: "wep_1", status: "ENABLED" })]);
    server.use(
      http.post("/api/v1/organizations/org_1/webhook-endpoints/wep_1/disable", () => {
        list.replace("wep_1", { status: "DISABLED" });
        return HttpResponse.json({
          id: "wep_1",
          url: "https://example.com/hook",
          eventTypes: null,
          status: "DISABLED",
          consecutiveFailures: 0,
          createdAt: "2026-01-01T00:00:00.000Z",
        } satisfies UpdatedWebhookEndpoint);
      }),
    );
    renderWebhookEndpointsPage();
    await screen.findByText("Enabled");

    fireEvent.click(screen.getByRole("button", { name: /^disable$/i }));

    await waitFor(() => expect(screen.getByText("Disabled")).toBeInTheDocument());
  });

  it("enabling a DISABLED_AUTO endpoint resets it to Enabled and clears its failure count in the list", async () => {
    const list = mockStatefulWebhookEndpointsList([endpoint({ id: "wep_1", status: "DISABLED_AUTO", consecutiveFailures: 5 })]);
    server.use(
      http.post("/api/v1/organizations/org_1/webhook-endpoints/wep_1/enable", () => {
        list.replace("wep_1", { status: "ENABLED", consecutiveFailures: 0 });
        return HttpResponse.json({
          id: "wep_1",
          url: "https://example.com/hook",
          eventTypes: null,
          status: "ENABLED",
          consecutiveFailures: 0,
          createdAt: "2026-01-01T00:00:00.000Z",
        } satisfies UpdatedWebhookEndpoint);
      }),
    );
    renderWebhookEndpointsPage();
    await screen.findByText("Auto-disabled");

    fireEvent.click(screen.getByRole("button", { name: /^enable$/i }));

    await waitFor(() => expect(screen.getByText("Enabled")).toBeInTheDocument());
  });

  it("deleting an endpoint is gated by TypeToConfirm on the endpoint's own id", async () => {
    mockWebhookEndpointsList([endpoint({ id: "wep_to_delete" })]);
    let deleteRequested = false;
    server.use(
      http.delete("/api/v1/organizations/org_1/webhook-endpoints/wep_to_delete", () => {
        deleteRequested = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderWebhookEndpointsPage();

    fireEvent.click(await screen.findByRole("button", { name: /^delete$/i }));
    const confirmButton = await screen.findByRole("button", { name: /delete endpoint/i });
    // The confirm button stays disabled until the operator types the exact
    // endpoint id — clicking it before typing must not delete anything.
    fireEvent.click(confirmButton);
    expect(deleteRequested).toBe(false);

    fireEvent.change(screen.getByRole("textbox"), { target: { value: "wep_to_delete" } });
    fireEvent.click(screen.getByRole("button", { name: /delete endpoint/i }));

    await waitFor(() => expect(deleteRequested).toBe(true));
  });

  it("does not delete when a wrong value is typed into the confirm field", async () => {
    mockWebhookEndpointsList([endpoint({ id: "wep_to_delete" })]);
    let deleteRequested = false;
    server.use(
      http.delete("/api/v1/organizations/org_1/webhook-endpoints/wep_to_delete", () => {
        deleteRequested = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderWebhookEndpointsPage();

    fireEvent.click(await screen.findByRole("button", { name: /^delete$/i }));
    await screen.findByRole("button", { name: /delete endpoint/i });
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "wrong-id" } });

    expect(screen.getByRole("button", { name: /delete endpoint/i })).toBeDisabled();
    expect(deleteRequested).toBe(false);
  });

  it("editing an endpoint's URL and filter sends the whole-object update, then reflects the new filter in the list", async () => {
    const list = mockStatefulWebhookEndpointsList([endpoint({ id: "wep_1", url: "https://example.com/old-hook", eventTypes: null })]);
    let capturedBody: unknown;
    server.use(
      http.put("/api/v1/organizations/org_1/webhook-endpoints/wep_1", async ({ request }) => {
        capturedBody = await request.json();
        list.replace("wep_1", { url: "https://example.com/new-hook", eventTypes: ["webhook.test"] });
        return HttpResponse.json({
          id: "wep_1",
          url: "https://example.com/new-hook",
          eventTypes: ["webhook.test"],
          status: "ENABLED",
          consecutiveFailures: 0,
          createdAt: "2026-01-01T00:00:00.000Z",
        } satisfies UpdatedWebhookEndpoint);
      }),
    );
    renderWebhookEndpointsPage();
    await screen.findByText("https://example.com/old-hook");

    fireEvent.click(screen.getByRole("button", { name: /^edit /i }));
    const urlInput = await screen.findByDisplayValue("https://example.com/old-hook");
    fireEvent.change(urlInput, { target: { value: "https://example.com/new-hook" } });
    fireEvent.click(screen.getByRole("radio", { name: /only specific event types/i }));
    fireEvent.click(screen.getByRole("checkbox", { name: /webhook test/i }));
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(capturedBody).toEqual({ url: "https://example.com/new-hook", eventTypes: ["webhook.test"] }));
    expect(await screen.findByText(/webhook test/i)).toBeInTheDocument();
  });
});
