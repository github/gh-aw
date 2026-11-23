---
title: How to Create a Repo Factory
description: Bootstrap a repository-aware GH-AW factory with four role families, shared coding capacity, and evidence-driven improvement.
sidebar:
  order: 296
---

A Repo Factory is a small portfolio of GitHub Agentic Workflows designed around
the target repository's mission and coordinated through issues and pull requests.
The reusable
[`repo-factory.md` instruction recipe](https://github.com/github/gh-aw/blob/main/.github/aw/repo-factory.md)
guides a coding agent through repository reconnaissance, policy selection, workflow
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
Read https://github.com/github/gh-aw/blob/main/.github/aw/repo-factory.md
and create a Repo Factory in this repository. First profile the repository and
reuse existing workflows. Ask only for missing provider/engine, backend/PR
authentication, Squad preference, or capacity choices; then open a bootstrap PR.
```

To use conservative defaults without an interview:

```text wrap
Read https://github.com/github/gh-aw/blob/main/.github/aw/repo-factory.md
and create a Repo Factory in auto mode. Reuse the established engine;
prefer Pi where consistent, use native GH-AW coding if Cloud Agent credentials
are not configured, and do not install Squad.
Allow three active coding work items, admit new work only on weekdays
in UTC, and open a bootstrap PR. Do not dispatch workflows.
```

These are prompts, not executable workflow definitions. The result is the smallest
useful set of self-contained, repository-specific Markdown workflows and compiled
locks. Existing automation counts toward the role map: extend an editable
implementation rather than installing a competing copy. Do not overwrite
upstream-managed `source:` workflows; use their approved update path and report
dependencies. Do not add a factory YAML schema, centralized service, or external
state infrastructure.

## Repository profile and installation

Reconnaissance comes first. Read root and relevant nested `AGENTS.md`, `README.md`,
architecture/contributing/development documentation, manifests, build configuration,
tests, Actions and GH-AW workflows, skills, recent relevant PRs/issues, repository
visibility, and available permissions. Record a concise repository profile:
mission, technology, success criteria, engineering risks, research/review
priorities, existing capabilities, and security, compatibility, performance, cost,
and maintenance constraints. Mark documented facts separately from inferred goals.
All role selection and workflow policy must derive from this profile.

Use the following installation sequence:

1. Establish the repository mission and profile.
2. Inventory existing workflows, skills, and review mechanisms. Check for
   `.github/workflows/squad.md` and an initialized `.squad/` team separately
   (see [Decide whether to reuse Squad](#decide-whether-to-reuse-squad)).
3. Detect visibility, integrity requirements, protected-file restrictions, and CI
   behavior.
4. Choose provider/credentials, GH-AW engine, coding backend, PR authentication,
   and model-routing policy as separate decisions.
5. Select the smallest useful Researchers and Reviewers, with the built-in Dreamer,
   Dispatcher, and Finisher.
6. Generate self-contained workflows with supported triggers, permissions, safe
   outputs, integrity policies, and small execution budgets.
7. Compile and validate permissions, MCP configuration, and CI-trigger behavior.
8. Document the role map, the Squad reuse or skip decision, selected policies,
   operating instructions, and blockers.
9. Let Dreamer propose future incremental changes from measured outcomes.

Keep the repository profile in Dreamer's prompt/context and ensure Dreamer can
consult the `repo-factory.md` recipe. Use only a supported workspace-context
mechanism; otherwise carry the concise profile and relevant architecture invariants
in its prompt.

## Roles and handoffs

| Role family | Default activation | Output and next owner |
| --- | --- | --- |
| Researchers (including Dreamer) | Daily; collect bounded evidence before Dreamer activation | Scoped issues; Dispatcher owns implementation assignment |
| Dispatcher | Weekdays by default; manual runs obey the same admission policy | One eligible issue assigned to the selected backend |
| Reviewers | Ready-for-review and follow-up review requests, including weekends | At most three actionable findings; Finisher coordinates follow-up |
| Finisher | Stalled PR/check/review events, including weekends | Deduplicated bounded repair or escalation |

Researchers investigate mission-aligned bugs, missing tests, performance,
maintainability, security, and documentation gaps. They produce evidence-backed,
deduplicated issues with motivation, acceptance criteria, and code references—not
implementation PRs. Reviewers prefetch diffs/context, select repository-specific
skills, and avoid repeating findings. Each role uses explicit output maxima,
timeouts, engine budgets, bounded tool calls, and subagent fan-out.

## Visibility, integrity, and trust

Detect public, private, or internal visibility from GitHub repository metadata.
For public repositories, preserve the documented `min-integrity: approved`
baseline. Configure private and internal repositories explicitly and conservatively;
private content is not inherently trusted. Verify that integrity filtering is
compatible with each trigger and event source. Any exception must be narrowly
scoped, justified by the task, and appropriately authorized—not a general trust
override.

Dispatcher treats issue content only as untrusted work description, never
authorization. Reviewers safely inspect untrusted PR diffs, comments, and metadata;
Finisher does not treat arbitrary comments as permission to edit protected files.
Dreamer must not let logs, prompts, or workflow output redefine factory policy.
Keep agent jobs read-only and use safe outputs for writes. Do not execute PR-head
code in reviewer, Dispatcher, or Finisher contexts.

## Provider, engine, model, and CI choices

During installation, determine the inference provider and available credentials,
preferred GH-AW engine, coding backend, and authentication for PR creation
separately. Strongly prefer Pi for background automation unless an explicit user
choice or established repository convention overrides it. Reuse configured
provider/engine settings and leave model choice to GH-AW routing; never hardcode a
model identifier. A Cloud Agent assignment PAT is not an inference credential.

Ordinary code work defaults to native GH-AW coding with Pi when no established
choice overrides it. Protected workflow/factory changes require either
PAT-backed `assign-to-agent` or native coding with a suitably scoped GitHub App;
document `allow-workflows` where supported and use least-privilege permissions.
Without an authorized path, Dreamer/Researchers may file a proposal but must not
bypass protected-file restrictions.

Default `GITHUB_TOKEN` PR creation does not trigger downstream GitHub Actions
workflows. If required CI needs a separate trigger, configure a supported App or
CI-trigger token, such as `github-token-for-extra-empty-commit: "app"` or the
`GH_AW_CI_TRIGGER_TOKEN` secret. Verify checks start against the expected PR SHA
and surface failures. Do not infer successful CI from PR creation.

## Shared coding capacity

Default capacity is **three active coding work items**, not three new PRs per run.
Count the union of pending assignments, reservations, in-flight native runs and
open coding PRs, including drafts. An assignment and its linked PR occupy one
slot; an assignment without a PR still occupies a slot. Include known coding
producers outside the factory. Even fully paginated GitHub MCP reads and
pre-agent `gh` calls can omit outside contributors' fork PRs under
`min-integrity: approved`. Require an authorized, read-only capacity check with
complete pagination and visibility across all coding producers. Limit the check
to metadata and keep the agent's integrity policy unchanged. Pause admission if
visibility, coding state, or required CI evidence is incomplete.

Dispatcher is the only factory role admitting new issue-to-PR work. Serialize
coding admission and repair requests with one shared repository-wide Actions
workflow-level concurrency group. Set `cancel-in-progress: false` and explicitly
set `queue: max` so newer triggers do not replace the only pending run; custom
groups do not inherit that queue setting. Hold the group through safe-output
completion and reread capacity inside the serialized run. Keep a trusted
issue-linked reservation while remote assignment or PR delivery is pending; use
verified terminal state to release it. Reconcile a delivered PR with its reservation
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

Finisher keys repairs by PR head SHA and unresolved-feedback fingerprint.
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
[Cloud Agent authentication reference](/gh-aw/reference/copilot-cloud-agent/#authentication)
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

Without configured Cloud credentials, default to native coding within Dispatcher
using the established engine. Publish through
`safe-outputs.create-pull-request`; repair existing branches through
`safe-outputs.push-to-pull-request-branch`. Restrict `allowed-files` and set
**`exclude-lock-yml: true`** on both.

`create-pull-request` defaults to drafts. Assign an authorized maintainer to mark
completed native PRs ready for review and verify the `ready_for_review` event
starts the reviewer workflow. A draft still occupies a coding slot but does not
pass the PR Sous-Chef prefilter.

The exclusion drops compiled `.github/workflows/*.lock.yml`, not all workflow
YAML. If workflow Markdown changes, an authorized maintainer must run
`gh aw compile` and commit regenerated locks before merge. Other workflow YAML
still needs workflow-write credentials. `allow-workflows: true` requires a
configured GitHub App with workflow-write permission. Do not add locks to
`.gitignore`.
The bootstrap PR includes compiled locks; exclusion applies to subsequent
runtime native coding PRs, not permission to bootstrap stale workflows.

## Evolve one small change at a time

Dreamer is the built-in specialized Researcher. It has factory-health, efficiency,
and learning goals: monitor failures, retries, timeouts and stalls; inspect token,
time, model, MCP-call and cost use; and study issues, assignments, PRs, reviews,
repairs, CI, merge or abandonment outcomes. Deterministic pre-agent collection
produces bounded evidence and reads previous experiment decisions. Prefer cached
summaries, then representative audits and traces. Compare similar task cohorts
and revisions; include failures and no-ops, not just successful runs.

Use [run analysis](https://github.com/github/gh-aw/blob/main/.github/aw/analyzing-runs.md),
[optimization](https://github.com/github/gh-aw/blob/main/optimize.md),
[debugging](https://github.com/github/gh-aw/blob/main/debug.md),
[debugging reference](/gh-aw/troubleshooting/debugging/),
[audits](/gh-aw/reference/audit/), recorded token/AIC usage and
[OpenTelemetry](/gh-aw/reference/open-telemetry/) evidence. Compare ordered tool-call
sequences, parent/child relationships, repeated reads, wasted context retrieval,
tool failures and subagent fan-out—not only aggregate tokens. Check run identity
and trace continuity before attributing patterns. No new observability service
is required; missing evidence remains unknown.

Correlate the full issue-to-merge lifecycle only through supported identifiers
(issue number, assignment, run ID, PR number/head SHA, review, checks, and merge
state). Prefer deterministic groups using structured execution metadata, outcomes,
failures, and graders. Use semantic clustering only when deterministic categories
cannot answer the question. Use `gh aw health` for health, `gh aw audit <run>` for
one run, and `gh aw audit <base> <compare>...` for comparisons. Enable MCP with
`tools: { agentic-workflows: true }`; see the
[MCP reference](/gh-aw/reference/gh-aw-as-mcp-server/).
Use `health` for health, `audit` for a single run, `audit-diff` for comparisons,
`logs` and `checks` for run evidence, `status` for workflow status, and
`work-queue` for queue inspection.

Keep routine Dreamer access read-only. By default, MCP `compile` validates with
`--no-emit --no-check-update --read-only`. Emitting lock files, applying fixes,
dry-run compilation, and external scanners require explicit write-tool opt-in.

Record the hypothesis, baseline/candidate revisions, eligible cohort, metric
definitions, sample window, results, uncertainty and decision in a linked issue
or PR. Freeze quality guardrails before reading candidate results. Reuse
[experiments](/gh-aw/reference/experiments/),
[session graders](/gh-aw/experimental/trace-graders/), and
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
delivery from human adoption using [impact measurement](/gh-aw/guides/measuring-impact/).

### Avoid redundant workflows

Maintain the role-to-workflow map and compare trigger, scope, output and observed
adoption before adding a specialist. A recurring coverage gap can justify a new
Reviewer or Researcher; overlapping behavior usually calls for extending or
consolidating an existing workflow. Dreamer may propose disabling or removing an
ineffective workflow, but never applies architecture changes directly. An open
proposal suppresses another proposal for the same work.

## Validate and operate

Compile all mapped workflows with the installed `gh aw compile`; inspect
generated permissions, triggers, safe-output targets and locks. Run existing
repository lint/tests, validate referenced links, and check maintained instruction
prefixes. Agent jobs remain read-only, with explicit network boundaries and safe
outputs for writes. Privileged Reviewer, Dispatcher, and Finisher contexts must not
execute PR-head code; issue, PR and log content is untrusted data.

Record the role map, UTC/selected schedule, capacity, backend prerequisites,
budgets, reservation reconciliation, pause/resume procedure, and test evidence
in the bootstrap PR and target repository's operator notes. To pause admission,
disable Dispatcher and any Finisher new-session path; keep reviews/CI active.
Resume only after reconciling pending assignments and CI. Setup blockers must be
reported rather than claiming the factory is operational.

Bootstrap does not authorize dispatch. Live testing requires a separately
requested dry-run test and the
[review and live-execution gates](https://github.com/github/gh-aw/blob/main/.github/aw/debug-agentic-workflow.md).

## Decide whether to reuse Squad

The presence of `.github/workflows/squad.md` does not mean a team is initialized.
If an existing team is approved for reuse, map factory roles to it and record
one owner for each trigger and handoff to avoid duplicate work.

Auto mode does not initialize Squad. For a separately requested local SDLC
bootstrap, see the [Squad issue recipe](https://github.com/github/gh-aw/blob/main/.github/aw/squad.md).

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
| [Test-improver pattern](https://github.com/githubnext/agentics/blob/main/docs/test-improver.md) and [Code Simplifier](https://github.com/githubnext/agentics/blob/main/docs/code-simplifier.md) | Repository-specific discovery; use issue-only Researcher outputs |
| [Agentic Token Optimizer](https://github.com/github/gh-aw/blob/main/.github/workflows/agentic-token-optimizer.md) and [Token Audit](https://github.com/github/gh-aw/blob/main/.github/workflows/agentic-token-audit.md) | Deterministic evidence bundles, prior optimization history, bounded cost analysis |

The recipe deliberately applies conservative policies: three shared coding
slots, weekday admission, issue-only Researchers and human-reviewed architecture.
