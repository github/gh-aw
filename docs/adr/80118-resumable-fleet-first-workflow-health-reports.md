# ADR-80118: Resumable Fleet-First Workflow Health Reports

**Date**: 2026-11-22
**Status**: Draft
**Deciders**: pelikhan (PR #80118 author); drafted by adr-writer agent [TODO: verify reviewers]

---

### Context

`gh aw health` without a workflow name used to produce a single time-bounded scan. A two-minute scan of the 369 active agentic workflows found failures in 65 and left 299 unassessed, and the caller had to restart the whole scan to fill the gap, repeating reads it had already completed. Health collection already runs under a shared `--timeout` deadline ([ADR-71786](71786-bound-health-collection-with-shared-time-budget.md)), so a deadline alone cannot guarantee coverage. The report also needs to separate "unhealthy" from "not yet verified" so that a timeout is never read as a healthy workflow or as proof that no current-revision runs exist.

### Decision

We will make `gh aw health` with no workflow name a **fleet-first**, **resumable** report over active agentic workflows on the default branch (excluding pull-request events). The report inventories Actions outcomes first, records coverage, marks unfinished verification as `not_collected`, and returns a typed `resume` or `audit` next action when available. A checkpoint pins the repository, time window and branch revision so later calls continue without replaying completed work, and the MCP tool uses an opaque, session-bound resume token.

### Alternatives Considered

#### Alternative 1: Keep the Single-Pass Scan and Restart on Partial Results

Keep the per-invocation scan and ask callers to rerun with a larger `--timeout` or to restart the scan when coverage is incomplete. This is the status quo and it is simple, with no new state. It was rejected because each restart repeats reads that already succeeded, so the fleet is never fully covered in bounded time, and callers still cannot tell a timeout from a healthy result without parsing free-text warnings.

#### Alternative 2: Make the New Report Opt-In

Retain the previous repository-wide report by default and expose the resumable report as an opt-in. This keeps existing scripts unchanged but leaves the coverage gap for callers that use the default. The changeset is marked `major` because the unnamed command changes scope and JSON schema.

#### Alternative 3: Remove the Deadline for Fleet Reports

Run fleet collection to completion with no time budget. This would give complete coverage in a single call without any checkpoint. It was rejected because MCP calls and agent turns are time-bounded, and the shared deadline from ADR-71786 exists to keep health output valid and prompt. Resumable checkpoints keep the deadline while still letting work accumulate across calls.

### Consequences

#### Positive
- Fleet health covers every active agentic workflow on the default branch, and the report states its coverage and stop reason when it is partial.
- A resumed run reuses the pinned inventory and completed verification instead of replaying them, so repeated calls make progress.
- Typed `next_action` values (`resume`, `audit`) let callers and agents continue without parsing prose.
- `not_collected` separates "verification did not finish" from "healthy" and from "no runs exist".

#### Negative
- Unnamed CLI and MCP consumers must migrate to the new scope and schema. Read `coverage` for completeness and opt in to `details.workflows` when per-workflow evidence is needed.
- Checkpoints store the full inventory and cached responses (capped at 128 MiB) on disk. They must be private regular files, and they need a clear retention story. [TODO: verify retention guidance for checkpoint files.]
- MCP checkpoints live in temporary directories and expire after 30 minutes. They are bound to the originating MCP session, so a new session cannot resume them.
- About 1,100 new lines in `pkg/cli` add state-machine complexity (inventory windows, registry paging, verification ordering) that future maintainers must understand.

#### Neutral
- Named-workflow reports keep their existing scope and JSON schema.
- Checkpoints carry a version number. Incompatible or stale checkpoints are rejected, and the caller must start a new checkpoint path.
- Use a named check for pull-request revision evidence, or `gh aw logs` for broader run history.
- The Auditor adopts the default-branch, non-PR scope and uses `coverage` and `details.failure_representatives` before its downstream evidence checks.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
