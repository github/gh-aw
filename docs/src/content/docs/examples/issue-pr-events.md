---
title: Issue & PR Event Workflows
description: Workflows triggered by GitHub events - issues opened, PRs created, labels added - for automated triage, analysis, and code assistance
sidebar:
  order: 3
---

Issue and PR event workflows run automatically when specific GitHub events occur. They're ideal for automated triage, intelligent labeling, code analysis, and quality checks that happen without any manual trigger.

## When to Use Event-Triggered Workflows

- **Immediate response**: Auto-triage new issues, welcome contributors
- **Automated analysis**: Accessibility audits, security scans
- **Smart labeling**: Classify issues/PRs based on content
- **Quality gates**: Run checks when PRs are opened or updated

## Patterns in This Section

- **[IssueOps](/gh-aw/patterns/issueops/)** - Automate issue triage and management
- **[LabelOps](/gh-aw/patterns/labelops/)** - Use labels as workflow triggers
- **[ProjectOps](/gh-aw/patterns/projectops/)** - Automate project board management
- **[Triage & Analysis](/gh-aw/examples/issue-pr-events/triage-analysis/)** - Intelligent triage and problem investigation
- **[Coding & Development](/gh-aw/examples/issue-pr-events/coding-development/)** - PR assistance and code improvements
- **[Quality & Testing](/gh-aw/examples/issue-pr-events/quality-testing/)** - Automated quality checks

## Example Event Triggers

```yaml
on:
  issues:
    types: [opened, labeled]
```

```yaml
on:
  pull_request:
    types: [opened, synchronize]
```

```yaml
on:
  pull_request_target:
    types: [labeled]
    branches: [main]
```

## Quick Start

Add event-triggered workflows to your repository:

```bash
gh aw add-wizard githubnext/agentics/issue-triage
gh aw add-wizard githubnext/agentics/pr-fix
```
