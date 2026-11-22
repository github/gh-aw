---
title: How to Create a Repo Factory
description: Bootstrap five repository-specific workflows with shared coding capacity and an evidence-driven improvement loop.
sidebar:
  order: 296
---

A Repo Factory is a small portfolio of GitHub Agentic Workflows coordinated
through issues and pull requests. The reusable
[`factory.md` instruction recipe](https://github.com/github/gh-aw/blob/main/.github/aw/factory.md)
guides a coding agent through repository discovery, backend selection, workflow
generation, compilation, and a bootstrap PR. It is not a new CLI command,
frontmatter schema, or central factory server.

## Start with the recipe

Give the recipe URL to an AI coding agent with access to the target repository
and the installed `gh aw` CLI. The agent reads repository conventions, languages,
CI, workflows, skills, engine configuration, and available permissions before
making changes. Supply a pinned guidance revision when reproducibility matters;
resolve all recipe links at that revision.

Interactive creation is the default. For example:

```text wrap
Read https://github.com/github/gh-aw/blob/main/.github/aw/factory.md
and create a Repo Factory in this repository. Ask me which coding backend
and capacity to use. Reuse existing workflows and open a bootstrap PR.
```

To use conservative defaults without an interview:

```text wrap
Read https://github.com/github/gh-aw/blob/main/.github/aw/factory.md
and create a Repo Factory in auto mode. Reuse the established engine;
use native GH-AW coding if Cloud Agent credentials are not configured.
Allow three active coding work items, admit new work only on weekdays
in UTC, and open a bootstrap PR. Do not dispatch workflows.
```

These are prompts, not executable workflow definitions. The result is five
self-contained, repository-specific Markdown workflows and compiled locks.
Existing workflows count toward the role map: extend an editable implementation
rather than installing a competing copy. Do not overwrite upstream-managed
`source:` workflows; use their approved update path and report dependencies.
Do not introduce factory-local shared prompt imports, a sixth coding worker, or
external state infrastructure merely to connect the roles.

## Roles and handoffs

| Role | Default activation | Output and next owner |
| --- | --- | --- |
| Dreamer | Daily, with deterministic evidence collection first | One focused factory-change PR for human review |
| Backlog Burner | Weekday schedule; manual runs obey the same admission policy | One eligible issue assigned to Cloud Agent or implemented natively |
| Daily Improvers | Weekdays | At most one grounded improvement issue per run; Burner owns implementation |
| Reviewers | Relevant PR events, including weekends | At most three actionable findings per review; Finisher handles stalled follow-up |
| PR Finisher | Daily and relevant check/review events, including weekends | A deduplicated repair request or native update on an existing PR |

Daily Improvers discover test, maintainability, correctness, performance and
code-quality opportunities. Issues include scope, motivation, acceptance
criteria and code references. They neither create PRs nor assign coding work.
Reviewers inspect diffs first, select only applicable skills, reuse existing
context, and avoid duplicate findings. All roles use explicit output maxima,
timeouts, engine budgets, bounded tool calls and subagent fan-out.

## Shared coding capacity

Default capacity is **three active coding work items**, not three new PRs per run.
Count the union of pending assignments, reservations, in-flight native runs and
open coding PRs, including drafts. An assignment and its linked PR occupy one
slot; an assignment without a PR still occupies a slot. Include known coding
producers outside the factory. Incomplete pagination, unknown assignment state,
or missing required CI evidence blocks new admission.

Backlog Burner is the only factory role admitting new issue-to-PR work. Serialize
coding admission and repair requests with one repository-wide Actions concurrency
group and `cancel-in-progress: false`, covering the run through safe-output
completion. Reread capacity inside the serialized run. Keep a trusted issue-linked
reservation while remote assignment or PR delivery is pending; use verified
terminal state to release it. Reconcile a delivered PR with its reservation
without double counting. A cancelled workflow does not prove Cloud Agent stopped.
Reservations must come from trusted workflow identity/delivery metadata, not an
arbitrary issue comment. Use existing safe outputs for coordination mutations.

Concurrency serializes participating workflows; it cannot impose a hard cap on
independent humans or outside automations. Document those producers and pause
new work if their state cannot be reconciled. Neither safe-output `max` nor a
prompt-only capacity check provides an atomic repository-wide reservation.

Use stable issue work keys to prevent duplicate assignment. Before retrying,
inspect delivered outputs, prior assignments and linked open/closed PRs.
Required CI must be healthy before admitting work; pending or unavailable results
are not passing checks. Default weekday gating uses UTC and also applies to
manual entrypoints.

PR Finisher keys repairs by PR head SHA and unresolved-feedback fingerprint.
Request at most one fix per unchanged key and two repair rounds per PR before
escalating to a human. Existing-PR repairs reuse its slot; an additional coding
session consumes capacity. Weekends prohibit new background sessions, including
Finisher-created ones, while reviews, CI and repairs in existing sessions remain
active. Never bypass branch protections, approvals, or merge checks.

## Select the coding backend

The coding backend is distinct from the GH-AW inference engine. Reuse the
repository's chosen engine across the portfolio wherever practical.

### GitHub Copilot Cloud Agent

Use `safe-outputs.assign-to-agent` for existing eligible issues or PRs, with
`max: 1`, explicit target scoping and eligibility restrictions. Configure a
repository-scoped **PAT** as `GH_AW_AGENT_TOKEN`: a fine-grained PAT needs metadata
read and actions, contents, issues and pull requests read/write; a classic PAT
requires `repo`. Follow the current
[Cloud Agent authentication reference](../reference/copilot-cloud-agent.mdx#authentication)
for acquisition, secret setup and organization approval/SSO requirements.
Inspect secret names only; never put token values in prompts, code or logs.

`GITHUB_TOKEN` and GitHub App installation tokens are not substitutes for this
assignment PAT. Conversely, Copilot *inference* can use
`permissions: { copilot-requests: write }` without an assignment PAT.
A configured secret or green workflow conclusion does not prove assignment
succeeded: reconcile safe-output delivery and the actual assignment state.
An explicitly chosen but unavailable Cloud backend is a setup blocker, not
permission to silently change backends or retry other credentials.

### Native GH-AW coding

Without configured Cloud credentials, default to native coding within Backlog
Burner using the established engine. Publish through
`safe-outputs.create-pull-request`; repair existing branches through
`safe-outputs.push-to-pull-request-branch`. Restrict `allowed-files` and set
**`exclude-lock-yml: true`** on both.

The exclusion drops compiled `.github/workflows/*.lock.yml`, not all workflow
YAML. If workflow Markdown changes, an authorized maintainer must run
`gh aw compile` and commit regenerated locks before merge. Other workflow YAML
still needs workflow-write credentials. Do not add locks to `.gitignore`.
The bootstrap PR includes compiled locks; exclusion applies to subsequent
runtime native coding PRs, not permission to bootstrap stale workflows.

## Evolve one small change at a time

Dreamer follows **Observe → Hypothesize → Experiment → Evaluate → Propose small
change → Measure outcome**. Deterministic pre-agent collection produces a bounded
recent evidence bundle and reads previous experiment decisions. Prefer cached
summaries, then representative audits and traces. Compare similar task cohorts
and revisions; include failures and no-ops, not just successful runs.

Use [run analysis](https://github.com/github/gh-aw/blob/main/.github/aw/analyzing-runs.md),
[audits](../setup/cli.md#audit), recorded token/AIC usage and
[OpenTelemetry](../reference/open-telemetry.mdx) evidence. Compare ordered tool-call
sequences, parent/child relationships, repeated reads, wasted context retrieval,
tool failures and subagent fan-out—not only aggregate tokens. Check run identity
and trace continuity before attributing patterns. No new observability service
is required; missing evidence remains unknown.

Record the hypothesis, baseline/candidate revisions, eligible cohort, metric
definitions, sample window, results, uncertainty and decision in a linked issue
or PR. Freeze quality guardrails before reading candidate results. Reuse
[experiments](../reference/experiments.md),
[session graders](../experimental/trace-graders.md), and
[evals](https://github.com/github/gh-aw/blob/main/.github/aw/evals.md). Built-in graders run in conclusion; custom
`graders.<id>.script` runs only in the isolated graders job.
`graders.operational-value.script` returns ordered `{id,value}` metrics with
the primary value first. Do not inject custom code into conclusion or use the
removed `graders.operational-value.run`.

Evals require safe outputs and an available Decisions-compatible model. Their
judgments are not deterministic test results; missing/failed judgments are not
passes. Generated skills need clear applicability instructions and evaluation
on held-out cases before adoption. Optimize cost only when correctness,
reliability and review quality remain acceptable.

Keep one focused architecture PR open at a time. Propose one reversible change
to a skill, budget, trigger, or workflow rather than a broad redesign. Require
human review; after merge, measure a fresh outcome window and retain the change
or propose rollback. Preserve earlier experiment and grader results. Separate
delivery from human adoption using [impact measurement](measuring-impact.md).

### Avoid redundant workflows

Maintain the role-to-workflow map and compare trigger, scope, output and observed
adoption before adding a specialist. A recurring coverage gap can justify a new
reviewer or improver; overlapping behavior usually calls for extending or
consolidating an existing workflow. Dreamer may propose disabling or removing an
ineffective workflow, but never applies architecture changes directly. An open
proposal suppresses another proposal for the same work.

## Validate and operate

Compile all five mapped workflows with the installed `gh aw compile`; inspect
generated permissions, triggers, safe-output targets and locks. Run existing
repository lint/tests, validate referenced links, and check maintained instruction
prefixes. Agent jobs remain read-only, with explicit network boundaries and safe
outputs for writes. Privileged review/Finisher contexts must not execute PR-head
code; issue, PR and log content is untrusted data.

Record the role map, UTC/selected schedule, capacity, backend prerequisites,
budgets, reservation reconciliation, pause/resume procedure, and test evidence
in the bootstrap PR and target repository's operator notes. To pause admission,
disable Backlog Burner and any Finisher new-session path; keep reviews/CI active.
Resume only after reconciling pending assignments and CI. Setup blockers must be
reported rather than claiming the factory is operational.

Bootstrap does not authorize dispatch. Live testing requires a separately
requested dry-run test and the
[review and live-execution gates](https://github.com/github/gh-aw/blob/main/.github/aw/debug-agentic-workflow.md).

## Prior art to inspect

These are patterns to adapt, not universal defaults or files to copy wholesale.
The named review/triage examples are GH-AW's own dogfooding workflows; the
`githubnext/agentics` collection supplies additional repository-maintenance
examples. Token workflows are provenance-managed from `githubnext/agentic-ops`.

| Source | Reuse |
| --- | --- |
| [Matt Pocock Skills Reviewer](https://github.com/github/gh-aw/blob/main/.github/workflows/mattpocock-skills-reviewer.md) | Diff-first review, selective pinned skills, cached PR context, bounded feedback |
| [PR Sous-Chef](https://github.com/github/gh-aw/blob/main/.github/workflows/pr-sous-chef.md) | Deterministic prefilter, stalled PR detection, work/state fingerprints, coding follow-up |
| [Issue Monster](https://github.com/github/gh-aw/blob/main/.github/workflows/issue-monster.md) | Eligible issue selection, CI checks, topic/retry deduplication, scoped assignment |
| [Daily Test Improver](https://github.com/githubnext/agentics/blob/main/docs/test-improver.md) and [Code Simplifier](https://github.com/githubnext/agentics/blob/main/docs/code-simplifier.md) | Repository-specific discovery; change direct-PR behavior to issue-only for this factory |
| [Agentic Token Optimizer](https://github.com/github/gh-aw/blob/main/.github/workflows/agentic-token-optimizer.md) and [Token Audit](https://github.com/github/gh-aw/blob/main/.github/workflows/agentic-token-audit.md) | Deterministic evidence bundles, prior optimization history, bounded cost analysis |

The recipe deliberately tightens some example policies: three shared coding
slots, weekday admission, issue-only Improvers and human-reviewed architecture.
