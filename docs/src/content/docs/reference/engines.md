---
title: AI Engines
description: Complete guide to AI engines (coding agents) usable with GitHub Agentic Workflows, including Copilot and custom engines with their specific configuration options.
sidebar:
  order: 600
---

GitHub Agentic Workflows use [AI Engines](/gh-aw/reference/glossary/#engine) (normally a coding agent) to interpret and execute natural language instructions. Each coding agent has unique capabilities and configuration options.

## Using Copilot CLI

[GitHub Copilot CLI](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/use-copilot-cli) is the default AI engine.

To use Copilot CLI with GitHub Agentic Workflows:

1. Copilot CLI is the default. You can optionally request the use of of the Copilot CLI in your workflow frontmatter:

   ```yaml wrap
   engine: copilot
   ```

2. Configure `COPILOT_GITHUB_TOKEN` repository secret

   You need a GitHub Personal Access Token (PAT) with the `copilot-requests` scope to authenticate Copilot CLI. Create a fine-grained PAT at <https://github.com/settings/personal-access-tokens/new>.

   - **IMPORTANT:** Select your user account, NOT an organization
   - **IMPORTANT:** Choose "Public repositories" access, even if adding to a private repo. Yes that's right just do it
   - **IMPORTANT:** Enable "Copilot Requests" permissions.

   You **must** have "Public repositories" selected; otherwise, you will not have access to the Copilot Requests permission option.

3. Add it to your repository:

   ```bash wrap
   gh aw secrets set COPILOT_GITHUB_TOKEN --value "<your-github-pat>"
   ```

## Using Claude by Anthropic (Claude Code)

[Claude by Anthropic](https://www.anthropic.com/index/claude) (aka Claude Code) is an AI engine option that provides full MCP tool support and allow-listing capabilities.

1. Request the use of the Claude by Anthropic engine in your workflow frontmatter:

   ```yaml wrap
   engine: claude
   ```

2. Configuring `ANTHROPIC_API_KEY`

   Create an Anthropic API key at <https://console.anthropic.com/api-keys> and add it to your repository:

   ```bash wrap
   gh aw secrets set ANTHROPIC_API_KEY --value "<your-anthropic-api-key>"
   ```

## Using OpenAI Codex

[OpenAI Codex](https://openai.com/blog/openai-codex) is a coding agent engine option.

1. Request the use of the Codex engine in your workflow frontmatter:

   ```yaml wrap
   engine: codex
   ```

2. Create an OpenAI API key at <https://platform.openai.com/account/api-keys> and add it to your repository:

   ```bash wrap
   gh aw secrets set OPENAI_API_KEY --value "<your-openai-api-key>"
   ```

## Extended Coding Agent Configuration

Workflows can specify extended configuration for the coding agent:

```yaml wrap
engine:
  id: copilot
  version: latest                       # defaults to latest
  model: gpt-5                          # defaults to claude-sonnet-4
  args: ["--add-dir", "/workspace"]     # custom CLI arguments
  agent: agent-id                       # custom agent file identifier
```

### Copilot Custom Configuration

For the Copilot engine, you can specify a specialized prompt to be used whenever the coding agent is invoked. This is called a "custom agent" in Copilot vocabulary. You specify this using the `agent` field. This references a file located in the `.github/agents/` directory:

```yaml wrap
engine:
  id: copilot
  agent: technical-doc-writer
```

The `agent` field value should match the agent file name without the `.agent.md` extension. For example, `agent: technical-doc-writer` references `.github/agents/technical-doc-writer.agent.md`.

See [Copilot Custom Agents](/gh-aw/reference/copilot-custom-agents/) for details on creating and configuring custom agents.

### Engine Environment Variables

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

### Engine Command-Line Arguments

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
- [Security Guide](/gh-aw/introduction/architecture/) - Security considerations for AI engines
- [MCPs](/gh-aw/guides/mcps/) - Model Context Protocol setup and configuration
