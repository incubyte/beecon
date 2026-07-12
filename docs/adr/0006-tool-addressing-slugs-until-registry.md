# ADR-0006: Tool addressing — slugs until the registry

Date: 2026-07-12 · Status: accepted (spec PD14 — developer-confirmed)

## Context
Phase 1 addressed its single tool by provider-prefixed slug (`outlook-list-messages`,
PD8). BOUNDARIES.md reserves a `tool_` id prefix, and the Phase 5 registry service is
the planned authority for catalog identity. Phase 2 grows the catalog (Hubspot, more
Outlook tools) and needs a stable addressing story now.

## Decision
Tools remain slug-addressed (`{provider}-{kebab-action}`) through Phases 2–4. No
`tool_` entity ids are minted until the Phase 5 registry assigns them; the registry
becomes the id authority and slugs remain as aliases from then on.

## Consequences
- No id-migration churn while the definition format is still settling.
- Consumers (rolai) code against human-readable slugs, matching Composio ergonomics.
- The `tool_` prefix stays reserved in the id scheme; nothing squats on it before
  Phase 5.
