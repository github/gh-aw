---
title: Frequently Asked Questions
description: Answers to common questions about GitHub Agentic Workflows, including security, costs, privacy, and configuration.
sidebar:
  order: 50
---

> [!NOTE]
> GitHub Agentic Workflows is in early development and may change significantly. Using automated agentic workflows requires careful attention to security considerations and careful human supervision, and even then things can still go wrong. Use it with caution, and at your own risk.

## Determinism

### I like deterministic CI/CD. Isn't this non-deterministic?

We fully embrace the need for deterministic CI/CD, especially in build, release, and test pipelines. Agentic workflows are **100% additive** to your existing CI/CD - they don't replace or modify your deterministic pipelines.

This is about something new and additive: **Continuous AI** in addition to **Continuous Integration** and **Continuous Deployment** - a new leg to make a trifecta. It just happens to run in GitHub Actions because that's where infrastructure for secure execution, permissions, and repository context is found. It's a new addition to your automation toolkit, not a replacement for existing workflows.

Your build, test, and release workflows should remain fully deterministic and reproducible. Agentic workflows complement these by handling tasks that are inherently non-deterministic anyway - like triaging issues, drafting documentation, researching dependencies, or proposing code improvements for human review. Many valuable automation tasks don't require determinism. When a coding agent reviews an issue and suggests a label, or drafts release notes from commits, the exact wording doesn't need to be reproducible - it needs to be helpful. Agentic workflows excel at these judgment-based tasks while your critical CI/CD pipelines remain unchanged.

Your first call should be reliable, deterministic CI/CD. If you use agentic workflows, you should use them for tasks that benefit from a coding agent's flexibility, not for core build and release processes that require strict reproducibility.

## Capabilities

### What's the difference between agentic workflows and regular GitHub Actions workflows?

Agentic workflows use AI to interpret natural language instructions in markdown instead of complex YAML. The AI engine can call pre-approved tools to perform tasks while running with read-only default permissions, safe outputs, and sandboxed execution.

### What's the difference between agentic workflows and just running a coding agent in GitHub Actions?

While you could install and run a coding agent directly in a standard GitHub Actions workflow, agentic workflows provide a structured framework with simpler markdown format, built-in security controls, pre-defined tools for GitHub operations, and easy switching between AI engines.

### Can agentic workflows write code and create pull requests?

Yes! Agentic workflows can create pull requests using the `create-pull-request` safe output. This allows the workflow to propose code changes, documentation updates, or other modifications as pull requests for human review and merging.

Some organizations may completely disable the creation of pull requests from GitHub Actions. In such cases, workflows can still generate diffs or suggestions in issues or comments for manual application.

### Can agentic workflows do more than code?

Yes! Agentic workflows can analyze repositories, generate reports, triage issues, research information, create documentation, and coordinate work. The AI interprets natural language instructions and uses available [tools](/gh-aw/reference/tools/) to accomplish tasks.

### Can agentic workflows mix regular GitHub Actions steps with AI agentic steps?

Yes! Agentic workflows can include both AI agentic steps and traditional GitHub Actions steps. You can add custom steps before the agentic job using the [`steps:` configuration](/gh-aw/reference/frontmatter/#custom-steps-steps). Additionally, [custom safe output jobs](/gh-aw/reference/safe-outputs/#custom-safe-output-jobs-jobs) can be used as consumers of agentic outputs. [Safe inputs](/gh-aw/reference/safe-inputs/) allow you to pass data between traditional steps and the AI agent with added checking.

### Can agentic workflows read other repositories?

Not by default, but yes with proper configuration. Cross-repository access requires:

1. A **Personal Access Token (PAT)** with access to target repositories
2. Configuring the token in your workflow

See [MultiRepoOps](/gh-aw/patterns/multirepoops/) for coordinating across repositories, or [SideRepoOps](/gh-aw/patterns/siderepoops/) for running workflows from a separate repository.

### Can I use agentic workflows in private repositories?

Yes, and in many cases we recommend it. Private repositories are ideal for proprietary code, creating a "sidecar" repository with limited access, testing workflows, and organization-internal automation. See [SideRepoOps](/gh-aw/patterns/siderepoops/) for patterns using private repositories.

### Can I edit workflows directly on GitHub.com without recompiling?

Yes! The **markdown body** (AI instructions) is loaded at runtime and can be edited directly on GitHub.com or in any editor. Changes take effect on the next workflow run without recompilation.

However, **frontmatter configuration** (tools, permissions, triggers, network rules) is embedded in the compiled workflow and requires recompilation when changed. Run `gh aw compile my-workflow` after editing frontmatter.

See [Editing Workflows](/gh-aw/guides/editing-workflows/) for complete guidance on when recompilation is needed.

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

## Guardrails

### Agentic workflows run in GitHub Actions. Can they access my repository secrets?

Repository secrets are not available to the agentic step by default. The AI agent runs with read-only permissions and cannot directly access your repository secrets unless explicitly configured. You should review workflows carefully, follow [GitHub Actions security guidelines](https://docs.github.com/en/actions/reference/security/secure-use), use least-privilege permissions, and inspect the compiled `.lock.yml` file. See the [Security Architectur](/gh-aw/introduction/architecture/) for details.

Some MCP tools may be configured using secrets, but these are only accessible to the specific tool steps, not the AI agent itself. Minimize the use of tools equipped with highly privileged secrets.

### Agentic workflows run in GitHub Actions. Can they write to the repository?

By default, the agentic "coding agent" step of agentic workflows runs with read-only permissions. Write operations require explicit approval through [safe outputs](/gh-aw/reference/safe-outputs/) or explicit general `write` permissions (not recommended). This ensures that AI agents cannot make arbitrary changes to your repository.

If safe outputs are configured, the workflow has limited, highly specific write operations that are then sanitized and executed securely.

### What sanitization is done on AI outputs before applying changes?

All safe outputs from the AI agent are sanitized before being applied to your repository. Sanitization includes secret redaction, URL domain filtering, XML escaping, size limits, control character stripping, GitHub reference escaping and HTTPS enforcement.

Additionally, safe outputs enforce permission separation - write operations happen in separate jobs with scoped permissions, never in the agentic job itself.

See [Safe Outputs - Sanitization](/gh-aw/reference/safe-outputs/#security-and-sanitization) for configuration options.

### Tell me more about guardrails

Guardrails is foundational to the design. Agentic workflows implement defense-in-depth through compilation-time validation (schema checks, expression safety, action SHA pinning), runtime isolation (sandboxed containers with network controls), permission separation (read-only defaults with [safe outputs](/gh-aw/reference/safe-outputs/) for writes), tool allowlisting, and output sanitization. See the [Security Architecture](/gh-aw/introduction/architecture/).

### How is my code and data processed?

By default, your workflow is run on GitHub Actions, like any other GitHub Actions workflow, and as one if its jobs it invokes your nominated [AI Engine (coding agent)](/gh-aw/reference/engines/), run in a container. This engine may in turn make tool calls and MCP calls. When using the default **GitHub Copilot CLI**, the workflow is processed by the `copilot` CLI tool which uses GitHub Copilot's services and related AI models. The specifics depend on your engine choice:

- **GitHub Copilot CLI**: See [GitHub Copilot documentation](https://docs.github.com/en/copilot) for details.
- **Claude/Codex**: Uses respective providers' APIs with their data handling policies.

See the [Security Architecture](/gh-aw/introduction/architecture/) for details on the execution and data flow.

### Does the underlying AI engine run in a sandbox?

Yes, the [AI engine](/gh-aw/reference/engines/) runs in a containerized sandbox with network egress control via the [Agent Workflow Firewall](/gh-aw/reference/sandbox/), container isolation, GitHub Actions resource constraints, and limited filesystem access to workspace and temporary directories. The sandbox container runs inside a GitHub Actions VM for additional isolation. See [Sandbox Configuration](/gh-aw/reference/sandbox/).

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

## Configuration & Setup

### What is a workflow lock file?

A **workflow lock file** (`.lock.yml`) is a traditional GitHub Actions workflow generated for your workflow markdown file (`.md`). It basically contains scaffolding and guardrails around a coding agent that is going to execute your workflow prompting in GitHub Actions. When you run `gh aw compile`, the lock file will appear or update and contain a complete GitHub Actions YAML file with all guradrail hardening applied.

Both files should be committed to version control:

- **`.md` file**: Your source file - easy to read, edit, and understand
- **`.lock.yml` file**: The lock file - what GitHub Actions actually runs

The lock file contains SHA-pinned actions, resolved imports, configured permissions, and all the infrastructure needed for secure execution. You can inspect it to see exactly what will run - there's no hidden configuration.

The naming convention (`.lock.yml`) reflects that this file "locks in" the exact workflow configuration at compile time, ensuring reproducibility and auditability. If you modify the frontmatter in your `.md` file, you must recompile to update the lock file.

When the workflow runs, the prompt itself is taken from the markdown file at runtime, so you can edit the markdown without recompiling and see changes in the next run. However, any changes to frontmatter (permissions, tools, triggers) require recompilation to update the lock file.

### Why do I need a token or key?

When using **GitHub Copilot CLI**, a Personal Access Token (PAT) with "Copilot Requests" permission authenticates and associates automation work with your GitHub account. This ensures usage tracking against your subscription, appropriate AI permissions, and auditable actions. In the future, this may support organization-level association. See [GitHub Tokens](/gh-aw/reference/tokens/).

### What hidden runtime dependencies does this have?

The executing agentic workflow uses your nominated coding agent (defaulting to GitHub Copilot CLI), a GitHub Actions VM with NodeJS, pinned Actions from [github/gh-aw](https://github.com/github/gh-aw) releases, and an Agent Workflow Firewall container for network control (optional but default). The exact YAML workflow can be inspected in the compiled `.lock.yml` file - there's no hidden configuration.

### I'm not using a supported AI Engine (coding agent). What should I do?

If you want to use a coding agent that isn't currently supported (Copilot, Claude, or Codex), you can use the custom engine to define your own GitHub Actions steps, contribute support to the [gh-aw repository](https://github.com/github/gh-aw), or open an issue describing your use case. See [AI Engines](/gh-aw/reference/engines/).

### Can I test workflows without affecting my repository?

Yes! Use [TrialOps](/gh-aw/patterns/trialops/) to test workflows in isolated trial repositories. This lets you validate behavior and iterate on prompts without creating real issues, PRs, or comments in your actual repository.

### Where can I find help with common issues?

See [Common Issues](/gh-aw/troubleshooting/common-issues/) for detailed troubleshooting guidance including workflow failures, debugging strategies, permission issues, and network problems.

## Workflow Design

### Should I focus on one workflow, or write many different ones?

One workflow is simpler to maintain and good for learning, while multiple workflows provide better separation of concerns, different triggers and permissions per task, and clearer audit trails. Start with one or two workflows, then expand as you understand the patterns. See [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/) for examples.

### Should I create agentic workflows by hand editing or using AI?

Either approach works well. AI-assisted authoring using `/agent agentic-workflows create` in GitHub Copilot Chat provides interactive guidance with automatic best practices, while manual editing gives full control and is essential for advanced customizations. See [Creating Workflows](/gh-aw/setup/creating-workflows/) for AI-assisted approach, or [Reference documentation](/gh-aw/reference/frontmatter/) for manual configuration.

### You use 'agent' and 'agentic workflow' interchangeably. Are they the same thing?

Yes, for the purpose of this technology. An **"agent"** is an agentic workflow in a repository - an AI-powered automation that can reason, make decisions, and take actions. We use **"agentic workflow"** as it's plainer and emphasizes the workflow nature of the automation, but the terms are synonymous in this context.

## Costs & Usage

### Who pays for the use of AI?

This depends on the AI engine (coding agent) you use:

- **GitHub Copilot CLI** (default): Usage is currently associated with the individual GitHub account of the user supplying the COPILOT_GITHUB_TOKEN, and is drawn from the monthly quota of premium requests for that account. See [GitHub Copilot billing](https://docs.github.com/en/copilot/about-github-copilot/subscription-plans-for-github-copilot).
- **Claude**: Usage is billed to the Anthropic account associated with ANTHROPIC_API_KEY Actions secret in the repository.
- **Codex**: Usage is billed to your OpenAI account associated with OPENAI_API_KEY Actions secret in the repository.

### What's the approximate cost per workflow run?

Costs vary depending on workflow complexity, AI model, and execution time. GitHub Copilot CLI uses 1-2 premium requests per workflow execution with agentic processing. Track usage with `gh aw logs` for runs and metrics, `gh aw audit <run-id>` for detailed token usage and costs, or check your AI provider's usage portal. Consider creating separate PAT/API keys per repository for tracking.

Reduce costs by optimizing prompts, using smaller models, limiting tool calls, reducing run frequency, and caching results.

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

