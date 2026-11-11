# ADR-76975: Preserve Workflows and Fail on Update Conflicts

**Date**: 2026-11-11
**Status**: Proposed (pending maintainer review)
**Deciders**: Not yet recorded; acceptance is pending.

---

### Context

`gh aw update` refreshes source-managed workflows with a three-way merge of the upstream base, the local file, and the new upstream version. Before this change, a merge conflict printed a warning and the command still returned a zero exit status. In practice, conflict markers were written into workflow sources while the update reported success, so CI jobs and scripts had no reliable signal that manual resolution was needed. Failures in conflict-marker scanning and package-asset rollback were also dropped silently, and CRLF line endings in release listings could produce incorrect version selection.

### Decision

The proposed behavior treats merge conflicts and non-rate-limit workflow update failures as errors. In default compile mode, a conflicted workflow is left unchanged along with its lock file, and the command exits nonzero. With `--no-compile`, conflict markers are still written for manual resolution, but the command also reports failure. After a workflow update failure, later update phases, including container pin refresh and recompilation, must not run. The primary driver is correctness: update failures must be detectable by scripts and CI, at the cost of a behavior change in exit status.

### Alternatives Considered

The following alternatives describe tradeoffs for review, not a record of prior team discussion or rejection.

#### Alternative 1: Keep a zero exit status and print a warning

Keep the previous behavior, where a conflict prints a warning and the command succeeds. This is the least disruptive option for existing scripts, but retains a success report that hides conflict markers in workflow sources. Warnings in CI logs are easy to miss.

#### Alternative 2: Write conflict markers in every mode and fail afterward

Apply the merged output with conflict markers even in default compile mode, then return an error. This uses one write path and gives the user the merged output to edit directly, but leaves the workflow source unparseable until the markers are resolved. Leaving the local source and lock unchanged in default mode instead preserves the last compiled workflow.

### Consequences

#### Positive
- Conflicts and partial failures produce a nonzero exit status, so CI and scripts can detect them.
- In default mode, a conflicted workflow and its lock file are left untouched, so the last compiled workflow remains runnable.
- Conflict-scan errors and package-asset rollback failures are reported instead of being silently ignored.
- CRLF release listings resolve to the correct version.

#### Negative
- This is a behavior change: `gh aw update` now exits nonzero for any non-rate-limit failure, including runs where other workflows updated successfully. Scripts that assume exit status 0 will fail.
- In default mode, a conflicted workflow is not updated at all, so users must reconcile local and upstream changes before retrying.
- With `--no-compile`, the source file contains conflict markers and is not a valid workflow until a user resolves them manually.

#### Neutral
- Rate-limit-only failures remain non-fatal, preserving the existing exception.
- Workflow update failures stop later update phases; successful updates to other workflows are not rolled back.
- Conflict-marker scan errors are surfaced rather than ignored.
- `docs/src/content/docs/setup/cli.md` documents the new conflict and exit-status semantics.

---

*Originally drafted by the adr-writer agent. Maintainer acceptance and deciders must be recorded before changing status to Accepted.*
