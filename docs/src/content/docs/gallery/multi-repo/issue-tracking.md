---
title: 'Example: Cross-Repository Issue Tracking'
description: Centralize issue tracking across multiple repositories with automated tracking issue creation and status synchronization.
sidebar:
  badge: { text: 'Multi-Repo', variant: 'note' }
---

Centralize issues across component repositories without direct access to each repository.

## When to Use

Use it for component architectures, external dependencies, cross-project initiatives, or distributed metrics.

## How It Works

```mermaid
flowchart LR
    subgraph comp["Component repos"]
        ev1([Issue opened
component-alpha]) --> agent1[Tracking agent]
        ev2([Issue opened
component-beta]) --> agent2[Tracking agent]
    end
    agent1 -->|create-issue| central[central-tracker]
    agent2 -->|create-issue| central
```

Component workflows update central references when local issues change.

## Basic Tracking Issue Creation

```mermaid
flowchart LR
    subgraph comp["component-alpha"]
        ev([Issue opened]) --> agent[Tracking agent]
    end
    agent -->|create-issue| central["central-tracker\n[component-alpha] ..."]
```

```aw wrap
---
on:
  issues:
    types: [opened]

permissions:
  contents: read
  actions: read

tools:
  github:
    toolsets: [issues]

safe-outputs:
  github-token: ${{ secrets.GH_AW_CROSS_REPO_PAT }}
  create-issue:
    target-repo: "myorg/central-tracker"
    title-prefix: "[component-alpha] "
    labels: [from-component-alpha, tracking-issue]
---

# Create Central Tracking Issue

**Original issue:** ${{ github.event.issue.html_url }}
**Issue number:** ${{ github.event.issue.number }}
**Content:** "${{ steps.sanitized.outputs.text }}"

Create a tracking issue with the original link, component identifier, summary, suggested priority, and labels `from-component-alpha` and `tracking-issue`.
```

## Status Synchronization

```mermaid
flowchart LR
    subgraph comp["component-alpha"]
        ev(["Issue closed /\nreopened / labeled"]) --> agent[Tracking agent]
    end
    agent -->|find tracking issue| central[central-tracker]
    agent -->|add-comment| central
```

```aw wrap
---
on:
  issues:
    types: [closed, reopened, labeled, unlabeled]

permissions:
  contents: read
  actions: read

tools:
  github:
    toolsets: [issues]

safe-outputs:
  github-token: ${{ secrets.GH_AW_CROSS_REPO_PAT }}
  add-comment:
    target-repo: "myorg/central-tracker"
    target: "*"  # Find related tracking issue
---

# Update Central Tracking Issue Status

**Original issue:** ${{ github.event.issue.html_url }}
**Action:** ${{ github.event.action }}

Find the tracking issue in `myorg/central-tracker` and comment with the status (✅ resolved, 🔄 reopened, or 🏷️ label change), issue link, and timestamp.
```

## Multi-Component Tracking

```mermaid
flowchart LR
    subgraph comp["Component repos"]
        ev([Cross-component\nissue opened]) --> agent[Tracking agent]
    end
    agent -->|create-issue primary| central[central-tracker]
    agent -->|create-issue child| componentAlpha[component-alpha]
    agent -->|create-issue child| componentBeta[component-beta]
```

```aw wrap
---
on:
  issues:
    types: [opened]
    # Triggered when issue has 'cross-component' label

permissions:
  contents: read
  actions: read

tools:
  github:
    toolsets: [issues]

safe-outputs:
  github-token: ${{ secrets.GH_AW_CROSS_REPO_PAT }}
  create-issue:
    max: 3  # May create issues in multiple tracking repos
    target-repo: "myorg/central-tracker"
    title-prefix: "[cross-component] "
    labels: [cross-component, needs-coordination]
---

# Track Cross-Component Issues

**Original issue:** ${{ github.event.issue.html_url }}

Identify affected components. Create a primary tracking issue in the central tracker with the component list and coordination requirements; create child issues in affected repos when needed. For high-priority issues, tag team leads and schedule coordination.
```

## External Dependency Tracking

```mermaid
flowchart LR
    subgraph trigger["Manual trigger"]
        ev([workflow_dispatch\nexternal URL]) --> agent[Tracking agent]
    end
    agent -->|web-fetch| ext[External issue]
    agent -->|create-issue| tracker["dependency-tracker\n[upstream] ..."]
```

```aw wrap
---
on:
  workflow_dispatch:
    inputs:
      external_issue_url:
        description: 'URL of external issue to track'
        required: true
        type: string

permissions:
  contents: read

engine: codex
tools:
  github:
    toolsets: [issues]
  web-fetch:

safe-outputs:
  github-token: ${{ secrets.GH_AW_CROSS_REPO_PAT }}
  create-issue:
    target-repo: "myorg/dependency-tracker"
    title-prefix: "[upstream] "
    labels: [external-dependency, upstream-issue]
---

# Track External Dependency Issue

**External issue URL:** ${{ github.event.inputs.external_issue_url }}

Fetch the external issue, identify affected internal projects, and create a tracking issue with its link, status, impact, affected repositories, and monitoring plan. Set a weekly reminder and notify affected teams.
```

## Automated Triage and Routing

```mermaid
flowchart LR
    subgraph comp["component-alpha"]
        ev([Issue opened]) --> agent[Triage agent]
    end
    agent -->|security| sec[security-tracker]
    agent -->|feature| feat[feature-tracker]
    agent -->|bug| bugs[bug-tracker]
    agent -->|infra| ops[ops-tracker]
```

```aw wrap
---
on:
  issues:
    types: [opened]

permissions:
  contents: read
  actions: read

tools:
  github:
    toolsets: [issues]

safe-outputs:
  github-token: ${{ secrets.GH_AW_CROSS_REPO_PAT }}
  create-issue:
    max: 2
    title-prefix: "[auto-triaged] "
---

# Triage and Route to Tracking Repos

**Original issue:** ${{ github.event.issue.html_url }}
**Content:** "${{ steps.sanitized.outputs.text }}"

Route security, feature, bug, and infrastructure issues to `myorg/security-tracker`, `myorg/feature-tracker`, `myorg/bug-tracker`, or `myorg/ops-tracker`. Include the original link, triage reasoning, priority, affected components, and SLA targets.
```

## Aggregated Reporting

```mermaid
flowchart LR
    schedule([Weekly schedule]) --> agent[Report agent]
    subgraph sources["Component repos"]
        componentAlpha[component-alpha]
        componentBeta[component-beta]
        componentN[component-N]
    end
    agent -->|query issues| componentAlpha & componentBeta & componentN
    agent -->|create-discussion| central["central-tracker\nweekly summary"]
```

```aw wrap
---
on: weekly on monday

permissions:
  contents: read

tools:
  github:
    toolsets: [issues]

safe-outputs:
  github-token: ${{ secrets.GH_AW_CROSS_REPO_PAT }}
  create-discussion:
    target-repo: "myorg/central-tracker"
    category: "Status Reports"
    title-prefix: "[weekly] "
---

# Weekly Cross-Repo Issue Summary

Summarize all component repositories by open count and priority, issues opened or closed this week, stale issues (>30 days), and blockers. Create a discussion with an executive summary, per-repo breakdown, trends, and markdown-table action items.
```

## Bidirectional Linking

```mermaid
flowchart LR
    subgraph comp["component-alpha"]
        ev([Issue opened]) --> agent[Tracking agent]
        original[original issue]
    end
    agent -->|create-issue| central["central-tracker\n[linked] ..."]
    agent -->|add-comment| original
```

```aw wrap
---
on:
  issues:
    types: [opened]

permissions:
  contents: read
  actions: read

tools:
  github:
    toolsets: [issues]

safe-outputs:
  github-token: ${{ secrets.GH_AW_CROSS_REPO_PAT }}
  create-issue:
    target-repo: "myorg/central-tracker"
    title-prefix: "[linked] "
  add-comment:
    max: 1
---

# Create Tracking Issue with Bidirectional Links

**Original issue:** ${{ github.event.issue.html_url }}

Create a tracking issue in `myorg/central-tracker` titled "[linked] ${{ github.event.issue.title }}" with a link to the original, then comment on the original with the tracking link to create a navigable audit trail.
```

## Priority-Based Routing

```mermaid
flowchart LR
    subgraph comp["component-alpha"]
        ev(["Issue opened /\nlabeled"]) --> agent[Priority router]
    end
    agent -->|P0| inc[incidents]
    agent -->|P1| p1[priority-tracker]
    agent -->|P2| central[central-tracker]
    agent -->|P3| backlog[backlog]
```

```aw wrap
---
on:
  issues:
    types: [opened, labeled]

permissions:
  contents: read
  actions: read

tools:
  github:
    toolsets: [issues]

safe-outputs:
  github-token: ${{ secrets.GH_AW_CROSS_REPO_PAT }}
  create-issue:
    max: 1
    title-prefix: "[priority-routed] "
---

# Route Issues Based on Priority

**Original issue:** ${{ github.event.issue.html_url }}
**Labels:** Check for priority labels (P0, P1, P2, P3)

Route P0 issues to `myorg/incidents`, P1 to `myorg/priority-tracker`, P2 to `myorg/central-tracker`, and P3 to `myorg/backlog`. Include the original link, priority, SLA, and escalation path; for P0, alert on-call and include the incident-response checklist.
```

## Authentication Setup

Cross-repo issue tracking requires appropriate authentication:

### PAT Configuration

```bash
# Create PAT with issues and repository read permissions
gh aw secrets set GH_AW_CROSS_REPO_PAT --value "ghp_your_token_here"
```

**Required Permissions:** `repo` for private repositories or `public_repo` for public repositories.

### GitHub App Configuration

For a scoped alternative, use a [GitHub App installation token](/gh-aw/reference/auth/#using-a-github-app-for-authentication).

## Learn More

See [MultiRepoOps](/gh-aw/patterns/multi-repo-ops/), [Feature Synchronization](/gh-aw/gallery/multi-repo/feature-sync/), [Safe Outputs](/gh-aw/reference/safe-outputs/), and [GitHub Tools](/gh-aw/reference/github-tools/) for multi-repo patterns, code sync, issue configuration, and API access.
