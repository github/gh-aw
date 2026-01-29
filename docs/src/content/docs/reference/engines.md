---
title: AI Engines
description: Complete guide to AI engines (coding agents) usable with GitHub Agentic Workflows, including Copilot and custom engines with their specific configuration options.
sidebar:
  order: 600
---

GitHub Agentic Workflows use AI [coding agents or engines](/gh-aw/reference/glossary/#engine) to interpret and execute natural language instructions. Each engine has unique capabilities and configuration options.

## GitHub Copilot CLI

[GitHub Copilot CLI](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/use-copilot-cli) is the default and recommended AI [coding agent engine](/gh-aw/reference/glossary/#engine).

## GitHub Copilot CLI Setup

GitHub Copilot CLI is the default engine. You can also request the use of of the GitHub Copilot CLI engine in your workflow frontmatter:

```yaml wrap
engine: copilot
```

or use extended configuration:

```yaml wrap
engine:
  id: copilot
  version: latest                       # defaults to latest
  model: gpt-5                          # defaults to claude-sonnet-4
  args: ["--add-dir", "/workspace"]     # custom CLI arguments
```

Configuration options: `model` (gpt-5 or claude-sonnet-4), `version` (CLI version), `args` (command-line arguments). Alternatively set model via `COPILOT_MODEL` environment variable.

Create a fine-grained PAT at <https://github.com/settings/personal-access-tokens/new>. Select your user account (not an organization), choose "Public repositories" access, and enable "Copilot Requests" permissions. Then add it to your repository:

> [!NOTE]
> You should leave "Public repositories" enabled; otherwise, you will not have access to the Copilot Requests permission option.

```bash wrap
gh aw secrets set COPILOT_GITHUB_TOKEN --value "<your-github-pat>"
```

### Required Secrets

**`COPILOT_GITHUB_TOKEN`**: GitHub [Personal Access Token](/gh-aw/reference/glossary/#personal-access-token-pat) (PAT, a token that authenticates you to GitHub's APIs) with "Copilot Requests" permission. **`GH_AW_GITHUB_TOKEN`** (optional): Required for [GitHub Tools Remote Mode](/gh-aw/reference/tools/#modes-and-restrictions).

For more information about GitHub Copilot CLI authentication, see the [official documentation](https://github.com/github/copilot-cli?tab=readme-ov-file#authenticate-with-a-personal-access-token-pat).

> [!NOTE]
> The Copilot engine does not have built-in `web-search` support. You can add web search capabilities using third-party MCP servers. See the [Using Web Search](/gh-aw/guides/web-search/) for available options and setup instructions.

For GitHub Tools Remote Mode, also configure:

```bash wrap
gh aw secrets set GH_AW_GITHUB_MCP_SERVER_TOKEN --value "<your-github-pat>"
```

## Anthropic Claude

[Anthropic Claude Code](https://www.anthropic.com/index/claude) is an AI engine option that provides full MCP tool support and allow-listing capabilities.

### Claude Setup

Request the use of the Claude engine in your workflow frontmatter:

```yaml wrap
engine: claude
```

Extended configuration is also supported.

Create an Anthropic API key at <https://console.anthropic.com/api-keys> and add it to your repository:

```bash wrap
gh aw secrets set ANTHROPIC_API_KEY --value "<your-anthropic-api-key>"
```

### Quick Example with Claude

Here's a minimal workflow that uses Claude to analyze GitHub issues:

**File**: `.github/workflows/issue-analyzer.md`

```yaml wrap
---
engine: claude
on: 
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
safe-outputs:
  add-comment:
---

# Issue Analysis

Analyze this issue and provide:
1. Summary of the problem
2. Suggested labels
3. Any immediate concerns
```

**Setup:**

1. Get your API key from [Anthropic Console](https://console.anthropic.com/api-keys)
2. Set the secret:
   ```bash wrap
   gh aw secrets set ANTHROPIC_API_KEY --value "<your-anthropic-api-key>"
   ```
3. Compile and run:
   ```bash wrap
   gh aw compile issue-analyzer.md
   git add .github/workflows/issue-analyzer.lock.yml
   git commit -m "Add issue analyzer workflow"
   git push
   ```

**What it does:**
- Triggers on new issues
- Claude analyzes the issue content
- Posts a comment with analysis
- Uses same safe-outputs system as all engines

## OpenAI Codex

[OpenAI Codex](https://openai.com/blog/openai-codex) is a coding agent engine option.

### Codex Setup

Request the use of the Codex engine in your workflow frontmatter:

```yaml wrap
engine: codex
```

Extended configuration is also supported.

Create an OpenAI API key at <https://platform.openai.com/account/api-keys> and add it to your repository:

```bash wrap
gh aw secrets set OPENAI_API_KEY --value "<your-openai-api-key>"
```

## Engine Environment Variables

All engines support custom environment variables through the `env` field:

```yaml wrap
engine:
  id: copilot
  env:
    DEBUG_MODE: "true"
    AWS_REGION: us-west-2
    CUSTOM_API_ENDPOINT: https://api.example.com
```

Environment variables can also be defined at workflow, job, step, and other scopes. See [Environment Variables](/gh-aw/reference/environment-variables/) for complete documentation on precedence and all 13 env scopes.

## Engine Command-Line Arguments

All engines support custom command-line arguments through the `args` field, injected before the prompt:

```yaml wrap
engine:
  id: copilot
  args: ["--add-dir", "/workspace", "--verbose"]
```

Arguments are added in order and placed before the `--prompt` flag. Common uses include adding directories (`--add-dir`), enabling verbose logging (`--verbose`, `--debug`), and passing engine-specific flags. Consult the specific engine's CLI documentation for available flags.

## Related Documentation

- [Frontmatter](/gh-aw/reference/frontmatter/) - Complete configuration reference
- [Tools](/gh-aw/reference/tools/) - Available tools and MCP servers
- [Security Guide](/gh-aw/guides/security/) - Security considerations for AI engines
- [MCPs](/gh-aw/guides/mcps/) - Model Context Protocol setup and configuration
