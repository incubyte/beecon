# ADR-0012: Mapping expression engine — deferred; CEL chosen when adopted

Date: 2026-07-16 · Status: accepted (developer-confirmed)

## Context
Beecon's tool/trigger mappings use a deliberately minimal token grammar (`{input.x}`,
`{params.x}`, `{config.x}`, `{watermark}` — see `execution/template.go`): pure substitution,
no conditionals, functions, or transforms. This keeps definitions *safe by construction*
(nothing to sandbox) but limits expressiveness — surfaced concretely as gaps A (composable
query/header/body values) and C (tool-input defaults) in
`docs/specs/beecon-phase-5-engine-gaps-spec.md`, and as low Membrane-importer fidelity
(conditional `$case`, multi-node trigger flows don't translate).

The question raised: replace the bespoke templating with a real expression/transformation
language. Candidates evaluated at a high level — CEL (`cel-go`), Expr (`expr-lang/expr`),
JSONata (`jsonata-go`), JMESPath. Key facts: mappings are evaluated **only server-side in
Go** (the TypeScript SDK never evaluates them), so there is **no cross-language parity
requirement** — only one solid Go implementation is needed. The main costs of adopting an
engine are a shifted security posture (evaluating an expression language from hot-loaded
definitions vs. sealed non-executable tokens), Go-library maturity, and a definition
format-v2 migration.

## Decision
**Defer adopting an expression engine.** Keep the minimal token grammar, and close the
concrete near-term needs with the scoped, safe additions in the engine-gaps strand
(composable values + tool-input defaults) — no general expression language yet.

**When a real provider need forces richer expressions, adopt CEL (`cel-go`)** — it is the
best fit for safe, non-Turing-complete, resource-bounded evaluation in Go, with a mature
Google-maintained implementation. This pre-commits the engine choice so the decision is
already made if/when triggered; it does not commit to building it now.

## Consequences
- Near term: build engine-gaps A/C as specced; the Membrane importer targets the token
  format (translating `$var`→`{input.x}`, `$firstNotEmpty`→an inputSchema `default`), and
  honestly flags `$case`/conditionals and multi-node triggers as needs-human.
- No format-v2 migration, no sandbox/resource-safety machinery, security stays
  safe-by-construction — consistent with the "small" ethos and the sealed-format posture.
- Output normalization (gap D) and conditional dispatch (gap B) remain out until CEL lands.
- The trigger arrives when the token grammar can't express a mapping a target provider
  genuinely requires (e.g. an OData filter that needs a conditional, or response reshaping);
  revisit then, adopting CEL behind a definition-format bump.
