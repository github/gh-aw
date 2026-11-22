## Customer Triage Instructions

Apply the following rules during triage in addition to the standard workflow:

- Use one primary `backlog:` label per classified issue from [the backlog taxonomy](backlog-labels.json). If the evidence is insufficient, leave the primary label unset and mark the issue `needs-triage` for manual review rather than guessing. Existing `cluster:` labels are separate and can coexist. Workflow producers should set their primary label in `safe-outputs.create-issue.labels` when their output always belongs in the same cluster; otherwise let triage classify the issue.
- Shared runtime run reports receive their primary label through this repository's Auto-Triage workflow, using the [backlog triage helper](scripts/backlog_issue_triage.cjs). The shared runtime does not know about this taxonomy.
- **OpenTelemetry issues and PRs**: Any issue or PR related to OpenTelemetry (otel, otlp, traces, telemetry, observability) must be assigned to @mnkeifer and labeled `otel`.
