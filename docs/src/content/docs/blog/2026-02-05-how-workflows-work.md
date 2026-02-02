---
title: "How Agentic Workflows Work"
description: "The technical foundation: from natural language to secure execution"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-02-05
draft: true
prev:
  link: /gh-aw/blog/2026-02-02-security-lessons/
  label: Security Lessons
next:
  link: /gh-aw/blog/2026-02-08-authoring-workflows/
  label: Authoring Workflows
---

[Previous Article](/gh-aw/blog/2026-02-02-security-lessons/)

---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

*Aha!* Time for a deep plunge into the *bubbly depths* of Peli's Agent Factory! Having explored the [security vault](/gh-aw/blog/2026-02-02-security-lessons/), we shall now peek behind the curtain and discover the *magnificent machinery* - the technical foundation that makes it all work!

Ever wonder what actually happens when you write an agentic workflow? Let's take a journey from a simple Markdown file all the way to secure, auditable execution in GitHub Actions.

Every agent in Peli's Agent Factory follows the same basic lifecycle, transforming natural language descriptions into production-ready workflows. Understanding this architecture helps you design effective agents and debug issues when they pop up.

Let's walk through the complete journey together!

## The Three-Stage Lifecycle

```text
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│   Write     │      │   Compile   │      │     Run     │
│  (Markdown) │ ───> │   (YAML)    │ ───> │  (Actions)  │
└─────────────┘      └─────────────┘      └─────────────┘
  Natural Lang         Secure Lock          Team Visible
  Declarative          Validated            Auditable
  Human-Friendly       Machine-Ready        Observable
```

Three stages, each with a clear purpose. Let's explore what happens at each step!

## Stage 1: Write in Natural Language

Agentic workflows start as **Markdown files** that combine natural language prompts with declarative configuration. Think of it as writing instructions for a helpful robot.

### Anatomy of a Workflow File

```markdown
---
description: Investigates failed CI workflows to identify root causes
on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]
permissions:
  contents: read
  issues: write
tools:
  github:
    toolsets: [issues, pull-requests]
  bash:
    commands: [git, jq]
network:
  allowed:
    - "api.github.com"
safe_outputs:
  create_issue:
    title_prefix: "[CI Doctor]"
    labels: ["ci", "automated"]
    max_items: 3
    expire: "+7d"
---

# CI Doctor

When a CI workflow fails, investigate the root cause:

1. Download the workflow logs
2. Analyze the failure patterns
3. Identify the root cause
4. Create an issue with diagnostic information

Include:
- Failure summary
- Relevant log excerpts
- Suggested fixes
- Related issues or PRs
```

### Frontmatter: Declarative Configuration

The YAML frontmatter defines **how** the workflow runs. Configure triggers (schedule, events, manual, or comments), permissions (start with `contents: read`, add write access sparingly), tools (GitHub API, bash commands, MCP servers - all explicitly enumerated), network access (allowlisted domains only), and safe outputs (templates with built-in guardrails for creating issues/PRs).

### Prompt: Natural Language Instructions

The Markdown content after frontmatter is the **agent's prompt** - natural language instructions describing what the agent should do, how it should behave, and what outputs to create. Effective prompts start with a clear objective, provide step-by-step guidance, include output examples, and reference available tools. This is where you give the agent personality and clear direction.

## Stage 2: Compile to Secure Workflows

The `gh aw compile` command transforms natural language workflows into GitHub Actions YAML files with embedded security controls.

### Compilation Process

```text
┌──────────────┐
│ workflow.md  │
└──────┬───────┘
       │
       ├─► Parse frontmatter
       │   └─► Validate schema
       │
       ├─► Load imports
       │   └─► Merge configurations
       │
       ├─► Validate security
       │   ├─► Check permissions
       │   ├─► Verify tool allowlists
       │   ├─► Validate network rules
       │   └─► Audit safe outputs
       │
       ├─► Generate GitHub Actions jobs
       │   ├─► Setup job (environment prep)
       │   ├─► Agent job (AI execution)
       │   └─► Safe output jobs (writes)
       │
       └─► Write workflow.lock.yml
           └─► Locked, validated, ready to run
```

### What Compilation Does

Compilation performs five key operations:

1. **Schema Validation** - Ensures frontmatter conforms to valid trigger syntax, permissions, tool configurations, and safe output templates
2. **Security Validation** - Enforces that permissions don't exceed requirements, tools are explicitly listed, network access is constrained, and safe outputs have appropriate limits
3. **Import Resolution** - Loads and merges shared component files, tool configurations, prompt instructions, and version pins
4. **Job Generation** - Creates Setup (environment prep), Agent (AI execution), and Safe Output (writes) jobs
5. **Lock File Generation** - Produces a complete, validated `.lock.yml` file ready for deployment

### Example Compilation

**Input: `ci-doctor.md`** (50 lines of natural language)

**Output: `ci-doctor.lock.yml`** (300 lines of validated YAML)

The lock file includes environment setup, tool installations, security controls, agent execution logic, safe output processing, error handling, and cleanup steps.

## Stage 3: Run and Produce Artifacts

Compiled workflows execute on GitHub Actions runners, producing team-visible artifacts.

### Execution Flow

```text
Workflow Triggered
    │
    ├─► Setup Job
    │   ├─► Install gh-aw CLI
    │   ├─► Configure MCP servers
    │   ├─► Setup network restrictions
    │   └─► Prepare safe output handlers
    │
    ├─► Agent Job
    │   ├─► Load prompt
    │   ├─► Gather context (issues, PRs, files)
    │   ├─► Execute against AI engine
    │   │   └─► Agent uses tools as needed
    │   ├─► Generate safe output requests
    │   └─► Upload artifacts
    │
    └─► Safe Output Jobs (parallel)
        ├─► Create Issue (if requested)
        ├─► Create PR (if requested)
        ├─► Add Comment (if requested)
        └─► Upload Assets (if requested)
```

### Agent Execution Environment

The agent runs in a sandboxed environment with access to GitHub API (via MCP), allowlisted bash commands, repository file system, and configured MCP servers. Context includes trigger event details, repository state, recent issues/PRs, relevant files, and previous workflow runs. Constraints enforce network allowlists, tool restrictions, permission boundaries, and safe output templates.

### Output Types

Agents produce issues, pull requests, comments, discussions, and artifacts (charts, data files, reports) through safe output templates:

```yaml
safe_outputs:
  create_issue:
    title: "CI failure in test suite"
    body: "Detailed analysis..."
    labels: ["ci", "automated"]
  create_pull_request:
    title: "Fix dependency vulnerability"
    body: "Updates package X..."
    branch: "agent/fix-vuln-123"
```

### Auditable Artifacts

Every agent action creates a permanent record. Workflow runs include full execution logs with start/end times, tool invocations, API calls, and errors. Issues, PRs, and comments are timestamped and attributed, showing who triggered the workflow, when it executed, and what it created. Discussions provide searchable historical reports, and artifacts offer versioned, downloadable charts, data files, and debug logs.

## The AI Engine Interface

Workflows support multiple AI engines. **Copilot** (default) provides code-aware context and GitHub API integration with usage tracked in your subscription. **Claude** offers long context windows and strong reasoning via ANTHROPIC_API_KEY. **Codex** provides enterprise integration with Azure OpenAI. **Custom** engines let you bring your own AI provider with full control over the stack.

```yaml
engine: copilot  # or claude, codex, custom
model: claude-sonnet-4
```

## Tool Architecture: MCP Servers

Model Context Protocol (MCP) servers provide specialized capabilities through a gateway. Built-in servers include `github` (API operations), `bash` (shell commands), and `filesystem` (file operations). External servers like `serena` (code analysis), `tavily` (web search), and `ast-grep` (structural search) extend functionality.

```yaml
tools:
  github:
    toolsets: [repos, issues, pull-requests]
  bash:
    commands: [git, jq, python]
  serena:
    mode: remote
    version: latest
```

## Error Handling and Debugging

When workflows fail, detailed logs capture job, step, tool invocation, and error information. Safe output validation provides clear error messages and example corrections while allowing workflows to fail gracefully. The [`mcp-inspector`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/mcp-inspector.md) workflow validates server availability and configuration. The [`audit-workflows`](https://github.com/github/gh-aw/tree/2c1f68a721ae7b3b67d0c2d93decf1fa5bcf7ee3/.github/workflows/audit-workflows.md) agent tracks runs, classifies failures, and creates issues for persistent problems.

## Performance Considerations

Typical workflows run in 2-6 minutes (30-60s setup, 1-5m agent execution, 10-30s safe outputs). Costs include GitHub Actions compute, AI engine API calls, MCP server usage, and artifact storage. Optimize by caching queries, batching operations, using concise prompts, and requesting only needed permissions.

## What's Next?

Now that you understand how workflows work under the hood, you're ready to start authoring your own agents for Peli's Agent Factory.

In our next article, we'll provide a practical guide to creating effective agentic workflows, with examples and best practices from the factory.

_More articles in this series coming soon._

[Previous Article](/gh-aw/blog/2026-02-02-security-lessons/)
