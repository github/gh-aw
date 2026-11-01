# ADR-73532: Bound MCP Compile and Audit Work and Return Sanitized Partial Results on Timeout

**Date**: 2026-10-31
**Status**: Draft
**Deciders**: Not formally recorded; pending maintainer review of PR #73532.

---

### Context

The `gh aw` MCP server runs `compile` and `audit` as CLI subprocesses within a bounded response budget (see [ADR-71886](71886-stage-aware-partial-results-for-mcp-tool-timeouts.md)). Strict dry-run compiles and first-attempt single-run audits still exceeded that budget (#73447). Compile model validation refreshed the observed-model inventory by downloading workflow logs during the request. Single-run audits ran automatic previous-run baseline discovery, which adds API and artifact downloads. When a timeout did occur, ADR-71886 had compile timeouts withhold all completed workflow results. Audit timeouts returned only `{"available": true}`. Callers therefore got little actionable diagnostic information, even though some work had already finished.

### Decision

We will cut optional network work out of MCP-initiated requests and return a bounded, sanitized summary of completed work when a timeout occurs. Specifically:

1. **Compile:** When `PrepareCompileModelValidation` runs for an MCP dry run (`DryRun` with a non-empty `MCPProgressFile`), it sets `refreshObserved: false`. It uses the locally cached observed-model inventory instead of downloading workflow logs (`pkg/cli/compile_model_validation.go`).
2. **Audit:** `buildAuditCommandArgs` adds `--no-baseline` when exactly one run is audited. Diff mode, which audits two or more runs, is unchanged. Users who need baseline comparison should run `gh aw audit` directly (`pkg/cli/mcp_tools_privileged.go`).
3. **Compile timeout diagnostics:** `readMCPCompileProgress` reads the per-workflow progress file through a 1 MiB `LimitReader`. It projects each entry into a `mcpCompilePartialResult` or `mcpCompilePartialIssue` DTO. It applies `sanitizeMCPUntrustedText` to scope, workflow, type, file, and severity. It applies `boundMCPCompileDiagnostic` to messages. It stops adding entries before the serialized result exceeds `maxMCPCompileFallbackBytes - 8 KiB` (`pkg/cli/mcp_compile_diagnostics.go`, `pkg/cli/mcp_timeout.go`).
4. **Audit timeout diagnostics:** `summarizeMCPAuditPartialResults` returns an allow-listed set of overview fields (`run_id`, `workflow_name`, `status`, `conclusion`, `event`), with string values sanitized. It also returns counts for `jobs`, `missing_tools`, `missing_data`, and `mcp_failures`. It never returns raw report bodies.

The main goal is reliability under the fixed MCP deadline. Diagnostics are still treated as untrusted data.

### Alternatives Considered

#### Alternative 1: Raise the MCP response deadline or run the optional work asynchronously

Give compile and audit more time so the log refresh and baseline discovery can finish. Or start them in the background and poll for results. This would keep full functionality in the MCP path. However, ADR-71886 deliberately keeps the caller's deadline with a 5-second response margin. Asynchronous work would also need new job-tracking state and a new protocol surface. Skipping the optional work is smaller and fixes the cold-start case directly.

#### Alternative 2: Keep withholding compile partial results (status quo from ADR-71886)

Continue returning `partial_results: []` on compile timeouts because subprocess output may contain sensitive or instruction-bearing text. This is the safest choice for prompt injection. However, callers lose results for workflows that already compiled and must re-run everything blindly. This PR instead returns results through typed DTOs with field-level sanitization and size limits. That narrows the exposure without discarding the data.

#### Alternative 3: Return the full raw progress file or audit report on timeout

This is simple and gives callers the most information. It was rejected because the content is unbounded and untrusted, and the response could exceed MCP size limits.

### Consequences

#### Positive
- MCP dry-run compiles no longer block on downloading workflow logs, and single-run MCP audits no longer pay for baseline discovery. Both reduce cold-start timeouts.
- Compile timeouts include completed per-workflow validity, errors, and warnings, so callers can act on finished work and retry only what is left.
- Audit timeouts include run identity and result counts instead of a bare `available` flag.
- Partial output is limited by both a read cap (1 MiB) and an output cap (about 40 KiB), and all text fields are sanitized.

#### Negative
- MCP dry-run model validation can use a stale or missing observed-model inventory. Configured models still require that inventory to exist, so results may differ from a CLI compile until the cache is refreshed.
- Single-run audits through MCP no longer report previous-run baseline comparisons, so this behavior now differs between MCP and `gh aw audit`.
- This revises the ADR-71886 decision to withhold compile timeout diagnostics. Sanitized but untrusted compiler messages now reach the calling agent, so the injection defense now depends on `sanitizeMCPUntrustedText` and `boundMCPCompileDiagnostic` being correct.
- The `partial_results` shape becomes a richer response contract (DTO fields and `*_count` keys) that clients and documentation must track.

#### Neutral
- Running a single audit through MCP is now identified by `len(runItems) == 1`, and MCP dry-run compiles by `DryRun && MCPProgressFile != ""`. Future callers that set these fields inherit the cheaper behavior.
- Partial-result truncation stops at the first entry that would exceed the limit, so later completed workflows are dropped silently except for the count in the diagnostic summary.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
