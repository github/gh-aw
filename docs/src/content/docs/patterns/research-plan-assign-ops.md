---
title: ResearchPlanAssignOps
description: Orchestrate deep research, structured planning, and automated assignment to drive AI-powered development cycles from insight to merged PR
sidebar:
  badge: { text: 'Multi-phase', variant: 'caution' }
---

ResearchPlanAssignOps is a four-phase development pattern that moves from automated discovery to merged code with human control at every decision point. A research agent surfaces insights, a planning agent converts them into actionable issues, a coding agent implements the work by [assigning issues to GitHub Copilot](/gh-aw/reference/copilot-cloud-agent/), and a human reviews and merges.

## The Four Phases

```mermaid
flowchart LR
    research([Research]) --> plan[Plan issues]
    plan --> assign[Assign to Copilot]
    assign --> merge[Review & merge]
```

Each phase produces a concrete artifact consumed by the next, and every transition is a human checkpoint.

### Phase 1: Research

A scheduled workflow investigates the codebase from a specific angle and publishes its findings as an issue or GitHub discussion. The research artifact is the contract between the research phase and everything that follows—it contains the analysis, recommendations, and context a planner needs.

The [`go-fan`](https://github.com/github/gh-aw/blob/main/.github/workflows/go-fan.md) workflow is a live example: it runs each weekday, picks one Go dependency, compares current usage against upstream best practices, and creates a `[go-fan]` issue.

```aw wrap
---
name: Go Fan

on:
  schedule: daily on weekdays
  workflow_dispatch:

engine: claude

safe-outputs:
  create-discussion:
    title-prefix: "[go-fan] "
    category: "audits"
    max: 1
    close-older-discussions: true

tools:
  cache-memory: true
  github:
    toolsets: [default]
---

Analyze today's Go dependency. Compare current usage in this
repository against upstream best practices and recent releases.
Save a summary to scratchpad/mods/ and create a discussion
with findings and improvement recommendations.
```

The research agent uses `cache-memory` to track which modules have been reviewed so it rotates through them systematically across runs.

### Phase 2: Plan

After reading the research issue or discussion, a developer triggers the `/plan` command on it. The [`plan`](https://github.com/github/gh-aw/blob/main/.github/workflows/plan.md) workflow reads the source, extracts concrete work items, and creates up to five sub-issues grouped under a parent tracking issue.

```
/plan focus on the quick wins and API simplifications
```

The planner formats each sub-issue for a coding agent: a clear objective, the files to touch, step-by-step implementation guidance, and acceptance criteria. Issues are tagged `[plan]` and `ai-generated`.

> [!TIP]
> The `/plan` command accepts inline guidance. Steer it toward high-priority findings or away from lower-priority ones before it generates issues.

### Phase 3: Assign

With well-scoped issues in hand, the developer [assigns them to Copilot](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr#assigning-an-issue-to-copilot) for automated implementation. Copilot opens a pull request and posts progress updates as it works.

Issues can be assigned individually through the GitHub UI, or pre-assigned in bulk via an orchestrator workflow:

```aw wrap
---
name: Auto-assign plan issues to Copilot

on:
  issues:
    types: [labeled]

engine: copilot

safe-outputs:
  assign-to-user:
    target: "*"
  add-comment:
    target: "*"
---

When an issue is labeled `plan` and has no assignee,
assign it to Copilot and add a comment indicating
automated assignment.
```

For multi-issue plans, assignments can run in parallel—Copilot works independently on each issue and opens separate PRs.

### Phase 4: Merge

Copilot's pull request is reviewed by a human maintainer. The maintainer checks correctness, runs tests, and merges. The tracking issue created in Phase 2 closes automatically when all sub-issues are resolved.

## End-to-End Example

A typical `go-fan` cycle spans two days: Monday morning the workflow creates an issue such as *"[go-fan] Go Module Review: spf13/cobra"* with recommendations like adopting `SetContext` and moving shared setup into `PersistentPreRunE`. That afternoon a developer runs `/plan`, which creates a `[plan] cobra improvements` tracking issue plus three sub-issues (context propagation, `PersistentPreRunE` refactor, and cancellation tests) and assigns the first two to Copilot. By Tuesday, the developer reviews the resulting PRs, requests any needed tweaks, and merges them; the tracking issue closes automatically once the sub-issues are resolved.

## Workflow Configuration Patterns

The Phase 1 example already shows the core research config (`create-discussion` with `close-older-discussions: true`, plus `cache-memory`). Two more safe-output knobs shape the later phases.

### Plan: group sub-issues

`group: true` creates the parent tracking issue automatically—do not create it manually:

```aw wrap
safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[plan] "
    labels: [plan, ai-generated]
    max: 5
    group: true
```

### Assign: skip planning with `assignees`

When research produces self-contained, well-scoped issues, assign directly and skip the manual plan phase—as `duplicate-code-detector` does for narrow duplication fixes:

```aw wrap
safe-outputs:
  create-issue:
    title-prefix: "[fix] "
    labels: [ai-generated]
    assignees: copilot
```

## Customization

Adapt the pattern by changing the **research focus** (for example static analysis, performance, documentation quality, security, code duplication, or test coverage), the **frequency** (daily, weekly, or on-demand), the **report format** (discussions for open-ended findings, issues for self-contained tasks), and the **assignment method** (pre-assign in the research workflow, bulk-assign via an orchestrator, or assign individually through the GitHub UI).

## When to Use ResearchPlanAssignOps

Use this pattern when the scope is unclear until analysis runs, the resulting issues need human prioritization, findings may be non-actionable, and multiple follow-up tasks can proceed in parallel.

Prefer a simpler pattern when the work is already well-defined ([IssueOps](/gh-aw/patterns/issue-ops/)), issues can go straight to Copilot via `assignees: copilot`, or the work spans multiple repositories ([MultiRepoOps](/gh-aw/patterns/multi-repo-ops/)).

## Limitations

The multi-phase approach is slower than direct execution because developers still need to review the research output and generated issues. It also depends on clean handoffs between phases, and research agents may produce false positives or need specialized MCPs such as Serena or Tavily for deeper analysis.

## Existing Workflows

| Phase | Workflow | Description |
|-------|----------|-------------|
| Research | [`go-fan`](https://github.com/github/gh-aw/blob/main/.github/workflows/go-fan.md) | Daily Go dependency analysis with best-practice comparison |
| Research | [`copilot-cli-deep-research`](https://github.com/github/gh-aw/blob/main/.github/workflows/copilot-cli-deep-research.md) | Weekly analysis of Copilot CLI feature usage |
| Research | [`static-analysis-report`](https://github.com/github/gh-aw/blob/main/.github/workflows/static-analysis-report.md) | Daily security scan with clustered findings |
| Research | [`duplicate-code-detector`](https://github.com/github/gh-aw/blob/main/.github/workflows/duplicate-code-detector.md) | Daily semantic duplication analysis (auto-assigns) |
| Plan | [`plan`](https://github.com/github/gh-aw/blob/main/.github/workflows/plan.md) | `/plan` slash command—converts issues or discussions into sub-issues |
| Assign | GitHub UI / workflow | [Assign issues to Copilot](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/coding-agent/create-a-pr#assigning-an-issue-to-copilot) for automated PR creation |

## Learn More

- [DispatchOps](/gh-aw/patterns/dispatch-ops/) — Manually triggered research and one-off investigations
- [WorkQueueOps](/gh-aw/patterns/workqueue-ops/) — Sequential queue processing for large backlogs
- [Safe Outputs](/gh-aw/reference/safe-outputs/) — Secure write operations
- [Copilot Cloud Agent](/gh-aw/reference/copilot-cloud-agent/) — Assigning issues to GitHub Copilot
