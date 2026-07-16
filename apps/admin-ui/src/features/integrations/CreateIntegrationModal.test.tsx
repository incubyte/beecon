import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";

import { server } from "@/test/msw/server";
import type { IntegrationSummary, ProviderDefinitionSummary, ProviderDefinitionsPage } from "@/lib/api-types";

import { CreateIntegrationModal } from "./CreateIntegrationModal";

function providerDefinition(overrides: Partial<ProviderDefinitionSummary> = {}): ProviderDefinitionSummary {
  return {
    slug: "outlook",
    name: "Outlook",
    logo: "",
    authScheme: "oauth2",
    formatVersion: 1,
    toolCount: 2,
    triggerCount: 1,
    ...overrides,
  };
}

function summary(overrides: Partial<IntegrationSummary> = {}): IntegrationSummary {
  return {
    id: "int_1",
    providerSlug: "outlook",
    name: "Outlook",
    logo: "",
    authScheme: "oauth2",
    ...overrides,
  };
}

function mockProviderDefinitions(items: ProviderDefinitionSummary[]) {
  server.use(http.get("/api/v1/provider-definitions", () => HttpResponse.json({ items } satisfies ProviderDefinitionsPage)));
}

function renderCreateIntegrationModal(onCreated = vi.fn()) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const utils = render(
    <QueryClientProvider client={queryClient}>
      <CreateIntegrationModal open={true} onOpenChange={vi.fn()} onCreated={onCreated} />
    </QueryClientProvider>,
  );
  return { onCreated, ...utils };
}

describe("CreateIntegrationModal", () => {
  it("populates the provider dropdown from provider definitions (label = name, value = slug)", async () => {
    mockProviderDefinitions([
      providerDefinition({ slug: "outlook", name: "Outlook" }),
      providerDefinition({ slug: "hubspot", name: "HubSpot" }),
    ]);
    renderCreateIntegrationModal();

    const outlook = (await screen.findByRole("option", { name: "Outlook" })) as HTMLOptionElement;
    const hubspot = screen.getByRole("option", { name: "HubSpot" }) as HTMLOptionElement;
    expect(outlook.value).toBe("outlook");
    expect(hubspot.value).toBe("hubspot");
  });

  it("keeps the submit button disabled until a provider, client id, and client secret are all supplied", async () => {
    mockProviderDefinitions([providerDefinition()]);
    renderCreateIntegrationModal();
    await screen.findByRole("option", { name: "Outlook" });

    const submit = screen.getByRole("button", { name: /^create integration$/i });
    expect(submit).toBeDisabled();

    fireEvent.change(screen.getByRole("combobox"), { target: { value: "outlook" } });
    fireEvent.change(screen.getByLabelText(/client id/i), { target: { value: "client-abc" } });
    expect(submit).toBeDisabled();

    fireEvent.change(screen.getByLabelText(/client secret/i), { target: { value: "s3cr3t" } });
    expect(submit).toBeEnabled();
  });

  it("submits the chosen provider slug and client credentials, then calls onCreated with the summary", async () => {
    mockProviderDefinitions([providerDefinition({ slug: "hubspot", name: "HubSpot" })]);
    let requestBody: unknown;
    const responseBody = summary({ id: "int_new", providerSlug: "hubspot", name: "HubSpot" });
    server.use(
      http.post("/api/v1/integrations", async ({ request }) => {
        requestBody = await request.json();
        return HttpResponse.json(responseBody, { status: 201 });
      }),
    );
    const { onCreated } = renderCreateIntegrationModal();
    await screen.findByRole("option", { name: "HubSpot" });

    fireEvent.change(screen.getByRole("combobox"), { target: { value: "hubspot" } });
    fireEvent.change(screen.getByLabelText(/client id/i), { target: { value: "client-abc" } });
    fireEvent.change(screen.getByLabelText(/client secret/i), { target: { value: "super-secret" } });
    fireEvent.click(screen.getByRole("button", { name: /^create integration$/i }));

    await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(1));
    expect(requestBody).toEqual({ providerSlug: "hubspot", clientId: "client-abc", clientSecret: "super-secret" });
    expect(onCreated).toHaveBeenCalledWith(responseBody);
  });

  it("shows a Creating… loading state while the create request is in flight", async () => {
    mockProviderDefinitions([providerDefinition()]);
    server.use(
      http.post("/api/v1/integrations", async () => {
        await new Promise((resolve) => setTimeout(resolve, 30));
        return HttpResponse.json(summary(), { status: 201 });
      }),
    );
    renderCreateIntegrationModal();
    await screen.findByRole("option", { name: "Outlook" });

    fireEvent.change(screen.getByRole("combobox"), { target: { value: "outlook" } });
    fireEvent.change(screen.getByLabelText(/client id/i), { target: { value: "client-abc" } });
    fireEvent.change(screen.getByLabelText(/client secret/i), { target: { value: "super-secret" } });
    fireEvent.click(screen.getByRole("button", { name: /^create integration$/i }));

    expect(await screen.findByRole("button", { name: /creating…/i })).toBeInTheDocument();
  });

  it("shows an inline error and does not call onCreated when the create request fails", async () => {
    mockProviderDefinitions([providerDefinition()]);
    server.use(
      http.post("/api/v1/integrations", () =>
        HttpResponse.json({ error: { code: "internal_error", message: "The integration could not be created." } }, { status: 500 }),
      ),
    );
    const { onCreated } = renderCreateIntegrationModal();
    await screen.findByRole("option", { name: "Outlook" });

    fireEvent.change(screen.getByRole("combobox"), { target: { value: "outlook" } });
    fireEvent.change(screen.getByLabelText(/client id/i), { target: { value: "client-abc" } });
    fireEvent.change(screen.getByLabelText(/client secret/i), { target: { value: "super-secret" } });
    fireEvent.click(screen.getByRole("button", { name: /^create integration$/i }));

    expect(await screen.findByText("The integration could not be created.")).toBeInTheDocument();
    expect(onCreated).not.toHaveBeenCalled();
  });

  it("never renders the submitted client secret back into the DOM", async () => {
    mockProviderDefinitions([providerDefinition()]);
    server.use(http.post("/api/v1/integrations", () => HttpResponse.json(summary(), { status: 201 })));
    const { onCreated } = renderCreateIntegrationModal();
    await screen.findByRole("option", { name: "Outlook" });

    fireEvent.change(screen.getByRole("combobox"), { target: { value: "outlook" } });
    fireEvent.change(screen.getByLabelText(/client id/i), { target: { value: "client-abc" } });
    fireEvent.change(screen.getByLabelText(/client secret/i), { target: { value: "top-secret-value" } });
    fireEvent.click(screen.getByRole("button", { name: /^create integration$/i }));

    await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(1));
    expect(screen.queryByText("top-secret-value")).not.toBeInTheDocument();
  });
});
