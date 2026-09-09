# ADR-58634: Add Payload-Based Grader Execution

**Date**: 2026-09-05
**Status**: Draft
**Deciders**: Unknown, adr-writer agent

---

### Context

This pull request adds a new `gh aw graders run <workflow-id> <grader-id> [run-id]` CLI subcommand, updates grader tracing to persist `grader_payload.json`, and adjusts workflow artifact uploads so saved grader payloads are available for later replay. The implementation also adds redaction before archived grader outputs are uploaded and documents stdin-based and artifact-based usage in the CLI docs. Together, these changes introduce a new supported execution path for replaying one grader independently of the original workflow run, which changes how grader data is stored, transported, and re-executed. Because this establishes a reusable debugging and inspection mechanism across the CLI and workflow runtime, it should be recorded as an explicit architectural decision.

### Decision

We will support single-grader replay by persisting a preprocessed grader payload during workflow execution and exposing a `gh aw graders run` command that can execute a declared grader against either that archived payload or JSON from standard input. We will keep the replay contract centered on the preprocessed trace payload rather than rehydrating raw workflow logs, and we will redact archived grader outputs before upload to reduce secret exposure risk. This favors a simple, reproducible replay model that works for built-in graders, inline JavaScript graders, and the operational-value evaluator.

### Alternatives Considered

#### Alternative 1: Keep grader execution tied to full workflow runs only

This would preserve the existing model where graders are observed only as part of a completed workflow run and inspected through final results rather than replayed directly. It was considered because it avoids introducing a new persisted payload artifact and another CLI execution mode. It was not chosen because the PR evidence shows a need to rerun one grader from either a saved run payload or stdin without rerunning the full workflow, which is better for debugging and iterative evaluation.

#### Alternative 2: Reconstruct replay inputs from raw logs or artifacts on demand

This would avoid storing a dedicated `grader_payload.json` by rebuilding the grader input each time from archived traces, logs, or other run artifacts. It was considered because it could reduce artifact surface area and avoid introducing a new persisted contract. It was not chosen because the implementation instead standardizes on one preprocessed payload written by `trace_graders.cjs`, which is simpler to validate, cheaper to consume from the CLI, and more deterministic across grader types.

### Consequences

#### Positive
- Developers can rerun one grader quickly from a prior run artifact or local JSON without rerunning the full workflow.
- The replay interface is consistent across built-in graders, inline graders, and operational-value grading because all consume the same preprocessed payload shape.
- Persisting the preprocessed payload makes grader investigations more reproducible and less dependent on log-mining or ad hoc reconstruction.
- Redacting grader files before artifact upload lowers the chance that replayable payloads expose sensitive values.

#### Negative
- The project now has to maintain `grader_payload.json` as a durable artifact contract between workflow execution and CLI replay.
- Artifact handling becomes more complex because workflows must archive the payload and, in some cases, explicitly redact grader outputs before upload.
- Persisting replay payloads increases artifact volume and may preserve sensitive contextual data if redaction misses a case.
- The CLI replay path adds more validation and runtime surface area, including JSON validation, payload size limits, sandboxed JavaScript execution, and artifact download logic.

#### Neutral
- Integration and unit tests now cover help text, invalid run IDs, stdin payload execution, inline script grading, and script-file operational-value grading.
- The CLI documentation now treats `graders run` and `graders operational-value` as separate replay-oriented entry points under the `graders` command.
- Future changes to trace preprocessing will need to preserve compatibility with the stored payload format or update both the runtime producer and replay consumer together.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
