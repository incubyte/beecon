import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";

import { server } from "@/test/msw/server";
import type { ProviderDefinitionDetail, ProviderDefinitionSummary, ProviderDefinitionsPage } from "@/lib/api-types";

import { ToolsPage } from "./ToolsPage";

function renderToolsPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <ToolsPage />
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
      tools: [
        {
          slug: "outlook-get-message",
          name: "Get email message",
          description: "Retrieves a message by id.",
          deprecated: false,
          inputSchema: { type: "object", properties: { id: { type: "string" } } },
          outputSchema: { type: "object", properties: { subject: { type: "string" } } },
        },
        {
          slug: "outlook-legacy-tool",
          name: "Legacy tool",
          description: "Deprecated.",
          deprecated: true,
          inputSchema: { type: "object" },
          outputSchema: { type: "object" },
        },
      ],
      triggers: [],
    },
  };
}

function slackBundle(): ProviderDefinitionDetail {
  return {
    slug: "slack",
    name: "Slack",
    formatVersion: 1,
    bundle: {
      formatVersion: 1,
      slug: "slack",
      name: "Slack",
      logo: "",
      authScheme: "oauth2",
      oauth: {},
      mapping: {},
      expectedParams: [],
      tools: [
        {
          slug: "slack-post-message",
          name: "Post message",
          description: "Posts a chat message.",
          deprecated: false,
          inputSchema: { type: "object" },
          outputSchema: { type: "object" },
        },
      ],
      triggers: [],
    },
  };
}

/** mockCatalog wires the same "list every provider, then fetch each one's
 * bundle" sequence useProviderDefinitionBundles performs (Slice 6, AC3):
 * ToolsPage/TriggerDefinitionsPage never call a dedicated tools/trigger-
 * definitions catalog endpoint — they derive their rows client-side by
 * flattening every loaded provider's bundle. */
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

describe("ToolsPage", () => {
  it("renders one row per tool flattened across every loaded provider's bundle, tagged with its owning provider", async () => {
    mockCatalog([outlookBundle(), slackBundle()]);
    renderToolsPage();

    const table = await screen.findByRole("table");
    await within(table).findByTitle("slack-post-message");
    expect(within(table).getByText("Post message")).toBeInTheDocument();
    expect(within(table).getByText("Slack")).toBeInTheDocument();
    expect(within(table).getByTitle("outlook-get-message")).toBeInTheDocument();
    expect(within(table).getAllByText("Outlook").length).toBeGreaterThan(0);
  });

  it("flags a deprecated tool with a text label, distinct from a non-deprecated tool's row", async () => {
    mockCatalog([outlookBundle()]);
    renderToolsPage();

    const legacyToolName = await screen.findByText("Legacy tool");
    const legacyRow = legacyToolName.closest("tr");
    expect(legacyRow).not.toBeNull();
    expect(within(legacyRow as HTMLElement).getByText("Deprecated")).toBeInTheDocument();

    const getMessageRow = screen.getByText("Get email message").closest("tr");
    expect(within(getMessageRow as HTMLElement).queryByText("Deprecated")).not.toBeInTheDocument();
    expect(within(getMessageRow as HTMLElement).getByText("—")).toBeInTheDocument();
  });

  it("filters the table down to one provider's tools via the provider select, restoring every row when reset", async () => {
    mockCatalog([outlookBundle(), slackBundle()]);
    renderToolsPage();

    const table = await screen.findByRole("table");
    await within(table).findByTitle("slack-post-message");
    within(table).getByTitle("outlook-get-message");

    const providerSelect = screen.getByLabelText(/^provider$/i) as HTMLSelectElement;
    fireEvent.change(providerSelect, { target: { value: "slack" } });

    await waitFor(() => expect(within(table).queryByTitle("outlook-get-message")).not.toBeInTheDocument());
    expect(within(table).getByTitle("slack-post-message")).toBeInTheDocument();

    fireEvent.change(providerSelect, { target: { value: "" } });
    await within(table).findByTitle("outlook-get-message");
  });

  it("opening a tool row shows its input and output JSON-Schema in the drawer via CodeViewer", async () => {
    mockCatalog([outlookBundle()]);
    renderToolsPage();

    const row = await screen.findByTitle("outlook-get-message");
    row.closest("tr")?.click();

    expect(await screen.findByText("Tool detail")).toBeInTheDocument();
    expect(screen.getByText(/"id": \{/)).toBeInTheDocument();
    expect(screen.getByText(/"subject": \{/)).toBeInTheDocument();
  });

  it("shows an inline error card with a Retry action when the provider-definitions list request fails", async () => {
    server.use(
      http.get("/api/v1/provider-definitions", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "The catalog is unavailable." } }, { status: 500 }),
      ),
    );
    renderToolsPage();

    expect(await screen.findByText("The catalog is unavailable.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
  });
});
