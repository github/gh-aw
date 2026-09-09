# ADR-58793: Report GitHub API rate limits in logs output

**Date**: 2026-09-05
**Status**: Draft
**Deciders**: gh-aw maintainers

---

## Context

The logs commands already consume GitHub API quota while collecting workflow run data, but their outputs do not show how much core API capacity was used or whether the command finished near exhaustion. This PR adds quota capture to the main logs orchestration paths, including single-repository runs, stdin-driven runs, empty-result JSON responses, and cross-run audit JSON, and it also distinguishes GitHub.com from GitHub Enterprise hosts. The implementation must expose this quota state in machine-readable output without breaking existing flows, and it must warn interactive users when the remaining core quota is low enough that follow-on commands may fail.

## Decision

We will record GitHub core API rate-limit snapshots at the start and end of logs collection, then attach those snapshots to logs JSON and cross-run JSON output as structured `github_api_rate_limit` or `github_api_rate_limits` fields. We will model the snapshot data as reusable report types in the CLI layer and thread them through the single-run, multi-target, stdin, and empty-result rendering paths so callers get consistent observability regardless of how logs are collected. We will also normalize rate-limit tracking by host so multi-host runs report quotas independently for GitHub.com and GitHub Enterprise, and we will emit a non-JSON warning when 20% or less of the core quota remains.

## Alternatives Considered

### Keep rate-limit visibility out of logs output

The project could have continued relying on operators to inspect quota separately with manual `gh api rate_limit` calls. This was considered because it avoids widening the logs output schema, but it was rejected because the PR evidence shows the missing visibility is the problem being fixed and manual checks do not preserve per-run quota context.

### Emit only human-readable warnings

Another option was to print a warning to stderr when quota is low and avoid adding new JSON fields. This was a realistic alternative because it is simpler for interactive use, but it was rejected because the PR explicitly adds start/end snapshots to standard, empty-result, cross-run, and multi-host JSON output, which automation can consume and compare across runs.

### Track a single aggregated quota for all hosts

For multi-target and stdin-driven runs, the implementation could have collapsed all API usage into one summary regardless of host. It was rejected because the diff shows explicit host parsing, normalization, deduplication, and separate reporting for GitHub.com and GitHub Enterprise, indicating that per-host quotas are materially different and need independent visibility.

## Consequences

### Positive

- Logs and audit JSON now expose concrete before/after GitHub API quota state, making quota consumption observable to both humans and automation.
- Multi-host workflows can distinguish GitHub.com and GitHub Enterprise limits, which helps debug partial failures and capacity issues in mixed-host environments.

### Negative

- Logs orchestration now has extra plumbing for rate-limit reports across multiple execution paths, which increases maintenance burden and the chance of propagation bugs.
- Each logs command performs additional rate-limit queries at the start and end of collection, increasing API traffic and test surface area.

### Neutral

- When rate-limit snapshots cannot be fetched, the implementation omits empty report objects rather than failing the logs command outright.
- Non-JSON output keeps the new information mostly out of the main report body and surfaces it only as a low-quota warning, while JSON callers receive the detailed structured fields.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
