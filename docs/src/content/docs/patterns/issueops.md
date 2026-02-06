---
title: IssueOps
description: Automate issue triage, categorization, and responses when issues are opened - fully automated issue management
sidebar:
  badge: { text: 'Event-triggered', variant: 'success' }
---

IssueOps transforms GitHub issues into automation triggers that analyze, categorize, and respond to issues automatically. Use it for auto-triage, smart routing, initial responses, and quality checks. GitHub Agentic Workflows makes this natural through issue triggers and [safe-outputs](/gh-aw/reference/safe-outputs/) that handle automated responses securely without write permissions for the main AI job.

When issues are created, workflows activate automatically. The AI analyzes content and provides intelligent responses through automated comments.

```aw wrap
---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  actions: read
safe-outputs:
  add-comment:
    max: 2
---

# Issue Triage Assistant

Analyze new issue content and provide helpful guidance. Examine the title and description for bug reports needing information, feature requests to categorize, questions to answer, or potential duplicates. Respond with a comment guiding next steps or providing immediate assistance.
```

This creates an intelligent triage system that responds to new issues with contextual guidance.

## Safe Output Architecture

IssueOps workflows use the `add-comment` safe output to ensure secure comment creation with minimal permissions. The main job runs with `contents: read` while comment creation happens in a separate job with `issues: write` permissions, automatically sanitizing AI content and preventing spam:

```yaml wrap
safe-outputs:
  add-comment:
    max: 3                    # Optional: allow multiple comments (default: 1)
    target: "triggering"      # Default: comment on the triggering issue/PR
```

## Accessing Issue Context

Access sanitized issue content through `needs.activation.outputs.text`, which combines title and description while removing security risks (@mentions, URIs, injections):

```yaml wrap
Analyze this issue: "${{ needs.activation.outputs.text }}"
```

## Common IssueOps Patterns

### Automated Bug Report Triage

```aw wrap
---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  actions: read
safe-outputs:
  add-labels:
    allowed: [bug, needs-info, enhancement, question, documentation]  # Restrict to specific labels
    max: 2                                                            # Maximum 2 labels per issue
---

# Bug Report Triage

Analyze new issues and add appropriate labels: "bug" (with repro steps), "needs-info" (missing details), "enhancement" (features), "question" or "documentation" (help/docs). Maximum 2 labels from the allowed list.
```

## Organizing Work with Sub-Issues

Break large work into agent-ready tasks using parent-child issue hierarchies. Create hierarchies with the `parent` field and temporary IDs, or link existing issues with `link-sub-issue`:

```aw wrap
---
on:
  command:
    name: plan
safe-outputs:
  create-issue:
    title-prefix: "[task] "
    max: 6
---

# Planning Assistant

Create a parent tracking issue, then sub-issues linked via parent field:

{"type": "create_issue", "temporary_id": "aw_abc123def456", "title": "Feature X", "body": "Tracking issue"}
{"type": "create_issue", "parent": "aw_abc123def456", "title": "Task 1", "body": "First task"}
```

> [!TIP]
> Hide sub-issues
> Filter sub-issues from `/issues` with `no:parent-issue`: `/issues?q=no:parent-issue`

Assign sub-issues to Copilot with `assignees: copilot` for parallel execution.

