# ADR-0004: "Organization" is the only tenancy term; isolation via typed OrgID

Date: 2026-07-10 · Status: accepted (developer decision at discovery review)

## Context
Discovery initially had Installation → Tenant → Organization → User. Developer
collapsed Tenant and Organization into one entity and retired the word "tenant"
entirely. Multi-org data isolation is the highest-risk property of the platform.

## Decision
- Layering: **Installation → Organization → User**. Organization is both isolation
  unit (API keys, webhook/JWT secrets, all data rows) and governance unit.
- The word "tenant" must not appear in code, schema, docs, or API.
- Isolation is enforced structurally, four layers deep:
  1. `organizations.OrgID` is a distinct type; minted only by org creation and key
     verification — raw strings don't compile into port calls.
  2. OrgID enters requests ONLY via authenticated key verification (context), never
     from path/body/query.
  3. Every adapter query filters `organization_id`; a cross-org hit is
     indistinguishable from not-found (404, no existence leak).
  4. Architecture tests enforce it: `test/arch/orgscope_test.go` (reflects over
     repository interfaces, requires OrgID param) and `imports_test.go` (DB imports
     confined to adapters; BOUNDARIES dependency graph).

## Consequences
- Entity naming: Provider ("Outlook") → Integration ("Outlook + installation's OAuth
  client credentials") → Connection ("a user's connected Outlook").
- Installation-level surfaces (org CRUD, Integrations, key-prefix lookup) are
  deliberately unscoped and whitelisted by name in the arch test.
