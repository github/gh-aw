# ADR-64354: Add standalone safe-output-backed ledger configuration

**Date**: 2026-09-29
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request redesigns ledger support around a new `tools.ledger` configuration instead of the legacy `tools.repo-memory.ledger` path. The diff adds standalone ledger parsing, schema validation, prompt generation, safe-output tool exposure, transaction normalization, and a dedicated `push_ledger_changes` workflow job that keeps durable Git reconciliation in a trusted post-agent boundary. The PR description also introduces read-only SQLite projections and `ledgers/<name>` branch conventions for inspecting prior records without granting agents direct write access to ledger state. The architectural decision is how gh-aw should model persistent ledgers while preserving the repository's existing safe-output and trusted-job separation.

### Decision

We will introduce standalone Git-backed ledgers through `tools.ledger`, expose agent writes only through the new `ledger_append` safe output, and defer durable persistence to a dedicated `push_ledger_changes` job. Ledger definitions may use a concise single-ledger form or named multi-ledger form, with repository-relative or inline schema validation, bounded record and segment limits, and deterministic record ID normalization for same-batch temporary references. We will reject the legacy `tools.repo-memory.ledger` configuration so workflows migrate to one explicit ledger model. We chose this because the PR evidence consistently separates agent-requested appends from trusted persistence and makes ledger behavior explicit in workflow configuration, prompts, validation, and job orchestration.

### Alternatives Considered

#### Alternative 1: Keep ledger support nested under `tools.repo-memory`

This was realistic because the repository already has repo-memory infrastructure and the PR explicitly removes the legacy `tools.repo-memory.ledger` shape. It was not chosen because the new diff treats ledgers as a separate storage capability with its own prompt semantics, safe-output tool, validation rules, and trusted reconciliation job, which would stay obscured and harder to govern if left embedded in repo-memory.

#### Alternative 2: Let the agent write ledger state directly during execution

This was considered because direct writes could reduce the number of workflow stages and avoid a follow-up reconciliation job. It was not chosen because the new implementation repeatedly enforces trusted boundaries: agents only emit `ledger_append` requests, SQLite projections are read-only and disposable, and `push_ledger_changes` is the only place intended to reconcile durable Git-backed state.

### Consequences

#### Positive
- Ledger persistence becomes an explicit first-class workflow capability with dedicated parsing, schema validation, and prompt guidance.
- Agents can reference prior ledger state through read-only SQLite projections while durable writes remain behind safe-output validation and a trusted post-agent job.
- Deterministic temporary-ID normalization and `ledgers/<name>` branch naming create a stable model for multi-record append batches and independent ledgers.

#### Negative
- The workflow compiler, safe-output configuration, and setup action gain new moving parts that increase implementation and maintenance complexity.
- Existing workflows using `tools.repo-memory.ledger` must migrate and will fail until they adopt the new `tools.ledger` structure.
- The design introduces another persistence boundary (`push_ledger_changes`) that future contributors must understand when debugging ledger behavior.

#### Neutral
- The change adds new JavaScript helpers and Go types/tests without yet showing full durable write reconciliation logic in this PR.
- Safe-output capability computation and validation now include `ledger_append` alongside existing tools.
- Workflow prompts now document ledger projections and persistence timing as part of the unified prompt assembly.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
