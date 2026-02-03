---
title: Frequently Asked Questions
description: Answers to common questions about GitHub Agentic Workflows, including security, costs, privacy, and configuration.
sidebar:
  order: 50
---

> [!NOTE]
> GitHub Agentic Workflows is in early development and may change significantly. Using automated agentic workflows requires careful attention to security considerations and careful human supervision, and even then things can still go wrong. Use it with caution, and at your own risk.

## Capabilities

### Can I edit workflows directly on GitHub.com without recompiling?

Yes! The **markdown body** (AI instructions) is loaded at runtime and can be edited directly on GitHub.com or in any editor. Changes take effect on the next workflow run without recompilation.

However, **frontmatter configuration** (tools, permissions, triggers, network rules) is embedded in the compiled workflow and requires recompilation when changed. Run `gh aw compile my-workflow` after editing frontmatter.

See [Editing Workflows](/gh-aw/guides/editing-workflows/) for complete guidance on when recompilation is needed.

### What's the difference between agentic workflows and regular GitHub Actions workflows?

Agentic workflows are a special type of GitHub Actions workflow that use AI agentic processing to interpret natural language instructions and make decisions. Key differences include

- **Natural language prompts**: You write instructions in plain markdown instead of complex YAML
- **AI-powered reasoning**: The workflow uses an AI engine (coding agent) to understand context and adapt to situations
- **Tool usage**: The AI can call pre-approved tools to perform tasks like creating issues, analyzing code, and generating content
- **Safety controls**: Built-in security features like read-only default permissions, safe outputs, and sandboxed execution ensure safe operation

### What's the difference between agentic workflows and just running a coding agent in GitHub Actions?

Agentic workflows provide a structured framework for building AI-powered automations within GitHub Actions. While you could run just install and run a coding agent directly in a standard GitHub Actions workflow, agentic workflows offer:

- **Simpler format**: Write in markdown with natural language prompts
- **Built-in security**: Read-only defaults, safe outputs, and sandboxing
- **Tool integration**: Pre-defined tools for common GitHub operations
- **Engine independence**: Easily switch between supported AI engines

### Can agentic workflows write code and create pull requests?

Yes! Agentic workflows can create pull requests using the `create-pull-request` safe output. This allows the workflow to propose code changes, documentation updates, or other modifications as pull requests for human review and merging.

Some organizations may completely disable the creation of pull requests from GitHub Actions. In such cases, workflows can still generate diffs or suggestions in issues or comments for manual application.

### Can agentic workflows do more than code?

Yes! Agentic workflows can perform a wide variety of tasks beyond writing code:

- **Analyze repositories**: Review issues, PRs, discussions, and commit history
- **Generate reports**: Create status reports, metrics summaries, and documentation
- **Triage issues**: Classify, label, and route issues to appropriate teams
- **Research**: Gather information from repository content, web sources, and APIs
- **Create content**: Write documentation, release notes, and issue descriptions
- **Coordinate work**: Track progress, create follow-up issues, and manage projects

The AI interprets natural language instructions and uses available [tools](/gh-aw/reference/tools/) to accomplish tasks.

### Can agentic workflows mix regular GitHub Actions steps with AI agentic steps?

Yes! Agentic workflows can include both AI agentic steps and traditional GitHub Actions steps. You can add custom steps before the agentic job using the [`steps:` configuration](/gh-aw/reference/frontmatter/#custom-steps-steps). Additionally, [custom safe output jobs](/gh-aw/reference/safe-outputs/#custom-safe-output-jobs-jobs) can be used as consumers of agentic outputs. [Safe inputs](/gh-aw/reference/safe-inputs/) allow you to pass data between traditional steps and the AI agent with added checking.

### Can agentic workflows read other repositories?

Not by default, but yes with proper configuration. Cross-repository access requires:

1. A **Personal Access Token (PAT)** with access to target repositories
2. Configuring the token in your workflow

See [MultiRepoOps](/gh-aw/guides/multirepoops/) for coordinating across repositories, or [SideRepoOps](/gh-aw/guides/siderepoops/) for running workflows from a separate repository.

### Can I use agentic workflows in private repositories?

Yes, and in many cases we recommend it. Private repositories are ideal for:

- Operating over proprietary source code
- Creating a "sidecar" repository with limited access for automation
- Testing workflows before deploying to public repositories
- Organization-internal automation

See [SideRepoOps](/gh-aw/guides/siderepoops/) for patterns using private repositories for automation.

## Security & Privacy

### Agentic workflows run in GitHub Actions. Can they access my repository secrets?

Repository secrets are not available to the agentic step by default. The AI agent runs with read-only permissions and cannot directly access your repository secrets unless explicitly configured. However, you should still:

- Review workflows carefully before installation
- Follow all existing [GitHub Actions security guidelines](https://docs.github.com/en/actions/reference/security/secure-use)
- Use least-privilege permissions
- Inspect the compiled `.lock.yml` file to understand actual permissions

See the [Security Guide](/gh-aw/guides/security/) guide for details.

Some MCP tools may be configured using secrets (for example, an MCP tool that can send a Slack message), but these are only accessible to the specific tool steps, not the AI agent itself. However care should be taken to minimize the use of tools equipped with highly privileged secrets.

### Agentic workflows run in GitHub Actions. Can they write to the repository?

By default, the agentic "coding agent" step of agentic workflows runs with read-only permissions. Write operations require explicit approval through [safe outputs](/gh-aw/reference/safe-outputs/) or explicit general `write` permissions (not recommended). This ensures that AI agents cannot make arbitrary changes to your repository.

If safe outputs are configured, the workflow has limited, highly specific write operations that are then sanitized and executed securely.

### What sanitization is done on AI outputs before applying changes?

All safe outputs from the AI agent are sanitized before being applied to your repository. Sanitization includes secret redaction, URL domain filtering, XML escaping, size limits, control character stripping, GitHub reference escaping and HTTPS enforcement.

Additionally, safe outputs enforce permission separation—write operations happen in separate jobs with scoped permissions, never in the agentic job itself.

See [Safe Outputs - Security and Sanitization](/gh-aw/reference/safe-outputs/#security-and-sanitization) for configuration options.

### Tell me more about security

Security is foundational to the design. Agentic workflows implement defense-in-depth through multiple layers:

- **Compilation-time validation**: Schema validation, expression safety checks, and action SHA pinning
- **Runtime isolation**: Agents run in sandboxed containers with network egress controls
- **Permission separation**: Read-only default permissions with [safe outputs](/gh-aw/reference/safe-outputs/) for write operations
- **Tool allowlisting**: Explicit control over which tools the AI can access
- **Output sanitization**: Content validation before applying changes

For documentation, see the [Security Architecture](/gh-aw/introduction/architecture/) and [Security Guide](/gh-aw/guides/security/).

### How is my code and data processed?

By default, your workflow is processed using your nominated [AI engine](/gh-aw/reference/engines/) (coding agent) and the tool calls it makes. When using the default **GitHub Copilot CLI**, the workflow is processed by the `copilot` CLI tool which uses GitHub Copilot's services and related AI models. The specifics depend on your engine choice:

- **GitHub Copilot CLI**: See [GitHub Copilot documentation](https://docs.github.com/en/copilot) for details.
- **Claude/Codex**: Uses respective providers' APIs with their data handling policies.

See the [Security Architecture](/gh-aw/introduction/architecture/) for details on the data flow.

### Does the underlying AI engine run in a sandbox?

Yes, the [AI engine](/gh-aw/reference/engines/) (coding agent) runs in a containerized sandbox environment by default. The sandbox provides:

- **Network egress control**: Domain-based access restrictions via the [Agent Workflow Firewall (AWF)](/gh-aw/reference/sandbox/)
- **Container isolation**: The agent process runs in an isolated container
- **Resource constraints**: CPU and memory limits of GitHub Actions
- **Limited filesystem access**: Only workspace and temporary directories are accessible

The sandbox container itself runs inside a GitHub Actions VM, providing an additional layer of isolation. See [Sandbox Configuration](/gh-aw/reference/sandbox/) for configuration options.

### Can an agentic workflow use outbound network requests?

Yes, but network access is restricted by the [Agent Workflow Firewall](/gh-aw/reference/sandbox/). You must explicitly declare which domains the workflow can access:

```yaml wrap
network:
  allowed:
    - defaults             # Basic infrastructure
    - python               # Python/PyPI ecosystem
    - "api.example.com"    # Custom domain
```

See [Network Permissions](/gh-aw/reference/network/) for complete configuration options.

## Costs & Usage

### Who pays for the use of AI?

This depends on the AI engine (coding agent) you use:

- **GitHub Copilot CLI** (default): Usage is currently associated with the individual GitHub account of the user supplying the COPILOT_GITHUB_TOKEN, and is drawn from the monthly quota of premium requests for that account. See [GitHub Copilot billing](https://docs.github.com/en/copilot/about-github-copilot/subscription-plans-for-github-copilot).
- **Claude**: Usage is billed to the Anthropic account associated with ANTHROPIC_API_KEY Actions secret in the repository.
- **Codex**: Usage is billed to your OpenAI account associated with OPENAI_API_KEY Actions secret in the repository.

### What's the approximate cost per workflow run?

Costs vary depending on workflow complexity, AI model used, and execution time. When using GitHub Copilot CLI, 1 or 2 premium requests are used per workflow execution that includes agentic processing. To track usage:

- Use `gh aw logs` to analyze workflow runs and metrics
- Use `gh aw audit <run-id>` to see detailed token usage and estimated costs
- Check your AI provider's usage portal for account-level tracking

Consider creating a separate PAT/API key for each repository to help track usage across projects.

Costs can be reduced by optimizing prompts, using smaller models where appropriate, limiting unnecessary tool calls, reducing frequency of runs, and caching results.

### Can I change the model being used, e.g., use a cheaper or more advanced one?

Yes! You can configure the model in your workflow frontmatter:

```yaml wrap
engine:
  id: copilot
  model: gpt-5                    # or claude-sonnet-4
```

Or switch to a different engine entirely:

```yaml wrap
engine: claude
```

See [AI Engines](/gh-aw/reference/engines/) for all configuration options.

## Configuration & Setup

### Why do I need a token or key?

When using **GitHub Copilot CLI**, a Personal Access Token (PAT) with "Copilot Requests" permission is needed to authenticate and associate the automation work with your GitHub account. This ensures:

- Usage is tracked against your Copilot subscription
- The AI has appropriate permissions for requested operations
- Actions are auditable and attributable

In the future, this restriction may be lifted to allow organization-level association. See [GitHub Tokens](/gh-aw/reference/tokens/) for complete token documentation.

### What hidden runtime dependencies does this have?

The executing agentic workflow uses:

- Your nominated **coding agent** (defaulting to GitHub Copilot CLI)
- A **GitHub Actions VM** with NodeJS
- A small library of **pinned Actions** distributed from releases of [github/gh-aw](https://github.com/github/gh-aw)
- An **Agent Workflow Firewall** container for network control (optional but default)

The exact YAML workflow executed can always be inspected in the compiled `.lock.yml` file—there's no hidden configuration.

### I'm not using a supported AI Engine (coding agent). What should I do?

If you want to use a coding agent that isn't currently supported (Copilot, Claude, or Codex), you can:

1. **Use the custom engine**: Define your own GitHub Actions steps without AI interpretation
2. **Contribute support**: Work with the coding agent's makers or community to contribute to the [gh-aw repository](https://github.com/github/gh-aw)
3. **Request support**: Open an issue describing your use case

See [AI Engines](/gh-aw/reference/engines/) for current options.

## Workflow Design

### Should I focus on one workflow, or write many different ones?

This depends on your style and goals:

**One workflow approach**:

- Simpler to maintain and understand
- Good for learning and experimentation
- Easier to track costs and behavior

**Multiple workflows approach**:

- Better separation of concerns
- Different triggers and permissions per task
- Easier to enable/disable specific automations
- Clearer audit trails per function

We recommend starting with one or two workflows, then expanding as you understand the patterns. See [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/) for workflow examples and inspiration.

### Should I create agentic workflows by hand editing or using AI?

Either approach works well:

**AI-assisted authoring**:

- Use `/agent agentic-workflows create` in GitHub Copilot Chat
- Interactive guidance through the creation process
- Automatic best practices and security configuration

**Manual editing**:

- Full control over every configuration option
- Good for learning the workflow format deeply
- Essential for advanced customizations

See [Creating Workflows](/gh-aw/setup/creating-workflows/) for the AI-assisted approach, or browse the [Reference documentation](/gh-aw/reference/frontmatter/) for manual configuration.

### You use 'agent' and 'agentic workflow' interchangeably. Are they the same thing?

Yes, for the purpose of this technology. An **"agent"** is an agentic workflow in a repository—an AI-powered automation that can reason, make decisions, and take actions. We use **"agentic workflow"** as it's plainer and emphasizes the workflow nature of the automation, but the terms are synonymous in this context.

## Troubleshooting

### Why did my workflow fail?

Common failure reasons include:

1. **Missing secrets**: Ensure required tokens (e.g., `COPILOT_GITHUB_TOKEN`) are configured or are incorrect
2. **Permission issues**: Check that workflow permissions match required operations
3. **Network restrictions**: Verify required domains are in the `network.allowed` list
4. **Tool access**: Ensure needed tools are enabled in the `tools:` configuration
5. **Rate limits**: AI API rate limits may cause failures during high usage

Use `gh aw audit <run-id>` to investigate specific failures. See [Common Issues](/gh-aw/troubleshooting/common-issues/) for detailed debugging guides.

### How do I debug a failing workflow?

1. **Check the logs**: View workflow logs in GitHub Actions or use `gh aw logs`. A high level summary is shown for the run.
2. **Audit the run**: Use `gh aw audit <run-id>` for detailed analysis
3. **Inspect the lock file**: Review the compiled `.lock.yml` for unexpected configuration
4. **Use the debug agent**: Run `/agent agentic-workflows debug` in Copilot Chat
5. **Test locally**: Use `gh aw compile --watch` during development

### Can I test workflows without affecting my repository?

Yes! Use [TrialOps](/gh-aw/guides/trialops/) to test workflows in isolated trial repositories. This lets you validate behavior and iterate on prompts without creating real issues, PRs, or comments in your actual repository.

## Advanced Topics

### Can workflows trigger other workflows?

Yes, using the `dispatch-workflow` safe output:

```yaml wrap
safe-outputs:
  dispatch-workflow:
    max: 1
```

This allows your workflow to trigger up to 1 other workflows with custom inputs. See [Safe Outputs](/gh-aw/reference/safe-outputs/#workflow-dispatch-dispatch-workflow) for details.

### Can I use MCP servers with agentic workflows?

Yes! [Model Context Protocol (MCP)](/gh-aw/reference/glossary/#mcp-model-context-protocol) servers extend workflow capabilities with custom tools and integrations. Configure them in your frontmatter:

```yaml wrap
tools:
  mcp-servers:
    my-server:
      image: "ghcr.io/org/my-mcp-server:latest"
      network:
        allowed: ["api.example.com"]
```

See [Getting Started with MCP](/gh-aw/guides/getting-started-mcp/) and [MCP Servers](/gh-aw/guides/mcps/) for configuration guides.

### Can workflows be broken up into shareable components?

Workflows can import shared configurations and components:

```yaml wrap
imports:
  - shared/github-tools.md
  - githubnext/agentics/shared/common-tools.md
```

This enables reusable tool configurations, network settings, and permissions across workflows. See [Imports](/gh-aw/reference/imports/) and [Packaging Imports](/gh-aw/guides/packaging-imports/) for details.

### Can I run workflows on a schedule?

Yes, use cron expressions in the `on:` trigger:

```yaml wrap
on:
  schedule:
    - cron: "0 9 * * MON"  # Every Monday at 9am UTC
```

See [Schedule Syntax](/gh-aw/reference/schedule-syntax/) for cron expression reference.

### Can I run workflows conditionally?

Yes, use the `if:` expression at the workflow level:

```yaml wrap
if: github.event_name == 'push' && github.ref == 'refs/heads/main'
```

See [Conditional Execution](/gh-aw/reference/frontmatter/#conditional-execution-if) in the Frontmatter Reference for details.
