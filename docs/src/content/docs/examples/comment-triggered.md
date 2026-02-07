---
title: Comment-Triggered Workflows
description: Interactive workflows triggered by slash commands in issues, PRs, and discussions - ChatOps patterns for human-in-the-loop automation
sidebar:
  order: 2
---

Comment-triggered workflows respond to slash commands typed in GitHub conversations. They enable ChatOps patterns where team members interact with AI agents through natural commands like `/review`, `/deploy`, or `/analyze`.

## When to Use Comment-Triggered Workflows

- **Interactive assistance**: Code review helpers, analysis on demand
- **Controlled automation**: Human decides when workflow runs
- **Context-aware responses**: AI acts on specific issue/PR context
- **Team collaboration**: Shared commands for common tasks

## Patterns in This Section

- **[ChatOps](/gh-aw/patterns/chatops/)** - Build interactive automation with command triggers

## Example Command Triggers

```yaml
on:
  slash_command:
    name: review           # Responds to /review
    events: [pull_request_comment]
```

```yaml
on:
  slash_command:
    name: analyze
    events: [issue_comment, pull_request_comment]
```

## Quick Start

Add a ChatOps workflow to your repository:

```bash
gh aw add-wizard githubnext/agentics/repo-ask
```

Then trigger it by commenting `/review` on a pull request!
