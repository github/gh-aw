---
description: Conversation-mode persona evaluation examples and invocation failure handling.
disable-model-invocation: true
---

# Workflow Scenario Evaluation

Use only for recommendation-only requests through the installed
`agentic-workflows` custom agent. Follow the
[creator's evaluation contract](create-agentic-workflow.md#ad-hoc-evaluation-mode).
Do not create/edit files; exit evaluation only on an explicit implementation request.

## Invocation surface

Address the custom agent in chat, an issue comment or a PR comment. CLI/MCP
commands (`compile`, `audit`, `status`, `update`) manage workflow files; none
accepts freeform `prompt`, `scenario` or `query` parameters. Do not script
evaluation by inventing tool parameters.

Single-scenario example:

> agentic-workflows evaluate this scenario without creating files: Information Worker -- weekly summary of stale documentation files not updated in the last 90 days

For multiple scenarios, use the same opener and list persona/task pairs.
Return one combined table, one row per scenario, using these fields:

| Scenario | Trigger | Scope | Read tools | Safe outputs | Permissions | Noop condition |
|---|---|---|---|---|---|---|
| Information Worker digest | schedule + workflow_dispatch | 7-day window by assignee | github, gh-proxy, default toolset | create-issue, close-older-issues | contents: read, issues: read | no assigned items in window |
| Product Manager backlog triage | schedule + workflow_dispatch | recurring window by staleness | github, gh-proxy, default toolset | create-issue, close-older-issues | contents: read, issues: read | no items cross threshold |
| Backend API review | pull_request with paths | API/schema files in PR | github, gh-proxy, default toolset | add-comment | contents: read, pull-requests: read | no contract changes |

Also state network requirements and resolve materially ambiguous terms with
labeled defaults. Name shared triggers or write paths after the table.
Offer generation only as a separate explicit implementation step.

## Failure classification

Classify tool/invocation failures separately from recommendation quality:

| Type | Action |
|---|---|
| Network, timeout, quota | Retry once unless the caller forbids retries; then record `invocation_unavailable` and continue with partial results. Authorization/host restrictions override retry advice. |
| Unknown command/option/parameter | Record `command_not_supported`; provide the recommendation directly from available local guidance. Do not retry an unsupported interface. |
| Invocation succeeds without design guidance | Record `response_unavailable`; report the capability gap, not an invented recommendation. |

Failed invocations have unavailable scores, not zero scores. Preserve partial
results and report missing evidence. Never infer successful design from an
invocation's exit status alone.
