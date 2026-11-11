---
description: Detailed trigger and escalation guidance referenced by create-agentic-workflow.md section 2.
---

## Reporting and digest guidance

For recurring reports, audits, and stakeholder digests, set these create-specific defaults:

- default to `create-issue`; use `create-discussion` only when the requester explicitly wants threaded discussion
- use `add-comment` only when updating an existing issue or pull request instead of creating a new report destination
- add `workflow_dispatch` when manual reruns, backfills, or preview runs should be possible

For the recurring-report window, grouping dimensions, deduplication key, `close-older-issues` lifecycle, and empty-window/missing-metadata `noop` rules, follow the canonical defaults in [report.md](report.md). Use [workflow-patterns.md](workflow-patterns.md) for the digest/incident skeletons.

## Persona-oriented scenario map

Base persona-to-trigger/tool/output facts are canonical in the [Persona-to-Pattern Quick Matrix](github-agentic-workflows.md#persona-to-pattern-quick-matrix); the table below adds only the prompt-authoring detail that matrix omits.

| Persona or scenario | Trigger and scope | Typical tools and outputs | Required prompt details |
|---|---|---|---|
| Program Manager or information-worker digest | `schedule` plus `workflow_dispatch` for previews, reruns, and backfills | `github` (`gh-proxy`); `create-issue` by default | Define the report window, grouping dimensions, deduplication key, and `noop` behavior for empty windows |
| Designer or design-governance review | `pull_request` with `paths:` scoped to UI, design-token, copy, or asset files | `github` (`gh-proxy`); optional `playwright`; `add-comment` on the PR | State the review rubric (for example accessibility, token consistency, asset policy), and call `noop` when scoped files are unchanged |
| Legal / compliance / documentation-policy review | `pull_request` with scoped `paths:` or `schedule` for recurring audits | `github` (`gh-proxy`); `add-comment` for findings; `create-issue` only for violations needing follow-up | Classify findings against the policy, search for existing open issues before escalating, and call `noop` when there is no in-scope change or violation |

## Milestone slip / dependency-escalation trigger decision

Coordination-style requests (for example "tell me when a milestone is slipping" or "flag blocked cross-team dependencies") are often ambiguous between a recurring digest and an event-driven alert. Use this decision order:

1. **Default to `schedule` (+ `workflow_dispatch`)** when the request is about ongoing visibility into milestone health or dependency status over time (a digest), not a single triggering event. Follow the [Recurring Digest Defaults](report.md#recurring-digest-defaults) for window, grouping, and dedup key.
2. **Use `issues` (`types: [labeled, milestoned, demilestoned, opened]`)** only when the requester explicitly wants an immediate reaction to a specific state change (for example the moment an issue is relabeled `blocked` or moved off a milestone), not a periodic summary. For milestone checks, include `opened` and guard it as described below; `milestoned` alone may miss creation-time assignment.
3. **Combine both** only when the requester explicitly asks for both an immediate alert and a periodic rollup; keep them as two distinct trigger blocks (or two workflows) rather than one ambiguous trigger, so each has its own dedup key.

| Signal in the request | Trigger | Grouping dimension | Dedup key example |
|---|---|---|---|
| "weekly/daily view of milestones at risk" | `schedule` + `workflow_dispatch` | milestone, owning team | `milestone-risk:<milestone>:<window-id>` |
| "let me know the moment a milestone slips" | `issues` (`opened`/`milestoned`/`demilestoned`) or `workflow_run` if computed by CI | milestone | `milestone-slip:<milestone>:<issue-number>` |
| "flag blocked dependencies across teams" (ongoing) | `schedule` + `workflow_dispatch` | dependency, blocking team, severity | `dependency-escalation:<dependency>:<window-id>` |

Call `noop` when the window has no slipped milestones or newly blocked dependencies, and search for an existing open issue with the same dedup key before creating a new one.

GitHub's [issue trigger documentation](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#issues) describes `milestoned` for when an issue is added to a milestone, but does not specify that creating an issue already assigned to a milestone emits that activity. Do not rely on `milestoned` alone: include `opened` and process that event only when `github.event.issue.milestone != null`. Continue using `milestoned` for later assignments and `demilestoned` for removals.

## Backend review guidance

For backend-focused PR automation (schema migrations and API compatibility):

- scope `pull_request.paths` to backend contract indicators instead of whole-repo review
- instruct the agent to classify changes as additive, backward-compatible, or breaking, then report only actionable risks
- include explicit `noop` criteria when no migration/API contract files changed

## PR analyzer escalation guidance

For PR-triggered automation that must decide between commenting, creating an issue, or doing nothing:

| Condition | Action |
|---|---|
| Findings affect only this PR (style, quality, risk) | `add-comment` on the PR |
| Finding is a cross-cutting or team-wide concern requiring follow-up beyond this PR | `create-issue` |
| No findings, or only docs/metadata changed outside scoped `paths:` | `noop` |

Rules:

- prefer `add-comment` over `create-issue` for PR-local findings; issues outlive the PR and create noise
- before creating an issue, search for an existing open issue covering the same concern (use a stable title prefix or label to avoid duplicates)
- if a matching open issue already exists, add a linked `add-comment` on the PR referencing it instead of opening a duplicate issue
- call `noop` explicitly whenever no actionable finding exists — do not comment with "no issues found" text

## Incident dedup-key templates (`workflow_run` and `deployment_status`)

For incident workflows, define one stable dedup key before creating output and search for an open issue containing that key.

Use and adapt these templates:

```text
# workflow_run incident key
incident:workflow_run:<workflow-name>:<job-name-or-unknown>:<error-signature>:<window-id>
example: incident:workflow_run:CI:lint:eslint-error:2026-07-05

# deployment_status incident key
incident:deployment_status:<environment-or-ref>:<provider-or-target>:<error-signature>:<window-id>
example: incident:deployment_status:production:vercel:build-timeout:2026-07-05
```

Template rules:

- keep `<error-signature>` stable (normalized failing step, error class, or provider error code)
- use `<window-id>` based on the selected reporting window (for example `2026-07-05` or `2026-W27`)
- create a new issue only when no open issue matches the same key
- call `noop` when the event is non-terminal, recovered, or already represented by an open issue with the same key

## Compliance review guidance

For dependency-license compliance and policy review on PRs:

- scope `pull_request.paths` to dependency manifest files (for example `package.json`, `go.mod`, `requirements.txt`, `Cargo.toml`, `pyproject.toml`, `composer.json`)
- classify each new dependency by license tier using the project's configured policy (the example tiers below represent a common MIT-compatible policy; adjust for your project): **allowed** (MIT, Apache-2.0, BSD, ISC), **needs-review** (unknown, dual-licensed, weak-copyleft), **blocked** (strong-copyleft such as GPL/AGPL, proprietary, or licenses incompatible with your project's license)
- publish per-tier findings with `add-comment` listing each dependency, its version, and detected license
- escalate to `create-issue` only when a **blocked** dependency was added or a policy violation requires team-wide follow-up beyond this PR
- before creating a new issue, search for an existing open issue with a stable key (for example `license-violation + dependency-name + version`) to avoid duplicates; if found, link to it from the PR comment instead
- call `noop` when no new dependencies were added or all additions are confirmed in the allowed tier

**Compliance escalation decision table:**

| Finding | Action |
|---|---|
| No dependency manifest files changed | `noop` immediately |
| All new dependencies in allowed tier | `noop` (or brief `add-comment` confirmation when the workflow prompt explicitly requests a confirmation comment) |
| Dependencies in needs-review tier | `add-comment` listing them with license details and requesting maintainer confirmation |
| Blocked dependency added | `add-comment` flagging the violation + `create-issue` for team-wide record (skip `create-issue` if a matching open issue already exists) |

### Scheduled compliance-policy audit example

Monthly (or otherwise recurring) audits of policy/disclosure files use `schedule` instead of `pull_request`, since there is no single triggering PR event to react to:

```yaml
on:
  schedule:
    - cron: "0 9 1 * *" # first of the month
  workflow_dispatch:
permissions:
  contents: read
  issues: write
safe-outputs:
  create-issue:
    close-older-issues: true
```

Prompt guidance:

- Check for the presence and freshness of required policy/disclosure files (for example `SECURITY.md`, `CODE_OF_CONDUCT.md`, `LICENSE`, a responsible-disclosure contact) against the project's compliance checklist.
- Reporting window: one calendar month; dedup key example `compliance-audit:<window-id>` (for example `compliance-audit:2026-08`).
- Group findings by policy area (security disclosure, licensing, code of conduct) rather than by file.
- Escalate with `create-issue` only when a required file is missing, stale (for example no update in over a year), or contains a broken disclosure contact; use `close-older-issues: true` so each month's audit supersedes the prior one.
- Call `noop` when every required policy/disclosure file is present and current.

## Coverage-analysis guidance

For workflows that read, analyze, or comment on test coverage (PR comments, trend tracking, coverage gates):

- **Prefer existing artifacts**: check for a coverage artifact from the current or parent CI run before recomputing; use `actions: read` via `gh-proxy` to list and download artifacts.
- **Prefer PR signals**: read existing check run annotations or coverage diff comments before fetching raw data; only recompute when no artifact or annotation is available.
- **Explicit fallback**: when no artifact exists, document the fallback computation step in the workflow prompt; never invent coverage values.
- call `noop` when no coverage data can be retrieved or computed and there is no meaningful output to report.

See [test-coverage.md](test-coverage.md) for the full coverage data strategy.
