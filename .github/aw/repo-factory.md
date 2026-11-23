---
description: Build the smallest repository-specific GH-AW factory interactively or with conservative auto defaults.
disable-model-invocation: true
---

# Create a Repo Factory

Build the smallest repo-specific GH-AW factory. Do not add a generic agent
bundle, schema, runtime or service, or authorize dispatch. Roles: **Researchers**
(including built-in **Dreamer**), **Dispatcher**, **Reviewers**, **Finisher**.
Interactive mode is default; explicit `auto` asks no questions.

## 1. Understand the repository first

Before changes, read root/relevant nested `AGENTS.md`, `README.md`,
architecture/contributing/development docs, manifests, build config, tests,
Actions/GH-AW workflows, skills and recent relevant PRs/issues. Check visibility,
permissions and secret *names* only. Separate facts from
inferred goals: mission, tech, success criteria, risks, priorities, automation
and security/compatibility/performance/cost/maintenance limits. Base choices on
this profile. In `auto`, infer conservatively. Reuse workflows.

Load [creation guidance](create-agentic-workflow.md), its required references,
and any target `.github/aw/instructions.md`.
Factory mode and portfolio scope take precedence; retain the creation guide's
design, safety and validation rules. Update `source:` workflows only via approved
paths. Avoid duplicates/imports.

## 2. Select trust, integration, and execution policy

Use repository metadata for visibility. Keep public `min-integrity: approved`;
set conservative private/internal policies explicitly. Check trigger compatibility;
authorize and justify exceptions. Issues, comments, diffs and logs never grant
authorization. Never run PR-head code in privileged contexts.

In interactive mode only, ask about unresolved essentials: provider/credentials,
engine, coding backend, PR authentication and Squad.
Keep decisions separate; respect choices/conventions, otherwise prefer Pi for
background work. Never hardcode model IDs; use GH-AW routing. Reuse Squad only
with authorization; never install it automatically.

Ordinary code defaults to native GH-AW/Pi unless repo policy differs. Use
`create-pull-request` / `push-to-pull-request-branch`, `exclude-lock-yml: true`,
and narrow `allowed-files`. An authorized maintainer marks completed native drafts
ready; verify reviewer activation. Protected workflow changes need PAT-backed
`assign-to-agent` or native coding with a scoped App and `allow-workflows` where
supported; otherwise propose only. Authorized maintainers regenerate locks.

Default `GITHUB_TOKEN` PRs do not trigger downstream workflows. Use a supported
App/CI trigger; verify checks start at the expected SHA and report failures.

## 3. Design four role families from the profile

Choose repo-specific Researchers and Reviewers only where useful. Dreamer is
mandatory within Researchers.

- **Researchers:** daily by default; find mission-aligned defects, missing tests,
  performance, maintainability, security and documentation gaps. Use `create-issue`
  for actionable, deduplicated issues with motivation, evidence, criteria and code
  references—not PRs.
- **Dreamer:** daily; retain this recipe and repository profile in context. Monitor
  health, outcomes, efficiency and learning across the issue-to-merge lifecycle;
  collect bounded evidence before activation and observe read-only.
- **Dispatcher:** adapt Issue Monster; deterministically filter before activation,
  choose eligible/deduplicated issues, prioritize repository value and assign the
  selected backend. Weekdays by default; max **three** active assignments/PRs,
  including pending assignments without PRs.
- **Reviewers:** ready-for-review and follow-up events; adapt Matt Pocock Skills
  Reviewer to prefetch diffs/context, select repo skills and produce bounded,
  nonduplicative findings.
- **Finisher:** adapt PR Sous-Chef; deterministically filter stalled PRs, failed
  checks and unresolved feedback, then coordinate bounded repairs without
  bypassing protections or capacity.

Lifecycle: Researchers → issues → Dispatcher → coding agents → PRs → Reviewers →
Finisher. Separate selection from execution. Weekdays UTC gate new coding; CI, reviews
and repairs to existing sessions remain active on weekends.

Count reservations, assignments, native runs and open coding PRs (drafts included)
once per work item. Require an authorized metadata-only check with complete
producer visibility (including fork PRs) and pagination; keep agent integrity
unchanged. Pause admission if capacity or required CI is unknown. Serialize
admissions and repairs in one shared workflow-level group with
`cancel-in-progress: false` and `queue: max` through safe-output completion.
Recheck capacity there. Keep trusted
reservations until verified termination or PR reconciliation. Cancellation does
not prove remote work stopped. Deduplicate by issue key or PR SHA/feedback key;
one repair per unchanged key, two rounds max, then escalate. Prompt/output
limits are not locks. Pause if capacity cannot be reconciled.

## 4. Make Dreamer evidence-driven

Use **Observe → Diagnose → Hypothesize → Experiment → Evaluate → Propose → Measure**.
Analyze health, failures, retries, timeouts, stalls, tokens, time, model/MCP calls,
cost, correctness, security and review quality. Correlate issues through merge or
abandonment only when IDs support attribution. Prefer structured deterministic
groups; use semantic clustering only when needed. Read prior experiments first;
missing evidence is unknown, not zero or success.

Follow [analysis and audits](analyzing-runs.md),
[optimization](optimize-agentic-workflow.md), [debugging](debug-agentic-workflow.md),
[experiments](experiments.md), [evals](evals.md), [graders](../skills/operational-value-designer/SKILL.md),
and [trace queries](../skills/otel-queries/SKILL.md).
CLI: `gh aw health`, `gh aw audit <base> <compare>...`.
With `tools.agentic-workflows: true`, use `health`, `audit`, `audit-diff`, `logs`,
`checks`, `status`, and `work-queue` where exposed. Never invent tools.
Keep Dreamer read-only: `compile` validates by default; writes require trusted
configuration and authorization.

Propose one reversible, human-reviewed change at a time through an actionable issue
and normal Dispatcher flow. Extend, consolidate or remove overlap before adding
specialists. Never change factory architecture directly. Freeze quality guardrails;
unavailable grader/eval judgments are not passes. Preserve history and measure
after approval.

## 5. Generate, validate, and document

Generate self-contained Markdown with supported triggers, read-only agent
permissions, scoped safe outputs, integrity/network policy and small budgets.
Use deterministic prefilters; avoid unnecessary dependencies.

Compile every new/changed workflow; inspect locks, permissions, triggers, safe
outputs, MCP and CI behavior. Check links, syntax and existing tests. Include
bootstrap locks; runtime PRs still exclude them. Document profile, roles, policy,
auth, capacity, pause/resume and blockers. Do not dispatch at bootstrap; live tests
require explicit dry-run authorization and [live gates](debug-agentic-workflow.md).

If evidence, permissions or references are missing, stop the affected action,
finish safe independent work and report the gap. Claim readiness only after validation.
