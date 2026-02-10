---
title: Glossary
description: Definitions of technical terms and concepts used throughout GitHub Agentic Workflows documentation.
sidebar:
  order: 1000
---

This glossary provides definitions for key technical terms and concepts used in GitHub Agentic Workflows.

> [!TIP]
> New to GitHub Agentic Workflows?
> Technical terms throughout the documentation link to their definitions here. Click any glossary link to understand unfamiliar concepts. Bookmark this page for quick reference!

## Core Concepts

### Agentic

The term **"agentic"** means having agency - the ability to act independently, make context-aware decisions, and adapt behavior based on circumstances. When applied to workflows:

- **Agentic workflows** use AI to understand context and choose appropriate actions, rather than just following predefined steps
- **Agentic systems** can reason about situations and make informed decisions without explicit programming for every scenario
- Contrasts with **deterministic** workflows that execute fixed sequences of actions

The word comes from "agent" (an entity that acts on behalf of someone) + "-ic" (having the characteristics of).

### Agentic Workflow

An AI-powered workflow that can reason, make decisions, and take autonomous actions using natural language instructions. Unlike traditional workflows with fixed if/then rules, agentic workflows interpret context and adapt their behavior based on the situation they encounter.

**Key characteristics:**

- Written in natural language markdown instead of complex YAML
- Uses AI to understand repository context (issues, PRs, code)
- Makes context-aware decisions without explicit conditionals
- Adapts responses to different situations flexibly

**Example:** Instead of "if issue has label X, do Y", you write "analyze this issue and provide helpful context", and the AI decides what's helpful based on the specific issue content.

### Orchestration

Workflows that coordinate one or more **worker workflows** toward a shared goal. A typical orchestrator/worker design:

- An **orchestrator** decides what work to do next and dispatches workers
- **Workers** execute the concrete tasks with scoped tools and limits

See the [Orchestration guide](/gh-aw/patterns/orchestration/) for implementation patterns.

### Orchestrator Workflow

A workflow that fans out work by dispatching other workflows (workers), aggregates results, and optionally posts summaries.

### Worker Workflow

A workflow dispatched by an orchestrator that performs a focused unit of work (triage, analysis, code changes, validation).

### Agentic Engine or Coding Agent

The AI system (typically GitHub Copilot CLI) that executes natural language instructions in an agentic workflow. The coding agent interprets tasks, uses available tools, and generates outputs based on context.

Think of the coding agent as an AI assistant with access to tools (GitHub API, file system, web search) that can understand your instructions and complete tasks autonomously. The coding agent is the "intelligence" that makes workflows agentic.

### Frontmatter

The configuration section at the top of a workflow file, enclosed between `---` markers. Contains YAML settings that control when the workflow runs, what permissions it has, and what tools it can use. Separates technical configuration from natural language instructions.

```yaml
---
on: issues
permissions: read-all
tools:
  github:
---
```

### Compilation

The process of translating Markdown workflows (`.md` files) into GitHub Actions YAML format (`.lock.yml` files). During compilation, workflows are validated, imports are resolved, tools are configured, and security hardening is applied.

### Workflow Lock File (.lock.yml)

The compiled GitHub Actions workflow file generated from a workflow markdown file (`.md`). Contains complete GitHub Actions YAML with security hardening applied. Both the `.md` source file and `.lock.yml` compiled file should be committed to version control. GitHub Actions runs the lock file, while the `.md` file remains easy to read and edit.

## Tools and Integration

### MCP (Model Context Protocol)

A standardized protocol that allows AI agents to securely connect to external tools, databases, and services. MCP enables workflows to integrate with GitHub APIs, web services, file systems, and custom integrations while maintaining security controls.

### MCP Gateway

A transparent proxy service that enables unified HTTP access to multiple MCP servers using different transport mechanisms (stdio, HTTP). The gateway provides protocol translation, server isolation, authentication, and health monitoring capabilities. It serves as an intermediary layer between MCP clients expecting HTTP communication and MCP servers that may use various transports, allowing clients to interact with multiple backends through a single HTTP endpoint.

### MCP Server

A service that implements the Model Context Protocol to provide specific capabilities to AI agents. Examples include the GitHub MCP server (for GitHub API operations), Playwright MCP server (for browser automation), or custom MCP servers for specialized tools.

### Tools

Capabilities that an AI agent can use during workflow execution. Tools are configured in the frontmatter and include GitHub operations (`github:`), file editing (`edit:`), web access (`web-fetch:`, `web-search:`), shell commands (`bash:`), browser automation (`playwright:`), and custom MCP servers.

## Security and Outputs

### Safe Inputs

Custom MCP tools defined inline in the workflow frontmatter using JavaScript or shell scripts. Allows lightweight tool creation without external dependencies while maintaining controlled access to secrets. Tools are generated at runtime and mounted as an MCP server. Each tool can have typed input parameters, default values, and environment variables. Configured using the `safe-inputs:` section in frontmatter.

### SARIF

Static Analysis Results Interchange Format - a standardized JSON format for reporting results from static analysis tools. Used by GitHub Code Scanning to display security vulnerabilities and code quality issues. Workflows can generate SARIF files using the `create-code-scanning-alert` safe output to report security findings discovered during AI analysis.

```yaml
safe-outputs:
  create-code-scanning-alert:
    max: 1
```

### SBOM

Software Bill of Materials - a comprehensive inventory of all components, libraries, and dependencies in a software project. Used for security auditing, vulnerability tracking, and compliance requirements. Helps identify when dependencies have known security issues. Common formats include SPDX and CycloneDX.

### Safe Outputs

Pre-approved actions the AI can take without requiring elevated permissions. The AI generates structured output describing what it wants to create (issues, comments, pull requests), which is processed by separate, permission-controlled jobs. Configured using the `safe-outputs:` section in frontmatter. This approach lets AI agents create GitHub content without direct write access, reducing security risks.

### Threat Detection

Automated security analysis that scans agent output and code changes for potential security issues before they are applied. When safe outputs are configured, a threat detection job automatically runs to identify prompt injection attempts, secret leaks, and malicious code patches. Uses AI-powered analysis to detect malicious instructions, exposed credentials, and suspicious code patterns. The threat detection job runs after the main agent job completes but before safe outputs are processed, providing an additional security layer. See [Threat Detection Reference](/gh-aw/reference/threat-detection/) for configuration options.

### Staged Mode

A preview mode where workflows simulate their actions without making changes. The AI generates output showing what would happen, but no GitHub API write operations are performed. Use for testing and validation before running workflows in production.

### Permissions

Access controls that define what operations a workflow can perform. Workflows follow the principle of least privilege, starting with read-only access by default. Write operations are typically handled through safe outputs rather than direct permissions.

### Safe Output Messages

Customizable messages that workflows can display during execution to communicate status and progress. Configured in the `safe-outputs.messages` section. Types include `run-started` (workflow begins), `run-success` (workflow completes successfully), `run-failure` (workflow fails), and `footer` (appended to all safe outputs). Supports GitHub context variables like `{workflow_name}` and `{run_url}`.

```yaml
safe-outputs:
  messages:
    run-started: "🔍 Analysis starting! [{workflow_name}]({run_url})"
    run-success: "✅ Analysis complete!"
    footer: "> 🤖 *Generated by [{workflow_name}]({run_url})*"
```

### Upload Assets

A safe output capability that allows workflows to upload generated files (screenshots, charts, reports) to an orphaned git branch for persistent storage. Configured in the `safe-outputs.upload-assets` section. The AI calls the `upload_asset` tool to register files for upload, which are then committed to a dedicated assets branch by a separate, permission-controlled job. Assets are accessible via predictable GitHub raw URLs. Commonly used for visual testing artifacts, data visualizations, and generated documentation.

```yaml
safe-outputs:
  upload-asset:
    branch: "assets/my-workflow"     # branch name (default: "assets/${{ github.workflow }}")
    max-size: 10240                  # max file size in KB (default: 10MB)
    allowed-exts: [.png, .jpg, .svg] # allowed extensions
```

### Minimize Comment

A safe output capability that allows workflows to hide or minimize GitHub comments without requiring write permissions. When minimized, comments are classified as SPAM. Requires GraphQL node IDs (format: `IC_kwDOABCD123456`) to identify comments. Useful for content moderation workflows.

```yaml
safe-outputs:
  minimize-comment:
    max: 5
    target-repo: "owner/repo"
```

## Workflow Components

### Engine

The AI system that powers the [agentic workflow](#agentic-workflow). GitHub Agentic Workflows supports multiple engines:

- **GitHub Copilot** (default): Uses GitHub's coding assistant

An engine is essentially "which AI to use" - think of it as choosing between different AI assistants (like Copilot, Claude, or others) to execute your workflow instructions.

### Triggers

Events that cause a workflow to run. Defined in the `on:` section of frontmatter. Includes issue events (`issues:`), pull request events (`pull_request:`), scheduled runs (`schedule:`), manual runs (`workflow_dispatch:`), and comment commands (`slash_command:`).

### Cron Schedule

A time-based trigger format. Use short syntax like `daily` or `weekly on monday` (recommended) or standard cron expressions for fixed times.

```yaml
on: weekly on monday  # Recommended: automatically scattered time
```

```yaml
on:
  schedule:
    - cron: "0 9 * * 1"  # Alternative: fixed time (Monday 9 AM UTC)
```

### workflow_dispatch

A manual trigger that runs a workflow on demand from the GitHub Actions UI or via the GitHub API. Requires explicit user initiation.

```yaml
on: workflow_dispatch
```

### Network Permissions

Controls over what external domains and services a workflow can access. Configured using the `network:` section in frontmatter. Options: `defaults` (common development infrastructure), custom allow-lists (specific domains), or `{}` (no network access).

### Imports

Reusable workflow components that can be shared across multiple workflows. Specified in the `imports:` field. Can include tool configurations, common instructions, or security guidelines stored in separate files.

### Labels

Optional workflow metadata containing an array of strings for categorization and organization. Labels help organize workflows by topic, purpose, or team, and enable filtering workflows in the CLI using the `--label` flag.

```yaml
labels: ["automation", "ci", "diagnostics"]
```

View workflows with specific labels:

```bash
gh aw status --label automation
```

## GitHub and Infrastructure Terms

### GitHub Actions

GitHub's built-in automation platform that runs workflows in response to repository events. Agentic workflows compile to GitHub Actions YAML format, leveraging the existing infrastructure for execution, permissions, and secrets management.

### GitHub Projects (Projects v2)

GitHub's project management and tracking system that organizes issues and pull requests using customizable boards, tables, and roadmaps. Projects v2 provides flexible custom fields (text, number, date, single-select, iteration), automation, and GraphQL API access. Agentic workflows can manage project boards using the `update-project` safe output to add items, update fields, and maintain ongoing monitoring/tracking. Requires organization-level Projects permissions for API access.

```yaml
safe-outputs:
  update-project:
    github-token: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}
```

### GitHub Actions Secret

A secure, encrypted variable stored in repository or organization settings. Holds sensitive values like API keys or tokens. Access in workflows using `${{ secrets.SECRET_NAME }}` syntax.

### YAML

A human-friendly data format used for configuration files. Uses indentation and simple syntax to represent structured data. In agentic workflows, YAML appears in the frontmatter section and in compiled `.lock.yml` files.

### Personal Access Token (PAT)

A token that authenticates you to GitHub's APIs with specific permissions. Required for GitHub Copilot CLI to access Copilot services. Created at github.com/settings/personal-access-tokens.

### Agent Files

Markdown files with YAML frontmatter stored in `.github/agents/` that define interactive Copilot Chat agents. Created by `gh aw init`, these files (like `agentic-workflows.agent.md`) can be invoked with the `/agent` command in Copilot Chat to guide workflow creation, debugging, and updates with specialized instructions. The `agentic-workflows` agent is a unified dispatcher that routes requests to specialized prompts based on your intent (create/debug/update/upgrade).

### Fine-grained Personal Access Token

A type of GitHub Personal Access Token with granular permission control. Specify exactly which repositories the token can access and what permissions it has (`contents: read`, `issues: write`, etc.). Created at github.com/settings/personal-access-tokens.

## Development and Compilation

### CLI (Command Line Interface)

The `gh-aw` extension for the GitHub CLI (`gh`) that provides commands for managing agentic workflows: `gh aw compile` (compile workflows), `gh aw run` (trigger runs), `gh aw status` (check status), `gh aw logs` (download and analyze logs), `gh aw add` (add workflows from repositories), and `gh aw project` (create and manage GitHub Projects V2).

### Validation

The process of checking workflow files for errors, security issues, and best practices. Occurs during compilation and can be enhanced with strict mode and security scanners (actionlint, zizmor, poutine).

## Advanced Features

### Cache Memory

Persistent storage for workflows that preserves data between runs. Configured using `cache-memory:` in the tools section, it enables workflows to remember information and build on previous interactions. Files are stored in GitHub Actions cache with 7-day retention. See [Memory Reference](/gh-aw/reference/memory/) for configuration options.

### Repo Memory

Persistent file storage via Git branches with unlimited retention. Unlike cache-memory (7-day retention via GitHub Actions cache), repo-memory stores files permanently in dedicated Git branches. The compiler automatically configures branch cloning, file access at `/tmp/gh-aw/repo-memory-{id}/`, commits, pushes, and merge conflict resolution. Useful for long-term data persistence, audit trails, and workflows requiring permanent storage. See [Memory Reference](/gh-aw/reference/memory/) for configuration details.

### Command Triggers

Special triggers that respond to slash commands in issue and PR comments (e.g., `/review`, `/deploy`). Configured using the `slash_command:` section with a command name.

### Concurrency Control

Settings that limit how many instances of a workflow can run simultaneously. Configured using the `concurrency:` field to prevent resource conflicts or rate limiting.

### Environment Variables (env)

Configuration section in frontmatter that defines environment variables for the workflow. Variables can reference GitHub context values, workflow inputs, or static values. Accessible to all workflow steps via `${{ env.VARIABLE_NAME }}` syntax.

```yaml
env:
  ORGANIZATION: ${{ github.event.inputs.organization || 'github' }}
  API_VERSION: v3
```

### Custom Agents

Specialized instructions or configurations that customize AI agent behavior for specific tasks or repositories. Can be stored as:

- **Agent files** (`.github/agents/*.agent.md`) - Used with Copilot Chat `/agent` command for interactive workflow authoring and execution-time customization
- **Instruction files** (`.github/copilot/instructions/`) - Path-specific or repository-wide Copilot instructions

### Strict Mode

An enhanced validation mode that enforces additional security checks and best practices. Enabled using `strict: true` in frontmatter or the `--strict` flag when compiling.

### Timeout

The maximum duration a workflow can run before being automatically cancelled. Configured using `timeout-minutes:` in frontmatter. Default GitHub Actions timeout is 360 minutes (6 hours); workflows can specify shorter timeouts to fail faster.

```yaml
timeout-minutes: 45
```

### Tracker ID

A unique identifier assigned to workflows that enables external monitoring and coordination without bidirectional coupling. Orchestrator workflows can use tracker IDs to correlate worker runs and discover outputs without workers needing to know about the orchestrator. This enables tracker-based monitoring where the orchestrator can observe its workers, but workers operate independently.

```yaml
tracker-id: daily-file-diet-v1
```

### Toolsets

Predefined collections of related MCP tools that can be enabled together. Used with the GitHub MCP server to group capabilities like `repos` (repository operations), `issues` (issue operations), and `pull_requests` (PR operations). Configured in the `toolsets:` field under tool configuration.

```yaml
tools:
  github:
    toolsets:
      - repos
      - issues
```

### Workflow Inputs

Parameters that can be provided when manually triggering a workflow with `workflow_dispatch`. Defined in the `on.workflow_dispatch.inputs` section with type, description, default value, and whether the input is required.

```yaml
on:
  workflow_dispatch:
    inputs:
      organization:
        description: "GitHub organization to scan"
        required: true
        type: string
        default: github
```

## Operational Patterns

Operational patterns (suffixed with "-Ops") are established workflow architectures for common automation scenarios. Each pattern addresses specific use cases with recommended triggers, tools, and safe outputs.

### ChatOps

Interactive automation triggered by slash commands (`/review`, `/deploy`) in issues and pull requests. Team members trigger workflows by typing commands directly in discussions, enabling human-in-the-loop automation where developers invoke AI assistance on demand.

**Use for:** Interactive code reviews, on-demand deployments, assisted analysis, and team collaboration through shared commands.

See [ChatOps](/gh-aw/patterns/chatops/) for implementation details.

### DailyOps

Scheduled workflows for incremental daily improvements that automate progress toward large goals through small, manageable changes. Work happens automatically on weekday schedules with changes easy to review and integrate.

**Use for:** Continuous code quality improvements, progressive migrations, documentation maintenance, and chipping away at technical debt one small PR at a time.

See [DailyOps](/gh-aw/patterns/dailyops/) for implementation details.

### DataOps

Hybrid pattern combining deterministic data extraction in `steps:` with agentic analysis in the workflow body. Shell commands reliably fetch and structure data, then the AI agent interprets results and produces insights.

**Use for:** Data aggregation from APIs or logs, report generation, trend analysis, and auditing workflows that gather evidence and generate reports.

See [DataOps](/gh-aw/patterns/dataops/) for implementation details.

### DispatchOps

Manual workflow execution via the GitHub Actions UI or CLI using the `workflow_dispatch` trigger. Enables on-demand tasks, testing, and workflows requiring human judgment about timing. Workflows can accept custom input parameters for runtime customization.

**Use for:** Research tasks, operational commands, development testing, debugging production issues, and ad-hoc analysis that doesn't fit scheduled or event-based triggers.

See [DispatchOps](/gh-aw/patterns/dispatchops/) for implementation details.

### IssueOps

Automated issue management that analyzes, categorizes, and responds to issues when they are created. Uses issue event triggers with safe outputs for secure, automated triage without requiring write permissions for the AI job.

**Use for:** Auto-triage, smart routing to teams, initial responses, quality checks, and content analysis of newly created issues.

See the [IssueOps Examples](/gh-aw/patterns/issueops/) for implementation details.

### LabelOps

Workflows triggered by label changes on issues and pull requests. Uses labels as workflow triggers, metadata, and state markers with filtering to activate only for specific label additions or removals.

**Use for:** Priority-based workflows, stage transitions, specialized processing based on label categories, and team coordination via label-based handoffs.

See the [LabelOps Examples](/gh-aw/patterns/labelops/) for implementation details.

### MemoryOps

Stateful workflows that persist data between runs using `cache-memory` and `repo-memory`. Enables workflows to track progress, resume after interruptions, share data between runs, and avoid API throttling through incremental processing.

**Use for:** Incremental data processing, trend analysis, multi-step tasks requiring state, workflow coordination, and long-running operations that need checkpointing.

See the [MemoryOps](/gh-aw/guides/memoryops/) for implementation details.

### MultiRepoOps

Cross-repository coordination that extends automation patterns across multiple GitHub repositories. Uses secure authentication and cross-repository safe outputs to synchronize features, centralize tracking, and enforce organization-wide policies.

**Use for:** Feature synchronization across repos, hub-and-spoke issue tracking, organization-wide policy enforcement, security patch rollouts, and coordinating services in separate repositories.

See the [MultiRepoOps](/gh-aw/patterns/multirepoops/) for implementation details.

### ProjectOps

AI-powered GitHub Projects board management that automates issue triage, routing, and field updates. Analyzes issue/PR content and makes intelligent decisions about project assignment, status, priority, and custom field values using the `update-project` safe output for secure board updates without elevated permissions.

**Use for:** Event-driven workflows (issue opened, PR created) that need to categorize and track work on project boards, content-based routing, AI-driven priority estimation, and automated status transitions.

See the [ProjectOps](/gh-aw/patterns/projectops/) for implementation details.

### SideRepoOps

Development pattern where workflows run from a separate "side" repository that targets your main codebase. Keeps AI-generated issues, comments, and workflow runs isolated from the main repository for cleaner separation between automation infrastructure and production code.

**Use for:** Getting started with agentic workflows, experimentation without affecting main repository, keeping automation artifacts separate, and reporting workflows that generate high volumes of content.

See the [SideRepoOps](/gh-aw/patterns/siderepoops/) for implementation details.

### SpecOps

Maintaining and propagating W3C-style specifications using the `w3c-specification-writer` agent. Creates formal specifications with RFC 2119 keywords (MUST, SHALL, SHOULD, MAY) and automatically synchronizes changes to consuming implementations.

**Use for:** Maintaining formal technical specifications, keeping specifications synchronized across repositories, and ensuring implementations stay compliant with specification updates.

See the [SpecOps](/gh-aw/patterns/specops/) for implementation details.

### TaskOps

Scaffolded AI-powered code improvement strategy with three phases: research agent investigates and reports findings, developer reviews and invokes planner agent to create actionable issues, then assigns approved issues to Copilot for automated implementation. Keeps developers in control with clear decision points at each phase.

**Use for:** Systematic code improvements requiring investigation before action, work needing breakdown for optimal AI agent execution, and situations where research findings vary in priority and require developer oversight.

See [TaskOps](/gh-aw/patterns/taskops/) for implementation details.

### TrialOps

Testing and validation pattern that executes workflows in isolated trial repositories before production deployment. Creates temporary private repositories where workflows run safely, capturing safe outputs without modifying your actual codebase.

**Use for:** Testing workflows before production, comparing implementation approaches, validating prompt changes, debugging in isolation, and demonstrating capabilities with real results.

See the [TrialOps](/gh-aw/patterns/trialops/) for implementation details.

## Related Resources

For detailed documentation on specific topics, see:

- [Frontmatter Reference](/gh-aw/reference/frontmatter/)
- [Tools Reference](/gh-aw/reference/tools/)
- [Safe Inputs Reference](/gh-aw/reference/safe-inputs/)
- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/)
- [Using MCPs Guide](/gh-aw/guides/mcps/)
- [Security Guide](/gh-aw/introduction/architecture/)
- [AI Engines Reference](/gh-aw/reference/engines/)
