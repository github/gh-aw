# ADR-58022: Add Resource Budgets to Logs Downloads

**Date**: 2026-09-02
**Status**: Draft
**Deciders**: pelikhan, adr-writer agent

---

### Context

This pull request changes `gh aw logs` so large log downloads do not consume unbounded GitHub API quota or runner disk space. The PR description and diff add two user-configurable budgets: one for GitHub core API usage during discovery and one for total storage consumed by downloaded log artifacts. The implementation also preserves both budgets in continuation data, shares the storage budget across multi-workflow downloads, and reports partial results when downloads stop because storage is full. Because this changes the operational contract and failure behavior of a core CLI command, the decision should be recorded explicitly.

### Decision

We will add explicit resource-budget controls to `gh aw logs`: `--max-github-api-rate-limit` to cap consumed GitHub core API requests during discovery-mode pagination, and `--max-storage` to stop new artifact downloads once the output directory reaches a configured size. We will carry these limits through single-workflow, multi-workflow, and stdin-driven execution paths where applicable, and we will emit resumable continuation metadata plus partial-result messaging when a storage limit interrupts processing. We chose this approach because the PR is solving a concrete operational problem: repeated large log indexing runs must be bounded so they do not starve other repository actors of API quota or exhaust runner storage.

### Alternatives Considered

#### Alternative 1: Keep Existing Best-Effort Downloads Without Explicit Budgets

Continue using the current cooldown and timeout behavior without exposing an API-usage ceiling or storage cap to users.

This was considered because it preserves the simplest command surface and avoids adding new flags, validation, continuation fields, and partial-result states. It was not chosen because the PR evidence shows that timeout-only behavior does not adequately protect shared API quota or disk capacity during large downloads, especially when runs are resumed or aggregated across multiple workflow targets.

#### Alternative 2: Enforce Fixed Internal Limits With No User Configuration

Add hard-coded API and storage limits inside the logs downloader and stop or wait automatically without exposing budget knobs in the CLI.

This was considered because it would reduce documentation and validation complexity while still introducing bounded resource usage. It was not chosen because the diff intentionally adds CLI flags, examples, validation, and continuation fields, which indicates a decision to let operators tune budgets for different repository sizes, retention windows, and runner environments.

### Consequences

#### Positive
- Large `gh aw logs` runs can now be bounded explicitly for both GitHub API consumption and local storage usage.
- Partial downloads become resumable because continuation data preserves the applied timeout, API budget, storage budget, and cursor state.
- Multi-workflow collection shares one storage budget, which reduces the chance that parallel downloads overrun runner disk space.

#### Negative
- The logs command contract is more complex because users must understand two new flags and the different semantics for positive versus negative API-budget values.
- The orchestrator now has additional partial-result and continuation paths to maintain, especially when storage is exhausted before any new run is fully processed.
- Stdin mode behaves differently from discovery mode because API-budget enforcement is only valid when the command performs paginated run discovery.

#### Neutral
- JSON and rendered outputs now surface storage-related partial-result messages and may include continuation metadata even when zero runs were newly processed.
- Internal logs-download types and orchestration state now carry explicit storage-limit and API-budget fields across more call paths.
- Test coverage expands around validation, empty-result messaging, and continuation behavior for resource-constrained runs.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
