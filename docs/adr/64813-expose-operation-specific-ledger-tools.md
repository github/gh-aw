# ADR-64813: Expose operation-specific ledger tools

**Date**: 2026-10-01
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

Map ledgers currently expose the low-level `ledger_append` format directly to agents. The PR description and diff show that this requires agents to construct operation envelopes even when the workflow already knows the built-in ledger type and expected operation fields. The change spans tool generation, safe-output handler routing, tool registration, generated workflow prompts, tests, and ledger documentation, which indicates a cross-cutting architectural decision rather than a local refactor. The visible constraints are that durable ingestion must continue through the existing safe-output pipeline, built-in ledger operations must still be validated against configured ledgers, and mixed workflows with both built-in and general ledgers must avoid exposing conflicting tool surfaces.

### Decision

We will expose dedicated safe-output tools for built-in `map` ledgers and route those tools through a shared handler that converts them into `ledger_append`-compatible entries. We will hide the generic `ledger_append` tool when all configured ledgers are built-in `map` types, and restrict it to non-built-in ledgers in mixed configurations. We chose this so agents can call operation-specific tools with simpler arguments while the trusted ingestion path, validation flow, and durable record format remain centralized.

### Alternatives Considered

#### Alternative 1: Keep exposing only the generic `ledger_append` tool

This preserves a single low-level interface for all ledgers and avoids extra tool-generation and dispatch logic. It was not chosen because the PR evidence shows that forcing agents to construct operation envelopes for built-in ledger types is unnecessarily low-level and makes the agent-facing tool surface harder to use correctly.

#### Alternative 2: Expose dedicated built-in tools but always keep `ledger_append` broadly available

This would give agents a friendlier path while retaining the escape hatch of the generic append tool for every ledger configuration. It was not chosen because the diff explicitly removes or narrows `ledger_append` when built-in ledger types are configured, which reduces overlapping interfaces and avoids encouraging agents to bypass the operation-specific tools.

### Consequences

#### Positive
- Agents can call `map` operations through purpose-built tools instead of manually constructing low-level append envelopes.
- Durable ingestion stays centralized because the built-in tools are translated into `ledger_append`-compatible records internally.
- Mixed-ledger configurations retain support for general ledgers while narrowing the generic append surface to the ledgers that still need it.

#### Negative
- Safe-output tool generation, registration, and handler routing become more complex and must stay synchronized across code, tests, and generated workflows.
- The system now depends on configuration-aware rules to decide when `ledger_append` is hidden, restricted, or required.
- Documentation and prompts must explain both the specialized built-in tools and the remaining generic path for other ledger types.

#### Neutral
- The persisted ledger record model does not change; the new tools are an agent-facing abstraction over the existing append flow.
- Validation still happens against configured ledgers and supported built-in operations before requests are queued.
- Generated workflow prompt text shifts from referring specifically to the `ledger append` safe output toward the configured ledger safe-output tools.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
