# GitHub Agentic Workflows

Write agentic workflows in natural language markdown, and run them in GitHub Actions.

## Contents

- [Quick Start](#quick-start)
- [Overview](#overview)
- [How It Works](#how-it-works)
- [Safe Agentic Workflows](#safe-agentic-workflows)
- [Documentation](#documentation)
- [Contributing](#contributing)
- [Share Feedback](#share-feedback)
- [Peli's Agent Factory](#pelis-agent-factory)
- [Related Projects](#related-projects)

## Quick Start

Ready to get your first agentic workflow running? Follow our step-by-step [Quick Start Guide](https://github.github.io/gh-aw/setup/quick-start/) to install the extension, add a sample workflow, and see it in action.

## Overview

Learn about the concepts behind agentic workflows, explore available workflow types, and understand how AI can automate your repository tasks. See [How It Works](https://github.github.io/gh-aw/introduction/how-it-works/).

## How It Works

GitHub Agentic Workflows transforms natural language markdown files into GitHub Actions that are executed by AI agents. Here's an example:

```markdown
---
on:
  schedule: daily
permissions:
  contents: read
  issues: read
  pull-requests: read
safe-outputs:
  create-issue:
    title-prefix: "[team-status] "
    labels: [report, daily-status]
    close-older-issues: true
---

## Daily Issues Report

Create an upbeat daily status report for the team as a GitHub issue.
```

The `gh aw` cli converts this into a GitHub Actions Workflow (.yml) that runs an AI agent (Copilot, Claude, Codex, ...) in a containerized environment on a schedule or manually.

The AI agent reads your repository context, analyzes issues, generates visualizations, and creates reports - all defined in natural language rather than complex code.

## Safe Agentic Workflows

Security is foundational to GitHub Agentic Workflows. Workflows run with read-only permissions by default, with write operations only allowed through sanitized `safe-outputs`. The system implements multiple layers of protection including sandboxed execution, input sanitization, network isolation, supply chain security (SHA-pinned dependencies), tool allow-listing, and compile-time validation. Access can be gated to team members only, with human approval gates for critical operations, ensuring AI agents operate safely within controlled boundaries. See the [Security Guide](https://github.github.io/gh-aw/guides/security/) for comprehensive details on threat modeling, implementation guidelines, and best practices.

> [!WARNING]
> Using agentic workflows in your repository requires careful attention to security considerations and careful human supervision, and even then things can still go wrong. Use it with caution, and at your own risk.

## Documentation

For complete documentation, examples, and guides, see the [Documentation](https://github.github.io/gh-aw/).

## Contributing

We welcome contributions to GitHub Agentic Workflows! Here's how you can help:

- **Report bugs and request features** by filing issues in this repository
- **Improve documentation** by contributing to our docs
- **Contribute code** by following our [Development Guide](DEVGUIDE.md)
  - **Quick Start**: See [Common Development Tasks](DEVGUIDE.md#common-development-tasks) for scenario-based command reference
- **Share ideas** in the `#continuous-ai` channel in the [GitHub Next Discord](https://gh.io/next-discord)

For development setup and contribution guidelines, see [CONTRIBUTING.md](CONTRIBUTING.md).

## Share Feedback

We welcome your feedback on GitHub Agentic Workflows! Please file bugs and feature requests as issues in this repository,
and share your thoughts in the `#continuous-ai` channel in the [GitHub Next Discord](https://gh.io/next-discord).

## Peli's Agent Factory

See the [Peli's Agent Factory](https://github.github.io/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/) for a guided tour through many uses of agentic workflows.

## Related Projects

GitHub Agentic Workflows is supported by companion projects that provide additional security and integration capabilities:

- **[Agent Workflow Firewall (AWF)](https://github.com/github/gh-aw-firewall)** - Network egress control for AI agents, providing domain-based access controls and activity logging for secure workflow execution
- **[MCP Gateway](https://github.com/github/gh-aw-mcpg)** - Routes Model Context Protocol (MCP) server calls through a unified HTTP gateway for centralized access management
- **[The Agentics](https://github.com/githubnext/agentics)** - A collection of reusable agentic workflow components, tools, and templates to accelerate workflow development

---

> [!TIP]
> **For AI Agents**: To learn about GitHub Agentic Workflows syntax, file formats, tools, and best practices, please read the comprehensive instructions at [.github/aw/github-agentic-workflows.md](.github/aw/github-agentic-workflows.md)
>
> **Repository Configuration**: To configure a repository for GitHub Agentic Workflows, follow the interactive setup wizard at [install.md](install.md)
>
> **Custom Agent**: Use the custom agent at [.github/agents/agentic-workflows.agent.md](.github/agents/agentic-workflows.agent.md) to interactively create agentic workflows.
