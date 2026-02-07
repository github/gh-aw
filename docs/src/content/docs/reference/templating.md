---
title: Templating
description: Expressions and conditional templating in agentic workflows
sidebar:
  order: 350
---

Agentic workflows support four simple templating/substitution mechanisms: 

* GitHub Actions expressions in frontmatter or markdown
* Conditional Templating blocks in markdown
* [Imports](/gh-aw/reference/imports/) in frontmatter or markdown (compile-time)
* Runtime imports in markdown (runtime file/URL inclusion)

## GitHub Actions Expressions

Agentic workflows restrict expressions in **markdown content** to prevent security vulnerabilities from exposing secrets or environment variables to the LLM.

> **Note**: These restrictions apply only to markdown content. YAML frontmatter can use secrets and environment variables for workflow configuration.

**Permitted expressions** in markdown include:
- Event properties: `github.event.*` (issue/PR numbers, titles, states, SHAs, IDs, etc.)
- Repository context: `github.actor`, `github.owner`, `github.repository`, `github.server_url`, `github.workspace`
- Run metadata: `github.run_id`, `github.run_number`, `github.job`, `github.workflow`
- Pattern expressions: `needs.*`, `steps.*`, `github.event.inputs.*`

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

Runtime imports allow you to include content from files and URLs directly within your workflow prompts **at runtime** during GitHub Actions execution. This differs from [frontmatter imports](/gh-aw/reference/imports/) which are processed at compile-time.

**Security Note:** File imports are **restricted to the `.github` folder** in your repository. This ensures workflow configurations cannot access arbitrary files in your codebase.

Runtime imports use the macro syntax: `{{#runtime-import filepath}}`

The macro supports:
- Line range extraction (e.g., `:10-20` for lines 10-20)
- URL fetching with automatic caching
- Content sanitization (front matter removal, macro detection)
- Automatic `.github/` prefix handling

### Macro Syntax

Use `{{#runtime-import filepath}}` to include file content at runtime. Optional imports use `{{#runtime-import? filepath}}` which don't fail if the file is missing.

**Important:** All file paths are resolved within the `.github` folder. You can specify paths with or without the `.github/` prefix:

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

The macro syntax supports HTTP/HTTPS URLs. Fetched content is **cached for 1 hour** to reduce network requests. URLs are **not restricted to `.github` folder** - you can fetch any public URL.

**Macro syntax:**

```aw wrap
{{#runtime-import https://raw.githubusercontent.com/org/repo/main/checklist.md}}
```

**URL with line range:**

```aw wrap
{{#runtime-import https://example.com/standards.md:10-50}}
```

### Security Features

All runtime imports include automatic security protections:

**Content Sanitization:**
- YAML front matter is automatically removed
- HTML/XML comments are stripped
- GitHub Actions expressions (`${{ ... }}`) are **rejected with error**

This prevents:
- Template injection attacks
- Unintended variable expansion
- Security vulnerabilities from imported content

**Path Validation:**

File paths are **restricted to the `.github` folder** to prevent access to arbitrary repository files:

```aw wrap
# ✅ Valid - Files in .github folder
{{#runtime-import shared-instructions.md}}           # Loads .github/shared-instructions.md
{{#runtime-import .github/shared-instructions.md}}  # Same - .github/ prefix is trimmed

# ❌ Invalid - Attempts to escape .github folder
{{#runtime-import ../src/config.go}}                # Error: Must be within .github folder
{{#runtime-import ../../etc/passwd}}                # Error: Must be within .github folder
```

### Caching

**URL caching** reduces network overhead:
- Cache location: `/tmp/gh-aw/url-cache/`
- Cache duration: 1 hour
- Cache key: SHA256 hash of URL
- Cache scope: Per workflow run (ephemeral)

First URL fetch adds latency (~500ms-2s), subsequent accesses use cached content.

### Processing Order

Runtime imports are processed as part of the overall templating pipeline:

```
1. {{#runtime-import}} macros processed (files and URLs)
2. ${GH_AW_EXPR_*} variable interpolation
3. {{#if}} template conditionals rendered
```

### Common Use Cases

**1. Shared coding standards:**

```aw wrap
# Code Review Agent

{{#runtime-import workflows/shared/review-standards.md}}
<!-- Loads .github/workflows/shared/review-standards.md -->

Review the pull request changes.
```

**2. External security checklists:**

```aw wrap
# Security Audit

Follow this checklist:

{{#runtime-import https://company.com/security/api-checklist.md}}
<!-- URLs are not restricted to .github folder -->
```

**3. Code context for analysis:**

```aw wrap
# Refactoring Assistant

Current implementation (from .github/docs/engine.go):

{{#runtime-import docs/engine.go:100-150}}

Suggested improvements needed.
```

**4. License attribution:**

```aw wrap
# Generated Report

## License

{{#runtime-import docs/LICENSE:1-10}}
<!-- Loads .github/docs/LICENSE -->
```

### Limitations

- **`.github` folder only:** File paths are restricted to `.github` folder for security
- **No authentication:** URL fetching doesn't support private URLs with tokens
- **No recursion:** Imported content cannot contain additional runtime imports
- **Per-run cache:** URL cache doesn't persist across workflow runs
- **Line numbers:** Refer to raw file content before front matter removal

### Error Handling

**File not found:**

```
Failed to process runtime import for missing.txt: 
Runtime import file not found: missing.txt
```

**Invalid line range:**

```
Invalid start line 100 for file docs/main.go (total lines: 50)
```

**Path security violation:**

```
Security: Path ../../../etc/passwd must be within .github folder
```

**GitHub Actions macros detected:**

```
File template.md contains GitHub Actions macros (${{ ... }}) 
which are not allowed in runtime imports
```

**URL fetch failure:**

```
Failed to fetch URL https://example.com/file.txt: HTTP 404
```

## Related Documentation

- [Markdown](/gh-aw/reference/markdown/) - Writing effective agentic markdown
- [Workflow Structure](/gh-aw/reference/workflow-structure/) - Overall workflow organization
- [Frontmatter](/gh-aw/reference/frontmatter/) - YAML configuration
- [Imports](/gh-aw/reference/imports/) - Compile-time imports in frontmatter
