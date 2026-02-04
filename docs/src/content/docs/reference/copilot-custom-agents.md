---
title: Reusing Copilot Custom Agents
description: Create and reuse specialized AI agents with custom instructions and behavior for GitHub Agentic Workflows
sidebar:
  order: 650
---

"Custom Agents" is a term used in GitHub Copilot for specialized prompts for behaviors for specific tasks. They are markdown documents stored in the `.github/agents/` directory and imported via the `imports` field. Copilot supports agents natively, while other engines (Claude, Codex) inject the markdown body as a prompt.

## Creating a Copilot Custom Agent

Create a markdown file in `.github/agents/` with agent-specific instructions:

```markdown title=".github/agents/my-agent.md"
---
name: My Copilot Custom Agent
description: Specialized prompt for code review tasks
---

# Agent Instructions

You are a specialized code review agent. Focus on:
- Code quality and best practices
- Security vulnerabilities
- Performance optimization
```

## Using Copilot Custom Agents from Agentic Workflows

Import agent files in your workflow using the `imports` field. Agents can be imported from local `.github/agents/` directories or from external repositories.

### Local Agent Import

Import an agent from your repository:

```yaml wrap
---
on: pull_request
engine: copilot
imports:
  - .github/agents/my-agent.md
---

Review the pull request and provide feedback.
```

### Remote Agent Import

Import an agent from an external repository using the `owner/repo/path@ref` format:

```yaml wrap
---
on: pull_request
engine: copilot
imports:
  - acme-org/shared-agents/.github/agents/code-reviewer.md@v1.0.0
---

Perform comprehensive code review using shared agent instructions.
```

Remote agent imports support versioning:

- **Semantic tags**: `@v1.0.0` (recommended for production)
- **Branch names**: `@main`, `@develop` (for development)
- **Commit SHAs**: `@abc123def` (for immutable references)

The agent instructions are merged with the workflow prompt, customizing the AI engine's behavior for specific tasks.

## Agent File Requirements

- **Location**: Must be in a `.github/agents/` directory (local or remote repository)
- **Format**: Markdown with YAML frontmatter
- **Frontmatter**: Can include `name`, `description`, `tools`, and `mcp-servers`
- **One per workflow**: Only one agent file can be imported per workflow
- **Caching**: Remote agents are cached by commit SHA in `.github/aw/imports/`

## The Copilot Custom Agent for Agentic Workflows

The [Copilot Custom Agent for Agentic Workflows](/gh-aw/reference/custom-agent-for-aw/) (`agentic-workflows.agent.md`) is a specialized custom agent designed to assist with creating, updating, importing, and debugging agentic workflows. It provides tailored instructions and behaviors to streamline workflow management tasks.