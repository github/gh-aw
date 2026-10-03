---
title: Using Claude Code with GitHub Agentic Workflows
description: Select and authenticate Claude Code as the AI engine for GitHub Agentic Workflows, understand its capabilities and limitations, and start from an example.
---

[Claude Code](https://claude.com/product/claude-code) is Anthropic's agentic coding interface for repository analysis and code changes. GitHub Agentic Workflows runs Claude Code in GitHub Actions, adding GitHub event triggers and guardrails.

## Selecting Claude Code as the AI engine

To select Claude Code as the AI engine, with inference hosted and and billed through an Anthropic subscription, add this to the workflow frontmatter:

```yaml
engine: claude
```

To authenticate, either

1. Provide [`ANTHROPIC_API_KEY`](/gh-aw/reference/auth/#anthropic_api_key) as a GitHub Actions repository secret, or

2. Use keyless [Anthropic Workload Identity Federation](/gh-aw/reference/auth/#anthropic-workload-identity-federation-wif).

Claude subscription OAuth tokens such as `CLAUDE_CODE_OAUTH_TOKEN` are not supported.

## Example: scheduled repository report

```aw wrap title=".github/workflows/daily-status.md"
---
on:
  schedule: daily

permissions:
  contents: read
  issues: read
  pull-requests: read

engine: claude

safe-outputs:
  create-issue:
    title-prefix: "[status] "
    labels: [report]
    close-older-issues: true
---

# Daily Repository Status

Analyze the repository and create a concise daily status report covering:
- Open issues and their priority
- Recent PR activity
- Upcoming work items
```

## Capabilities and limitations

Claude Code supports native web search, bare mode, top-level `max-turns`, per-command bash pre-approval, and custom `engine.harness` scripts. It does not support Copilot-specific `max-continuations` or native `engine.agent` selection. See the [AI engine feature comparison](/gh-aw/reference/engines/#engine-feature-comparison).

The default permission mode is `dontAsk`: actions requiring approval run only when pre-approved. Disabled Bash/web tools are removed, and `tools.edit: false` denies repository edits even with an explicit permission-mode override. Safe outputs do not independently grant file-write access. Sandbox and MCP gateway policies provide the isolation boundaries.

Bare mode skips ambient hooks, instructions, skills, and subagents. Workflow-declared skills and inline subagents are explicitly loaded through the `gh-aw-workflow` plugin; their names are plugin-qualified, such as `gh-aw-workflow:reviewer`. Top-level pinned plugins continue to load explicitly.

## Guided workflow authoring with Claude Code

Claude Code users can initialize the repository and author agentic workflows with interactive guidance — no Copilot subscription required.

### Initialize for Claude

Run `gh aw init --engine claude` to configure the repository. The `--engine claude` flag skips Copilot-specific files (MCP server configuration, Copilot dispatcher skill) and only writes the files useful for any engine: `.gitattributes`, VS Code settings, and the custom agent file.

```bash
gh aw init --engine claude
```

### Create a workflow

Start Claude Code in your repository and run:

```text wrap
Create a workflow for GitHub Agentic Workflows using https://raw.githubusercontent.com/github/gh-aw/main/create.md

The purpose of the workflow is <describe your automation goal here>.
```

The agent fetches `create.md`, installs the `gh aw` CLI if needed, guides you through trigger selection, tools, safe outputs, and permissions, then generates `.github/workflows/<name>.md` and compiles it to a `.lock.yml`.

After the files are committed, set `engine: claude` in workflow frontmatter (if not already set) and configure `ANTHROPIC_API_KEY` or Anthropic WIF.

## GitHub Agentic Workflows vs. Claude Code in Actions

Running coding agent CLIs such as `claude` directly in GitHub Actions without an adequate security architecture is not recommended. GitHub Agentic Workflows gives an appropriate security architecture and workflow portability across AI engines.

The [`anthropics/claude-code-action`](https://github.com/anthropics/claude-code-action) workflow has a security model with fewer guardrails. It is not recommended for use in GitHub Actions if GitHub Agentic Workflows are available for use.

## Learn More

- [Quick start](/gh-aw/setup/quick-start/)
- [Engine reference](/gh-aw/reference/engines/)
- [Authentication](/gh-aw/reference/auth/)
- [Security architecture](/gh-aw/introduction/architecture/)
- [Gallery](/gh-aw/gallery/)
