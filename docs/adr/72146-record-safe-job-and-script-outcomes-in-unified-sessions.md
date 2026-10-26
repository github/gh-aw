# ADR-72146: Record Safe Job and Script Outcomes in Unified Sessions

**Date**: 2026-10-26
**Status**: Draft
**Deciders**: pelikhan, Copilot (PR #72146) [TODO: verify]

---

### Context

The conclusion job builds a unified session (`aw_session.jsonl`) that graders and reports consume to judge what a workflow run actually accomplished. Built-in safe outputs already write manifest entries with resource URLs, but custom safe jobs (`safe-outputs.jobs.*`) and custom safe scripts were opaque: the session only knew the GitHub Actions job result, not whether the job's external operation succeeded or which resource it produced. Graders therefore could not credit (or reject) work done by custom safe outputs, and any agent-supplied target URL could not be trusted as evidence of a created resource. Custom safe-job outputs may also contain private data that must not leak into the collector's environment.

### Decision

We will add an explicit, opt-in outcome contract for custom safe outputs. Safe jobs may declare `outputs.status` and `outputs.url` (GitHub Actions output expressions, schema-restricted to just those two keys); the compiler passes the safe-job names (`GH_AW_SAFE_JOB_NAMES`) and only the declared status/url outputs plus job result (`GH_AW_SAFE_JOB_RESULTS`, each value wrapped in `toJSON`) to the conclusion collector, which records them as `safe_output.result` events. Safe scripts returning `{ success: true, url }` persist `status: "success"` and the URL in the safe-output manifest. A URL is recorded only when the effective status is `success`, the job result is `success`, and the URL parses as an absolute URI without credentials or control characters; absent `status` falls back to the Actions job result.

### Alternatives Considered

#### Alternative 1: Infer outcomes from the Actions job result alone

Record only `needs.<job>.result` for safe jobs. This needs no authoring changes, but a job can succeed at the Actions level while its external operation is skipped, staged, or deferred, and it provides no canonical resource URI. Rejected because graders would over-credit success and could not link to produced resources.

#### Alternative 2: Forward all safe-job outputs (or the agent's requested target) into the session

Pass `toJSON(needs)` wholesale or record the URL the agent asked for. Simpler to implement, but it exposes private dependency outputs to the collector environment and lets agent-supplied targets masquerade as evidence of creation. Rejected in favour of projecting only declared `status`/`url` and accepting only service-returned URLs.

#### Alternative 3: Require custom safe jobs to write manifest entries themselves

Have custom jobs append to the safe-output manifest like built-in handlers. This unifies storage but couples user jobs to an internal file format and artifact layout. Rejected as a heavier, more fragile contract than two well-known outputs.

### Consequences

#### Positive
- Graders and session reports can distinguish successful, failed, cancelled, and skipped custom safe-output operations and link to the canonical resource created.
- Failed, cancelled, skipped, staged, or deferred operations cannot claim successful resource creation, keeping success evidence trustworthy.
- Only declared outputs reach the collector, limiting exposure of private job outputs.

#### Negative
- Workflow authors must explicitly wire `outputs.status`/`outputs.url` (and scripts must return `url`) to get rich outcomes; undeclared jobs only get the Actions result.
- Adds new environment variables and validation paths (`invalid_safe_job_status`, `invalid_safe_job_url`, etc.) to the conclusion job, and recompiles affected `.lock.yml` files.
- The `outputs` key on safe jobs is schema-locked to `status` and `url`; extending it later requires a schema change.

#### Neutral
- Existing result-only dependency metadata is preserved; work-queue participants are excluded because they do not execute safe jobs.
- The manifest `ManifestEntry` gains an optional `status: "success"` field.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
