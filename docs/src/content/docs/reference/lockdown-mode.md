---
title: Lockdown Mode
description: Security feature that filters public repository content to only show items from users with push access, protecting workflows from unauthorized input manipulation.
sidebar:
  order: 660
---

**Lockdown mode** is [a security feature of the GitHub MCP server](https://github.com/github/github-mcp-server/blob/main/docs/server-configuration.md#lockdown-mode) that filters content in public repositories to only surface items (issues, pull requests, comments, discussions, etc.) from users with **push access** to the repository. This protects agentic workflows from processing potentially malicious or misleading content from untrusted users.

> [!TIP]
> **Automatic Protection**: Lockdown mode is **automatically enabled** for public repositories. It is also enabled whenever using a custom GitHub token `GH_AW_GITHUB_MCP_SERVER_TOKEN` indicating remote mode or multi-repo mode.
This provides secure defaults without manual configuration.

## Security Benefits

Lockdown mode protects against several attack vectors:

### Input Manipulation

Without lockdown, an attacker could:

1. Create an issue with malicious code snippets or links
2. Trigger an agentic workflow (e.g., issue triage, planning assistant)
3. Attempt to hijack the workflow through prompt-injection

**With lockdown**: Only trusted contributors' issues are visible to workflows.

### Context Poisoning

Attackers could flood public repositories with spam issues to:
- Overwhelm the AI context window with noise
- Manipulate AI decisions through volume of malicious suggestions
- Exhaust rate limits or credits

**With lockdown**: Only legitimate contributor content consumes workflow resources.

### Social Engineering

Malicious users could craft issues that:
- Impersonate maintainers
- Request sensitive information
- Trick AI into revealing secrets or internal data

**With lockdown**: Only verified contributors can interact with workflows.

## Configuration

### Automatic Mode (Recommended)

Lockdown is automatically determined based on repository visibility:

```yaml wrap
tools:
  github:
    mode: remote
    toolsets: [repos, issues, pull_requests]
    # Lockdown automatically enabled for public repos
    # Automatically disabled for private/internal repos
```

### Manual Override

Explicitly enable or disable lockdown for specific workflows:

```yaml wrap
tools:
  github:
    lockdown: true   # Force enable (use in public repos to ensure protection)
    # or
    lockdown: false  # Explicitly disable (see "When to Disable" below)
```

> [!WARNING]
> **Security Consideration**: Setting `lockdown: false` in public repositories allows workflows to process content from any GitHub user. Only use this for workflows specifically designed to handle untrusted input safely.

## When to Disable Lockdown

Some workflows are **designed** to process content from all users and include appropriate safety controls. Safe use cases for `lockdown: false` in public repositories:

### Issue Triage

Workflows that label, categorize, or route issues from all users:

```yaml wrap
---
name: Issue Triage
on:
  issues:
    types: [opened]

permissions:
  issues: write

tools:
  github:
    lockdown: false  # Process all issues
    toolsets: [issues, repos]

safe-outputs:
  add-labels:
    labels: ["needs-triage", "bug", "enhancement", "question"]
---

Analyze the issue and add appropriate labels based on content.
Only use labels from the allowed list.
```

**Safety controls**:

- Write operations restricted through `safe-outputs`
- Limited to adding specific labels (no arbitrary actions)
- No bash tools or web access

### Issue Organization

Workflows that organize issues into projects, milestones, or documentation:

```yaml wrap
---
name: Issue Organizer
on:
  issues:
    types: [labeled]
tools:
  github:
    lockdown: false
    toolsets: [issues, projects]
safe-outputs:
  add-to-project:
    max: 5
---

When an issue is labeled, add it to the appropriate project board.
```

### Issue Planning

Workflows that estimate complexity, suggest related issues, or draft implementation plans:

```yaml wrap
---
name: Issue Planning Assistant
on:
  issues:
    types: [opened, labeled]
tools:
  github:
    lockdown: false
    toolsets: [issues, repos]
safe-outputs:
  create-comment:
    max: 1
---

Analyze the issue, estimate complexity, and suggest related issues.
Post a comment with your analysis (read-only suggestions only).
```

### Daily Repository Status

Workflows that generate daily summaries or metrics including all activity:

```yaml wrap
---
name: Daily Status Report
on:
  schedule:
    - cron: "0 9 * * *"
tools:
  github:
    lockdown: false  # Include all activity in metrics
    read-only: true
---

Generate a daily activity report including:
- All new issues and PRs (from any user)
- Comment activity trends
- Community engagement metrics
```

### Spam Detection

Workflows that identify and flag spam content:

```yaml wrap
---
name: Spam Detector
on:
  issues:
    types: [opened]
  issue_comment:
    types: [created]
permissions:
  issues: write
tools:
  github:
    lockdown: false  # Must see all content to detect spam
    toolsets: [issues]
safe-outputs:
  add-labels:
    labels: ["spam", "needs-review"]
  create-comment:
    max: 1
---

Analyze the issue or comment for spam indicators.
If spam is detected, add the "spam" label and notify moderators.
```

### Command Workflows

Workflows triggered by maintainer commands in issue comments (e.g., `/plan`, `/analyze`, `/assign`):

```yaml wrap
---
name: Issue Command Handler
on:
  issue_comment:
    types: [created]
tools:
  github:
    lockdown: false  # See all comments to detect commands
    toolsets: [issues, repos]
---

If the comment starts with a command (e.g., "/plan"):
1. Verify the commenter has push access
2. Execute the requested command
3. Post results as a comment

Ignore comments from non-contributors.
```

**Safety controls**:

- Workflow explicitly checks user permissions before taking action
- Commands are restricted to specific prefixes
- Actions limited by safe-outputs configuration

## When Lockdown is Always Recommended

**Keep lockdown enabled** (default) for:

- **Code generation workflows**: Any workflow that creates pull requests or modifies code
- **Repository configuration**: Workflows that change settings, branches, or webhooks
- **Credential access**: Workflows that interact with secrets or deployment keys
- **Cross-repository operations**: Workflows that access multiple repositories
- **Web interactions**: Workflows with `web-fetch`, `playwright`, or network access
- **Bash execution**: Workflows with shell command capabilities
- **Sensitive analysis**: Workflows that process code, dependencies, or security data

## Related Documentation

- [GitHub Tokens](/gh-aw/reference/tokens/) - Token configuration and security
- [Tools](/gh-aw/reference/tools/) - GitHub tools configuration
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Write operation controls
- [Permissions](/gh-aw/reference/permissions/) - GitHub Actions permissions
- [FAQ: Lockdown Mode](/gh-aw/reference/faq/#lockdown-mode) - Common questions
- [Troubleshooting: Access Issues](/gh-aw/troubleshooting/common-issues/#lockdown-mode-blocking-expected-content) - Resolving access problems
- [GitHub MCP Server Documentation](https://github.com/github/github-mcp-server/blob/main/docs/server-configuration.md#lockdown-mode) - Upstream reference
