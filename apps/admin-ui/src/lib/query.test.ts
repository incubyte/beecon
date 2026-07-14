import { describe, expect, it } from "vitest";

import { queryKeys } from "./query";

/**
 * Pins architecture doc §2.4's org-scoping guarantee at the query-key level:
 * every org-bound key embeds the selected organization's id, so switching
 * the org changes the key TanStack Query caches under — a stale cache entry
 * for one org can never be read back for another org's key. The
 * UI-level proof that a switch actually triggers a refetch and re-renders
 * lives in ConnectionsPage.test.tsx's own re-scoping test.
 */
describe("queryKeys (org-scoped key factory)", () => {
  it("embeds the org id in the connections list key, producing distinct keys per org", () => {
    const orgAKey = queryKeys.connections.list("org_a");
    const orgBKey = queryKeys.connections.list("org_b");

    expect(orgAKey).toContain("org_a");
    expect(orgBKey).toContain("org_b");
    expect(orgAKey).not.toEqual(orgBKey);
  });

  it("embeds the org id in the connections detail key, distinct per org for the same connection id", () => {
    const orgAKey = queryKeys.connections.detail("org_a", "conn_1");
    const orgBKey = queryKeys.connections.detail("org_b", "conn_1");

    expect(orgAKey).toContain("org_a");
    expect(orgBKey).toContain("org_b");
    expect(orgAKey).not.toEqual(orgBKey);
  });

  it("embeds the org id in the trigger-instances list key, producing distinct keys per org", () => {
    const orgAKey = queryKeys.triggerInstances.list("org_a");
    const orgBKey = queryKeys.triggerInstances.list("org_b");

    expect(orgAKey).toContain("org_a");
    expect(orgBKey).toContain("org_b");
    expect(orgAKey).not.toEqual(orgBKey);
  });

  it("embeds the org id in the trigger-instances detail key, distinct per org for the same instance id", () => {
    const orgAKey = queryKeys.triggerInstances.detail("org_a", "trg_1");
    const orgBKey = queryKeys.triggerInstances.detail("org_b", "trg_1");

    expect(orgAKey).toContain("org_a");
    expect(orgBKey).toContain("org_b");
    expect(orgAKey).not.toEqual(orgBKey);
  });

  // Slice 3 additions.

  it("embeds the org id in the logs list key, distinct per org for identical filters", () => {
    const filters = { connectionId: "", userId: "", toolSlug: "", from: "", to: "" };
    const orgAKey = queryKeys.logs.list("org_a", filters);
    const orgBKey = queryKeys.logs.list("org_b", filters);

    expect(orgAKey).toContain("org_a");
    expect(orgBKey).toContain("org_b");
    expect(orgAKey).not.toEqual(orgBKey);
  });

  it("embeds the org id in the events list key, distinct per org for identical filters", () => {
    const filters = { type: "", deliveryStatus: "" };
    const orgAKey = queryKeys.events.list("org_a", filters);
    const orgBKey = queryKeys.events.list("org_b", filters);

    expect(orgAKey).toContain("org_a");
    expect(orgBKey).toContain("org_b");
    expect(orgAKey).not.toEqual(orgBKey);
  });

  // The dashboard summary is installation-wide (architecture doc §3, §2.4):
  // unlike every other Slice 3 key above, it must NOT embed an org id at
  // all — switching the selected org must never change (and so never
  // refetch) this key.
  it("the dashboard metrics key carries no org id and never changes when the selected org changes", () => {
    const key = queryKeys.dashboard.metrics();

    expect(key).toEqual(["dashboard", "metrics"]);
    expect(key).not.toContain("org_a");
    expect(key).not.toContain("org_b");
    expect(queryKeys.dashboard.metrics()).toEqual(key);
  });

  // Slice 6 additions: the CATALOG area (Providers/Tools/Trigger
  // Definitions) reads GET /provider-definitions (+/{slug}), an
  // admin-guarded, installation-wide route with no orgId in its path at all
  // (architecture doc §3.1, AC7 — never governance-filtered). Mirrors the
  // dashboard metrics key's own "no org id, never changes on org switch"
  // guarantee above.
  it("the provider-definitions list key carries no org id and never changes when the selected org changes", () => {
    const key = queryKeys.providerDefinitions.list();

    expect(key).toEqual(["provider-definitions", "list"]);
    expect(key).not.toContain("org_a");
    expect(key).not.toContain("org_b");
    expect(queryKeys.providerDefinitions.list()).toEqual(key);
  });

  it("the provider-definitions detail key carries no org id, only the requested slug", () => {
    const key = queryKeys.providerDefinitions.detail("outlook");

    expect(key).toEqual(["provider-definitions", "detail", "outlook"]);
    expect(key).not.toContain("org_a");
    expect(key).not.toContain("org_b");
  });

  it("the provider-definitions bundles key (Tools/Trigger Definitions catalog) carries no org id and never changes when the selected org changes", () => {
    const key = queryKeys.providerDefinitions.bundles();

    expect(key).toEqual(["provider-definitions", "bundles"]);
    expect(key).not.toContain("org_a");
    expect(key).not.toContain("org_b");
    expect(queryKeys.providerDefinitions.bundles()).toEqual(key);
  });
});
