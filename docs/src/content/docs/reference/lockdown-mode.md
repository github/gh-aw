---
title: GitHub Lockdown Mode
description: Security feature of GitHub that filters public repository content to only show items from users with push access, protecting workflows from unauthorized input manipulation.
sidebar:
  order: 660
---

**GitHub lockdown mode** is [a security feature of the GitHub MCP server](https://github.com/github/github-mcp-server/blob/main/docs/server-configuration.md#lockdown-mode) that filters content in public repositories to only surface items (issues, pull requests, comments, discussions, etc.) from users with **push access** to the repository. This protects agentic workflows from processing potentially malicious or misleading content from untrusted users.

> [!TIP]
> **Enabling Lockdown Mode**: Set `lockdown: true` in your workflow frontmatter and configure [`GH_AW_GITHUB_TOKEN`](/gh-aw/reference/auth/#gh_aw_github_token) as a repository secret. For public repositories, this provides enhanced security against untrusted input.

## Security Benefits

GitHub lockdown mode protects against several attack vectors:

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

### Enabling Lockdown Mode

To enable lockdown mode for your workflow:

1. **Set `lockdown: true` in your workflow frontmatter**
2. **Configure `GH_AW_GITHUB_TOKEN` as a repository secret** (see [Authentication](/gh-aw/reference/auth/#gh_aw_github_token))

```yaml wrap
---
engine: copilot

tools:
  github:
    lockdown: true
    mode: remote
    toolsets: [repos, issues, pull_requests]
---

# Your workflow that requires lockdown protection
```

```bash
# Configure the required token
gh aw secrets set GH_AW_GITHUB_TOKEN --value "YOUR_FINE_GRAINED_PAT"
```

**Requirements**:

- `GH_AW_GITHUB_TOKEN` must be configured as a repository secret
- The token requires appropriate permissions (Contents: Read, Issues: Read, Pull requests: Read)
- Without `GH_AW_GITHUB_TOKEN` or a similar token, workflows with `lockdown: true` will fail at runtime

### Disabling Lockdown Mode

Explicitly disable lockdown for workflows designed to process content from all users:

```yaml wrap
tools:
  github:
    lockdown: false  # Explicitly disable (see "When to Disable" below)
```

> [!WARNING]
> **Security Consideration**: Setting `lockdown: false` in public repositories allows workflows to process content from any GitHub user. Only use this for workflows specifically designed to handle untrusted input safely.

## When to Disable Lockdown

If working in a public repository, it is recommended that you use an explicit `lockdown: true` or `lockdown: false`.

Some workflows are **designed** to process content from all users and include appropriate safety controls. Safe use cases for `lockdown: false` in public repositories:

- **Issue Triage**: Workflows that label, categorize, or route issues from all users
- **Issue Organization**: Workflows that add issues to projects or milestones based on labels or content
- **Issue Planning**: Workflows that estimate complexity, suggest related issues, or draft implementation plans based on issue content
- **Spam Detection**: Workflows that identify and flag spam issues or comments
- **Public Dashboards**: Workflows that generate public reports or metrics based on all repository activity
- **Command Workflows**: Workflows that respond to specific commands in issue comments (e.g., `/plan`, `/analyze`) and verify user permissions before taking action

## Related Documentation

- [Authentication](/gh-aw/reference/auth/) - Token configuration and security
- [Tools](/gh-aw/reference/tools/) - GitHub tools configuration
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Write operation controls
- [Permissions](/gh-aw/reference/permissions/) - GitHub Actions permissions
- [FAQ: Lockdown Mode](/gh-aw/reference/faq/#what-is-github-lockdown-mode-and-when-is-it-enabled) - Common questions
- [Troubleshooting: Access Issues](/gh-aw/troubleshooting/common-issues/#github-lockdown-mode-blocking-expected-content) - Resolving access problems
- [GitHub MCP Server Documentation](https://github.com/github/github-mcp-server/blob/main/docs/server-configuration.md#lockdown-mode) - Upstream reference
