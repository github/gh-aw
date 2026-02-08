# mdflow vs GitHub Agentic Workflows: Syntax Comparison

**Date**: 2025-12-29  
**mdflow Repository**: https://github.com/johnlindquist/mdflow  
**Status**: Comparative Analysis

## Executive Summary

This document provides a detailed syntax comparison between mdflow and GitHub Agentic Workflows (gh-aw). Both projects enable AI-driven automation through markdown files, but they target different use cases and have different design philosophies.

### Key Differences at a Glance

| Aspect | mdflow | GitHub Agentic Workflows |
|--------|---------|-------------------------|
| **Primary Use Case** | Local CLI execution, ad-hoc tasks | GitHub Actions workflows, CI/CD automation |
| **Execution Environment** | Local machine, any context | GitHub Actions runners |
| **File Naming** | `task.COMMAND.md` (command inferred) | `workflow-name.md` (any name) |
| **Trigger Mechanism** | Manual CLI invocation | GitHub events (push, PR, issues, schedule) |
| **Frontmatter Purpose** | CLI flags for AI command | GitHub Actions + AI engine configuration |
| **Template System** | LiquidJS with `_varname` convention | GitHub Actions expressions `${{ }}` |
| **Security Model** | User-managed permissions | Strict mode, safe-outputs, read-only default |
| **Output Handling** | Print to stdout/terminal | Safe-outputs to GitHub API (issues, PRs, discussions) |

---

## 1. File Naming and Command Resolution

### mdflow

**Pattern**: Filename encodes the AI command to execute

```markdown
review.claude.md      # Runs: claude --print "..."
commit.gemini.md      # Runs: gemini "..."
task.copilot.md       # Runs: copilot --silent --prompt "..."
explain.i.claude.md   # Runs: claude "..." (interactive)
```

**Key Features**:
- Command inferred from filename (`task.COMMAND.md`)
- `.i.` marker enables interactive mode
- Can override with `--_command` flag
- Supports generic `.md` files with explicit command flag

**Philosophy**: Convention over configuration - the filename tells you what runs

### GitHub Agentic Workflows

**Pattern**: Filename is descriptive; engine specified in frontmatter

```markdown
# File: issue-responder.md
---
engine: copilot
---
```

**Key Features**:
- Filename can be anything descriptive (`issue-responder.md`, `pr-reviewer.md`)
- Engine explicitly declared in frontmatter
- Compiles to `.lock.yml` GitHub Actions workflow
- No command inference from filename

**Philosophy**: Explicit configuration - frontmatter declares all behavior

---

## 2. Frontmatter Design

### mdflow

**Purpose**: Convert YAML keys to CLI flags for the AI command

```yaml
---
model: opus                        # → --model opus
dangerously-skip-permissions: true # → --dangerously-skip-permissions
mcp-config: ./mcp.json            # → --mcp-config ./mcp.json
add-dir:                          # → --add-dir ./src --add-dir ./tests
  - ./src
  - ./tests
_feature_name: Authentication     # Template variable
_inputs:                          # Interactive form inputs
  _name:
    type: text
    description: "Enter your name"
---
```

**Key Features**:
- Every key becomes a CLI flag (1:1 mapping)
- Single character keys become short flags (`p: true` → `-p`)
- Array values repeat the flag (`key: [a, b]` → `--key a --key b`)
- `_varname` keys are template variables (not passed to command)
- `_inputs` defines interactive prompts
- No validation of flag names (passed directly to command)

**Reserved Keys** (hijacked by mdflow):
- `_command`, `-_c`: Override command
- `_varname`: Template variables
- `_interactive`, `_i`: Enable interactive mode
- `_env`: Set environment variables
- `_cwd`: Working directory
- `_subcommand`: Prepend subcommands

### GitHub Agentic Workflows

**Purpose**: Configure GitHub Actions workflow + AI engine

```yaml
---
description: Workflow that responds to issues
on:
  issues:
    types: [opened]
permissions:
  issues: write
  contents: read
engine: copilot
tools:
  github:
    toolsets: [issues]
  bash: ["gh issue comment"]
safe-outputs:
  create-issue:
  create-discussion:
network:
  allowed:
    - defaults
strict: true
---
```

**Key Features**:
- Combines GitHub Actions properties (`on`, `permissions`, `runs-on`)
- AI-specific properties (`engine`, `tools`, `safe-outputs`)
- Schema validation of all fields
- Compiled to GitHub Actions YAML (`.lock.yml`)
- Security constraints enforced at compile time

**Categories of Fields**:
1. **GitHub Actions Standard**: `on`, `permissions`, `runs-on`, `timeout-minutes`, `env`, `steps`, `jobs`
2. **AI Engine**: `engine`, `tools`, `network`, `safe-inputs`, `safe-outputs`
3. **Security**: `strict`, `roles`, `github-token`
4. **Metadata**: `description`, `source`, `labels`, `metadata`

---

## 3. Template Systems

### mdflow

**Engine**: LiquidJS template system

**Variables**:
```markdown
---
_feature_name: Authentication
_target_dir: src/features
---
Build {{ _feature_name }} in {{ _target_dir }}.

{% if _verbose == "yes" %}
Detailed analysis mode enabled.
{% endif %}
```

**Special Variables**:
- `{{ _stdin }}` - Piped input
- `{{ _1 }}`, `{{ _2 }}` - Positional CLI arguments
- `{{ _args }}` - All positional args as numbered list
- `_varname` - User-defined variables (from frontmatter or CLI flags)

**Override via CLI**:
```bash
mdflow create.claude.md --_feature_name "Payments" --_target_dir "src/billing"
```

### GitHub Agentic Workflows

**Engine**: GitHub Actions expression syntax

**Variables**:
```markdown
---
on:
  issues:
    types: [opened]
---
Respond to issue #${{ github.event.issue.number }} 
by @${{ github.event.issue.user.login }}.

Repository: ${{ github.repository }}
Run ID: ${{ github.run_id }}
```

**Context Objects**:
- `${{ github.* }}` - GitHub event context
- `${{ secrets.* }}` - Repository secrets
- `${{ env.* }}` - Environment variables
- `${{ needs.job-name.outputs.* }}` - Job outputs
- `${{ inputs.* }}` - Workflow dispatch inputs

**No CLI Override**: Variables come from GitHub Actions context only

---

## 4. Imports and File Inclusion

### mdflow

**Multiple Import Methods**:

```markdown
# File imports
@~/path/to/file.md
@./relative/path.md
@/absolute/path

# Glob patterns
@./src/**/*.ts

# Line ranges
@./src/api.ts:10-50

# Symbol extraction
@./src/types.ts#UserInterface
@./src/api.ts#fetchUser

# URL imports (cached 1 hour)
@https://raw.githubusercontent.com/user/repo/main/README.md

# Command inlines
Current branch: !`git branch --show-current`
```

**Features**:
- Respects `.gitignore` automatically
- Recursive imports supported
- Glob imports formatted as XML with paths
- Symbol extraction for TS/JS
- URL caching with TTL
- Command output interpolation

### GitHub Agentic Workflows

**Single Import Method**:

```yaml
---
imports:
  - shared/mcp/gh-aw.md
  - shared/jqschema.md
  - shared/reporting.md
---
```

**Features**:
- Relative paths from workflow file
- Imported content prepended to prompt
- No glob support
- No symbol extraction
- No command interpolation
- Files must exist at compile time

---

## 5. Interactive vs Non-Interactive Execution

### mdflow

**Print Mode (Default)**:
```bash
mdflow task.claude.md      # Non-interactive, exits after completion
```

**Interactive Mode**:
```bash
# Via filename marker
mdflow task.i.claude.md

# Via frontmatter
---
_interactive: true
---

# Via CLI flag
mdflow task.claude.md --_interactive
```

**Per-Command Behavior**:
- Claude: `--print` flag for print mode
- Copilot: `--silent --prompt` for print mode
- Gemini: One-shot by default

### GitHub Agentic Workflows

**Always Non-Interactive**:
- Runs in GitHub Actions (no TTY)
- AI agent executes with available tools
- Uses MCP servers for tool access
- Output via safe-outputs (issues, PRs, discussions)

**No Interactive Mode**: All execution is batch/automated

---

## 6. Tool Access and Permissions

### mdflow

**Tool Access**: Direct access to user's system

```yaml
---
mcp-config: ./mcp.json
dangerously-skip-permissions: true
---
```

**Security Model**:
- Runs with user's permissions
- `dangerously-skip-permissions` flag to bypass prompts
- MCP servers run locally
- Full filesystem access
- Network access unrestricted

**Trust Model**: User trusts the markdown file they execute

### GitHub Agentic Workflows

**Tool Access**: Explicitly configured tools only

```yaml
---
tools:
  github:
    mode: remote
    toolsets: [default, actions]
  bash: ["gh issue comment", "jq"]
  playwright:
    allowed_domains: ["github.com"]
  cache-memory: true
network:
  allowed:
    - defaults
    - python
---
```

**Security Model**:
- Read-only permissions by default
- Write operations via safe-outputs only
- Network firewall (deny-by-default)
- Tool allowlisting required
- Sandboxed execution in containers
- SHA-pinned dependencies

**Trust Model**: Repository owner controls what workflows can do

---

## 7. Output Handling

### mdflow

**Output**: Printed to terminal

```bash
# Direct stdout
mdflow review.claude.md

# Pipe to other commands
mdflow review.claude.md | jq .

# Disable rendering
mdflow task.claude.md --raw
```

**Features**:
- Markdown rendering with syntax highlighting
- Can pipe to other tools
- `--raw` flag for machine-readable output
- Logs always written to `~/.mdflow/logs/`

### GitHub Agentic Workflows

**Output**: Structured GitHub API operations

```yaml
---
safe-outputs:
  create-issue:
    title: "Issue title from AI"
    body: "Body with AI content"
  create-discussion:
    category: "announcements"
    max: 1
  upload-asset:
  create-pull-request:
---
```

**Features**:
- AI generates structured output
- Safe-outputs sanitized and validated
- Creates GitHub resources (issues, PRs, discussions)
- No direct stdout (runs in Actions)
- Logs accessible via Actions UI or `gh aw logs`

---

## 8. Trigger Mechanisms

### mdflow

**Trigger**: Manual CLI invocation

```bash
# Direct execution
mdflow task.claude.md

# Pipe input
git diff | mdflow review.claude.md

# Chain agents
mdflow plan.claude.md | mdflow implement.codex.md

# With arguments
mdflow translate.claude.md "hello" "French"
```

**Shell Integration**:
```bash
# Make .md files executable
mdflow setup

# Then run directly
task.claude.md
review.claude.md
```

### GitHub Agentic Workflows

**Trigger**: GitHub webhook events

```yaml
---
on:
  issues:
    types: [opened, labeled]
  pull_request:
    types: [opened]
  schedule:
    - cron: '0 0 * * *'
  workflow_dispatch:
---
```

**Event-Driven**:
- GitHub sends webhook
- Actions runner executes workflow
- No manual invocation needed
- Can be triggered manually via `workflow_dispatch`

---

## 9. Configuration and Defaults

### mdflow

**Global Config**: `~/.mdflow/config.yaml`

```yaml
commands:
  claude:
    model: sonnet
  copilot:
    silent: true
```

**Priority Order**:
1. CLI flags (highest)
2. Frontmatter
3. Global config
4. Built-in defaults (lowest)

**Environment Variables**:
- `.env` file loading (development/production variants)
- `MDFLOW_FORCE_CONTEXT=1` to disable token limits
- `NODE_ENV` to select environment

### GitHub Agentic Workflows

**No Global Config**: Each workflow is self-contained

**Priority Order**:
1. Workflow frontmatter (only source)
2. Compilation defaults

**Environment Variables**:
- Standard GitHub Actions `env:` section
- Workflow-level, job-level, step-level scopes
- Secrets via `${{ secrets.NAME }}`

---

## 10. Use Case Alignment

### mdflow

**Ideal For**:
- Ad-hoc AI tasks on local machine
- Quick code reviews, commits, explanations
- Personal productivity automation
- Chaining multiple AI agents
- Interactive problem-solving
- Learning prompts as reusable commands

**Example Workflows**:
- `review.claude.md` - Review current code changes
- `commit.gemini.md` - Generate commit messages
- `explain.claude.md` - Explain complex code
- `debug.claude.md` - Debug issues interactively

### GitHub Agentic Workflows

**Ideal For**:
- Repository automation (CI/CD)
- Issue/PR management and triage
- Scheduled reports and analysis
- Team workflows and governance
- Security scanning and compliance
- Documentation updates
- Release management

**Example Workflows**:
- Issue responders and auto-labeling
- PR review automation
- Daily repository health reports
- Security vulnerability scanning
- Documentation sync from code

---

## 11. Philosophy and Design Principles

### mdflow

**Philosophy**: Unix philosophy for AI agents

**Principles**:
1. **No magic mapping** - Frontmatter keys pass directly to commands
2. **Stdin/stdout** - Pipe data in and out
3. **Composable** - Chain agents together
4. **Transparent** - See what runs in logs
5. **Convention over configuration** - Filename determines command
6. **Batteries included** - Built-in defaults for common cases

**Target User**: Developer working locally, wants quick AI assistance

### GitHub Agentic Workflows

**Philosophy**: Safe-by-default automation for repositories

**Principles**:
1. **Security first** - Read-only default, explicit permissions
2. **Schema validation** - Catch errors at compile time
3. **Explicit configuration** - No hidden behavior
4. **GitHub-native** - Integrates with Actions ecosystem
5. **Team workflows** - Multi-user, shared automation
6. **Auditability** - All changes tracked in GitHub

**Target User**: Team/org wanting secure, repeatable repository automation

---

## 12. Extensibility

### mdflow

**Extensibility**:
- Custom MCP servers via `mcp-config`
- Any CLI AI tool (claude, gemini, copilot, codex)
- Shell command inlines
- File imports from anywhere
- Template system for dynamic prompts

**Adding New Commands**:
1. Name file `task.newcommand.md`
2. Add default flags to `~/.mdflow/config.yaml` (optional)
3. Ready to use

### GitHub Agentic Workflows

**Extensibility**:
- Custom MCP servers in `tools:` section
- Custom inline tools via `safe-inputs:`
- Custom GitHub Actions in `steps:` and `jobs:`
- Import shared markdown prompts
- Multiple AI engines (copilot, claude, codex, custom)

**Adding New Engines**:
1. Implement engine interface in Go
2. Add to engine registry
3. Rebuild binary
4. Use in frontmatter: `engine: myengine`

---

## 13. Error Handling and Debugging

### mdflow

**Debugging**:
- `md explain task.claude.md` - Show what will run
- `md task.claude.md --_dry-run` - Preview without executing
- `md task.claude.md --_edit` - Edit prompt before running
- `md task.claude.md --_context` - Show context tree
- Logs in `~/.mdflow/logs/<agent-name>/`

**Error Handling**:
- Command failures propagate to exit code
- Template errors shown before execution
- Import errors caught early
- Validation on interactive inputs

### GitHub Agentic Workflows

**Debugging**:
- `gh aw compile --dry-run` - Show compiled YAML
- `gh aw logs` - Download workflow logs
- `gh aw audit 123456` - Analyze workflow run
- GitHub Actions UI for step-by-step execution
- Validation errors at compile time

**Error Handling**:
- Compile-time schema validation
- Permission validation before execution
- Network firewall logs
- Safe-output validation
- Tool execution errors logged

---

## 14. Example: Side-by-Side Comparison

### Task: Review Code and Create Issue

#### mdflow Version

**File**: `review-and-issue.claude.md`

```markdown
---
model: opus
mcp-config: ./github-mcp.json
_target: src/
---
Review the code in {{ _target }} for bugs and security issues.

If you find any critical issues, create a GitHub issue with details.

@./{{ _target }}/**/*.ts
```

**Usage**:
```bash
mdflow review-and-issue.claude.md --_target "src/api"
```

#### GitHub Agentic Workflows Version

**File**: `code-review.md`

```yaml
---
description: Review code and create issues for problems
on:
  pull_request:
    types: [opened]
permissions:
  contents: read
  issues: write
engine: copilot
tools:
  github:
    toolsets: [repos, issues]
safe-outputs:
  create-issue:
strict: true
---

# Code Review Agent

Review the changed files in PR #${{ github.event.pull_request.number }}.

Analyze for:
- Security vulnerabilities
- Logic bugs
- Code quality issues

If you find critical issues, create a GitHub issue with:
- Clear title
- Detailed description
- Severity level
- Suggested fix
```

**Usage**: Automatically runs when PR is opened

---

## 15. Syntax Comparison: Quick Reference

### Starting a Workflow

| Feature | mdflow | gh-aw |
|---------|--------|-------|
| Filename | `task.claude.md` | `workflow-name.md` |
| Engine | Inferred from filename | `engine: copilot` |
| Execution | `mdflow task.claude.md` | Triggered by GitHub event |

### Configuration

| Feature | mdflow | gh-aw |
|---------|--------|-------|
| Flags | `model: opus` → `--model opus` | `engine.model: opus` |
| Variables | `_name: value` | `${{ github.event.* }}` |
| Templates | `{{ _name }}` | `${{ expression }}` |
| Imports | `@./path/file.md` | `imports: [path/file.md]` |

### Security

| Feature | mdflow | gh-aw |
|---------|--------|-------|
| Permissions | User's system permissions | `permissions:` explicit |
| Network | Unrestricted | `network.allowed:` allowlist |
| Filesystem | Full access | Sandboxed /tmp/gh-aw |
| Tools | Any installed | `tools:` explicit allowlist |

### Output

| Feature | mdflow | gh-aw |
|---------|--------|-------|
| Primary | Terminal stdout | GitHub API (safe-outputs) |
| Format | Markdown rendered | Structured (issues, PRs) |
| Piping | Yes (`| jq`) | No (Actions environment) |
| Logs | `~/.mdflow/logs/` | GitHub Actions logs |

---

## 16. Potential Cross-Pollination Ideas

### mdflow Could Adopt from gh-aw:

1. **Schema Validation**: Validate frontmatter against known flags
2. **Safe-Outputs**: Structured output validation
3. **Permission Checking**: Warn about dangerous operations
4. **Strict Mode**: Optional validation for production use

### gh-aw Could Adopt from mdflow:

1. **Simplified Imports**: Glob patterns, symbol extraction, command inlines
2. **Template Variables**: LiquidJS-style conditionals and loops
3. **Interactive Forms**: `_inputs` for workflow_dispatch parameters
4. **Context Dashboard**: Pre-flight display of what will be included
5. **Dry-Run Preview**: Show expanded prompt before execution
6. **File Organization**: Consider allowing workflow "libraries" with shared patterns

---

## 17. Recommendations

### For mdflow Users Looking at gh-aw:

- **Different domain**: mdflow is for local tasks, gh-aw is for GitHub automation
- **More structured**: Expect more configuration, stricter validation
- **Event-driven**: Workflows run automatically, not on command
- **Security-focused**: Explicit permissions, sandboxing, validation
- **GitHub-native**: Deep integration with Issues, PRs, Actions

### For gh-aw Users Looking at mdflow:

- **Simpler for local tasks**: Less configuration for ad-hoc AI tasks
- **Unix philosophy**: Compose with pipes, chain agents
- **Interactive mode**: Work with AI conversationally
- **Flexible**: Any CLI tool, any file, any context
- **Learning tool**: Build reusable AI command library

### Opportunities for Improvement:

1. **gh-aw**: Could simplify import syntax with glob support
2. **gh-aw**: Could add template helpers for common patterns
3. **mdflow**: Could add validation mode for production scripts
4. **mdflow**: Could add structured output formats (JSON, YAML)
5. **Both**: Could share MCP server configurations and patterns

---

## Conclusion

mdflow and GitHub Agentic Workflows solve different problems with different design philosophies:

- **mdflow**: Personal productivity, local execution, Unix-style composition, convention over configuration
- **gh-aw**: Team automation, CI/CD integration, security-first, explicit configuration

Each tool serves its domain effectively. mdflow provides quick, ad-hoc AI tasks on a developer's machine. GitHub Agentic Workflows provides safe, auditable, team-wide repository automation.

The syntax differences reflect these different goals:
- mdflow prioritizes simplicity and flexibility for local use
- gh-aw prioritizes security and validation for shared workflows

Neither is "better" - they're optimized for different use cases and user needs.

---

## References

- mdflow Repository: https://github.com/johnlindquist/mdflow
- GitHub Agentic Workflows Documentation: https://github.github.com/gh-aw/
- mdflow README: Full documentation of syntax and features
- gh-aw Reference: `/docs/src/content/docs/reference/`

**Document Version**: 1.0  
**Last Updated**: 2025-12-29
