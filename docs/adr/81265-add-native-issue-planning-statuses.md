# ADR-81265: Add Native Issue Planning Statuses

**Date**: 2026-11-26
**Status**: Draft
**Deciders**: pelikhan (PR author)
**Related**: [PR #81265](https://github.com/github/gh-aw/pull/81265)

## Context

Issues can carry a native planning status with a category (`backlog`, `planned`, `started`, `completed`, `not_planned`, `duplicate`). Before this PR, gh-aw could change only the open/closed state, through `update-issue.status`. Its `issues` trigger could not react to planning-status transitions, and no safe output could set a planning status.

Forces:

- `update-issue.status` already controls open/closed. Existing workflows depend on it, so its meaning must not change.
- The planning-status API is a public preview (`GET /repos/{owner}/{repo}/planning-statuses`, `X-GitHub-Api-Version: 2026-03-10`). Where it is unavailable, a write must fail. It must not fall back to open/closed.
- Closing or reopening an issue is more consequential than changing a status name, so those permissions must be explicit and independent of other enabled safe outputs.
- A `status_changed` trigger that activates for every transition starts the agent on events it does not need. Filtering must happen before activation.
- Unknown from PR evidence: how long the preview API and its category set will remain stable, and whether Work Queue and Repo Factory should consume these statuses. The PR excludes both.

## Decision

1. Add a `set-issue-status` safe output. It resolves the requested status by name or positive integer ID from the repository's planning-status list before any write. Unknown, ambiguous, or disallowed statuses fail.
2. `set-issue-status` accepts `allowed` (names or IDs). When it is omitted, any resolved status is accepted.
3. Moving an issue to a closed category (`completed`, `not_planned`, `duplicate`) requires `allow-close: true`. Reopening requires `allow-reopen: true`. Both default to denied.
4. The `duplicate` status requires `duplicate_of`, which must reference another accessible issue in the target repository. The canonical relationship is read through GraphQL `duplicateOf`. Changing an existing canonical issue requires both `allow-close` and `allow-reopen`.
5. After each write, the handler re-reads the issue and fails if GitHub did not apply the status, state, or duplicate relationship. A request that is already satisfied is skipped.
6. Native planning statuses reject `issue_intent`, `suggest`, `rationale`, and `confidence`.
7. `create-issue` gains `planning-status` (must be an open category) and `allowed-statuses`.
8. `on.issues` gains `status-categories` and `status-ids`. They match the captured `event.status` of a `status_changed` event, not the issue's current status, and are checked before activation. They require explicit `types` that include `status_changed`.
9. `update-issue.status` keeps its open/closed meaning and is unchanged.
10. Work Queue and Repo Factory integration are out of scope.

## Alternatives Considered

1. **Extend `update-issue.status` to accept planning statuses.** Rejected. `status` already means open/closed. Overloading it would change the meaning of working configurations and would hide the close/reopen permission boundary inside an existing operation.
2. **Represent planning state with labels (for example `status:started`).** Rejected. Labels have no native category, cannot be verified against the planning-status list, and are not the `status_changed` events the trigger is defined on.
3. **Filter status transitions inside the agent after activation.** Rejected. Every transition would still start a run, which costs time and leaves an unrestricted listener. The PR's trigger audit found no unrestricted issue listeners, and pre-activation filtering keeps it that way.

## Consequences

Positive:

- Planning-status changes become guarded, verified writes with before and after snapshots, rather than unchecked field updates.
- Closing and reopening are denied unless explicitly enabled, so turning on `set-issue-status` cannot close issues by accident.
- Status-change workflows run only for the transitions they name, and the filter is applied before the agent starts.
- `update-issue.status` behaves as before for existing workflows.
- Status evidence flows into manifests, audit, retention, and outcome evaluation.

Negative:

- The feature depends on a public-preview API. Where it is unavailable, writes fail. Operators must confirm availability.
- Each planning-status write makes several extra API calls: the status lookup, the issue read, the duplicate lookup when relevant, and the post-write verification.
- The change touches safe-output and trigger schemas, the compiler, lifecycle rules, outcome evaluation, and docs. This widens the review surface and the maintenance cost.
- The hard-coded API version and category lists must be updated if GitHub changes the preview.
- Work Queue and Repo Factory do not consume these statuses. Their integration needs its own decision.

*ADR drafted by the Design Decision Gate from PR evidence. The PR author should review it and complete any context the diff does not show.*
