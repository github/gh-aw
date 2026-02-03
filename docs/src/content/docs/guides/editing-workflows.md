---
title: Editing Workflows
description: Learn when you can edit workflows directly on GitHub.com versus when recompilation is required, and best practices for iterating on agentic workflows.
sidebar:
  order: 5
---

Agentic workflows consist of two distinct parts with different editing requirements: the **YAML frontmatter** (configuration) and the **markdown body** (AI instructions). Understanding when changes require recompilation helps you iterate quickly and efficiently.

See [Creating Agentic Workflows](/gh-aw/setup/creating-workflows/) for guidance on creating workflows with AI assistance.

## Overview

Workflow files (`.md`) are compiled into GitHub Actions workflow files (`.lock.yml`). The compilation process:

- **Embeds frontmatter** directly into the lock file (changes require recompilation)
- **Loads the markdown body** at runtime from the source file (changes do NOT require recompilation)

This design allows you to quickly iterate on AI instructions without recompilation while maintaining strict control over security-sensitive configuration.

## Editing Without Recompilation

> [!TIP]
> You can edit the **markdown body** directly on GitHub.com or in any editor without recompiling. Changes take effect on the next workflow run.

### What You Can Edit

The markdown body is loaded at runtime from the original `.md` file. You can freely edit:

- **AI instructions**: Task descriptions, step-by-step guidance, examples
- **Context explanations**: Project conventions, background information
- **Output formatting**: Templates for issues, PRs, comments
- **Conditional logic**: "If X, then do Y" instructions
- **Documentation**: Headers, examples, clarifications

### Workflow for Quick Iterations

```bash
# 1. Edit the markdown body on GitHub.com
#    Navigate to .github/workflows/my-workflow.md
#    Click "Edit" and modify instructions

# 2. Commit changes directly to main (or create PR)

# 3. Trigger the workflow
#    Changes are immediately active - no recompilation needed!
```

### Example: Adding Instructions

**Before** (in `.github/workflows/issue-triage.md`):
```markdown
---
on:
  issues:
    types: [opened]
---

# Issue Triage

Read issue #${{ github.event.issue.number }} and add appropriate labels.
```

**After** (edited on GitHub.com):
```markdown
---
on:
  issues:
    types: [opened]
---

# Issue Triage

Read issue #${{ github.event.issue.number }} and add appropriate labels.

## Labeling Criteria

Apply these labels based on content:
- `bug`: Issues describing incorrect behavior with reproduction steps
- `enhancement`: Feature requests or improvements
- `question`: Help requests or clarifications needed
- `documentation`: Documentation updates or corrections

For priority, consider:
- `high-priority`: Security issues, critical bugs, blocking issues
- `medium-priority`: Important features, non-critical bugs
- `low-priority`: Nice-to-have improvements, minor enhancements
```

✅ This change takes effect immediately without recompilation.

## Editing With Recompilation Required

> [!WARNING]
> Changes to the **YAML frontmatter** always require recompilation. These are security-sensitive configuration options.

### What Requires Recompilation

Any changes to the frontmatter configuration between `---` markers:

- **Triggers** (`on:`): Event types, filters, schedules
- **Permissions** (`permissions:`): Repository access levels
- **Tools** (`tools:`): Tool configurations, MCP servers, allowed tools
- **Network** (`network:`): Allowed domains, firewall rules
- **Safe outputs** (`safe-outputs:`): Output types, threat detection
- **Safe inputs** (`safe-inputs:`): Input validation rules
- **Runtimes** (`runtimes:`): Node, Python, Go version overrides
- **Imports** (`imports:`): Shared configuration files
- **Custom jobs** (`jobs:`): Additional workflow jobs
- **Engine** (`engine:`): AI engine selection (copilot, claude, codex)
- **Timeout** (`timeout-minutes:`): Maximum execution time
- **Roles** (`roles:`): Permission requirements for actors

### Example: Adding a Tool (Requires Recompilation)

**Before**:
```yaml
---
on:
  issues:
    types: [opened]

permissions:
  issues: write
---
```

**After** (must recompile):
```yaml
---
on:
  issues:
    types: [opened]

permissions:
  issues: write

tools:
  github:
    toolsets: [issues]
---
```

⚠️ Run `gh aw compile my-workflow` before committing this change.

## Expressions and Environment Variables

> [!CAUTION]
> Adding GitHub Actions expressions (`${{ ... }}`) in the markdown body does NOT require recompilation, but they are subject to security validation.

### Allowed Expressions

You can safely use these expressions in markdown without recompilation:

```markdown
# Process Issue

Read issue #${{ github.event.issue.number }} in repository ${{ github.repository }}.

Issue title: "${{ github.event.issue.title }}"

Use sanitized content: "${{ needs.activation.outputs.text }}"

Actor: ${{ github.actor }}
Repository: ${{ github.repository }}
```

These expressions are evaluated at runtime and validated for security. See [Templating](/gh-aw/reference/templating/) for the complete list of allowed expressions.

### Prohibited Expressions

Arbitrary expressions are blocked for security. This will fail at runtime:

```markdown
# ❌ WRONG - Will be rejected
Run this command: ${{ github.event.comment.body }}
```

Use `needs.activation.outputs.text` for sanitized user input instead.

## Quick Reference

| Change Type | Example | Recompilation? | Edit Location |
|-------------|---------|----------------|---------------|
| **AI instructions** | Add task steps | ❌ No | GitHub.com or any editor |
| **Output templates** | Change issue format | ❌ No | GitHub.com or any editor |
| **Conditional logic** | "If bug, then..." | ❌ No | GitHub.com or any editor |
| **GitHub expressions** | Add `${{ github.actor }}` | ❌ No | GitHub.com or any editor |
| **Tools** | Add GitHub toolset | ✅ Yes | Local + compile |
| **Permissions** | Add `contents: write` | ✅ Yes | Local + compile |
| **Triggers** | Add `schedule:` | ✅ Yes | Local + compile |
| **Network rules** | Add allowed domain | ✅ Yes | Local + compile |
| **Safe outputs** | Add `create-issue:` | ✅ Yes | Local + compile |
| **Engine** | Change to Claude | ✅ Yes | Local + compile |
| **Imports** | Add shared config | ✅ Yes | Local + compile |

## Related Documentation

- [Workflow Structure](/gh-aw/reference/workflow-structure/) - Overall file organization
- [Frontmatter Reference](/gh-aw/reference/frontmatter/) - All configuration options
- [Markdown Reference](/gh-aw/reference/markdown/) - Writing effective instructions
- [Compilation Process](/gh-aw/reference/compilation-process/) - How compilation works
- [Templating](/gh-aw/reference/templating/) - Expression syntax and substitution
