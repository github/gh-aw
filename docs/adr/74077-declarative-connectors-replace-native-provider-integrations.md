# ADR-74077: Declarative Connectors Replace Native Provider Integrations

**Date**: 2026-11-03
**Status**: Proposed (pending maintainer review)
**Deciders**: Pending maintainer review; no approval recorded

---

### Context

Third-party integrations need provider-specific API contracts, credentials,
resource grants, event admission, and mutation policies. Maintaining separate
native handlers for each provider duplicates these concerns and makes extending
an integration require changes to the compiler or trusted runtime. Importing an
integration must not implicitly enable its tools, writes, or events, and agent
arguments or provider responses must not expand reviewed authority. The
experimental connector implementation in [PR #74077](https://github.com/github/gh-aw/pull/74077)
replaces the former built-in Azure DevOps, Jira, and Linear integration paths
with shared declarative infrastructure.

### Decision

Use editable AW Markdown declarations under
`.github/workflows/connectors/<provider>.md`, composed through ordinary
`imports`, `uses`, `with`, and `import-schema`, rather than a compiler-owned
provider catalog. This keeps provider contracts reviewable as repository source
while concentrating execution and authority checks in a shared compiler and
runtime.

Keep the following boundaries:

- Importing a declaration selects nothing. Consumers independently select
  bounded reads through `tools.<id>`, qualified effects through
  `safe-outputs.<id>-<operation>`, and protected ingress through
  `on.gateway-dispatch.<id>` or its qualified trigger shorthand. No public
  connector `context` capability or automatic event-body hydration is exposed.
- Compile fixed endpoints, exact resource grants, typed operations, and explicit
  selections into frozen phase-specific snapshots. Remove unselected
  capabilities, endpoints, grants, and credential references before delivery to
  the agent or trusted safe-output host. Snapshots contain secret names, not
  values; their digest detects corruption, not authorship or authorization.
- Expose a small selected MCP surface using bounded REST/GraphQL readers or
  exact allowlisted hosted HTTP MCP methods. Do not provide an arbitrary-call
  proxy or infer unsupported provider behavior. Execute selected mutations
  outside the agent credential boundary through the existing safe-output
  pipeline, with typed inputs, operation budgets, and staging.
- Keep staging an explicit workflow/deployment choice, not a claim that every
  effect is staged by default. Global, per-operation, or compiler-forced staging
  suppresses connector mutations, writer credential resolution and minting,
  preflights, and creation receipts; it does not suppress reads or arbitrary
  workflow scripts.
- Lower connector triggers through the existing exclusive webhook gateway,
  retaining source/resource checks, App provenance, and workflow-version
  admission. Compilation produces source-subscription policy, not an
  authenticated receiver deployment. Provider authentication, consent, refresh,
  source enrollment, native-delivery verification, and replay protection remain
  external trusted-host responsibilities. Use manual dispatch until the exact
  receiver integration is supported and approved.

The [runtime contract](../../pkg/connectors/CONTRACT.md),
[connector reference](../src/content/docs/reference/connectors.md), and
[authoring guide](../../.github/aw/connectors.md) describe the supported
declaration and deployment boundaries. The compiler constructs snapshots in
[`pkg/workflow/connectors.go`](../../pkg/workflow/connectors.go) and prunes them
through [`Plan.ForPhase`](../../pkg/connectors/compiled.go); generated readers
and effects use the shared
[`connector_runtime.cjs`](../../actions/setup/js/connector_runtime.cjs).

### Alternatives Considered

#### Retain native provider handlers

Native handlers can implement provider-specific behavior directly and preserve
existing integration paths. They also require compiler/runtime changes for
routine provider additions and duplicate lifecycle and credential boundaries.
Prefer the shared declaration path for supported behavior; unsupported behavior
remains a contract gap rather than an implicit native fallback.

#### Embed a built-in provider catalog or mandatory bundle

A compiler-distributed catalog could provide centrally versioned defaults and
reduce local setup. It would also couple provider changes to CLI releases or
introduce hidden fallback definitions when a reviewed local file differs or is
absent. Keep declarations explicit, editable, and imported through the existing
resolver; pin remote imports to a reviewed revision for reproducibility.

#### Replace all integrations with hosted MCP servers

Hosted MCP servers can maintain provider schemas and offer native read tools;
the connector model supports exact selected methods where suitable. Using them
wholesale would make the model depend on hosted availability, authentication,
tool surfaces, and provider-specific mutation semantics rather than preserving
the shared safe-output boundary. Use hosted MCP selectively for reads, not as a
replacement for protected ingress or trusted mutation execution.

### Consequences

#### Positive

- Provider contracts and grants can be reviewed and changed as ordinary
  Markdown without adding a provider-specific compiler handler.
- Independent selections and phase pruning keep unused capabilities and writer
  credential references out of the agent snapshot.
- Shared execution centralizes request bounds, response evidence, staging, and
  safe-output policy instead of duplicating them per integration.

#### Negative

- The generic compiler and both Go and bundled Node runtime implementations
  carry substantial validation and compatibility complexity. Shared defects can
  affect multiple providers, so parity and boundary tests remain necessary.
- Declarations require ongoing provider API, scope, schema, and response-evidence
  maintenance. Unsupported pagination, authentication routes, or provider
  features cannot be worked around by widening grants or arbitrary API calls.
- Removing native paths requires consumers to migrate selectors and credentials
  and use a connector-capable compiler with its matching bundled runtime.

#### Neutral

- Declarations are trusted, reviewed workflow source; provider content, events,
  MCP responses, samples, and agent arguments remain untrusted data. A frozen
  digest does not replace source review or authenticate ingress.
- Source-only golden consumers live under
  [`pkg/cli/workflows/`](../../pkg/cli/workflows/), not as installed Actions
  workflows or checked-in generated locks. Their manual dispatch, exact specimen
  grants, small read surfaces, and staged effects provide offline compilation
  examples, not proof of live access, model quality, or delivered mutations.
- This record documents the proposed architecture, not maintainer approval,
  security certification, or operational readiness. External credential and
  receiver setup and final deployment review remain separate work.
