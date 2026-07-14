import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { ProviderDefinitionDetail } from "@/lib/api-types";

import { ProviderDefinitionDetailPage } from "./ProviderDefinitionDetailPage";

function renderDetailApp(initialPath: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({
    validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
  });
  const detailRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/providers/$slug",
    component: ProviderDefinitionDetailPage,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([detailRoute]),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
}

function outlookBundleDetail(): ProviderDefinitionDetail {
  return {
    slug: "outlook",
    name: "Outlook",
    formatVersion: 1,
    bundle: {
      formatVersion: 1,
      slug: "outlook",
      name: "Outlook",
      logo: "",
      authScheme: "oauth2",
      oauth: { authorizeUrl: "https://example.com/authorize", tokenUrl: "https://example.com/token" },
      mapping: { baseUrl: "https://graph.microsoft.com" },
      expectedParams: [],
      tools: [
        {
          slug: "outlook-get-message",
          name: "Get email message",
          description: "Retrieves a message by id.",
          deprecated: false,
          inputSchema: { type: "object" },
          outputSchema: { type: "object" },
        },
      ],
      triggers: [
        {
          slug: "outlook-message-received",
          name: "New message received",
          description: "Triggered when a new message arrives.",
          configSchema: { type: "object" },
          payloadSchema: { type: "object" },
          ingestion: "poll",
          pollIntervalSeconds: 60,
        },
      ],
    },
  };
}

function mockProviderDefinitionDetail(slug: string, detail: ProviderDefinitionDetail | null) {
  server.use(
    http.get(`/api/v1/provider-definitions/${slug}`, () => {
      if (!detail) {
        return HttpResponse.json({ error: { code: "not_found", message: "provider not found" } }, { status: 404 });
      }
      return HttpResponse.json(detail);
    }),
  );
}

describe("ProviderDefinitionDetailPage", () => {
  it("renders the provider's identity, overview counts, and slug copy chip", async () => {
    mockProviderDefinitionDetail("outlook", outlookBundleDetail());
    renderDetailApp("/providers/outlook");

    expect(await screen.findByRole("heading", { name: "Outlook" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /copy id outlook/i })).toBeInTheDocument();
    expect(screen.getByText("oauth2")).toBeInTheDocument();
  });

  // AC2 + AC6: the full versioned bundle renders through the mono CodeViewer
  // as pretty-printed, structured text — every field is present as real DOM
  // text (not just a color swatch), so the bundle's structure is legible in
  // grayscale and to a screen reader, not only via syntax color.
  it("renders the full versioned bundle through CodeViewer as legible structured text, not color-only", async () => {
    mockProviderDefinitionDetail("outlook", outlookBundleDetail());
    renderDetailApp("/providers/outlook");

    await screen.findByRole("heading", { name: "Outlook" });
    expect(screen.getByText(/"formatVersion": 1/)).toBeInTheDocument();
    expect(screen.getByText(/"authorizeUrl": "https:\/\/example\.com\/authorize"/)).toBeInTheDocument();
    expect(screen.getByText(/"slug": "outlook-get-message"/)).toBeInTheDocument();
    expect(screen.getByText(/"slug": "outlook-message-received"/)).toBeInTheDocument();
    expect(screen.getByText(/"ingestion": "poll"/)).toBeInTheDocument();
  });

  it("shows an inline error card with a Retry action for an unknown/not-found slug", async () => {
    mockProviderDefinitionDetail("does-not-exist", null);
    renderDetailApp("/providers/does-not-exist");

    expect(await screen.findByText("provider not found")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });
});
