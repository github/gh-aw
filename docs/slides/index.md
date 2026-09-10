---
marp: true
theme: gh-aw
paginate: true
---

<script src="../js/mermaid.min.js">
mermaid.initialize({ startOnLoad: true });
</script>

# GitHub Agentic Workflows

## Technical Preview

<https://github.com/github/gh-aw>

---

# Software Engineer → Agentic Engineer

> From writing code to designing agentic processes

---

# Agentic Human Processes

> Human oversight, AI execution

---

# Pull Request

> Code -> Reviewer - > Merge

---

# Pull Request + Agent

> Issue -> Agent -> Reviewer - > Merge

---

# Research → Plan → Act

> Agent Research -> Human review -> Issue planning -> Assign to Agent -> Human Review -> Merge

---

# Continuous Integration to Continuous AI

- **Accessibility review** — Automated WCAG compliance checks
- **Documentation** — Auto-generate API docs and README files
- **Code review** — AI-powered PR analysis and suggestions
- **Test improvement** — Identify missing test coverage
- **Bundle analysis** — Monitor package size and dependencies
- **Issue triage** — Automated labeling and prioritization

> <https://githubnext.com/projects/continuous-ai/>

<!--
https://github.com/github/gh-aw/issues/1920
-->

---

# CI/CD with GitHub Actions

YAML workflows stored in `.github/workflows/`, triggered on events like push, pull requests, or issues.

```yaml
on:
  issues:
    types: [opened]
permissions:
  issues: write # DANGER zone
jobs:
  agent:
    steps:
      - run: copilot -p "Summarize issue and respond in a comment."
```

---

# CA with GitHub Agentic Workflows

```yaml
---
on:
  issues:
    types: [opened]
permissions:
  issues: read # agent read-only, zero secrets
safe-outputs:
  add-comment: # guardrails for write operations
---
Summarize issue and respond in a comment.
```

> Natural language → compiled to GitHub Actions YAML

---

# The "Lethal Trifecta" for AI Agents

AI agents become dangerous when these **three capabilities** combine:

- **Private data access**

- **Untrusted content**

- **External communication**

> <https://simonw.substack.com/p/the-lethal-trifecta-for-ai-agents>

---

# Useful Sandboxes

## Safe by design, Useful by default

> The best developer tools protect you from catastrophe while letting you build something real

---

# From Scratch to MakeCode

## Kid dev environments got here first

- **Scratch** — Block-based coding (MIT) — can't break anything important
- **MakeCode / pxt** — Hardware + game programming for beginners
- **BASIC** — First language for a generation of developers

These environments share one superpower:

> Protected from catastrophe — still building something **real and delightful**

---

# Agentic Sandbox for Enterprise

## Safe by default

- **Containers**: Isolated GitHub Actions Jobs

- **Firewall**: Network Control

- **Minimal Permissions**: Read-only by default

- **MCP Gateway**: Secure tool access

- **Threat Detection**: Agentic detection of threats

- **Safe Outputs**: Deterministic, guardrailed outputs

- **Plan / Check / Act**: Human in the loop

---

# Safe Outputs

```yaml
---
on: 
  pull_request:
    types: [opened]
permissions: 
  contents: read
safe-outputs:
  create-issue:
---
Check for breaking changes in package.json and create an issue.
```

**Security:** AI agents cannot directly write to GitHub. Safe-outputs validate AI responses and execute actions in isolated containers.

---

# Staged Mode

## Preview before you deploy

```yaml
---
on:
  issues:
    types: [opened]
safe-outputs:
  staged: true  # Preview only — no writes
  create-issue:
---
Analyze issue and suggest follow-up tasks.
```

> See exactly what a workflow _would_ create — before it does

---

# Network Permissions

```yaml
---
on:
  pull_request:
network:
  allowed:
    - defaults  # Basic infrastructure
    - node      # NPM ecosystem
tools:
  web-fetch:
---
Fetch latest TypeScript docs and report findings in a comment.
```

> Control external access for security

---

# Safe Outputs → Copilot Handoff

```yaml
---
on:
  issues:
    types: [opened]
safe-outputs:
  create-issue:
    assignees: ["copilot"]
---
Analyze issue and break down into implementation tasks
```

> Triage agent → Creates tasks → @copilot implements → Review

---

# AI Engines

## Multiple AI providers supported

- **GitHub Copilot CLI** (default, recommended)
- **Claude Code**
- **OpenAI Codex**
- **Google Gemini CLI**
- **Pi** — provider-agnostic coding agent

```yaml
engine: copilot  # sensible defaults
```

> GitHub Copilot offers MCP support and conversational workflows

---

# MCP Servers Configuration

<!-- agent: do not add 'users' to the toolsets comment; the slide intentionally omits it for brevity -->
```yaml
# GitHub MCP (recommended: use toolsets)
tools:
  github:
    toolsets: [default]  # context, repos, issues, pull_requests

# Custom MCP servers
mcp-servers:
  bundle-analyzer:
    command: "node"
    args: ["path/to/mcp-server.js"]
    allowed: ["*"]
```

**MCP:** Extend AI with [Model Context Protocol](https://modelcontextprotocol.io/)

---

# Containerized, Firewalled MCPs

```yaml
mcp-servers:
  web-scraper:
    container: mcp/fetch
    network:
      allowed: ["npmjs.com", "*.jsdelivr.com"]
    allowed: ["fetch"]
```

**Defense in depth:** Container + network + permissions

---

# Monitoring & Optimization

```sh
# View recent runs
gh aw logs

# Filter by date range
gh aw logs --start-date -1w accessibility-review

# Cross-run analysis report
gh aw logs --format markdown --count 10

# Audit a specific workflow run
gh aw audit 123456

# Generate the lock file for a workflow
gh aw compile
```

> Lock files (`.lock.yml`) ensure reproducibility and auditability

---

# OpenTelemetry Distributed Tracing

```yaml
---
on:
  pull_request:
    types: [opened]
observability:
  otlp:
    endpoint: "${{ secrets.OTLP_ENDPOINT }}"
    headers: "${{ secrets.OTLP_HEADERS }}"
---
Analyze this pull request for security issues.
```

**Export spans** to Honeycomb, Grafana Tempo, or Sentry — every job emits setup and conclusion spans, MCP tool calls are correlated under the same trace ID

---

# Cache & Persistent Memory

## Speed up workflows and maintain context

```yaml
---
on:
  pull_request:
    types: [opened]
tools:
  cache-memory:  # AI remembers across runs
---
Review this PR with context from previous reviews:
- Check for repeated issues
- Track improvement trends
- Reference past discussions
```

**Benefits:** Faster builds + contextual AI analysis

---

# Playwright + Upload Assets

```yaml
---
on:
  pull_request:
    types: [ready_for_review]
tools:
  playwright:      # Headless browser automation
safe-outputs:
  create-issue:
  upload-asset:   # Attach screenshots to artifacts
---
Test the web application:
1. Navigate to the preview URL
2. Take screenshots of key pages
3. Check for visual regressions and responsive design
4. Create issue with findings and screenshots
```

**Use cases:** Visual regression, accessibility audits, E2E validation for SPAs

---

# Sanitized Context & Security

## Protect against prompt injection

```yaml
---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  actions: read
safe-outputs:
  add-comment:
---
# RECOMMENDED: Use sanitized context
Analyze this issue content (safely sanitized):
"${{ steps.sanitized.outputs.text }}"
```

**Auto-sanitization:** @mentions neutralized, bot triggers blocked, URIs filtered

---

# Security Architecture

## Multi-layered defense in depth

- Container isolation for all components
- Network firewall controls at every layer
- Minimal permissions by default
- Separation of concerns (agent, tools, outputs)

---

# Security Architecture Diagram

<pre class="mermaid" style="font-size: 11px;">
flowchart TB
    subgraph ActionJobVM["Action Job VM"]
        subgraph Sandbox1["Sandbox"]
            Agent["Agent Process"]
        end

        Proxy1["Proxy / Firewall"]
        Gateway["Gateway&lt;br/&gt;(mcpg)"]

        Agent --> Proxy1
        Proxy1 --> Gateway

        subgraph Sandbox2["Sandbox"]
            MCP["MCP Server"]
        end

        subgraph Sandbox3["Sandbox"]
            Skill["Skill"]
        end

        Gateway --> MCP
        Gateway --> Skill

        Proxy2["Proxy / Firewall"]
        Proxy3["Proxy / Firewall"]

        MCP --> Proxy2
        Skill --> Proxy3
    end

    Service1{{"Service"}}
    Service2{{"Service"}}

    Proxy2 --> Service1
    Proxy3 --> Service2
</pre>

---

# Security Layer 1: Coding Agent Sandbox

**Agent sandbox** — isolated container, read-only by default, limited system access

**Primary proxy/firewall** — filters outbound traffic, controls MCP Gateway access, enforces network allowlists

---

# Security Layer 2: MCP Gateway

**MCP Gateway (mcpg)** — central routing between agents and services

- Validates tool invocations and enforces permission boundaries
- Single point of control — no direct agent-to-service access
- Full audit trail for tool calls

---

# Security Layer 3: Tool Sandboxes

**MCP servers & skills** — each runs in its own container, non-root, dropped capabilities

**Secondary proxies** — egress filtering, domain allowlists, defense against data exfiltration

---

# Security Layer 4: Service Access

External services accessed only through proxies — multiple controls before reaching any service

> Defense in depth: if one layer is compromised, additional controls remain in place

---

# Security Features Summary

| Layer | Protection |
|---|---|
| **Containers** | VMs + sandboxes for agent, MCP servers, skills |
| **Network** | Proxy/firewall at every layer, domain allowlisting |
| **Permissions** | Read-only default, safe outputs for writes |
| **Supply Chain** | Pinned action SHAs, protected CI/CD files |
| **Integrity** | `min-integrity`, access & integrity metadata |
| **Monitoring** | Threat detection, audit logs, run analysis |

---

# Best Practices: Human in the Loop

**Manual Approval Gates:**

```yaml
---
on:
  issues:
    types: [labeled]
  manual-approval: production
safe-outputs:
  create-pull-request:
---
Analyze issue and create implementation PR
```

**Plan / Check / Act:** plan (read-only) → review & approve → execute via safe outputs

---

# Learn More About Security

**Documentation:**

- Security Best Practices Guide
- Threat Detection Configuration
- Network Configuration Reference
- Safe Outputs Reference

**Visit:** <https://github.github.com/gh-aw/introduction/architecture/>

---

# Getting Started (Agentically)

```sh
# Install GitHub Agentic Workflows extension
gh extension install github/gh-aw
gh aw init

# Agentic setup with Copilot CLI (optional)
npx --yes @github/copilot -i "activate https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install.md"
```

> Built with AI agents in mind from day 0

> Quick Start: <https://github.github.com/gh-aw/setup/quick-start/>

---
