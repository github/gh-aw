# ADR-65163: Report Work Queue Operations in Audit and Logs

**Date**: 2026-10-03
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

The work queue already records evidence across activation snapshots, agent finish-intent files, and workflow job logs, but that evidence is split across artifacts and is difficult to inspect from a single `gh aw` report. This pull request extends both `gh aw logs` and `gh aw audit` so operators can investigate queue publication and worker reconciliation without manually correlating multiple downloaded files. The change must preserve the existing distinction between activation-time queue facts and run-time queue operations, keep compact `logs` defaults unchanged, and work with cached summaries so older downloads can be refreshed when queue evidence is incomplete.

### Decision

We will introduce a first-class `work_queue` report section backed by a `WorkQueueReport` extracted from activation artifacts, agent finish-intent artifacts, and workflow logs. We will add a dedicated `work-queue` artifact set for `gh aw logs`, include the same evidence in default `gh aw audit`, and persist the resulting queue data through audit and cached-logs schema updates so cached reports can be regenerated or backfilled when workflow logs are missing. We will treat activation snapshot transactions as historical queue facts, `finish_intent` as an unverified agent request, and workflow-log operations as separate publication and reconciliation diagnostics with source-file provenance.

Artifact selection is preserved explicitly rather than inferred from expanded artifact names: `github-api` and `work-queue` download the same artifact names but have different reporting intent. Usage-only logs do not expose previously cached work-queue evidence. Work Queue operations are read only from compiler-owned step log paths, excluding agent execution and whole-job copies. Workflow logs are staged and published with a completion marker after successful extraction; incomplete directories are not accepted as evidence. Required snapshot fields are checked for presence even when nullable, and text rendering escapes control characters without changing JSON values.

### Alternatives Considered

#### Alternative 1: Keep queue evidence only in raw artifacts

This would avoid new report fields, artifact-set plumbing, and cache-schema changes. It was not chosen because the PR's stated goal is to make queue publication and worker reconciliation investigable from a single logs or audit report, which raw artifact spelunking does not provide.

#### Alternative 2: Add queue reporting only to `audit`

Limiting the feature to `gh aw audit` would reduce the surface area of the change and avoid adding a new `logs` artifact set. It was not chosen because the PR explicitly adds `gh aw logs <run-id> --artifacts work-queue --json` support and updates cached logs output, indicating that operators need the same evidence in logs workflows as well as in audit workflows.

### Consequences

#### Positive
- Operators can inspect queue snapshots, assigned worker identifiers, finish intents, and timestamped queue operations from one structured `work_queue` section.
- `gh aw logs` gains an explicit `work-queue` download mode while `gh aw audit` includes the evidence by default, improving discoverability without changing compact logs defaults.
- Cached audit and logs summaries retain queue evidence and can backfill partial older reports when artifacts or workflow logs were previously missing.

#### Negative
- The implementation increases cache schema versions and adds more coupling between artifact download logic, workflow-log availability, and report rendering.
- Work Queue reporting now depends on parsing and validating multiple evidence sources, including workflow log text patterns that may require maintenance if runtime messages change.
- Downloading queue evidence for logs requires workflow logs in addition to activation and agent artifacts, which can increase report generation cost compared with usage-only logs.

#### Neutral
- The report remains diagnostic, not queue authority. Current snapshots carry the
closed QueueCommit chain, immutable assignment provenance and per-Claim intents.
Historical scalar/fact artifacts may still be displayed as historical evidence;
that decoding cannot authorize current operations or upgrade an old queue.
- The report separates activation-time observations, staged per-Claim requests,
durable request/Claim references and run-specific diagnostics. Detailed traces
can reconstruct the causal decision prefix, but a workflow log line or successful
native conclusion does not establish a verified Result.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
