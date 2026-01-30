---
title: Workflow Structure
description: Learn how agentic workflows are organized and structured within your repository, including directory layout and file organization.
sidebar:
  order: 100
---

Each workflow consists of:

1. **YAML Frontmatter**: Configuration options wrapped in `---`. See [Frontmatter](/gh-aw/reference/frontmatter/) for details.
2. **Markdown**: Natural language instructions for the AI. See [Markdown](/gh-aw/reference/markdown/).

For example:

```aw wrap
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

# Workflow Description

Read the issue #${{ github.event.issue.number }}. Add a comment to the issue listing useful resources and links.
```

## File Organization

Agentic workflows are stored in the `.github/workflows` folder as Markdown files (`*.md`)
and they are compiled to GitHub Actions Workflows files (`*.lock.yml`)

```text
.github/
└── workflows/
  ├── ci-doctor.md # Agentic Workflow
  └── ci-doctor.lock.yml # Compiled GitHub Actions Workflow
```

When you run the `compile` command you generate the lock file.

```sh wrap
gh aw compile
```

## Editing Workflows

> [!TIP]
> The **markdown body** is loaded at runtime and can be edited directly on GitHub.com without recompilation. Only **frontmatter changes** require recompilation.

See [Editing Workflows](/gh-aw/guides/editing-workflows/) for complete guidance on when recompilation is needed.

## Best Practices

- Use descriptive names: `issue-responder.md`, `pr-reviewer.md`
- Follow kebab-case convention: `weekly-summary.md`
- Avoid spaces and special characters
- **Commit source files**: Always commit `.md` files
- **Commit generated files**: Also commit `.lock.yml` files for transparency

## Related Documentation

- [Editing Workflows](/gh-aw/guides/editing-workflows/) - When to recompile vs edit directly
- [Frontmatter](/gh-aw/reference/frontmatter/) - Configuration options for workflows
- [Markdown](/gh-aw/reference/markdown/) - The main markdown content of workflows
- [Imports](/gh-aw/reference/imports/) - Modularizing workflows with includes
- [CLI Commands](/gh-aw/setup/cli/) - CLI commands for workflow management
- [MCPs](/gh-aw/guides/mcps/) - Model Context Protocol configuration
