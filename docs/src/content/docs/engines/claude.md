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

Bare mode skips ambient discovery and restricts native tools to Bash, Edit, and Read in Claude Code 2.1.288. Workflow-declared skills load through the `gh-aw-workflow` plugin and can be explicitly invoked as `/gh-aw-workflow:<skill-name>`. Top-level pinned plugins continue to load explicitly. Do not rely on automatic Skill-tool invocation or subagent delegation in bare mode; use `bare: false` when the workflow needs those capabilities.

Claude Code [dynamic workflows](https://code.claude.com/docs/en/workflows) can run in the default headless (`claude -p`) mode when the prompt explicitly requests a saved workflow. The interactive keyword trigger does not work in headless mode. gh-aw pre-approves the `Workflow` tool by default, while the agents it launches remain subject to the configured tool permissions. Set `engine.dynamic-workflows: false` to disable `Workflow` tool approval and restore engine configuration only when PR checkout requires it. Save project scripts and any files their agents read under `.claude/workflows/`: the existing engine-config restoration snapshots the engine-declared `.claude/` directory recursively from the activation checkout and restores it from the activation artifact after the agent checkout, including when the PR checkout is disabled. For supporting files elsewhere in the repository, use `ambient-folders` to include them in the activation artifact. Dynamic workflows require ambient discovery; do not enable `engine.bare: true` for them.

The built-in harness passes a short continuation prompt when resuming an interrupted session, preserving its session ID and prior work. It uses `CLAUDE_CODE_MAX_RETRIES: 0` to leave transient-error retries to the harness; `ANTHROPIC_MAX_RETRIES` does not control the Claude CLI retry loop.

The repository's [`smoke-claude-dynamic` workflow](https://github.com/github/gh-aw/blob/main/.github/workflows/smoke-claude-dynamic.md) is a minimal end-to-end example. It explicitly enables dynamic workflows with `bare: false`, invokes a saved script with structured arguments, and verifies that the script and a nested hidden fixture match the trusted activation artifact. Its post-step checks native `Workflow` tool evidence and the returned result instead of accepting an agent's success claim.

For an offline native-CLI compatibility check against an installed version, run the opt-in contract suite from `actions/setup/js`:

```bash
GH_AW_CLAUDE_TEST_CLI=/absolute/path/to/claude npm run test:js -- claude_cli_live.test.cjs
```

The suite uses synthetic prompts, an isolated home directory, and a loopback Anthropic-compatible server. It does not use real credentials or external inference.

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
