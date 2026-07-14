import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { ProviderDefinitionDetail, ProviderDefinitionSummary, ProviderDefinitionsPage } from "@/lib/api-types";

import { TriggerDefinitionsPage } from "./TriggerDefinitionsPage";

function renderTriggerDefinitionsPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <TriggerDefinitionsPage />
    </QueryClientProvider>,
  );
}

function outlookBundle(): ProviderDefinitionDetail {
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
      oauth: {},
      mapping: {},
      expectedParams: [],
      tools: [],
      triggers: [
        {
          slug: "outlook-message-received",
          name: "New message received",
          description: "Triggered when a new message arrives.",
          configSchema: { type: "object", properties: { folderId: { type: "string" } } },
          payloadSchema: { type: "object", properties: { id: { type: "string" } } },
          ingestion: "poll",
          pollIntervalSeconds: 60,
        },
      ],
    },
  };
}

function hubspotBundle(): ProviderDefinitionDetail {
  return {
    slug: "hubspot",
    name: "Hubspot",
    formatVersion: 1,
    bundle: {
      formatVersion: 1,
      slug: "hubspot",
      name: "Hubspot",
      logo: "",
      authScheme: "oauth2",
      oauth: {},
      mapping: {},
      expectedParams: [],
      tools: [],
      triggers: [
        {
          slug: "hubspot-contact-created",
          name: "New contact created",
          description: "Triggered when a new CRM contact is created.",
          configSchema: { type: "object" },
          payloadSchema: { type: "object", properties: { id: { type: "string" } } },
          ingestion: "poll",
          pollIntervalSeconds: 60,
        },
      ],
    },
  };
}

/** mockCatalog mirrors ToolsPage.test.tsx's own helper: the trigger
 * definitions catalog is derived client-side from ListProviderDefinitions +
 * ProviderDefinitionDetail (Slice 6, AC3) — no dedicated endpoint exists. */
function mockCatalog(bundles: ProviderDefinitionDetail[]) {
  const summaries: ProviderDefinitionSummary[] = bundles.map((b) => ({
    slug: b.slug,
    name: b.name,
    logo: "",
    authScheme: b.bundle.authScheme,
    formatVersion: b.formatVersion,
    toolCount: b.bundle.tools.length,
    triggerCount: b.bundle.triggers.length,
  }));
  server.use(
    http.get("/api/v1/provider-definitions", () => HttpResponse.json({ items: summaries } satisfies ProviderDefinitionsPage)),
    ...bundles.map((detail) => http.get(`/api/v1/provider-definitions/${detail.slug}`, () => HttpResponse.json(detail))),
  );
}

describe("TriggerDefinitionsPage", () => {
  it("renders one row per trigger definition flattened across every loaded provider's bundle, tagged with its owning provider and ingestion mode", async () => {
    mockCatalog([outlookBundle(), hubspotBundle()]);
    renderTriggerDefinitionsPage();

    const table = await screen.findByRole("table");
    await within(table).findByTitle("hubspot-contact-created");
    expect(within(table).getByText("New contact created")).toBeInTheDocument();
    expect(within(table).getByText("Hubspot")).toBeInTheDocument();
    expect(within(table).getByTitle("outlook-message-received")).toBeInTheDocument();
    expect(within(table).getByText("Outlook")).toBeInTheDocument();
    expect(within(table).getAllByText("poll").length).toBe(2);
  });

  it("filters the table down to one provider's triggers via the provider select, restoring every row when reset", async () => {
    mockCatalog([outlookBundle(), hubspotBundle()]);
    renderTriggerDefinitionsPage();

    const table = await screen.findByRole("table");
    await within(table).findByTitle("hubspot-contact-created");
    within(table).getByTitle("outlook-message-received");

    const providerSelect = screen.getByLabelText(/^provider$/i) as HTMLSelectElement;
    fireEvent.change(providerSelect, { target: { value: "hubspot" } });

    await waitFor(() => expect(within(table).queryByTitle("outlook-message-received")).not.toBeInTheDocument());
    expect(within(table).getByTitle("hubspot-contact-created")).toBeInTheDocument();
    expect(screen.getByText(/provider: hubspot/i)).toBeInTheDocument();

    fireEvent.change(providerSelect, { target: { value: "" } });
    await within(table).findByTitle("outlook-message-received");
  });

  it("opening a trigger definition row shows its config schema, payload schema, and ingestion mode in the drawer via CodeViewer", async () => {
    mockCatalog([outlookBundle()]);
    renderTriggerDefinitionsPage();

    const row = await screen.findByTitle("outlook-message-received");
    row.closest("tr")?.click();

    expect(await screen.findByText("Trigger definition detail")).toBeInTheDocument();
    expect(screen.getByText(/"folderId": \{/)).toBeInTheDocument();
    expect(screen.getByText(/"id": \{/)).toBeInTheDocument();
    // Ingestion mode is a DetailRow — plain text, never color-only.
    const ingestionRow = screen.getByText("Ingestion mode").closest("dt")?.parentElement;
    expect(within(ingestionRow as HTMLElement).getByText("poll")).toBeInTheDocument();
  });

  it("shows the empty state when no loaded provider declares any trigger definitions", async () => {
    mockCatalog([]);
    renderTriggerDefinitionsPage();

    expect(await screen.findByText(/no trigger definitions declared/i)).toBeInTheDocument();
  });

  it("shows an inline error card with a Retry action when the provider-definitions list request fails", async () => {
    server.use(
      http.get("/api/v1/provider-definitions", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "The catalog is unavailable." } }, { status: 500 }),
      ),
    );
    renderTriggerDefinitionsPage();

    expect(await screen.findByText("The catalog is unavailable.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });
});
