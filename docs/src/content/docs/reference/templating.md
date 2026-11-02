---
title: Templating
description: Expressions and conditional templating in agentic workflows
sidebar:
  order: 350
---

Agentic workflows support four templating and substitution mechanisms: GitHub Actions expressions in frontmatter or markdown, conditional markdown blocks, [compile-time imports](/gh-aw/reference/imports/), and runtime imports for local files or static public HTTPS content.

## GitHub Actions Expressions

Agentic workflows restrict expressions in **markdown content** to an allowlist. Secrets and configuration variables cannot be read directly from prompts; environment expressions have different rules for compiler-bound author prompts and runtime-imported fragments.

> **Note**: These restrictions apply only to markdown content. YAML frontmatter can still use secrets and environment variables for workflow configuration.

Markdown allows event properties (`github.event.*`), repository context (`github.actor`, `github.owner`, `github.repository`, `github.server_url`, `github.workspace`), run metadata (`github.run_id`, `github.run_number`, `github.job`, `github.workflow`), and pattern expressions such as `needs.*`, `steps.*`, and `github.event.inputs.*`.

### Activation Outputs

Use `steps.sanitized.outputs.text`, `.title`, or `.body` in markdown prompts to access sanitized event content. `text` includes the full sanitized context (title + body for issues and PRs, body for comments), while `title` and `body` expose those fields individually.

Other activation outputs such as `comment_id`, `comment_repo`, and `slash_command` are available as `needs.activation.outputs.*` in _downstream_ jobs, not in the markdown prompt itself.

### Prohibited Expressions

All other expressions are disallowed, including `secrets.*`, `vars.*`, and complex functions like `toJson()` or `fromJson()`.

The main workflow's author-written prompt supports `env.*` expressions (including comparisons and defaults such as `${{ env.LOG_LEVEL || 'info' }}`). The compiler creates explicit expression bindings evaluated by GitHub Actions; this is not access to the runner's entire environment. These values become prompt content, so do not reference environment variables containing secrets.

Runtime-imported fragments do **not** support `env.*`, even if the main prompt binds the same expression. This restriction also applies to nested imports and compound expressions. The compiler loads the main workflow as an author prompt source, separately from runtime-imported fragments, and retains its explicit bindings. Locally assembled system and user prompts can contain imports; local fragments can nest imports within the permitted folders, while downloaded HTTPS content remains static and cannot trigger imports or expressions.

Expression safety is validated during compilation. Unauthorized expressions produce errors like:

```text
error: unauthorized expressions: [secrets.TOKEN, vars.MY_VAR].
allowed: [github.repository, github.actor, github.workflow, ...]
```

## Conditional Markdown

Include or exclude prompt sections based on boolean expressions using `{{#if ...}} ... {{/if}}` blocks.

Only directives written in workflow templates or imported prompt sources execute. Substituted event content and job outputs remain literal, including template markers inside inline code or fenced code blocks. Template examples written inside code regions are also preserved. Values referenced by author-written conditions still determine which branch is selected.

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

Runtime imports include content in workflow prompts **at runtime** (unlike [compile-time imports](/gh-aw/reference/imports/)). Local file paths are restricted to `.github` or explicit `.agents/` paths. Remote imports accept literal public HTTPS URLs with expression-free content. Use `{{#runtime-import target}}` or `{{#runtime-import? target}}` for optional imports.

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

### Public HTTPS Imports

Remote imports accept only literal, unauthenticated HTTPS URLs. Both the URL and downloaded content must be free of GitHub Actions expressions (`${{ ... }}`) and internal expression/experiment placeholders. Downloaded content must also contain no template directives, including conditionals or nested imports. These checks cover comments, front matter, lines outside a selected range, and text after sanitization; even otherwise-allowed expressions such as `${{ github.actor }}` are rejected in remote text.

Directive rejection includes bare branch aliases such as `{{elseif true}}`, `{{else-if true}}`, and `{{else_if true}}`; remote text cannot use them to change a surrounding author-written conditional.

```aw wrap
{{#runtime-import https://raw.githubusercontent.com/org/repo/main/checklist.md}}
{{#runtime-import https://example.com/standards.md:10-50}}
```

HTTPS requests run during prompt creation on the runner, outside the agent network sandbox. All DNS answers must be ordinary public unicast IPv4/IPv6 addresses, and the connection uses those same validated addresses without resolving again. Private, loopback, link-local, metadata, special-purpose, and translation/tunneling destinations are rejected. Redirects, credentials in URLs, HTTP, other URI schemes, and network-path references are not supported. TLS hostname/certificate verification remains enabled.

URL disk caches are not used; repeated imports may reuse validated content within the current prompt assembly. Optional network failures produce a warning and empty content, but invalid targets and expressions/directives in remote content fail even when optional. To use expressions in shared content, store it in a permitted local file instead. Existing local repo-root forms such as `/.github/`, `//.github/`, and `/.agents/` remain supported.

### Security Features

Runtime imports automatically strip YAML front matter and HTML/XML comments. Local imported GitHub Actions expressions (`${{ ... }}`) use a restricted set of expression capabilities. `env.*`, `secrets.*`, and `vars.*` are rejected, including inside comparisons and compound expressions; imported text cannot read the runner's ambient environment or action input tokens. Supported job outputs and inputs are read only through their compiler-provided bindings or GitHub event context. Remote content has no expression capabilities.

Wrapped fragment conditions are checked before conversion to internal placeholders, including inside code fences or unmatched conditionals. They cannot acquire an author-only environment binding through later substitution.

`needs.*.outputs.*` expressions are allowed inside local imported markdown and are validated recursively across nested local runtime imports. At compile time, gh-aw checks existing referenced runtime-import files under the permitted folders for disallowed expressions, including `env.*`, and follows any nested `{{#runtime-import ...}}` macros it finds. Missing files and remote content cannot be checked at compilation; runtime validation still applies when content is loaded. This lets local shared prompt fragments safely reference job outputs such as `${{ needs.build.outputs.version }}` without bypassing expression safety checks.

Imported prompt content is still evaluated in the activation job context. In practice, that means imported `needs.*` expressions must refer only to outputs that are available before activation runs, such as `needs.pre_activation.outputs.*` or outputs from custom jobs that explicitly run before activation.

File paths are restricted to `.github` or explicit `.agents/` paths to prevent access to arbitrary files. Traversal outside those folders and arbitrary absolute paths are rejected:

```aw wrap
{{#runtime-import ../src/config.go}}  # Error: Relative traversal outside .github
{{#runtime-import /etc/passwd}}       # Error: Absolute path not allowed
```

### Processing Order

Runtime imports run before other substitutions:

1. `{{#runtime-import}}` macros for local files or static public HTTPS content
2. Expression and experiment values are replaced with opaque placeholders
3. Author-written `{{#if}}` conditionals are rendered, resolving placeholder values only when evaluating their conditions
4. Literal values are restored in the selected content without further template processing

### Limitations

Runtime imports accept local files within permitted folders or expression-free public HTTPS content. Line numbers refer to raw content before front matter removal. Remote content is checked for expressions and directives before line selection or stripping.

### Deprecated `{{#import}}`

`{{#import filepath}}` (without `runtime-`) is a **deprecated** body-level shorthand. It normalizes to `{{#runtime-import filepath}}` at runtime for backward compatibility, but emits deprecation warnings at both compile time and runtime. Use `{{#runtime-import}}` directly for all new workflows. See [Imports](/gh-aw/reference/imports/) for details.

### Error Handling

| Error | Message |
|-------|---------|
| File not found | `Runtime import file not found: missing.txt` |
| Invalid line range | `Invalid start line 100 for file docs/main.go (total lines: 50)` |
| Path traversal | `Security: Path ../src/main.go must be within .github folder` |
| GitHub Actions macros | `File template.md contains GitHub Actions macros (${{ ... }}) which are not allowed in runtime imports` |
| Unsupported target | `Runtime imports require local files or literal public HTTPS URLs` |
| Remote expression or directive | `URL runtime imports must not contain expressions or template directives` |

## Learn More

- [Markdown](/gh-aw/reference/markdown/) for writing effective agentic markdown
- [Workflow Structure](/gh-aw/reference/workflow-structure/) for overall workflow organization
- [Frontmatter](/gh-aw/reference/frontmatter/) for YAML configuration
- [Imports](/gh-aw/reference/imports/) for compile-time imports in frontmatter
