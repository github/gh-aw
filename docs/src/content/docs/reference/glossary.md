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

Having agency - the ability to act independently, make context-aware decisions, and adapt behavior based on circumstances. Agentic workflows use AI to understand context and choose appropriate actions, contrasting with deterministic workflows that execute fixed sequences. From "agent" + "-ic" (having the characteristics of).

### Agentic Workflow

An AI-powered workflow that reasons, makes decisions, and takes autonomous actions using natural language instructions. Written in markdown instead of complex YAML, agentic workflows interpret context and adapt behavior flexibly. For example, instead of "if issue has label X, do Y", you write "analyze this issue and provide helpful context", and the AI decides what's helpful based on the specific issue content.

### Orchestration

Workflows that coordinate one or more worker workflows toward a shared goal. An orchestrator decides what work to do next and dispatches workers, while workers execute concrete tasks with scoped tools and limits. See the [Orchestration guide](/gh-aw/patterns/orchestration/).

### Orchestrator Workflow

A workflow that fans out work by dispatching other workflows (workers), aggregates results, and optionally posts summaries.

### Worker Workflow

A workflow dispatched by an orchestrator that performs a focused unit of work (triage, analysis, code changes, validation).

### Agentic Engine or Coding Agent

The AI system (typically GitHub Copilot CLI) that executes natural language instructions in an agentic workflow. The agent interprets tasks, uses available tools (GitHub API, file system, web search), and generates outputs based on context autonomously.

### Frontmatter

Configuration section at the top of a workflow file, enclosed between `---` markers. Contains YAML settings controlling when the workflow runs, permissions, and available tools, separating technical configuration from natural language instructions.

### Compilation

Translating Markdown workflows (`.md` files) into GitHub Actions YAML format (`.lock.yml` files), including validation, import resolution, tool configuration, and security hardening.

### Workflow Lock File (.lock.yml)

The compiled GitHub Actions workflow file from a workflow markdown file (`.md`). Contains complete GitHub Actions YAML with security hardening applied. Both `.md` and `.lock.yml` files should be committed to version control. At runtime, GitHub Actions executes the lock file using a coding agent while referencing the markdown for instructions.

## Tools and Integration

### MCP (Model Context Protocol)

A standardized protocol that allows AI agents to securely connect to external tools, databases, and services. MCP enables workflows to integrate with GitHub APIs, web services, file systems, and custom integrations while maintaining security controls.

### MCP Gateway

A transparent proxy service that enables unified HTTP access to multiple MCP servers using different transport mechanisms (stdio, HTTP). Provides protocol translation, server isolation, authentication, and health monitoring, allowing clients to interact with multiple backends through a single HTTP endpoint.

### MCP Server

A service that implements the Model Context Protocol to provide specific capabilities to AI agents. Examples include the GitHub MCP server (for GitHub API operations), Playwright MCP server (for browser automation), or custom MCP servers for specialized tools.

### Tools

Capabilities that an AI agent can use during workflow execution. Tools are configured in the frontmatter and include GitHub operations (`github:`), file editing (`edit:`), web access (`web-fetch:`, `web-search:`), shell commands (`bash:`), browser automation (`playwright:`), and custom MCP servers.

## Security and Outputs

### Safe Inputs

Custom MCP tools defined inline in workflow frontmatter using JavaScript or shell scripts. Enables lightweight tool creation without external dependencies while maintaining controlled secret access. Tools are generated at runtime and mounted as an MCP server with typed input parameters, default values, and environment variables. Configured via `safe-inputs:` section.

### SARIF

Static Analysis Results Interchange Format - a standardized JSON format for reporting results from static analysis tools. Used by GitHub Code Scanning to display security vulnerabilities and code quality issues. Workflows can generate SARIF files using the `create-code-scanning-alert` safe output.

### SBOM

Software Bill of Materials - a comprehensive inventory of all components, libraries, and dependencies in a software project. Used for security auditing, vulnerability tracking, and compliance. Common formats include SPDX and CycloneDX.

### Safe Outputs

Pre-approved actions the AI can take without elevated permissions. The AI generates structured output describing what to create (issues, comments, pull requests), processed by separate permission-controlled jobs. Configured via `safe-outputs:` section, letting AI agents create GitHub content without direct write access.

### Threat Detection

Automated security analysis that scans agent output and code changes for potential security issues before application. When safe outputs are configured, a threat detection job automatically runs between the agent job and safe output processing to identify prompt injection attempts, secret leaks, and malicious code patches. See [Threat Detection Reference](/gh-aw/reference/threat-detection/).

### Staged Mode

A preview mode where workflows simulate actions without making changes. The AI generates output showing what would happen, but no GitHub API write operations are performed. Use for testing before production runs.

### Permissions

Access controls defining workflow operations. Workflows follow least privilege, starting with read-only access by default. Write operations are typically handled through safe outputs.

### Safe Output Messages

Customizable messages workflows can display during execution. Configured in `safe-outputs.messages` with types `run-started`, `run-success`, `run-failure`, and `footer`. Supports GitHub context variables like `{workflow_name}` and `{run_url}`.

### Upload Assets

A safe output capability for uploading generated files (screenshots, charts, reports) to an orphaned git branch for persistent storage. The AI calls the `upload_asset` tool to register files, which are committed to a dedicated assets branch by a separate permission-controlled job. Assets are accessible via GitHub raw URLs. Commonly used for visual testing artifacts, data visualizations, and generated documentation.

### Base Branch

Configuration field in the `create-pull-request` safe output specifying which branch the pull request should target. Defaults to `github.ref_name` if not specified. Useful for cross-repository pull requests targeting non-default branches.

### Minimize Comment

A safe output capability for hiding or minimizing GitHub comments without requiring write permissions. When minimized, comments are classified as SPAM. Requires GraphQL node IDs to identify comments. Useful for content moderation workflows.

## Workflow Components

### Engine

The AI system that powers the agentic workflow - essentially "which AI to use" to execute workflow instructions. GitHub Agentic Workflows supports multiple engines, with GitHub Copilot as the default.

### Triggers

Events that cause a workflow to run, defined in the `on:` section of frontmatter. Includes issue events, pull requests, schedules, manual runs, and slash commands.

### Cron Schedule

A time-based trigger format. Use short syntax like `daily` or `weekly on monday` (recommended with automatic time scattering) or standard cron expressions for fixed times.

### workflow_dispatch

A manual trigger that runs a workflow on demand from the GitHub Actions UI or via the GitHub API. Requires explicit user initiation.

### Network Permissions

Controls over external domains and services a workflow can access. Configured via `network:` section with options: `defaults` (common infrastructure), custom allow-lists, or `{}` (no access).

### Imports

Reusable workflow components shared across multiple workflows. Specified in the `imports:` field, can include tool configurations, common instructions, or security guidelines.

### Labels

Optional workflow metadata for categorization and organization. Enables filtering workflows in the CLI using the `--label` flag.

## GitHub and Infrastructure Terms

### GitHub Actions

GitHub's built-in automation platform that runs workflows in response to repository events. Agentic workflows compile to GitHub Actions YAML format, leveraging existing infrastructure for execution, permissions, and secrets.

### GitHub Projects (Projects v2)

GitHub's project management and tracking system organizing issues and pull requests using customizable boards, tables, and roadmaps. Provides flexible custom fields, automation, and GraphQL API access. Agentic workflows can manage project boards using the `update-project` safe output. Requires organization-level Projects permissions.

### GitHub Actions Secret

A secure, encrypted variable stored in repository or organization settings holding sensitive values like API keys or tokens. Access via `${{ secrets.SECRET_NAME }}` syntax.

### YAML

A human-friendly data format for configuration files using indentation and simple syntax to represent structured data. In agentic workflows, YAML appears in frontmatter and compiled `.lock.yml` files.

### Personal Access Token (PAT)

A token authenticating you to GitHub's APIs with specific permissions. Required for GitHub Copilot CLI to access Copilot services. Created at github.com/settings/personal-access-tokens.

### Agent Files

Markdown files with YAML frontmatter stored in `.github/agents/` defining interactive Copilot Chat agents. Created by `gh aw init`, these files can be invoked with the `/agent` command in Copilot Chat to guide workflow creation, debugging, and updates. The `agentic-workflows` agent is a unified dispatcher routing requests to specialized prompts.

### Fine-grained Personal Access Token

A GitHub Personal Access Token with granular permission control, specifying exactly which repositories the token can access and what permissions it has. Created at github.com/settings/personal-access-tokens.

## Development and Compilation

### CLI (Command Line Interface)

The `gh-aw` extension for GitHub CLI providing commands for managing agentic workflows: compile, run, status, logs, add, and project management.

### Validation

Checking workflow files for errors, security issues, and best practices. Occurs during compilation and can be enhanced with strict mode and security scanners.

## Advanced Features

### Cache Memory

Persistent storage for workflows preserving data between runs. Configured via `cache-memory:` in tools section with 7-day retention in GitHub Actions cache. See [Memory Reference](/gh-aw/reference/memory/).

### Repo Memory

Persistent file storage via Git branches with unlimited retention. Unlike cache-memory (7-day retention), repo-memory stores files permanently in dedicated Git branches with automatic branch cloning, file access, commits, pushes, and merge conflict resolution. See [Memory Reference](/gh-aw/reference/memory/).

### Command Triggers

Special triggers responding to slash commands in issue and PR comments. Configured using the `slash_command:` section with a command name.

### Concurrency Control

Settings limiting how many workflow instances can run simultaneously. Configured via `concurrency:` field to prevent resource conflicts or rate limiting.

### Environment Variables (env)

Configuration section in frontmatter defining environment variables for the workflow. Variables can reference GitHub context values, workflow inputs, or static values. Accessible via `${{ env.VARIABLE_NAME }}` syntax.

### Custom Agents

Specialized instructions customizing AI agent behavior for specific tasks or repositories. Stored as agent files (`.github/agents/*.agent.md`) for Copilot Chat or instruction files (`.github/copilot/instructions/`) for path-specific Copilot instructions.

### Strict Mode

Enhanced validation mode enforcing additional security checks and best practices. Enabled via `strict: true` in frontmatter or `--strict` flag when compiling.

### Timeout

Maximum duration a workflow can run before automatic cancellation. Configured via `timeout-minutes:` in frontmatter. Default is 360 minutes (6 hours); workflows can specify shorter timeouts to fail faster.

### Tracker ID

A unique identifier enabling external monitoring and coordination without bidirectional coupling. Orchestrator workflows use tracker IDs to correlate worker runs and discover outputs while workers operate independently.

### Toolsets

Predefined collections of related MCP tools enabled together. Used with the GitHub MCP server to group capabilities like `repos`, `issues`, and `pull_requests`. Configured in the `toolsets:` field.

### Workflow Inputs

Parameters provided when manually triggering a workflow with `workflow_dispatch`. Defined in the `on.workflow_dispatch.inputs` section with type, description, default value, and required status.

## Operational Patterns

Operational patterns (suffixed with "-Ops") are established workflow architectures for common automation scenarios. Each pattern addresses specific use cases with recommended triggers, tools, and safe outputs.

### ChatOps

Interactive automation triggered by slash commands (`/review`, `/deploy`) in issues and pull requests, enabling human-in-the-loop automation where developers invoke AI assistance on demand. See [ChatOps](/gh-aw/patterns/chatops/).

### DailyOps

Scheduled workflows for incremental daily improvements, automating progress toward large goals through small, manageable changes on weekday schedules. See [DailyOps](/gh-aw/patterns/dailyops/).

### DataOps

Hybrid pattern combining deterministic data extraction in `steps:` with agentic analysis in the workflow body. Shell commands fetch and structure data, then the AI agent interprets results and produces insights. See [DataOps](/gh-aw/patterns/dataops/).

### DispatchOps

Manual workflow execution via GitHub Actions UI or CLI using `workflow_dispatch` trigger. Enables on-demand tasks, testing, and workflows requiring human judgment about timing. Workflows can accept custom input parameters. See [DispatchOps](/gh-aw/patterns/dispatchops/).

### IssueOps

Automated issue management that analyzes, categorizes, and responds to issues when created. Uses issue event triggers with safe outputs for secure automated triage without requiring write permissions for the AI job. See [IssueOps Examples](/gh-aw/patterns/issueops/).

### LabelOps

Workflows triggered by label changes on issues and pull requests. Uses labels as triggers, metadata, and state markers with filtering for specific label additions or removals. See [LabelOps Examples](/gh-aw/patterns/labelops/).

### MemoryOps

Stateful workflows that persist data between runs using `cache-memory` and `repo-memory`, enabling progress tracking, resumption after interruptions, and incremental processing to avoid API throttling. See [MemoryOps](/gh-aw/guides/memoryops/).

### MultiRepoOps

Cross-repository coordination extending automation patterns across multiple repositories. Uses secure authentication and cross-repository safe outputs to synchronize features, centralize tracking, and enforce organization-wide policies. See [MultiRepoOps](/gh-aw/patterns/multirepoops/).

### ProjectOps

AI-powered GitHub Projects board management automating issue triage, routing, and field updates. Analyzes issue/PR content to make intelligent decisions about project assignment, status, priority, and custom fields using the `update-project` safe output. See [ProjectOps](/gh-aw/patterns/projectops/).

### SideRepoOps

Development pattern where workflows run from a separate "side" repository targeting your main codebase. Keeps AI-generated issues, comments, and workflow runs isolated from the main repository for cleaner separation between automation infrastructure and production code. See [SideRepoOps](/gh-aw/patterns/siderepoops/).

### SpecOps

Maintaining and propagating W3C-style specifications using the `w3c-specification-writer` agent. Creates formal specifications with RFC 2119 keywords and automatically synchronizes changes to consuming implementations. See [SpecOps](/gh-aw/patterns/specops/).

### TaskOps

Scaffolded AI-powered code improvement strategy with three phases: research agent investigates, developer reviews and invokes planner agent to create actionable issues, then assigns approved issues to Copilot for automated implementation. Keeps developers in control with clear decision points. See [TaskOps](/gh-aw/patterns/taskops/).

### TrialOps

Testing and validation pattern executing workflows in isolated trial repositories before production deployment. Creates temporary private repositories where workflows run safely, capturing safe outputs without modifying your actual codebase. See [TrialOps](/gh-aw/patterns/trialops/).

## Related Resources

For detailed documentation on specific topics, see:

- [Frontmatter Reference](/gh-aw/reference/frontmatter/)
- [Tools Reference](/gh-aw/reference/tools/)
- [Safe Inputs Reference](/gh-aw/reference/safe-inputs/)
- [Safe Outputs Reference](/gh-aw/reference/safe-outputs/)
- [Using MCPs Guide](/gh-aw/guides/mcps/)
- [Security Guide](/gh-aw/introduction/architecture/)
- [AI Engines Reference](/gh-aw/reference/engines/)
