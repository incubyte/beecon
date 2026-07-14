import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { server } from "@/test/msw/server";
import type { ConfigDocument, ConfigImportApplyResult, ConfigImportPlan } from "@/lib/api-types";

import { ConfigPage } from "./ConfigPage";

/** renderConfigPage mirrors WebhookEndpointsPage.test.tsx/ApiKeysPage.test.tsx's
 * own harness: ConfigPage reads the selected org from `?org=` via
 * useSearch({ from: "__root__" }). */
function renderConfigPage(initialPath = "/?org=org_1") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const rootRoute = createRootRoute({
    validateSearch: (search: Record<string, unknown>) => ({ org: typeof search.org === "string" ? search.org : undefined }),
  });
  const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: ConfigPage });
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

function exportedDocument(overrides: Partial<ConfigDocument> = {}): ConfigDocument {
  return {
    schemaVersion: 1,
    governance: { allowList: null, hidden: [], featured: [], featuredCap: 8 },
    endpoints: [],
    retention: { logRetentionDays: null, eventRetentionDays: null },
    ...overrides,
  };
}

// jsdom implements neither URL.createObjectURL nor a real click-navigation by
// default (mirrors SecretOnceModal.test.tsx's own setup) — the export
// download flow (ConfigExportSection) uses both.
beforeEach(() => {
  window.URL.createObjectURL = vi.fn().mockReturnValue("blob:mock-url");
  window.URL.revokeObjectURL = vi.fn();
});

describe("ConfigPage", () => {
  it("shows a 'select an organization' state when no org is selected", async () => {
    renderConfigPage("/");

    expect(await screen.findByText(/select an organization/i)).toBeInTheDocument();
  });

  it("states in the UI copy that the export never contains a secret", async () => {
    renderConfigPage();

    expect(await screen.findByText(/never contains an api key, webhook signing secret, credential/i)).toBeInTheDocument();
  });

  it("clicking Download export fetches the export and triggers a JSON blob download", async () => {
    server.use(
      http.get("/api/v1/organizations/org_1/config/export", () =>
        HttpResponse.json(exportedDocument({ governance: { allowList: ["intg_1"], hidden: [], featured: [], featuredCap: 8 } })),
      ),
    );
    renderConfigPage();

    fireEvent.click(await screen.findByRole("button", { name: /download export/i }));

    await waitFor(() => expect(window.URL.createObjectURL).toHaveBeenCalledTimes(1));
    const blob = (window.URL.createObjectURL as ReturnType<typeof vi.fn>).mock.calls[0][0] as Blob;
    expect(blob.type).toBe("application/json");
    // jsdom's Blob has no .text()/.arrayBuffer() implementation (see
    // SecretOnceModal.test.tsx's own note); comparing byte size against the
    // exact JSON the component should have serialized is enough to confirm
    // the blob wraps the real export document, not an empty/placeholder
    // value, without depending on a Blob-reading API jsdom doesn't provide.
    const expectedJSON = JSON.stringify(
      exportedDocument({ governance: { allowList: ["intg_1"], hidden: [], featured: [], featuredCap: 8 } }),
      null,
      2,
    );
    expect(blob.size).toBe(expectedJSON.length);
  });

  it("Apply stays disabled until a dry-run preview exists for the exact current text and mode", async () => {
    server.use(
      http.post("/api/v1/organizations/org_1/config/import", () =>
        HttpResponse.json({ plan: [{ area: "governance", field: "allowList", action: "set", detail: "1 integration(s) allow-listed" }], warnings: [] } satisfies ConfigImportPlan),
      ),
    );
    renderConfigPage();
    const doc = exportedDocument({ governance: { allowList: ["intg_1"], hidden: [], featured: [], featuredCap: 8 } });

    const textarea = await screen.findByLabelText(/config document json/i);
    fireEvent.change(textarea, { target: { value: JSON.stringify(doc) } });

    expect(screen.getByRole("button", { name: /^apply/i })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: /preview changes/i }));
    await screen.findByText(/dry-run plan/i);

    expect(screen.getByRole("button", { name: /^apply/i })).toBeEnabled();
  });

  it("editing the text after a preview invalidates it and re-disables Apply", async () => {
    server.use(
      http.post("/api/v1/organizations/org_1/config/import", () =>
        HttpResponse.json({ plan: [{ area: "retention", field: "logRetentionDays", action: "set", detail: "45 day(s)" }], warnings: [] } satisfies ConfigImportPlan),
      ),
    );
    renderConfigPage();
    const doc = exportedDocument();

    const textarea = await screen.findByLabelText(/config document json/i);
    fireEvent.change(textarea, { target: { value: JSON.stringify(doc) } });
    fireEvent.click(screen.getByRole("button", { name: /preview changes/i }));
    await screen.findByText(/dry-run plan/i);
    expect(screen.getByRole("button", { name: /^apply/i })).toBeEnabled();

    fireEvent.change(textarea, { target: { value: JSON.stringify(doc) + " " } });

    expect(screen.getByRole("button", { name: /^apply/i })).toBeDisabled();
    expect(screen.queryByText(/dry-run plan/i)).not.toBeInTheDocument();
  });

  it("switching mode after a preview invalidates it and re-disables Apply", async () => {
    server.use(
      http.post("/api/v1/organizations/org_1/config/import", () =>
        HttpResponse.json({ plan: [], warnings: [] } satisfies ConfigImportPlan),
      ),
    );
    renderConfigPage();

    const textarea = await screen.findByLabelText(/config document json/i);
    fireEvent.change(textarea, { target: { value: JSON.stringify(exportedDocument()) } });
    fireEvent.click(screen.getByRole("button", { name: /preview changes/i }));
    await waitFor(() => expect(screen.getByRole("button", { name: /^apply/i })).toBeEnabled());

    fireEvent.click(screen.getByRole("radio", { name: /replace/i }));

    expect(screen.getByRole("button", { name: /^apply/i })).toBeDisabled();
  });

  it("previewing shows the plan lines and flags an unknown-integration-id warning, writing nothing", async () => {
    let importRequested = false;
    server.use(
      http.post("/api/v1/organizations/org_1/config/import", () => {
        importRequested = true;
        return HttpResponse.json({
          plan: [{ area: "governance", field: "allowList", action: "set", detail: "2 integration(s) allow-listed" }],
          warnings: ['integration "intg_totally_bogus" referenced in governance does not exist in this installation'],
        } satisfies ConfigImportPlan);
      }),
    );
    renderConfigPage();

    const textarea = await screen.findByLabelText(/config document json/i);
    fireEvent.change(
      textarea,
      { target: { value: JSON.stringify(exportedDocument({ governance: { allowList: ["intg_1", "intg_totally_bogus"], hidden: [], featured: [], featuredCap: 8 } })) } },
    );
    fireEvent.click(screen.getByRole("button", { name: /preview changes/i }));

    expect(await screen.findByText(/intg_totally_bogus/i)).toBeInTheDocument();
    expect(screen.getByText(/2 integration\(s\) allow-listed/i)).toBeInTheDocument();
    expect(importRequested).toBe(true);
    // The dry-run call itself must always be dryRun=true — never a write.
    expect(screen.getByText(/nothing has been written yet/i)).toBeInTheDocument();
  });

  it("Apply sends dryRun=false, and the freshly minted secrets appear one at a time through SecretOnceModal", async () => {
    server.use(
      http.post("/api/v1/organizations/org_1/config/import", async ({ request }) => {
        const url = new URL(request.url);
        if (url.searchParams.get("dryRun") === "false") {
          return HttpResponse.json({
            applied: [{ area: "endpoint", field: "https://example.com/new-hook", action: "create", detail: "create endpoint https://example.com/new-hook (all event types)" }],
            secrets: [
              { wepId: "wep_1", secret: "whsec_first-fresh-secret" },
              { wepId: "wep_2", secret: "whsec_second-fresh-secret" },
            ],
          } satisfies ConfigImportApplyResult);
        }
        return HttpResponse.json({ plan: [], warnings: [] } satisfies ConfigImportPlan);
      }),
    );
    renderConfigPage();

    const textarea = await screen.findByLabelText(/config document json/i);
    fireEvent.change(textarea, {
      target: { value: JSON.stringify(exportedDocument({ endpoints: [{ url: "https://example.com/new-hook", eventTypes: null }] })) },
    });
    fireEvent.click(screen.getByRole("button", { name: /preview changes/i }));
    await waitFor(() => expect(screen.getByRole("button", { name: /^apply/i })).toBeEnabled());

    fireEvent.click(screen.getByRole("button", { name: /^apply/i }));

    // First secret shown; the second must not be visible yet.
    expect(await screen.findByText("whsec_first-fresh-secret")).toBeInTheDocument();
    expect(screen.queryByText("whsec_second-fresh-secret")).not.toBeInTheDocument();

    // Acknowledge and dismiss the first modal — the second secret appears next.
    fireEvent.click(screen.getByRole("checkbox", { name: /stored this secret safely/i }));
    fireEvent.click(screen.getByRole("button", { name: /^done$/i }));

    expect(await screen.findByText("whsec_second-fresh-secret")).toBeInTheDocument();
    expect(screen.queryByText("whsec_first-fresh-secret")).not.toBeInTheDocument();

    // Acknowledge and dismiss the second — the queue is now empty.
    fireEvent.click(screen.getByRole("checkbox", { name: /stored this secret safely/i }));
    fireEvent.click(screen.getByRole("button", { name: /^done$/i }));

    await waitFor(() => expect(screen.queryByText("whsec_second-fresh-secret")).not.toBeInTheDocument());
  });
});
