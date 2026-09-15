# ADR-28494: Embed Tool-Call Breakdown and Tokens-per-Turn Metrics in Audit Diff Output

**Date**: 2026-04-25
**Status**: Draft
**Deciders**: pelikhan, Copilot


---

## Part 1 — Narrative (Human-Friendly)

### Context

The `audit diff` command compares two workflow runs side-by-side and surfaces metrics like total token usage, turns, duration, and cache efficiency. When agents investigate cost regressions between runs they frequently cannot determine *why* tokens changed — whether the regression comes from more turns, heavier per-turn usage, or a shift in which tools (bash, gh, edit, etc.) are being called. The existing `RunMetricsDiff` structure exposed aggregate token counts and turn counts, but no per-turn token rate and no breakdown by tool type. Engine-level tool call data was already being parsed into `RunSummary.Metrics.ToolCalls` (populated by the Claude, Codex, and Copilot log parsers) but was never surfaced in the diff output.

### Decision

We will enrich `RunMetricsDiff` with two new data structures — `ToolCallsDiff` and a tokens-per-turn scalar — and render both in the existing pretty-console and markdown diff renderers. Tokens per turn uses AI Credits from the firewall proxy when available, falling back to the engine-level token count. Tool call data is sourced from the already-computed `LogMetrics.ToolCalls` slice; bash-related entries (`bash`, `Bash`, `bash_*`) are separated into a dedicated `BashCommandsDiff` sub-structure to expose the Codex-style per-command granularity. All new types follow the existing JSON-serialisable struct conventions used by `TokenUsageDiff` and `GitHubRateLimitDiff`.

### Alternatives Considered

#### Alternative 1: Separate `audit tool-calls` subcommand

A dedicated subcommand could show tool-call detail for a single run or a pair. It was considered because it avoids enlarging the diff output and keeps concerns separated. It was not chosen because the tool-call delta is meaningful only in the context of a comparison; the driver is always "why did cost change between run A and run B?" Putting the data in the diff keeps it contextual and eliminates extra round trips for agents.

#### Alternative 2: Log-level analysis only — do not change the diff output

Agents could perform deeper log analysis themselves by querying the raw run logs. This was considered because it keeps the diff output lean. It was not chosen because the relevant data (`RunSummary.Metrics.ToolCalls`) is already materialised in memory during diff computation; re-parsing logs adds latency and requires agents to implement the aggregation logic every time, which is exactly what prompted this change.

#### Alternative 3: Add data to JSON output only, no render changes

Extending the JSON struct without adding renderer support would satisfy machine consumers but not the human-readable console/markdown reports that are the primary use-case for `audit diff`. This approach was not chosen because the primary consumer is the rendered diff report read by agents and engineers.

### Consequences

#### Positive
- Agents can immediately see which tool types drove a token or call-count change between two runs without additional log queries.
- Per-turn token efficiency distinguishes "more turns" regressions from "heavier per-turn" regressions, enabling more targeted fixes.
- Bash command granularity (via Codex's `bash_*` naming) exposes specific shell commands that changed frequency — actionable detail for prompt/workflow optimisation.
- The `AllTools` slice provides a complete cross-run view, not just the delta, which helps verify expected tool usage patterns.

#### Negative
- The diff output grows in length; runs with many tool types will produce lengthy "Tool Call Breakdown" sections that may be noisy when there are no significant changes.
- The `isBashTool` helper encodes engine-specific naming conventions (`bash`, `Bash`, `bash_*`) directly in the diff logic, creating a coupling point that must be updated if a new engine uses a different shell tool naming scheme.
- Tokens-per-turn uses integer division, silently discarding the fractional part; the resulting value can appear identical between two runs even when there is a small real difference.

#### Neutral
- The new `ToolCallInfo` type alias is exported from `logs_models.go` alongside the existing `LogMetrics` alias, following the established aliasing pattern for shared workflow types.
- Bash diff computation receives pre-filtered maps from the parent iteration to avoid a second traversal, which is a performance micro-optimisation that future readers should be aware of when modifying the iteration logic.

---

## Part 2 — Normative Specification (RFC 2119)

> The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**, **SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** in this section are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

### Tokens-per-Turn Computation

1. Implementations **MUST** compute tokens-per-turn as `WorkflowRun.TokenUsage / turns` when token usage and turn count are both greater than zero.
2. Implementations **MUST NOT** use AI Credits totals as token counts when computing tokens per turn.
3. Implementations **MUST NOT** compute a tokens-per-turn value when the turn count is zero (to avoid division by zero).
4. Implementations **SHOULD** format the tokens-per-turn change as a percentage string (e.g., `+50%`, `-10%`) using the same `formatVolumeChange` helper applied to other percentage-point metrics.

### Tool Calls Diff

1. Implementations **MUST** source tool call data from `RunSummary.Metrics.ToolCalls` (`LogMetrics.ToolCalls`) and **MUST NOT** re-parse raw log files during diff computation.
2. Implementations **MUST** produce a `ToolCallsDiff` that classifies each tool as `new`, `removed`, `changed`, or `unchanged` relative to the baseline run.
3. Implementations **MUST** include every tool seen in either run in the `AllTools` slice, sorted lexicographically by tool name.
4. Implementations **MUST** return `nil` for `ToolCallsDiff` when both runs have no tool call data, to keep the output clean for runs predating this feature.
5. Implementations **SHOULD** include per-entry `MaxInputSize` and `MaxOutputSize` values to provide token-size context for each tool type.

### Bash-Specific Breakdown

1. Implementations **MUST** treat tool names matching `bash` or `Bash` (case-insensitive equality) and names with the prefix `bash_` (case-insensitive) as bash tool invocations.
2. Implementations **MUST** aggregate all bash tool entries into a `BashCommandsDiff` sub-structure and report their combined call count for each run.
3. Implementations **MUST** collect bash tool entries during the main tool iteration loop and **MUST NOT** perform a second traversal of the tool maps to build the bash diff.
4. Implementations **MUST** return `nil` for `BashCommandsDiff` when no bash tool calls are present in either run.

### Rendering

1. Implementations **MUST** render `ToolCallsDiff` in both the pretty-console and markdown output paths when the diff is non-nil.
2. Implementations **MUST** render the tokens-per-turn row in the Run Metrics table when at least one of `Run1TokensPerTurn` or `Run2TokensPerTurn` is greater than zero.
3. Implementations **SHOULD** use a `formatMaxSizeCell` helper (or equivalent) to format `run1 / run2` size pairs, displaying `—` when both values are zero and omitting the individual value when it is zero.
4. Implementations **MAY** omit the Bash Commands sub-section from the rendered output when `BashDiff` is nil.

### Conformance

An implementation is considered conformant with this ADR if it satisfies all **MUST** and **MUST NOT** requirements above. Failure to meet any **MUST** or **MUST NOT** requirement constitutes non-conformance.

---

*This is a DRAFT ADR generated by the [Design Decision Gate](https://github.com/github/gh-aw/actions/runs/24940226956) workflow. The PR author must review, complete, and finalize this document before the PR can merge.*
