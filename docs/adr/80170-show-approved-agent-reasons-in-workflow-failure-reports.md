# ADR-80170: Show Approved Agent Reasons in Workflow Failure Reports

**Date**: 2026-11-22
**Status**: Draft
**Deciders**: pelikhan (PR author); gh-aw maintainers [TODO: verify reviewers]

---

### Context

When an agent run stops without completing, the failure issue and comments hid the reason inside the collapsed **Runtime evidence** block. Issue #80162 reports that readers had to expand that block to learn why the agent stopped. The agent's own stated reason is the most direct explanation, but it is free text written by the agent and has not been verified. Showing it must not bypass the existing safety checks on agent output: threat detection and secret redaction. The failure report also has to keep GitHub alerts readable. An alert nested in a Markdown container that does not render alerts loses its formatting, and nesting can also occur at runtime, after compile-time checks have run.

### Decision

We will show the agent-declared reason next to the completion warning in failure issues and comments, but only when threat detection explicitly accepts the agent output and secret redaction succeeds. The reason is labeled "agent-declared, unverified." A successful detector job alone is not enough. Machine diagnostics stay in the collapsed Runtime evidence block.

We will also warn at compile time when a GitHub alert is nested in an unsupported Markdown container. If nesting is introduced at runtime, the alert is rendered as a labeled blockquote so its content stays readable. The bundled templates affected by this are fixed in the same change. The primary driver is readability of failure reports for the issue #80162 case, without weakening the trust gate for agent-authored text.

### Alternatives Considered

#### Alternative 1: Keep the agent reason only in Runtime evidence

This is the status quo that #80162 reports as a problem. It needs no new gate and no new rendering path, so it has the smallest change surface. It was not chosen because the most direct explanation of a failure stays behind a collapsed block.

#### Alternative 2: Gate on the threat-detection job's success alone

A successful detector job would be the only condition for showing the reason. It is simpler to evaluate because it needs no per-output decision. It was not chosen because the PR explicitly requires an explicit acceptance of this agent output plus successful redaction, and states that "a successful detector job alone is not enough." [TODO: verify whether other gating designs were discussed.]

#### Alternative 3: Hard-fail compilation on any nested alert

Rejecting nested alerts at compile time would prevent unreadable output in the common case and avoid a runtime fallback. It was not chosen because the PR warns at compile time and handles runtime-introduced nesting with a labeled blockquote, which a compile-time check cannot see. The PR does not record whether a hard failure was evaluated, so this rationale is inferred from the runtime-nesting case. [TODO: verify.]

### Consequences

#### Positive
- The agent's stated reason appears next to the completion warning, so readers no longer need to expand Runtime evidence to learn why the run stopped (#80162).
- The reason is shown only after threat detection accepts the output and secret redaction succeeds, and it is labeled as unverified, so the reader can see its provenance.
- Nested alerts produce a compile-time warning and fall back to a labeled blockquote at runtime, so content stays readable. The affected bundled templates (`agent_failure_issue.md`, `daily_workflow_aic_exceeded.md`, `stale_lock_file_failed.md`) are fixed.

#### Negative
- The displayed reason is still agent-authored free text. The label, detection, and redaction gate are the main defenses, not verification of the claim.
- When detection rejects the output or redaction fails, the reason is hidden, so some failure reports show less than the reader might expect.
- Two rendering paths now exist for alerts: native GitHub alerts and labeled blockquotes. Both must be maintained.
- The change regenerates 305 `.lock.yml` files, and the PR validation notes that 370 workflows were compiled. The diff is large and mostly mechanical.
- The full `npm test` suite is not yet sandbox-compatible. Validation covered 664 failure-handler tests and 794 impacted JavaScript tests. Failures outside the changed fixtures remain. [TODO: verify current status.]

#### Neutral
- The compile-time nested-alert check is a warning, not an error. Authors can still ship nested alerts, and readers then see the runtime blockquote fallback.
- The change touches `actions/setup/js/handle_agent_failure.cjs`, `failure_report.cjs`, `report_ast.cjs`, `report_size.cjs`, and `pkg/workflow/publication_markdown_validation.go` (new). It also updates the safe-outputs reference documentation.

---

*ADR created by [design-decision-gate agent]. Review and finalize before changing status from Draft to Accepted.*
