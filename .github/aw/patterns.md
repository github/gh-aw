---
description: Agentic workflow pattern router for selecting the best documented pattern and playbook.
disable-model-invocation: true
---

# Agentic Workflow Patterns Router

Use this router when a user asks for a workflow architecture, strategy, operating model, or design pattern.

## Routing Rules

1. Identify the user's primary goal and constraints.
2. Match the request to the closest pattern in the index below.
3. Load and follow the matched pattern document.
4. If multiple patterns apply, pick one primary pattern and list 1-2 secondary patterns to combine.
5. If no pattern clearly fits, ask a short clarifying question before proceeding.

## Pattern Index

Pattern docs base path: `https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/`

### MonitorOps
- **Load when:** The user needs repository-wide workflow observability, trend reporting, and escalation for recurring failures or token waste.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/monitor-ops.md

### BatchOps
- **Load when:** The user needs to process large worksets in shards/chunks with throttling and aggregation.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/batch-ops.md

### CentralRepoOps
- **Load when:** The user needs a private control repository that coordinates rollouts across many target repositories.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/central-repo-ops.mdx

### ChatOps
- **Load when:** The user wants slash-command driven, human-in-the-loop automation in issues or pull requests.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/chat-ops.md

### CorrectionOps
- **Load when:** The user wants to improve workflow behavior from trusted human corrections without retraining the model.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/experimental/correction-ops.md

### DailyOps
- **Load when:** The user wants scheduled, small, recurring improvements that compound over time.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/daily-ops.md

### DeterministicOps
- **Load when:** The user needs deterministic data collection steps followed by agentic analysis and reporting.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/deterministic-ops.md

### DispatchOps
- **Load when:** The user needs manual trigger flows (`workflow_dispatch`) with custom inputs for testing or controlled runs.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/dispatch-ops.md

### Feature Grower
- **Load when:** The user wants a scheduled agent to advance long-lived features one implementation-ready sub-issue at a time.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/feature-grower.md

### IssueOps
- **Load when:** The user needs fully automated issue triage, categorization, and responses on issue events.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/issue-ops.md

### LabelOps
- **Load when:** The user needs label-driven workflow behavior when specific labels are added or removed.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/label-ops.md

### Monitoring with Projects
- **Load when:** The user needs durable tracking and monitoring of work items with GitHub Projects and safe outputs.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/experimental/monitoring-with-projects.md

### MultiRepoOps
- **Load when:** The user needs coordination and synchronization across multiple repositories.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/multi-repo-ops.md

### Orchestration
- **Load when:** The user needs orchestrator/worker architecture using reusable workflows or workflow dispatch.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/orchestration.md

### ProjectOps
- **Load when:** The user needs intelligent routing and controlled field updates in GitHub Projects.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/project-ops.mdx

### ResearchPlanAssignOps
- **Load when:** The user needs a flow from deep research to planning to automated issue assignment/implementation.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/research-plan-assign-ops.md

### SideRepoOps
- **Load when:** The user wants low-friction reporting/automation from a side repository targeting a primary repository.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/side-repo-ops.mdx

### SpecOps
- **Load when:** The user needs to maintain formal specifications and propagate spec updates to consuming implementations.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/spec-ops.md

### TrialOps
- **Load when:** The user needs isolated trial repositories to validate workflows before production rollout.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/experimental/trial-ops.md

### WorkQueueOps
- **Load when:** The user needs durable queue processing for many items via issues, sub-issues, discussions, or cache-memory.
- **Pattern doc:** https://github.com/github/gh-aw/blob/main/docs/src/content/docs/patterns/workqueue-ops.md

## Notes

- Prefer documented patterns over ad hoc architecture when a strong match exists.
- When relevant, combine pattern guidance with core workflow rules from:
  - https://github.com/github/gh-aw/blob/main/.github/aw/github-agentic-workflows.md
