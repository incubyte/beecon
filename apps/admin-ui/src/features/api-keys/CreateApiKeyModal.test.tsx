import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";

import { server } from "@/test/msw/server";
import type { IssuedApiKey } from "@/lib/api-types";

import { CreateApiKeyModal } from "./CreateApiKeyModal";

const ORG_ID = "org_1";

function renderCreateApiKeyModal(onIssued = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const utils = render(
    <QueryClientProvider client={queryClient}>
      <CreateApiKeyModal orgId={ORG_ID} open={true} onOpenChange={vi.fn()} onIssued={onIssued} />
    </QueryClientProvider>,
  );
  return { onIssued, ...utils };
}

function issued(overrides: Partial<IssuedApiKey> = {}): IssuedApiKey {
  return {
    id: "key_1",
    key: "beecon_sk_the-full-secret",
    prefix: "beecon_sk_ab",
    scope: "read-write",
    createdAt: "2026-01-01T00:00:00.000Z",
    ...overrides,
  };
}

describe("CreateApiKeyModal", () => {
  it("defaults to the read-write scope choice", () => {
    renderCreateApiKeyModal();

    expect(screen.getByRole("radio", { name: /read-write/i })).toBeChecked();
    expect(screen.getByRole("radio", { name: /read-only/i })).not.toBeChecked();
  });

  it("choosing read-only sends scope=read-only in the create request", async () => {
    let requestBody: unknown;
    server.use(
      http.post(`/api/v1/organizations/${ORG_ID}/api-keys`, async ({ request }) => {
        requestBody = await request.json();
        return HttpResponse.json(issued({ scope: "read-only" }), { status: 201 });
      }),
    );
    const { onIssued } = renderCreateApiKeyModal();

    fireEvent.click(screen.getByRole("radio", { name: /read-only/i }));
    fireEvent.click(screen.getByRole("button", { name: /create key/i }));

    await waitFor(() => expect(onIssued).toHaveBeenCalledTimes(1));
    expect(requestBody).toEqual({ scope: "read-only" });
  });

  it("choosing read-write (the default, left unchanged) sends scope=read-write", async () => {
    let requestBody: unknown;
    server.use(
      http.post(`/api/v1/organizations/${ORG_ID}/api-keys`, async ({ request }) => {
        requestBody = await request.json();
        return HttpResponse.json(issued({ scope: "read-write" }), { status: 201 });
      }),
    );
    const { onIssued } = renderCreateApiKeyModal();

    fireEvent.click(screen.getByRole("button", { name: /create key/i }));

    await waitFor(() => expect(onIssued).toHaveBeenCalledTimes(1));
    expect(requestBody).toEqual({ scope: "read-write" });
  });

  it("calls onIssued with the full returned secret exactly once, handing the credential ceremony to the caller", async () => {
    const responseBody = issued({ scope: "read-only", key: "beecon_sk_freshly-issued-secret" });
    server.use(http.post(`/api/v1/organizations/${ORG_ID}/api-keys`, () => HttpResponse.json(responseBody, { status: 201 })));
    const { onIssued } = renderCreateApiKeyModal();

    fireEvent.click(screen.getByRole("radio", { name: /read-only/i }));
    fireEvent.click(screen.getByRole("button", { name: /create key/i }));

    await waitFor(() => expect(onIssued).toHaveBeenCalledTimes(1));
    expect(onIssued).toHaveBeenCalledWith(responseBody);
  });

  it("shows an inline error and does not call onIssued when the create request fails", async () => {
    server.use(
      http.post(`/api/v1/organizations/${ORG_ID}/api-keys`, () =>
        HttpResponse.json({ error: { code: "internal_error", message: "Could not issue the key." } }, { status: 500 }),
      ),
    );
    const { onIssued } = renderCreateApiKeyModal();

    fireEvent.click(screen.getByRole("button", { name: /create key/i }));

    expect(await screen.findByText("Could not issue the key.")).toBeInTheDocument();
    expect(onIssued).not.toHaveBeenCalled();
  });
});
