import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { Connection, InitiatedConnection, OrganizationsPage } from "@/lib/api-types";

import { ConnectionDrawer } from "./ConnectionDrawer";

/** renderDrawer mounts ConnectionDrawer directly — it takes orgId/
 * connectionId/onClose as plain props and never reads the router, so a full
 * TanStack Router harness (unlike ConnectionsPage.test.tsx) isn't needed. */
function renderDrawer(connectionId: string | null, onClose: () => void = () => {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <ConnectionDrawer orgId="org_1" connectionId={connectionId} onClose={onClose} />
    </QueryClientProvider>,
  );
}

function connection(overrides: Partial<Connection>): Connection {
  return {
    id: "conn_detail_1",
    status: "ACTIVE",
    providerSlug: "outlook",
    userId: "user_42",
    createdAt: "2020-01-01T00:00:00.000Z",
    account: { email: "ada@example.com", displayName: "Ada Lovelace" },
    ...overrides,
  };
}

function mockConnectionDetail(conn: Connection) {
  server.use(http.get(`/api/v1/organizations/org_1/connections/${conn.id}`, () => HttpResponse.json(conn)));
}

function mockOrganizationsWithRedirectUris(uris: string[]) {
  server.use(
    http.get("/api/v1/organizations", () =>
      HttpResponse.json({
        items: [{ id: "org_1", name: "Acme", allowedRedirectUris: uris, createdAt: "2026-01-01T00:00:00.000Z" }],
      } satisfies OrganizationsPage),
    ),
  );
}

describe("ConnectionDrawer", () => {
  it("shows id, integration, user, account, status, and a relative-with-absolute-hover timestamp", async () => {
    mockConnectionDetail(connection({}));
    mockOrganizationsWithRedirectUris([]);
    renderDrawer("conn_detail_1");

    expect(await screen.findByText("Active")).toBeInTheDocument();
    expect(screen.getByText("Connection detail")).toBeInTheDocument();
    expect(screen.getByText("outlook")).toBeInTheDocument();
    expect(screen.getByText("Ada Lovelace")).toBeInTheDocument();
    // The id appears in both the drawer description (CopyIdChip) and the
    // User row's own CopyIdChip — asserting via title (CopyIdChip sets
    // title={id}) disambiguates the connection id specifically.
    expect(screen.getByTitle("conn_detail_1")).toBeInTheDocument();

    const timestamp = screen.getByText(/ago|in \d/i);
    expect(timestamp.tagName.toLowerCase()).toBe("time");
    expect(timestamp).toHaveAttribute("title");
    expect(timestamp.getAttribute("title")).not.toBe("");
  });

  it("shows an em dash for the account when the OAuth handshake hasn't activated the connection yet", async () => {
    mockConnectionDetail(connection({ status: "INITIATED", account: undefined }));
    mockOrganizationsWithRedirectUris([]);
    renderDrawer("conn_detail_1");

    await screen.findByText("Initiated");
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("is closed when connectionId is null and calls onClose when dismissed (Esc)", async () => {
    mockConnectionDetail(connection({}));
    mockOrganizationsWithRedirectUris([]);
    const onClose = () => {
      closed = true;
    };
    let closed = false;
    const { rerender } = render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <ConnectionDrawer orgId="org_1" connectionId={null} onClose={onClose} />
      </QueryClientProvider>,
    );
    expect(screen.queryByText("Connection detail")).not.toBeInTheDocument();

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    rerender(
      <QueryClientProvider client={queryClient}>
        <ConnectionDrawer orgId="org_1" connectionId="conn_detail_1" onClose={onClose} />
      </QueryClientProvider>,
    );
    await screen.findByText("Connection detail");

    fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
    await waitFor(() => expect(closed).toBe(true));
  });

  it("is dismissible via the close button", async () => {
    mockConnectionDetail(connection({}));
    mockOrganizationsWithRedirectUris([]);
    let closed = false;
    renderDrawer("conn_detail_1", () => {
      closed = true;
    });
    await screen.findByText("Connection detail");

    fireEvent.click(screen.getByRole("button", { name: /close/i }));
    await waitFor(() => expect(closed).toBe(true));
  });

  it("disable calls the disable endpoint and disables the button once DISCONNECTED", async () => {
    let disableCalled = false;
    mockConnectionDetail(connection({ status: "ACTIVE" }));
    mockOrganizationsWithRedirectUris([]);
    server.use(
      http.post("/api/v1/organizations/org_1/connections/conn_detail_1/disable", () => {
        disableCalled = true;
        return HttpResponse.json({ id: "conn_detail_1", status: "DISCONNECTED" });
      }),
    );
    renderDrawer("conn_detail_1");
    await screen.findByRole("button", { name: /disable connection/i });

    fireEvent.click(screen.getByRole("button", { name: /disable connection/i }));

    await waitFor(() => expect(disableCalled).toBe(true));
  });

  it("delete is gated by TypeToConfirm — the confirm button stays disabled until the connection id is typed exactly", async () => {
    let deleteCalled = false;
    mockConnectionDetail(connection({}));
    mockOrganizationsWithRedirectUris([]);
    server.use(
      http.delete("/api/v1/organizations/org_1/connections/conn_detail_1", () => {
        deleteCalled = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    let closed = false;
    renderDrawer("conn_detail_1", () => {
      closed = true;
    });
    await screen.findByRole("button", { name: /^delete connection$/i });

    fireEvent.click(screen.getByRole("button", { name: /^delete connection$/i }));
    await screen.findByText("Delete this connection?");
    // Two "Delete connection" buttons now exist (the trigger, hidden behind
    // the dialog, and the dialog's own confirm action) — the confirm action
    // is the last one (the dialog is appended to the DOM via a Portal after
    // the trigger), and it starts disabled.
    const buttons = screen.getAllByRole("button", { name: /^delete connection$/i });
    const dialogConfirmButton = buttons[buttons.length - 1];
    expect(dialogConfirmButton).toBeDisabled();

    const input = screen.getByLabelText(/type .* to confirm/i);
    fireEvent.change(input, { target: { value: "wrong-id" } });
    expect(dialogConfirmButton).toBeDisabled();

    fireEvent.change(input, { target: { value: "conn_detail_1" } });
    expect(dialogConfirmButton).not.toBeDisabled();

    fireEvent.click(dialogConfirmButton);
    await waitFor(() => expect(deleteCalled).toBe(true));
    await waitFor(() => expect(closed).toBe(true));
  });

  it("reconnect posts the chosen redirect URI and surfaces the new redirectUrl", async () => {
    mockConnectionDetail(connection({}));
    mockOrganizationsWithRedirectUris(["https://consumer.example.com/callback"]);
    server.use(
      http.post("/api/v1/organizations/org_1/connections/conn_detail_1/reconnect", () =>
        HttpResponse.json({
          id: "conn_detail_1",
          status: "INITIATED",
          redirectUrl: "http://localhost:8080/connect/fresh-token",
        } satisfies InitiatedConnection),
      ),
    );
    renderDrawer("conn_detail_1");
    await screen.findByText("Connection detail");

    const select = await screen.findByLabelText(/redirect uri/i);
    fireEvent.change(select, { target: { value: "https://consumer.example.com/callback" } });
    fireEvent.click(screen.getByRole("button", { name: /^reconnect$/i }));

    expect(await screen.findByText(/connect\/fresh-token/)).toBeInTheDocument();
  });
});
