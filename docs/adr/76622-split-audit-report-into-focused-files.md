# ADR-76622: Split Audit Report Implementation into Focused Files

**Date**: 2026-11-10
**Status**: Draft
**Deciders**: [TODO: verify] PR #76622 was authored by `copilot-swe-agent` from issue #76514 (Daily File Diet) and requested by pelikhan. Reviewers to confirm who decided this.

---

### Context

`pkg/cli/audit_report.go` had grown to 1,201 lines, above the 800-line threshold in issue #76514. It mixed report data types, audit data assembly, downloaded-file and manifest extraction, and pre-agent step-log error scanning in one file. The issue requires behavior-preserving, API-preserving moves within package `cli`, with each resulting file under 500 lines. Developers working in this area need to find types and helpers by responsibility.

### Decision

We will split the audit report implementation in package `cli` by responsibility, without changing behavior or exported API. Report data types and `MCPToolSummary` sync methods move to `audit_report_types.go`. File and manifest helpers move to `audit_report_files.go`. Step-log error scanning moves to `audit_step_errors.go`. `audit_report.go` keeps the audit data assembly. The primary driver is maintainability: the change is a mechanical move, so it lowers navigation cost at little risk.

### Alternatives Considered

#### Alternative 1: Keep one file and trim it in place

Remove dead code or shorten functions inside `audit_report.go` and leave the file's mixed responsibilities alone. This is the smallest diff and was not chosen because it does not address the mixed responsibilities that issue #76514 targets. The PR body and issue do not record this trade-off, so this rationale is inferred. [TODO: verify]

#### Alternative 2: Move the code into a new sub-package (for example `pkg/cli/audit/`)

A new package would enforce boundaries at compile time. It was not chosen because the issue requires "pure moves within package `cli`" and no public API change. A new package would require exporting package-private identifiers shared across these files and changing call sites throughout `cli`, which is a larger and riskier change. This rationale is inferred from the issue text. [TODO: verify]

### Consequences

#### Positive

- Each file has one responsibility: types (`audit_report_types.go`, 293 lines), file and manifest helpers (`audit_report_files.go`, 204 lines), step-log error scanning (`audit_step_errors.go`, 397 lines), and assembly (`audit_report.go`, 329 lines). All four are under the issue's 500-line target.
- The diff is mechanical. After whitespace normalization, all 872 removed lines from `audit_report.go` reappear in the new files, so reviewers can verify the move by content comparison.
- No exported identifiers or package boundaries change, so callers in package `cli` need no edits.

#### Negative

- Moved code loses per-line blame history in the new files unless reviewers use `git blame -C` or `--follow`.
- The `audit_report_*` family of files in package `cli` grows further (analysis, experiments, graders, render, files, types), so names such as `audit_report_files.go` are easy to confuse with related files.
- Test code is not split. `audit_report_test.go` (about 71 KB) still covers all moved code, and the test plan in issue #76514 (new cases and per-file coverage) is not part of this PR.
- Relocated lines count toward the gate's business-logic volume threshold (894 added lines across four files), so a pure refactor triggers ADR enforcement by volume alone.

#### Neutral

- Every new file stays in `package cli`. Each file now has its own import block that lists only what it uses.
- The move is intended to be behavior-neutral. This ADR relies on the existing test suite and does not record a test run for this PR.
- The decision is inferred from the PR body, linked issue #76514, and the diff. The rationale for alternatives is marked `[TODO: verify]`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
