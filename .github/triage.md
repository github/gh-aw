## Customer Triage Instructions

Apply the following rules during triage in addition to the standard workflow:

- Use one primary `backlog:` label per issue from [the backlog taxonomy](backlog-labels.json). Existing `cluster:` labels are separate and can coexist. Workflow producers should set their primary label in `safe-outputs.create-issue.labels` when their output always belongs in the same cluster; otherwise let triage classify the issue.
- **OpenTelemetry issues and PRs**: Any issue or PR related to OpenTelemetry (otel, otlp, traces, telemetry, observability) must be assigned to @mnkeifer and labeled `otel`.
