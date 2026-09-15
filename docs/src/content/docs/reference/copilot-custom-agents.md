---
title: Importing Copilot Agent Files
description: Import and reuse Copilot agent files with GitHub Agentic Workflows
sidebar:
  order: 650
---

GitHub Copilot custom agents are Markdown prompt files stored in `.github/agents/` and imported with `imports`. Copilot supports these files natively; other engines such as Claude and Codex receive the Markdown body as prompt text.

A typical agent file looks like this:

```markdown title=".github/agents/my-agent.md"
---
name: My Copilot Agent
description: Specialized prompt for code review tasks
---

# Agent Instructions

You are a specialized code review agent. Focus on:
- Code quality and best practices
- Security vulnerabilities
- Performance optimization
```

## Using Copilot Agent Files from Agentic Workflows

Use `imports` to load an agent file from your repository or from another repository.

### Local Agent File Import

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

### Remote Agent File Import

Import an agent file from another repository with the `owner/repo/path@ref` format:

```yaml wrap
---
on: pull_request
engine: copilot
imports:
  - acme-org/shared-agents/.github/agents/code-reviewer.md@v1.0.0
---

Perform comprehensive code review using shared agent instructions.
```

The imported instructions are merged into the workflow prompt.

## Agent File Requirements

Agent files must live in `.github/agents/`, use Markdown with YAML frontmatter, and may define fields such as `name`, `description`, `tools`, and `mcp-servers`. Remote imports are cached by commit SHA in `.github/aw/imports/`.

## Copilot Agent File Collections

Organizations can keep shared agent files in a dedicated repository:

```text
acme-org/ai-agents/
└── .github/
    └── agents/
        ├── code-reviewer.md         # General code review
        ├── security-auditor.md      # Security-focused analysis
        ├── performance-analyst.md   # Performance optimization
        ├── accessibility-checker.md # WCAG compliance
        └── documentation-writer.md  # Technical documentation
```

Teams can then import the agent that fits the workflow:

```yaml wrap title="Security-focused PR review"
---
on: pull_request
engine: copilot
imports:
  - acme-org/ai-agents/.github/agents/security-auditor.md@v2.0.0
---

# Security Review

Perform comprehensive security review of this pull request.
```

## Combining Copilot Agent Files with Other Imports

Agent files can be combined with shared tools, MCP servers, and policy imports:

```yaml wrap
---
on: pull_request
engine: copilot
imports:
  - acme-org/ai-agents/.github/agents/security-auditor.md@v2.0.0
  - acme-org/workflow-library/shared/tools/github-standard.md@v1.0.0
  - acme-org/workflow-library/shared/mcp/database.md@v1.0.0
  - acme-org/workflow-library/shared/config/security-policies.md@v1.0.0
permissions:
  contents: read
safe-outputs:
  create-pull-request-review-comment:
    max: 10
---

# Comprehensive Security Review

Perform detailed security analysis using specialized agent files and tools.
```

## Defining Copilot Sub-agents Inline

Instead of (or alongside) importing agent files from `.github/agents/`, you can define agents directly inside the workflow markdown. See [Inline Sub-Agents](/gh-aw/reference/inline-sub-agents/) for the complete syntax reference, including name constraints and frontmatter fields.

## Learn More

- [Imports Reference](/gh-aw/reference/imports/) - Complete import system documentation
- [Inline Sub-Agents](/gh-aw/reference/inline-sub-agents/) - Defining Copilot sub-agents inside a workflow file
- [Adding Existing Workflows](/gh-aw/guides/working-with-workflows/#adding-existing-workflows) - Adding workflows from other repositories
- [Frontmatter](/gh-aw/reference/frontmatter/) - Configuration options reference
