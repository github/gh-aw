# ADR-80583: Record Routed Runs in an Adaptive Routing History (Observe Mode)

**Date**: 2026-11-24
**Status**: Draft
**Deciders**: rabeckett (PR author)
**Related**: [Issue #80554](https://github.com/github/gh-aw/issues/80554), [PR #80583](https://github.com/github/gh-aw/pull/80583), [ADR-80468](80468-record-routing-observation-verdict.md)

## Context

A routed workflow lets a router pick the model and effort for its main agent. Before a router can act on its own picks (live mode), maintainers need evidence about how each pick performs: whether the run's result was evidence about the pair AWF selected, what the run cost, and why a run did not count. Issue #80554 asks for that evidence to be recorded for every eligible run, not a sample, while the workflow keeps running the ordinary pick.

Forces:

- Every eligible run must be recorded, including runs that did not count, and each run must count at most once across re-runs, repeated saves, and concurrent saves.
- The agent must not be able to edit its own record, so the write belongs in a trusted job that runs after the agent finishes.
- The history must be stored where maintainers expect stored state: a repo-memory branch per workflow, readable with a read-only command.
- An edit that fixes a workflow should forgive earlier failures but keep successes. A late run of an older definition must not count against the new one.
- The verdict (ADR-80468), the selected pair, and the cost already exist in the run's unified session, so the history should reuse them rather than add a second accounting path.
- Live mode, selection receipts, and the firewall's adaptive selection are not yet available (PR body: AW-3, github/gh-aw-firewall#12027).

## Decision

Add an `adaptive-routing: observe` workflow setting, with a repository default in `.github/workflows/aw.json`, that:

1. Keeps the router's ordinary pick unchanged. Observe mode never changes which model runs.
2. Requires `tools.repo-memory`. Only workflows whose main model the router picks, on a Copilot, Claude, or Codex engine, are eligible. Work-queue workers, reusable workflows (`on: workflow_call`), and workflows that run experiments are not. An explicit opt-in that cannot work fails to compile with the reason; the repository default skips such workflows silently. `adaptive-routing: true` is reserved for live mode and fails to compile.
3. Adds one `push_routing_history` job after `conclusion`, with `contents: write` and `actions: read`. It reads the run's routing verdict, the model and effort AWF selected, and the AWF-priced cost from the unified session, then saves them to `memory/adaptive-routing/<workflow>` (`state.json` and `runs.jsonl`) through repo-memory's save loop. The agent job, its prompt, and its permissions are unchanged.
4. Adds an opt-in `reapply` option to repo-memory's save loop, used only by this job. When another run moved the branch first, the loop resets to the new head and applies this run again to the newest files. Repo-memory's own saves do not use it.
5. Reserves the `memory/adaptive-routing/` branch namespace. The compiler rejects it as a repo-memory branch, and repo-memory refuses it at run time.
6. Records only first attempts whose workflow definition comes from the default branch. Runs that do not count say why (for example `infrastructure`, `cancelled`, `rate_limited`, `no_agent`). Editing the workflow starts a new generation; `gh aw routing reset` fences off all earlier runs.
7. Adds `gh aw routing status` (read-only) and `gh aw routing reset`. Status reports no recommendation until live routing exists.
8. Records per-run token counts, counting new input tokens as AWF's credit meter does, so that live mode can later estimate the cost of an untried model.

Workflows that do not opt in compile exactly as before.

## Alternatives Considered

1. **Act on the router's picks now (live routing).** Rejected for this step. No recorded evidence yet justifies changing which model runs. Live mode and selection receipts are deferred to AW-3.
2. **Write the history from the agent job.** Rejected. The agent could edit its own record, and its permissions and prompt would have to change. The write moves to a trusted job that runs after the agent finishes.
3. **Record a sample of eligible runs.** Rejected by the issue's requirement to record every eligible run. A sample can miss the rare failures that a later live mode would act on.
4. **Keep the history only in per-run workflow artifacts.** Not adopted. Artifacts are scoped to a single run and do not form a cumulative, maintainer-readable history. This PR does not evaluate their retention.

## Consequences

Positive:

- Maintainers can read `gh aw routing status` to see what each routed pick achieved and what it cost before any routing change is made.
- The agent cannot write its own record, and each run counts once across re-runs, repeated saves, and concurrent saves.
- Workflows that do not opt in compile exactly as before, with no new branches, jobs, permissions, or writes. The PR's drift check found every lock file in the repository unchanged.

Negative:

- Each opted-in workflow gains a job with `contents: write`, and every eligible run adds a signed commit to `memory/adaptive-routing/<workflow>`. History grows with run volume. No retention or size limit is defined in this PR.
- The `reapply` option and the reserved namespace add logic to repo-memory's shared save loop.
- Token-count estimates can undercount a request whose new input exceeds its cached input (PR body).

Unknown context (not established by the PR evidence):

- How branch rulesets on `memory/**` interact with the job's writes. Issue #80554 asks for this to be documented; it is not covered in the PR body or diff.
- Retention and storage limits for the history branch.
