# oh-my-opencode vs GitHub Agentic Workflows: Deep Research Comparison

**Date**: 2026-01-05  
**oh-my-opencode Repository**: https://github.com/code-yeongyu/oh-my-opencode  
**GitHub Agentic Workflows Repository**: https://github.com/github/gh-aw  
**Status**: Comprehensive Analysis

## Executive Summary

This document provides a deep research comparison between **oh-my-opencode** and **GitHub Agentic Workflows** (gh-aw). Both projects represent recent approaches to AI-powered software development automation, but they target fundamentally different use cases, execution environments, and philosophies.

### Key Differences at a Glance

| Aspect | oh-my-opencode | GitHub Agentic Workflows |
|--------|----------------|-------------------------|
| **Primary Use Case** | Local IDE development with AI agents | GitHub Actions CI/CD automation |
| **Execution Environment** | Local machine (OpenCode IDE) | GitHub Actions runners (cloud) |
| **Target User** | Individual developers, power users | Teams, organizations, repositories |
| **Security Model** | User-managed, local permissions | Strict mode, sandboxed, read-only default |
| **Multi-Agent** | Native multi-agent orchestration | Single-agent per workflow (event-driven) |
| **Tool Ecosystem** | LSP, AST-Grep, MCP servers | GitHub MCP, Bash allowlist, Playwright |
| **State Management** | Stateful, persistent across sessions | Stateless workflow runs |
| **Philosophy** | Provide extensive agent capabilities | "Safe by default" - minimize risk |

---

## 1. Architecture Overview

### 1.1 oh-my-opencode Architecture

```
Local Development Environment (OpenCode IDE)
├── Main Agent: Sisyphus (Claude Opus 4.5 High)
│   ├── Background Agents (async execution)
│   │   ├── Frontend UI/UX Engineer (Gemini 3 Pro)
│   │   ├── Librarian (Claude Sonnet 4.5)
│   │   └── Oracle (GPT 5.2 Medium)
│   └── Specialized Workers
│       └── Explore Agent (Grok Code)
├── Tool System (20+ tools in 6 categories)
│   ├── LSP Tools (11 tools - hover, definition, references, etc.)
│   ├── AST-Grep Tools (syntax-aware search/replace)
│   ├── File System Tools (grep, glob)
│   ├── Agent Delegation Tools
│   ├── Background Management Tools
│   └── Terminal/Interactive Tools (tmux)
├── Hook System
│   ├── PreToolUse - Context injection
│   ├── PostToolUse - Result processing
│   ├── UserPromptSubmit - Prompt enhancement
│   └── Stop - Session cleanup
└── MCP Servers (curated)
    ├── Exa (web search)
    ├── Context7 (official documentation)
    └── Grep.app (GitHub code search)
```

**Key Characteristics**:
- **Multi-agent by design**: Main agent delegates to specialized agents
- **Stateful execution**: Sessions persist, agents can continue work across restarts
- **Local control**: Full filesystem access, user permissions
- **IDE-integrated tools**: LSP and AST-Grep for code manipulation
- **Extensible**: Plugin architecture, hook system, custom agents

### 1.2 GitHub Agentic Workflows Architecture

```
GitHub Repository
├── .github/workflows/
│   ├── workflow.md (Natural language definition)
│   └── workflow.lock.yml (Compiled GitHub Actions)
├── Workflow Compiler (Go)
│   ├── Frontmatter Parser
│   ├── Schema Validator
│   ├── Security Analyzer
│   └── YAML Generator
└── GitHub Actions Runtime
    ├── Activation Job (read-only)
    │   ├── Text sanitization
    │   ├── Context preparation
    │   └── AI agent execution
    ├── Safe Output Jobs (write operations)
    │   ├── Validation & sanitization
    │   ├── GitHub API operations
    │   └── Threat detection
    └── Custom Actions
        ├── Setup action (environment)
        ├── MCP gateway (tool access)
        └── Network firewall (AWF)
```

**Key Characteristics**:
- **Single-agent per workflow**: Each workflow run executes one AI agent
- **Stateless execution**: Each run is independent, no persistent state
- **GitHub-native**: Deep integration with GitHub API, Issues, PRs, Discussions
- **Security-first**: Sandboxed containers, read-only default, explicit permissions
- **Event-driven**: Triggered by GitHub events (push, PR, issues, schedule)

---

## 2. Use Case Alignment

### 2.1 oh-my-opencode: Local Development Acceleration

**Ideal Scenarios**:

1. **Large-scale refactoring**: Use LSP and AST-Grep to rename symbols, restructure code, migrate APIs across 100+ files
2. **Multi-component development**: Frontend agent works on UI while backend agent implements API simultaneously
3. **Deep code exploration**: Librarian agent searches official docs, codebase history, and GitHub implementations
4. **Interactive debugging**: Oracle agent provides design feedback and debugging assistance in real-time
5. **Code implementation**: Todo Continuation Enforcer ensures tasks complete; Comment Checker prevents unnecessary comments

**Example Workflow**:
```
User: "ultrawork - Implement OAuth authentication system"

1. Sisyphus (Main): Creates implementation plan
2. Librarian (Background): Researches OAuth best practices, finds implementations
3. Frontend Engineer (Background): Implements login UI
4. Sisyphus (Main): Implements backend OAuth flow
5. Oracle (Consultation): Reviews security implications
6. Explore (Search): Finds similar patterns in codebase
7. Sisyphus (Main): Integrates all components, runs tests
8. Todo Enforcer: Ensures all subtasks complete before stopping
```

**Key Benefits**:
- Multiple agents work in parallel
- Context management handled automatically
- IDE-integrated tools (LSP) available to agents
- Agents continue until task completion
- Code follows project conventions

### 2.2 GitHub Agentic Workflows: Repository Automation

**Ideal Scenarios**:

1. **Issue triage**: Auto-label, prioritize, and respond to new issues
2. **PR automation**: Review code, check security, validate tests, create reviews
3. **Scheduled reports**: Daily/weekly repository health, metrics, trend analysis
4. **Documentation sync**: Keep docs in sync with code changes
5. **Security monitoring**: Scan for vulnerabilities, create issues for findings
6. **Release management**: Generate changelogs, create releases, notify teams

**Example Workflow**:
```yaml
---
description: PR Review and Security Check
on:
  pull_request:
    types: [opened, synchronize]
permissions:
  contents: read
  pull-requests: write
engine: copilot
tools:
  github:
    toolsets: [repos, pull_requests]
safe-outputs:
  create-review:
  create-issue:
strict: true
---

# PR Security Review

Review PR #${{ github.event.pull_request.number }}.

1. Analyze code changes for security issues
2. Check for secret leaks, SQL injection, XSS vulnerabilities
3. Create review with findings
4. If critical issues found, create blocking issue

Use GitHub MCP to access PR files and repository context.
```

**Key Benefits**:
- Runs automatically on GitHub events
- Sandboxed, secure execution
- Supports multi-user repository automation
- Audit trail via GitHub Actions logs
- Integration with Issues, PRs, Discussions

---

## 3. Multi-Agent Coordination

### 3.1 oh-my-opencode: Native Multi-Agent Orchestration

**Architecture**: Main agent + specialized background agents

**Agent Roster**:
| Agent | Model | Role | Execution Mode |
|-------|-------|------|----------------|
| **Sisyphus** | Claude Opus 4.5 High | Main implementation, coordination | Foreground |
| **Oracle** | GPT 5.2 Medium | Design, debugging, problem-solving | On-demand |
| **Frontend Engineer** | Gemini 3 Pro | UI/UX implementation | Background |
| **Librarian** | Claude Sonnet 4.5 | Documentation research, codebase exploration | Background |
| **Explore** | Grok Code | Fast codebase grep/search | Background |

**Coordination Mechanisms**:

1. **Background Execution**: Agents run asynchronously while main agent works
   ```typescript
   // Sisyphus delegates to background agent
   await backgroundAgent('frontend-ui-ux-engineer', {
     task: 'Implement login form UI',
     context: currentFiles,
     notify: true
   });
   // Main agent continues working on backend
   ```

2. **Agent Delegation**: Main agent can call specialized agents for expertise
   ```typescript
   // Ask Oracle for design advice
   const advice = await callAgent('oracle', 'How should I structure this authentication flow?');
   ```

3. **Context Sharing**: Agents share workspace, files, and project context automatically

4. **Hook-Based Communication**: Agents communicate via lifecycle hooks
   - `PreToolUse`: Inject context before tool execution
   - `PostToolUse`: Process results after tool execution
   - `UserPromptSubmit`: Enhance prompts with shared knowledge

**Todo Continuation Enforcer**: Critical feature that prevents agents from quitting halfway
- Monitors agent progress
- Forces continuation if todos remain incomplete
- Ensures work completes before session ends
- "This is what keeps Sisyphus rolling that boulder"

**Benefits**:
- Parallel execution across multiple agents
- Specialized expertise (design, frontend, backend, research)
- Context management handled automatically
- Agent coordination and delegation

### 3.2 GitHub Agentic Workflows: Event-Driven Single-Agent

**Architecture**: One agent per workflow run, coordination via GitHub events

**Coordination Pattern**:
```
Workflow 1 (Issue Responder)
  ↓ Creates Issue
Workflow 2 (Issue Analyzer)
  ↓ Labels Issue
Workflow 3 (Task Assigner)
  ↓ Updates Issue
Workflow 4 (Progress Tracker)
  ↓ Creates Report
```

**Example Multi-Workflow Coordination**:

```yaml
# workflow-1-creator.md
on:
  issues:
    types: [opened]
safe-outputs:
  update-issue:
    labels: ["needs-analysis"]

# workflow-2-analyzer.md
on:
  issues:
    types: [labeled]
safe-outputs:
  update-issue:
    labels: ["high-priority"]

# workflow-3-tracker.md
on:
  schedule:
    - cron: '0 6 * * *'
safe-outputs:
  create-discussion:
```

**Limitations of Current Model**:
- No direct workflow-to-workflow communication
- No shared state between workflows
- No centralized orchestration
- Difficult to track multi-workflow campaigns

**Potential Future Enhancement**: Campaign Orchestrator Pattern (inspired by Gastown)

```markdown
# campaign-orchestrator.md
---
on: workflow_dispatch
inputs:
  campaign_id: string
tools:
  github:
    toolsets: [issues, workflows]
safe-outputs:
  dispatch-workflow:
  update-issue:
---

## Campaign Orchestrator

Coordinate multiple worker workflows:

1. Find issues with label `campaign:{{ inputs.campaign_id }}`
2. Create worker issues for each task
3. Dispatch worker workflows with assignments
4. Monitor progress (poll every 5 minutes)
5. Detect blockers (no progress in 30+ minutes)
6. Escalate to team when needed
7. Create completion report
```

**Benefits of Event-Driven Model**:
- Security: Each workflow runs independently with explicit permissions
- Auditability: Clear trail of which workflow did what
- Simplicity: No complex coordination protocol
- GitHub-native: Uses Issues/PRs as communication medium

---

## 4. Tool Ecosystems

### 4.1 oh-my-opencode: Professional IDE Tools

**Tool Categories** (20+ tools in 6 categories):

#### Category 1: LSP Tools (11 tools)
IDE-integrated code intelligence via Language Server Protocol:

| Tool | Purpose | Example |
|------|---------|---------|
| `lsp_hover` | Get type info, documentation | Hover over variable to see type |
| `lsp_definition` | Jump to definition | Navigate to function definition |
| `lsp_references` | Find all references | Where is this function used? |
| `lsp_implementation` | Find implementations | Which classes implement this interface? |
| `lsp_rename` | Safe symbol rename | Rename variable across project |
| `lsp_code_action` | Quick fixes, refactorings | Extract method, add import |
| `lsp_document_symbols` | Get all symbols in file | List functions, classes, variables |
| `lsp_workspace_symbols` | Search symbols globally | Find symbol across entire codebase |
| `lsp_diagnostics` | Get errors/warnings | Show compilation errors |
| `lsp_format_document` | Format code | Auto-format file |
| `lsp_format_selection` | Format code region | Format selected lines |

**Key Advantage**: Agents access full IDE code intelligence capabilities

#### Category 2: AST-Grep Tools
Syntax-aware search and replace for 25+ languages:

```typescript
// Find all API calls matching pattern
astgrep_search({
  pattern: "fetch($URL, { method: 'POST', body: $BODY })",
  language: "typescript"
});

// Replace all with new API
astgrep_replace({
  from: "fetch($URL, { method: 'POST', body: $BODY })",
  to: "apiClient.post($URL, $BODY)",
  language: "typescript"
});
```

**Key Advantage**: Structural refactoring that's guaranteed syntax-correct (no regex fragility)

#### Category 3: File System Tools
- `grep`: Search file contents (regex)
- `glob`: Find files by pattern
- `contextual_grep`: Smart search with relevance ranking

#### Category 4: Agent Delegation Tools
- `call_agent`: Synchronous agent call (wait for response)
- `background_agent`: Async agent spawn (parallel work)
- `notify_agent`: Send message to running agent

#### Category 5: Background Management Tools
- `list_background_tasks`: See running agents
- `wait_for_task`: Block until agent completes
- `cancel_task`: Stop background agent

#### Category 6: Terminal/Interactive Tools
- `tmux`: Run commands in persistent terminal
- `interactive_shell`: Interactive command execution

**Curated MCP Servers**:
- **Exa**: Web search for documentation, examples
- **Context7**: Official documentation lookup
- **Grep.app**: Search 50M+ GitHub repositories

### 4.2 GitHub Agentic Workflows: GitHub-Native Tools

**Tool Categories**:

#### Category 1: GitHub MCP Server
Comprehensive GitHub API access via Model Context Protocol:

| Toolset | Tools Included | Example Operations |
|---------|---------------|-------------------|
| `default` | Core GitHub operations | Create issues, PRs, comments |
| `repos` | Repository management | List files, get content, search code |
| `issues` | Issue operations | Create, update, label, assign issues |
| `pull_requests` | PR operations | Create, review, merge, get diff |
| `discussions` | Discussion management | Create, comment, resolve discussions |
| `actions` | Workflow operations | List runs, get logs, download artifacts |

**Configuration**:
```yaml
tools:
  github:
    mode: remote  # or "local" for Docker
    toolsets: [default, repos, issues, pull_requests]
    allowed: ["issue_read", "add_issue_comment"]  # Explicit allowlist
```

**Key Advantage**: Native GitHub integration with fine-grained permissions

#### Category 2: Bash Tools (Allowlist)
Explicit command allowlisting for security:

```yaml
tools:
  bash: 
    - "echo"
    - "git status"
    - "gh issue list"
    - "jq"
```

**Security**: Only listed commands can execute; wildcards forbidden in strict mode

#### Category 3: Playwright Tools
Browser automation for web scraping, testing, accessibility:

```yaml
tools:
  playwright:
    allowed_domains: ["github.com", "docs.github.com"]
```

**Features**:
- Containerized browser (Chromium/Firefox/Safari)
- Domain-restricted network access
- Accessibility analysis
- Visual testing
- Screenshot/PDF generation

#### Category 4: Safe Outputs (GitHub API Write Operations)
Sanitized write operations to GitHub:

| Safe Output | Purpose | Validation |
|------------|---------|-----------|
| `create-issue` | Create GitHub issue | Title/body sanitization, XSS prevention |
| `update-issue` | Update existing issue | Field validation, authorization check |
| `create-pull-request` | Create PR | Branch validation, conflict detection |
| `create-discussion` | Create discussion | Category validation, content sanitization |
| `add-comment` | Add comment | Context validation, mention safety |
| `create-review` | Create PR review | Review state validation, comment safety |

**Security Model**: AI runs with read-only permissions; write operations in separate validated jobs

**Key Advantage**: Secure-by-default with automatic sanitization and validation

---

## 5. Security Models

### 5.1 oh-my-opencode: User-Managed Security

**Philosophy**: Developer-local environment with user control

**Security Characteristics**:

| Aspect | Implementation | Risk Level |
|--------|---------------|-----------|
| **Execution Environment** | Local user machine | User-managed |
| **Permissions** | Full user permissions (filesystem, network) | User-controlled environment |
| **Network Access** | Unrestricted (can access any URL) | User-controlled |
| **Code Execution** | Direct shell command execution | User-trusted |
| **State Management** | Persistent sessions, file modifications | User-reviewed |
| **MCP Servers** | User-installed, local execution | User-vetted |

**Trust Model**: 
- User trusts the AI agents they configure
- User reviews code changes before committing
- Local execution = local consequences
- No sandboxing (full system access)

**Safety Features**:
1. **Comment Checker**: Prevents excessive AI-generated comments (code should look human-written)
2. **Todo Continuation Enforcer**: Ensures complete work (prevents half-finished changes)
3. **Hook System**: Allows custom security checks via `PreToolUse`, `PostToolUse` hooks
4. **Agent Specialization**: Limits blast radius (Frontend agent doesn't touch backend)

**Best Practices**:
- Review agent output before committing
- Use version control (git) for rollback
- Configure agents with appropriate model choices
- Test in safe environments first

**Risk Profile**: **High trust, high power** - Suitable for experienced developers who want maximum agent capabilities

### 5.2 GitHub Agentic Workflows: Defense-in-Depth

**Philosophy**: Secure-by-default, zero-trust model

**Security Layers**:

```
Layer 1: Compile-Time Validation
  ├── Schema validation (frontmatter correctness)
  ├── Permission validation (no excessive permissions)
  ├── Expression sanitization (prevent injection)
  ├── Tool allowlist enforcement
  └── Supply chain checks (SHA-pinned actions)

Layer 2: Sandboxed Execution
  ├── Isolated GitHub Actions container
  ├── Read-only filesystem (except /tmp)
  ├── Network firewall (domain allowlist)
  └── Limited token scope

Layer 3: Safe Outputs
  ├── Read/write separation (agent reads, job writes)
  ├── Content sanitization (XSS, injection prevention)
  ├── Validation checks (schema, permissions)
  └── Threat detection (AI-powered scanning)

Layer 4: Runtime Monitoring
  ├── Workflow logs (full audit trail)
  ├── Cost monitoring (token usage tracking)
  ├── Timeout enforcement (max 6 hours)
  └── Human approval gates (critical operations)
```

**Strict Mode Enforcement**:
```yaml
strict: true
permissions:
  contents: read    # Read-only default
network:
  allowed: [defaults]  # Secure network defaults
```

**Strict mode enforces**:
1. Blocks write permissions (`contents:write`, `issues:write`) - use safe-outputs instead
2. Requires explicit network configuration (no unrestricted access)
3. Refuses wildcard `*` in network domains
4. Requires network config for custom MCP containers
5. Enforces action pinning to commit SHAs
6. Refuses deprecated frontmatter fields

**Threat Detection** (automatic with safe outputs):
```yaml
safe-outputs:
  create-pull-request:
  threat-detection:
    enabled: true
    prompt: "Focus on SQL injection and secret leaks"
```

**Analysis includes**:
- Prompt injection attempts
- Secret leakage (API keys, tokens, passwords)
- Malicious code patterns (SQL injection, XSS)
- Supply chain attacks (suspicious dependencies)

**Fork Protection**:
```yaml
on:
  pull_request:
    forks: ["trusted-org/*"]  # Explicit fork allowlist
    # Default: blocks all forks
```

**Key Security Features**:

| Feature | Implementation | Benefit |
|---------|---------------|---------|
| **Read-only default** | `permissions: contents: read` | AI cannot modify code |
| **Safe outputs** | Separate validation jobs | Write operations sanitized |
| **Network isolation** | Domain allowlist + firewall | Prevent data exfiltration |
| **Sandboxing** | GitHub Actions containers | Limited blast radius |
| **SHA pinning** | `actions/checkout@abc123...` | Supply chain security |
| **Expression limits** | Max 120 chars per expression | Prevent injection attacks |
| **Context sanitization** | `needs.activation.outputs.text` | Neutralized @mentions, safe XML |
| **Audit logging** | GitHub Actions logs | Full traceability |

**Risk Profile**: **Low trust, controlled power** - Suitable for team/org automation with strict security requirements

---

## 6. State Management and Persistence

### 6.1 oh-my-opencode: Stateful Sessions

**State Model**: Persistent across agent sessions

**Session Characteristics**:
- Workspace state preserved between invocations
- Agents can continue work across restarts
- Background agents maintain context
- Todo lists persist until complete

**Example Session Flow**:
```
Session 1 (Day 1, 9am):
  - User: "Implement authentication system"
  - Sisyphus: Creates plan, delegates frontend to background agent
  - User: Closes IDE for lunch

Session 2 (Day 1, 2pm):
  - User: Opens IDE
  - Sisyphus: Resumes from last checkpoint
  - Frontend agent: Reports UI complete
  - Sisyphus: Continues with backend integration

Session 3 (Day 2, 9am):
  - User: Opens IDE
  - Todo Enforcer: Detects incomplete tests
  - Sisyphus: Completes test suite
  - All todos complete → Session ends
```

**State Storage**:
- Workspace files (modified by agents)
- Agent context (conversation history)
- Todo tracking (incomplete tasks)
- Background agent status

**Benefits**:
- Long-running tasks span multiple sessions
- No work lost on interruption
- Agents pick up where they left off
- User controls when work is "done"

### 6.2 GitHub Agentic Workflows: Stateless Execution

**State Model**: Each workflow run is independent

**Run Characteristics**:
- Fresh environment for each run
- No state preserved between runs
- Each trigger creates new execution
- Timeout limit (max 6 hours per run)

**Example Workflow Sequence**:
```
Run 1 (PR opened):
  - Trigger: pull_request.opened
  - Agent: Reviews PR files
  - Output: Creates review comment
  - State: Discarded

Run 2 (PR updated):
  - Trigger: pull_request.synchronize
  - Agent: Reviews new changes (no memory of Run 1)
  - Output: Creates new review
  - State: Discarded

Run 3 (Daily report):
  - Trigger: schedule (cron)
  - Agent: Analyzes all PRs (including above)
  - Output: Creates discussion with metrics
  - State: Discarded
```

**State Communication** (via GitHub API):
- Issues: Track work items, progress
- PR comments: Record review history
- Discussions: Store reports, summaries
- Labels: Coordinate workflow handoffs
- Artifacts: Pass data between jobs (within run)

**Potential Future Enhancement**: Checkpoint/Resume Pattern

```yaml
# Long-running workflow with checkpoints
on: workflow_dispatch
inputs:
  checkpoint_id:
    description: 'Resume from checkpoint'
    required: false

# In workflow body:
# 1. Check for existing checkpoint (issue or artifact)
# 2. Load state if checkpoint exists
# 3. Execute only incomplete steps
# 4. Save checkpoint after each phase
# 5. On failure, next run resumes from last checkpoint
```

**Benefits of Stateless Model**:
- Predictable, reproducible runs
- Transparent behavior (no hidden state)
- Scales horizontally (parallel runs)
- Audit trail via GitHub Actions logs

**Trade-offs**:
- Must re-do work on failure
- Long workflows inefficient on retry
- No "memory" between runs

---

## 7. Configuration and Extensibility

### 7.1 oh-my-opencode: Plugin Architecture

**Configuration File**: `~/.config/opencode/opencode.json` + `.opencode/oh-my-opencode.json`

**Configuration Structure**:
```json
{
  "plugin": [
    "oh-my-opencode",
    "opencode-antigravity-auth@1.1.2",
    "opencode-openai-codex-auth@4.1.1"
  ],
  "providers": {
    "anthropic": { "models": ["claude-opus-4.5-high"] },
    "google": { "models": ["gemini-3-pro-high"] },
    "openai": { "models": ["gpt-5.2-medium"] }
  }
}
```

**oh-my-opencode Configuration**:
```json
{
  "google_auth": true,
  "agents": {
    "sisyphus": {
      "model": "claude-opus-4.5-high",
      "role": "Main implementation agent"
    },
    "frontend-ui-ux-engineer": {
      "model": "google/gemini-3-pro-high",
      "background": true
    },
    "oracle": {
      "model": "openai/gpt-5.2-medium",
      "on_demand": true
    }
  },
  "hooks": {
    "preToolUse": "./hooks/inject-context.ts",
    "postToolUse": "./hooks/process-results.ts"
  },
  "mcps": {
    "exa": { "enabled": true },
    "context7": { "enabled": true },
    "grep-app": { "enabled": true }
  },
  "lsp": {
    "typescript": { "enabled": true },
    "python": { "enabled": true },
    "rust": { "enabled": true }
  }
}
```

**Extensibility Points**:

1. **Custom Agents**: Add specialized agents for domain-specific tasks
   ```json
   "agents": {
     "security-auditor": {
       "model": "claude-sonnet-4.5",
       "role": "Security code review"
     }
   }
   ```

2. **Custom Hooks**: Inject logic at key lifecycle points
   ```typescript
   // hooks/preToolUse.ts
   export async function preToolUse(tool, args, context) {
     // Inject additional context before tool execution
     if (tool === 'lsp_code_action') {
       args.context = await loadProjectContext();
     }
     return args;
   }
   ```

3. **Custom MCP Servers**: Add new tool capabilities
   ```json
   "mcps": {
     "custom-database": {
       "command": "node",
       "args": ["./mcp-servers/database-server.js"],
       "env": { "DB_URL": "..." }
     }
   }
   ```

4. **Custom LSP Servers**: Add language support
   ```json
   "lsp": {
     "zig": {
       "command": "zls",
       "enabled": true
     }
   }
   ```

**Magic Word Feature**: `ultrawork` or `ulw`
- Include in prompt to activate all features automatically
- Parallel agents, background tasks, deep exploration
- Relentless execution until completion
- Agent figures out coordination automatically

### 7.2 GitHub Agentic Workflows: Frontmatter Configuration

**Configuration File**: Workflow markdown frontmatter (YAML)

**Workflow Structure**:
```yaml
---
# Metadata
description: "What this workflow does"
labels: ["automation", "security"]

# GitHub Actions Standard
on:
  pull_request:
    types: [opened, synchronize]
permissions:
  contents: read
  pull-requests: write
runs-on: ubuntu-latest
timeout-minutes: 30

# AI Engine
engine: copilot
engine:
  model: gpt-4.5-preview

# Tools
tools:
  github:
    mode: remote
    toolsets: [default, repos, pull_requests]
    allowed: ["issue_read", "add_issue_comment"]
  bash: ["gh", "jq", "git"]
  playwright:
    allowed_domains: ["github.com"]

# Security
strict: true
network:
  allowed: [defaults, python]

# Safe Outputs (write operations)
safe-outputs:
  create-review:
  create-issue:
  threat-detection:
    enabled: true

# Access Control
roles: [admin, maintainer, write]
---

# Workflow Instructions (Natural Language)

Review the PR and check for security issues...
```

**Extensibility Points**:

1. **Custom Engines**: Add new AI providers
   ```go
   // pkg/workflow/custom_engine.go
   func NewCustomEngine(config EngineConfig) Engine {
     // Implement Engine interface
   }
   ```

2. **Custom MCP Servers**: Add tool capabilities
   ```yaml
   tools:
     custom-database:
       mcp:
         container: "myorg/db-mcp:latest@sha256:abc..."
         network:
           allowed: ["db.company.com"]
       allowed: ["query", "schema"]
   ```

3. **Shared Instructions**: Import reusable components
   ```yaml
   imports:
     - shared/jqschema.md        # JSON schema patterns
     - shared/python-dataviz.md  # Data visualization helpers
     - shared/reporting.md       # Report formatting
   ```

4. **Custom Actions**: Extend GitHub Actions
   ```yaml
   steps:
     - uses: ./.github/actions/custom-security-scan
       with:
         severity: high
   ```

**Schema Validation**: All frontmatter validated against JSON schemas at compile time
- Catches configuration errors early
- Provides autocomplete in IDEs
- Ensures consistency across workflows

---

## 8. Developer Experience

### 8.1 oh-my-opencode: Power User Paradise

**Installation**:
```bash
# Interactive installer
bunx oh-my-opencode install

# Or let an agent do it
# Paste into OpenCode: "Install and configure oh-my-opencode"
```

**Daily Workflow**:
```bash
# Start OpenCode IDE
opencode

# In chat:
User: "ultrawork - Refactor authentication to use OAuth2"

# Agents activate:
- Sisyphus: Plans refactoring strategy
- Librarian: Researches OAuth2 best practices (background)
- Frontend Engineer: Updates login UI (background)
- Sisyphus: Refactors backend authentication
- Oracle: Reviews security implications (on-demand)
- Explore: Finds similar OAuth patterns in codebase
- Todo Enforcer: Ensures all subtasks complete

# Output: Code passing validation, tests meeting coverage targets, documentation generated
```

**Key Features**:
- **Zero config**: Works out of box with sensible defaults
- **Battery included**: All tools, agents, hooks pre-configured
- **Magic word**: Just type `ultrawork` for full power
- **Real-time**: See agents working in parallel
- **Interactive**: Ask questions, get design feedback
- **Persistent**: Work continues across sessions

**Learning Curve**: 
- Beginner: Use magic word `ultrawork` (zero learning)
- Intermediate: Configure agents, models (30 minutes)
- Advanced: Custom agents, hooks, MCPs (1-2 hours)

### 8.2 GitHub Agentic Workflows: Team Automation Made Safe

**Installation**:
```bash
# Install gh-aw CLI extension
gh extension install github/gh-aw

# Or use npm
npm install -g gh-aw
```

**Workflow Creation**:
```bash
# Create workflow from template
gh aw create issue-responder.md

# Edit frontmatter and instructions
vim .github/workflows/issue-responder.md

# Compile to GitHub Actions YAML
gh aw compile issue-responder.md

# Test locally (dry-run)
gh aw compile --dry-run issue-responder.md

# Commit and push
git add .github/workflows/issue-responder.*
git commit -m "feat: Add issue responder workflow"
git push
```

**Daily Workflow**:
```bash
# Workflow runs automatically on GitHub events
# - No manual invocation needed
# - Triggered by issues, PRs, schedule, etc.

# Monitor workflow runs
gh aw logs

# Audit specific run
gh aw audit 123456

# Download detailed logs
gh aw logs --run-id 123456 --download

# Inspect MCP servers
gh aw mcp list
gh aw mcp inspect issue-responder
```

**Key Features**:
- **Declarative**: Write in natural language markdown
- **Validated**: Compile-time checks catch errors
- **Secure**: Strict mode enforces safety by default
- **Auditable**: Full logs in GitHub Actions
- **Team-wide**: Automation for entire organization
- **Event-driven**: No manual triggers needed

**Learning Curve**:
- Beginner: Use templates, basic frontmatter (15 minutes)
- Intermediate: Custom workflows, tools config (1 hour)
- Advanced: Multi-workflow orchestration, custom engines (3-4 hours)

---

## 9. Performance and Scale

### 9.1 oh-my-opencode: Local Performance

**Resource Usage**:
- **CPU**: Depends on agent count (2-5 agents typical)
- **Memory**: 2-8 GB (depends on language servers)
- **Network**: API calls to LLM providers (Claude, GPT, Gemini)
- **Storage**: Local workspace + IDE state

**Performance Characteristics**:
| Metric | Typical Value | Notes |
|--------|--------------|-------|
| **Agent spawn time** | 1-3 seconds | Local process creation |
| **Tool execution** | 10-100ms | LSP, grep, file operations |
| **LLM response** | 2-10 seconds | Network latency to provider |
| **Background agents** | 2-5 parallel | Limited by user machine |
| **Session persistence** | Instant | Local state, no cold start |

**Scalability**:
- **Horizontal**: Run multiple OpenCode instances (different projects)
- **Vertical**: Higher-spec machine (more CPU/RAM) = more background agents
- **Cost**: Pay for LLM API usage only (no CI/CD minutes)

**Optimization Tips**:
- Use faster models for background agents (Gemini Flash)
- Use slower, smarter models for main agent (Opus 4.5)
- Cache context with Librarian agent
- Leverage LSP for instant code intelligence

### 9.2 GitHub Agentic Workflows: Cloud Scale

**Resource Usage**:
- **Compute**: GitHub Actions runners (2 vCPU, 7 GB RAM standard)
- **Storage**: Workflow artifacts (retention configurable)
- **Network**: GitHub API + MCP server calls + engine network access
- **Cost**: GitHub Actions minutes + LLM API usage

**Performance Characteristics**:
| Metric | Typical Value | Notes |
|--------|--------------|-------|
| **Cold start** | 30-60 seconds | Container provisioning, setup action |
| **Warm start** | 10-20 seconds | Cached container |
| **Tool execution** | 100ms-2s | Network call to GitHub API/MCP |
| **LLM response** | 5-30 seconds | Copilot/Claude/Codex latency |
| **Max workflow time** | 6 hours | GitHub Actions limit |
| **Parallelism** | Unlimited | Multiple workflows run concurrently |

**Scalability**:
- **Horizontal**: Unlimited parallel workflows (GitHub manages)
- **Vertical**: Use higher-spec runners (self-hosted or GitHub)
- **Cost**: Pay for Actions minutes + LLM tokens

**Optimization Tips**:
- Use caching for dependencies (setup-node, setup-python)
- Enable concurrency limits to avoid rate limits
- Use workflow artifacts for data passing
- Set appropriate timeout-minutes (avoid wasted time)

**Cost Monitoring**:
```bash
# Track token usage and costs
gh aw logs --metrics

# Example output:
# Workflow: pr-reviewer
# Run: 123456
# Duration: 3m 45s
# Tokens: 15,234 (input: 12,000, output: 3,234)
# Cost: $0.15 (estimated)
```

---

## 10. Comparison Matrix

### 10.1 Feature Comparison

| Feature | oh-my-opencode | GitHub Agentic Workflows |
|---------|----------------|-------------------------|
| **Multi-Agent** | ✅ Native (5+ agents) | ⚠️ Event-driven (1 per run) |
| **Background Execution** | ✅ Yes (async agents) | ❌ No (stateless runs) |
| **Persistent State** | ✅ Yes (sessions) | ❌ No (each run fresh) |
| **LSP Integration** | ✅ Yes (11 tools) | ❌ No |
| **AST-Grep** | ✅ Yes (25+ languages) | ❌ No |
| **GitHub API Access** | ⚠️ Via MCP (manual) | ✅ Native (GitHub MCP) |
| **Security Sandboxing** | ❌ No (user permissions) | ✅ Yes (containers) |
| **Strict Mode** | ❌ No | ✅ Yes |
| **Safe Outputs** | ❌ No | ✅ Yes (validated writes) |
| **Threat Detection** | ❌ No | ✅ Yes (AI-powered) |
| **Network Isolation** | ❌ No (unrestricted) | ✅ Yes (domain allowlist) |
| **Fork Protection** | N/A | ✅ Yes |
| **Audit Logging** | ⚠️ Local logs | ✅ GitHub Actions |
| **Event-Driven** | ❌ No (manual) | ✅ Yes (GitHub events) |
| **Team Collaboration** | ❌ Individual tool | ✅ Repository-wide |
| **IDE Quality Tools** | ✅ Yes (LSP, AST) | ❌ Limited |
| **Interactive Mode** | ✅ Yes (tmux, chat) | ❌ No (batch only) |
| **Cost Model** | LLM API only | LLM API + Actions minutes |

### 10.2 Use Case Suitability

| Use Case | oh-my-opencode | GitHub Agentic Workflows | Winner |
|----------|----------------|-------------------------|--------|
| **Local development** | ⭐⭐⭐⭐⭐ | ⭐ | oh-my-opencode |
| **Code refactoring** | ⭐⭐⭐⭐⭐ | ⭐⭐ | oh-my-opencode |
| **Interactive debugging** | ⭐⭐⭐⭐⭐ | ⭐ | oh-my-opencode |
| **Multi-component dev** | ⭐⭐⭐⭐⭐ | ⭐⭐ | oh-my-opencode |
| **Issue automation** | ⭐⭐ | ⭐⭐⭐⭐⭐ | gh-aw |
| **PR automation** | ⭐⭐ | ⭐⭐⭐⭐⭐ | gh-aw |
| **Scheduled reports** | ⭐ | ⭐⭐⭐⭐⭐ | gh-aw |
| **Security scanning** | ⭐⭐ | ⭐⭐⭐⭐⭐ | gh-aw |
| **Team workflows** | ⭐ | ⭐⭐⭐⭐⭐ | gh-aw |
| **Compliance/audit** | ⭐ | ⭐⭐⭐⭐⭐ | gh-aw |
| **CI/CD integration** | ⭐⭐ | ⭐⭐⭐⭐⭐ | gh-aw |
| **Long-running tasks** | ⭐⭐⭐⭐⭐ | ⭐⭐ | oh-my-opencode |

---

## 11. Cross-Pollination Opportunities

### 11.1 What gh-aw Could Learn from oh-my-opencode

#### 1. Multi-Agent Coordination Pattern

**oh-my-opencode Pattern**: Main agent + specialized background agents

**Potential gh-aw Enhancement**:
```yaml
# Multi-agent workflow (future concept)
agents:
  coordinator:
    engine: copilot
    role: main
  
  security-scanner:
    engine: claude
    role: background
    task: "Scan for security issues"
  
  documentation-writer:
    engine: gpt-4
    role: background
    task: "Update documentation"

# Coordinator waits for background agents to complete
# Then integrates results
```

**Benefits**: Parallel execution, specialized expertise, faster completion

#### 2. LSP Tool Integration

**oh-my-opencode Pattern**: 11 LSP tools for professional code manipulation

**Potential gh-aw Enhancement**:
```yaml
tools:
  lsp:
    languages: [typescript, python, rust]
    enabled: [hover, definition, references, rename]
```

**Benefits**: AI agents can navigate code like professional developers

#### 3. AST-Aware Refactoring

**oh-my-opencode Pattern**: AST-Grep for syntax-correct transformations

**Potential gh-aw Enhancement**:
```yaml
tools:
  ast-grep:
    languages: [typescript, python, rust, go]
```

**Benefits**: Guaranteed syntax-correct refactoring at scale

#### 4. Persistent State / Checkpoint Pattern

**oh-my-opencode Pattern**: Sessions persist across restarts

**Potential gh-aw Enhancement**:
```yaml
state:
  storage: github-issues  # or artifacts
  checkpoint-interval: 5m
  resume-on-failure: true
```

**Benefits**: Long-running workflows survive failures, resume from last checkpoint

#### 5. Magic Word / Auto-Configuration

**oh-my-opencode Pattern**: `ultrawork` activates all features automatically

**Potential gh-aw Enhancement**:
```yaml
mode: auto  # Automatically configure tools, permissions based on task
# Or detect from workflow instructions
```

**Benefits**: Lower barrier to entry, easier for beginners

### 11.2 What oh-my-opencode Could Learn from gh-aw

#### 1. Security-by-Default Model

**gh-aw Pattern**: Strict mode, read-only default, safe outputs

**Potential oh-my-opencode Enhancement**:
```json
"security": {
  "strict_mode": true,
  "read_only_default": true,
  "require_approval": ["file_delete", "git_push"]
}
```

**Benefits**: Safer for production use, prevents accidental damage

#### 2. Safe Output Validation

**gh-aw Pattern**: Separate read and write operations, validate all writes

**Potential oh-my-opencode Enhancement**:
```json
"safe_outputs": {
  "git_commit": {
    "validate_message": true,
    "require_approval": true
  },
  "file_write": {
    "sanitize_content": true
  }
}
```

**Benefits**: Prevent accidental commits, validate changes before applying

#### 3. Threat Detection

**gh-aw Pattern**: AI-powered analysis of agent output

**Potential oh-my-opencode Enhancement**:
```json
"threat_detection": {
  "enabled": true,
  "scan_for": ["secret_leak", "malicious_code", "prompt_injection"]
}
```

**Benefits**: Detect malicious behavior, prevent security issues

#### 4. Network Isolation

**gh-aw Pattern**: Domain allowlist, network firewall

**Potential oh-my-opencode Enhancement**:
```json
"network": {
  "allowed": ["api.github.com", "docs.python.org"],
  "blocked": ["*"]  # Default deny
}
```

**Benefits**: Prevent data exfiltration, limit attack surface

#### 5. Audit Logging and Cost Tracking

**gh-aw Pattern**: Detailed logs, token usage metrics

**Potential oh-my-opencode Enhancement**:
```bash
# Track costs per agent
opencode metrics --agent sisyphus
# Tokens: 45,000 (input: 40,000, output: 5,000)
# Cost: $1.23 (Claude Opus)
# Duration: 15m 30s
```

**Benefits**: Understand costs, optimize agent usage

---

## 12. Complementary Use Cases

### 12.1 Hybrid Workflow: oh-my-opencode + gh-aw

**Scenario**: Large-scale feature development with team coordination

**Workflow**:

1. **Local Development** (oh-my-opencode):
   ```
   Developer uses oh-my-opencode locally:
   - Sisyphus: Implements feature
   - Frontend Engineer: Builds UI (background)
   - Librarian: Researches best practices (background)
   - Oracle: Reviews design decisions (on-demand)
   - Output: Complete feature implementation, tests, docs
   ```

2. **Create Pull Request** (manual):
   ```bash
   git add .
   git commit -m "feat: Add OAuth2 authentication"
   git push origin feature/oauth2
   gh pr create
   ```

3. **PR Review** (gh-aw):
   ```yaml
   # .github/workflows/pr-reviewer.md
   ---
   on:
     pull_request:
       types: [opened, synchronize]
   permissions:
     contents: read
     pull-requests: write
   engine: copilot
   tools:
     github:
       toolsets: [repos, pull_requests]
   safe-outputs:
     create-review:
   strict: true
   ---
   
   Review PR for:
   - Security issues (SQL injection, XSS, secret leaks)
   - Code quality (complexity, readability)
   - Test coverage
   - Documentation completeness
   
   Create review with findings.
   ```

4. **Continuous Monitoring** (gh-aw):
   ```yaml
   # .github/workflows/daily-report.md
   ---
   on:
     schedule:
       - cron: '0 9 * * *'
   safe-outputs:
     create-discussion:
   ---
   
   Generate daily report:
   - Open PRs status
   - Security findings
   - Test coverage trends
   - Recent deployments
   ```

**Benefits**:
- **Best of both**: Power of oh-my-opencode for development + safety of gh-aw for automation
- **Separation of concerns**: Local dev vs team automation
- **Security**: Personal work unrestricted, team automation secured
- **Audit trail**: Local changes via git, automation via GitHub Actions

### 12.2 Recommended Architecture Patterns

**Pattern 1: Development Acceleration**
```
Use oh-my-opencode for:
- Feature implementation
- Code refactoring
- Debugging and exploration
- Documentation writing

Use gh-aw for:
- PR review and validation
- Security scanning
- Team notifications
- Metrics and reporting
```

**Pattern 2: Security-First Teams**
```
Use gh-aw exclusively:
- All automation in GitHub Actions
- Strict mode enforced
- Human review required
- Full audit trail

Optional oh-my-opencode:
- Individual developer productivity
- Non-production code
- Experimental features
- Local testing
```

**Pattern 3: Hybrid Power Users**
```
Use oh-my-opencode with gh-aw integration:
- Develop locally with oh-my-opencode
- oh-my-opencode creates PRs with specific labels
- gh-aw workflows triggered by labels
- Automated review, testing, deployment
- Best of both worlds
```

---

## 13. Recommendations

### 13.1 When to Choose oh-my-opencode

**Choose oh-my-opencode if**:
- ✅ You're an individual developer or small team
- ✅ You need professional IDE tools (LSP, AST-Grep)
- ✅ You want multi-agent coordination for parallel work
- ✅ You need long-running tasks that span days
- ✅ You're comfortable managing security yourself
- ✅ You want maximum agent power and flexibility
- ✅ You primarily work locally, not in CI/CD

**Ideal User Profiles**:
- **Power Users**: Developers who want "coding on steroids"
- **Refactoring Projects**: Large-scale code transformations
- **Rapid Prototyping**: Build features using parallel agents
- **Open Source Contributors**: Personal productivity tool
- **Consultants**: Accelerate client projects

**Example Scenarios**:
- "Refactor 200 files to use new API"
- "Implement authentication system with UI and backend"
- "Debug complex race condition with Oracle agent"
- "Migrate codebase from JavaScript to TypeScript"
- "Implement test coverage meeting 80%+ threshold across all modules"

### 13.2 When to Choose GitHub Agentic Workflows

**Choose GitHub Agentic Workflows if**:
- ✅ You're a team or organization
- ✅ You need secure, auditable automation
- ✅ You want GitHub-native integration (Issues, PRs, Actions)
- ✅ You need event-driven workflows (auto-trigger)
- ✅ You require strict security controls
- ✅ You want separation of concerns (read vs write)
- ✅ You primarily work in GitHub ecosystem

**Ideal User Profiles**:
- **Engineering Teams**: Repository-wide automation
- **Open Source Maintainers**: Issue/PR triage at scale
- **Security Teams**: Automated security scanning
- **DevOps/Platform Engineers**: CI/CD automation
- **Enterprise Organizations**: Compliance and audit requirements

**Example Scenarios**:
- "Auto-label and prioritize all new issues"
- "Review every PR for security vulnerabilities"
- "Generate weekly repository health reports"
- "Sync documentation with code changes"
- "Monitor and escalate security alerts"
- "Automate release notes and changelogs"

### 13.3 Use Both Together

**Hybrid Approach**:

```
Local Development Phase:
  └─> oh-my-opencode
       - Fast iteration
       - Multi-agent development
       - Professional code quality
       - Complete implementation

Create Pull Request:
  └─> GitHub (manual)
       - Review local changes
       - Commit and push
       - Open PR

Automated Review Phase:
  └─> GitHub Agentic Workflows
       - Security scanning
       - Code quality checks
       - Test validation
       - Team notifications

Merge and Deploy:
  └─> GitHub Agentic Workflows
       - Release automation
       - Documentation updates
       - Metrics and reporting
```

**Benefits**:
- Maximize development speed (oh-my-opencode)
- Ensure production safety (gh-aw)
- Clear separation of concerns
- Best tools for each phase

---

## 14. Key Takeaways

### What oh-my-opencode Does Best

1. **Multi-Agent Orchestration**: Native coordination of 5+ specialized agents
2. **Professional IDE Tools**: LSP and AST-Grep for production-quality code
3. **Interactive Development**: Real-time collaboration with AI agents
4. **Persistent Sessions**: Work continues across days/weeks
5. **Maximum Power**: No artificial constraints, full capabilities
6. **Local Control**: Developer owns security decisions

### What GitHub Agentic Workflows Does Best

1. **Security by Default**: Strict mode, sandboxing, read-only permissions
2. **GitHub Integration**: Native Issues, PRs, Discussions, Actions
3. **Event-Driven**: Automatic triggers, no manual invocation
4. **Team Automation**: Repository-wide workflows for entire org
5. **Audit Trail**: Full logging, compliance, cost tracking
6. **Safe Outputs**: Validated write operations, threat detection

### Complementary Strengths

**Neither project is "better"** - they solve different problems:

- **oh-my-opencode**: Personal productivity maximizer for local development
- **gh-aw**: Team automation platform for secure GitHub workflows

**Use cases overlap minimally**:
- oh-my-opencode: Build features, refactor code, debug issues
- gh-aw: Automate repository management, security, reporting

**Together, they cover the full software lifecycle**:
- Develop fast locally (oh-my-opencode)
- Automate safely in CI/CD (gh-aw)

---

## 15. Future Directions

### 15.1 Potential gh-aw Enhancements Inspired by oh-my-opencode

1. **Multi-Agent Workflows**
   - Add support for parallel agent execution
   - Enable agent specialization (frontend, backend, security)
   - Implement agent coordination protocol

2. **LSP Integration**
   - Add LSP tools for code navigation
   - Enable safe refactoring operations
   - Support symbol-aware searches

3. **State Persistence**
   - Implement checkpoint/resume for long workflows
   - Add workflow state storage (issues or artifacts)
   - Enable crash recovery

4. **Simplified Configuration**
   - Add "auto" mode that detects needed tools
   - Implement preset configurations for common workflows
   - Reduce boilerplate for basic use cases

### 15.2 Potential oh-my-opencode Enhancements Inspired by gh-aw

1. **Security Hardening**
   - Add strict mode for production use
   - Implement permission approval gates
   - Enable threat detection on agent output

2. **Audit and Compliance**
   - Add detailed logging with cost tracking
   - Implement audit trail for all agent actions
   - Enable compliance reporting

3. **Safe Output Pattern**
   - Separate read and write operations
   - Validate all file changes before applying
   - Implement rollback mechanisms

4. **Network Isolation**
   - Add domain allowlist for MCP servers
   - Implement network firewall
   - Enable data exfiltration prevention

### 15.3 Convergence Possibilities

**Both projects could benefit from**:

1. **Standard MCP Protocol**: Shared MCP server ecosystem
2. **Common Security Patterns**: Best practices for agent safety
3. **Tool Interoperability**: Portable tool configurations
4. **Benchmark Sharing**: Performance and cost comparisons
5. **Community Learning**: Cross-project knowledge exchange

---

## 16. Conclusion

oh-my-opencode and GitHub Agentic Workflows represent two complementary approaches to AI-powered software development:

**oh-my-opencode** maximizes individual developer productivity through:
- Multi-agent orchestration with specialized expertise
- Professional IDE-quality tools (LSP, AST-Grep)
- Persistent sessions for long-running tasks
- Maximum flexibility and power
- Local control and customization

**GitHub Agentic Workflows** enables secure team automation through:
- Event-driven GitHub integration
- Security-by-default with strict mode
- Safe outputs with validation and threat detection
- Audit trails and compliance
- Repository-wide coordination

**Neither replaces the other** - they solve different problems in different contexts:
- Use **oh-my-opencode** when you need full-featured local development with AI agents as teammates
- Use **gh-aw** when you need secure, auditable automation for team/org workflows

**Together, they provide an integrated AI development automation stack**:
- Rapid local development (oh-my-opencode)
- Safe CI/CD automation (gh-aw)
- Clear separation of concerns
- Best tool for each job

The future likely involves both approaches: developers using AI agents locally for implementation, and teams using automated workflows for integration, security, and coordination.

---

## 17. References

### oh-my-opencode
- **Repository**: https://github.com/code-yeongyu/oh-my-opencode
- **OpenCode**: https://github.com/sst/opencode
- **Documentation**: Embedded in README.md and AGENTS.md
- **Discord**: https://discord.gg/PUwSMR9XNk
- **Reviews**: Multiple testimonials from professionals

### GitHub Agentic Workflows
- **Repository**: https://github.com/github/gh-aw
- **Documentation**: https://github.github.com/gh-aw/
- **Security Guide**: https://github.github.com/gh-aw/introduction/architecture/
- **Discord**: #continuous-ai in GitHub Next Discord

### Related Comparisons
- **mdflow Comparison**: scratchpad/mdflow-comparison.md
- **Gastown Concepts**: scratchpad/gastown.md
- **GitHub Actions Security**: https://docs.github.com/en/actions/reference/security/secure-use

---

**Document Version**: 1.0  
**Last Updated**: 2026-01-05  
**Authors**: GitHub Copilot Agent (research and analysis)  
**Review Status**: Initial research - subject to updates as both projects evolve
