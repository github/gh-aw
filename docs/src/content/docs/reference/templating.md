---
title: Templating
description: Expressions and conditional templating in agentic workflows
sidebar:
  order: 350
---

Agentic workflows support four templating and substitution mechanisms: GitHub Actions expressions in frontmatter or markdown, conditional markdown blocks, [compile-time imports](/gh-aw/reference/imports/), and runtime imports for files or URLs.

## GitHub Actions Expressions

Agentic workflows restrict expressions in **markdown content** so prompts cannot expose secrets or environment variables to the LLM.

> **Note**: These restrictions apply only to markdown content. YAML frontmatter can still use secrets and environment variables for workflow configuration.

Markdown allows event properties (`github.event.*`), repository context (`github.actor`, `github.owner`, `github.repository`, `github.server_url`, `github.workspace`), run metadata (`github.run_id`, `github.run_number`, `github.job`, `github.workflow`), and pattern expressions such as `needs.*`, `steps.*`, and `github.event.inputs.*`.

### Activation Outputs

Use `steps.sanitized.outputs.text`, `.title`, or `.body` in markdown prompts to access sanitized event content. `text` includes the full sanitized context (title + body for issues and PRs, body for comments), while `title` and `body` expose those fields individually.

Other activation outputs such as `comment_id`, `comment_repo`, and `slash_command` are available as `needs.activation.outputs.*` in _downstream_ jobs, not in the markdown prompt itself.

### Prohibited Expressions

All other expressions are disallowed, including `secrets.*`, `env.*`, `vars.*`, and complex functions like `toJson()` or `fromJson()`.

Expression safety is validated during compilation. Unauthorized expressions produce errors like:

```text
error: unauthorized expressions: [secrets.TOKEN, env.MY_VAR]. 
allowed: [github.repository, github.actor, github.workflow, ...]
```

## Conditional Markdown

Include or exclude prompt sections based on boolean expressions using `{{#if ...}} ... {{/if}}` blocks.

### Syntax

```markdown wrap
{{#if expression}}
Content to include if expression is truthy
{{/if}}
```

The compiler automatically wraps expressions with `${{ }}` for GitHub Actions evaluation. For example, `{{#if github.event.issue.number}}` becomes `{{#if ${{ github.event.issue.number }} }}`.

**Falsy values:** `false`, `0`, `null`, `undefined`, `""` (empty string)
**Truthy values:** Everything else

### Example

```aw wrap
---
on:
  issues:
    types: [opened]
---

# Issue Analysis

Analyze issue #${{ github.event.issue.number }}.

{{#if github.event.issue.number}}
## Issue-Specific Analysis
You are analyzing issue #${{ github.event.issue.number }}.
{{/if}}

{{#if github.event.pull_request.number}}
## Pull Request Analysis
You are analyzing PR #${{ github.event.pull_request.number }}.
{{/if}}
```

### Limitations

The template system supports only basic conditionals - no nesting, `else` clauses, variables, loops, or complex evaluation.

## Runtime Imports

Runtime imports include content from files and URLs in workflow prompts **at runtime** (unlike [compile-time imports](/gh-aw/reference/imports/)). File paths are restricted to the `.github` folder. Use `{{#runtime-import filepath}}` or `{{#runtime-import? filepath}}` for optional imports.

### Macro Syntax

Use `{{#runtime-import filepath}}` to include file content at runtime. Use `{{#runtime-import? filepath}}` when the file is optional. All file paths resolve within `.github`, with or without the `.github/` prefix:

```aw wrap
---
on: issues

engine: copilot
---

# Code Review Agent

Follow these coding guidelines:

{{#runtime-import coding-standards.md}}
<!-- Same as: {{#runtime-import .github/coding-standards.md}} -->

Review the code changes and provide feedback.
```

**Line range extraction:**

```aw wrap
# Bug Fix Validator

The original buggy code was (from .github/docs/auth.go):

{{#runtime-import docs/auth.go:45-52}}

Verify the fix addresses the issue.
```

**Optional imports:**

```aw wrap
# Issue Analyzer

{{#runtime-import? shared-instructions.md}}

Analyze issue #${{ github.event.issue.number }}.
```

### URL Imports

The macro syntax supports HTTP/HTTPS URLs. URLs are **not restricted to `.github` folder** and content is cached for 1 hour.

```aw wrap
{{#runtime-import https://raw.githubusercontent.com/org/repo/main/checklist.md}}
{{#runtime-import https://example.com/standards.md:10-50}}
```

### Security Features

Runtime imports automatically strip YAML front matter and HTML/XML comments. GitHub Actions expressions (`${{ ... }}`) are rejected to prevent template injection or unintended variable expansion.

`needs.*.outputs.*` expressions are allowed inside imported markdown and are validated recursively across nested runtime imports. At compile time, gh-aw resolves every referenced runtime-import file under `.github`, checks the imported content for disallowed expressions, and follows any nested `{{#runtime-import ...}}` macros it finds. This lets shared prompt fragments safely reference job outputs such as `${{ needs.build.outputs.version }}` without bypassing expression safety checks.

Imported prompt content is still evaluated in the activation job context. In practice, that means imported `needs.*` expressions must refer only to outputs that are available before activation runs, such as `needs.pre_activation.outputs.*` or outputs from custom jobs that explicitly run before activation.

File paths are restricted to `.github` to prevent access to arbitrary repository files. Path traversal and absolute paths are rejected:

```aw wrap
{{#runtime-import ../src/config.go}}  # Error: Relative traversal outside .github
{{#runtime-import /etc/passwd}}       # Error: Absolute path not allowed
```

### Caching

Fetched URLs are cached for 1 hour per workflow run at `/tmp/gh-aw/url-cache/` (keyed by SHA256 hash). The first fetch adds ~500ms–2s latency; subsequent accesses use cached content.

### Processing Order

Runtime imports run before other substitutions:

1. `{{#runtime-import}}` macros for files and URLs
2. `${GH_AW_EXPR_*}` variable interpolation
3. `{{#if}}` conditional rendering

### Limitations

Runtime imports are limited to the `.github` folder for files, do not support authenticated URL fetches, use a per-run URL cache that does not persist across workflow runs, and interpret line numbers against the raw file before front matter removal.

### Deprecated `{{#import}}`

`{{#import filepath}}` (without `runtime-`) is a **deprecated** body-level shorthand. It normalizes to `{{#runtime-import filepath}}` at runtime for backward compatibility, but emits deprecation warnings at both compile time and runtime. Use `{{#runtime-import}}` directly for all new workflows. See [Imports](/gh-aw/reference/imports/) for details.

### Error Handling

| Error | Message |
|-------|---------|
| File not found | `Runtime import file not found: missing.txt` |
| Invalid line range | `Invalid start line 100 for file docs/main.go (total lines: 50)` |
| Path traversal | `Security: Path ../src/main.go must be within .github folder` |
| GitHub Actions macros | `File template.md contains GitHub Actions macros (${{ ... }}) which are not allowed in runtime imports` |
| URL fetch failure | `Failed to fetch URL https://example.com/file.txt: HTTP 404` |

## Learn More

- [Markdown](/gh-aw/reference/markdown/) for writing effective agentic markdown
- [Workflow Structure](/gh-aw/reference/workflow-structure/) for overall workflow organization
- [Frontmatter](/gh-aw/reference/frontmatter/) for YAML configuration
- [Imports](/gh-aw/reference/imports/) for compile-time imports in frontmatter
