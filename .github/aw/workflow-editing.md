---
description: Shared guidance for editing, recompiling, and validating GitHub Agentic Workflow files.
---

# Workflow Editing Basics

Agentic workflows are single markdown files at `.github/workflows/<workflow-id>.md`.

## File Structure

1. **YAML frontmatter** between `---` markers: triggers, permissions, tools, network, imports, safe outputs.
2. **Markdown body**: the agent prompt.

## Recompile When Changing Frontmatter Fields

Run `gh aw compile <workflow-id>` after changing:

- `on:`
- `permissions:`
- `tools:`
- `network:`
- `imports:`
- `safe-outputs:`
- `mcp-servers:`
- engine, timeout, concurrency, or other YAML configuration

## No Recompile Required for Runtime Behavior

Body-only edits take effect on the next run without recompilation.

Edit the markdown body directly for:

- agent instructions
- task descriptions
- examples
- formatting guidance
- clarifications and guardrails

Body changes take effect on the next run.

**Always run `gh aw compile` after any change** (frontmatter or body) to keep `.lock.yml` metadata in sync.

## Validation Commands

```bash
gh aw compile <workflow-id>
gh aw compile <workflow-id> --strict
gh aw compile --purge
```

Use `--strict` for production-quality validation.

## Detect Stale Lock Files

Run `gh aw list` from the repository root and check the `COMPILED` column. `No` means the source frontmatter does not match the existing `.lock.yml`; `N/A` means the lock file is missing.

To inspect stale or missing lock files interactively, use `--stale`:

```bash
gh aw list --stale
```

For an automated check that fails when any lock file is stale or missing, use:

```bash
gh aw list --json | jq -e 'all(.[]; .compiled == "Yes")'
```

Recompile any stale workflows and commit each generated `.lock.yml` with its source:

```bash
gh aw compile <workflow-id>
```

## Editing Rules

- Smallest change that satisfies the request.
- Preserve structure unless reorganization is the task.
- Never leave a workflow broken.
- Always run `gh aw compile <workflow-id>` after any change (frontmatter or body) to keep `.lock.yml` in sync.
- If compile fails, fix all errors before stopping.
- After any change, review the generated `.lock.yml`.

## Prompt-Authoring Rules

- Specific and imperative.
- Short examples over long tutorials.
- Reference dedicated instruction files instead of duplicating.
- Tell agents to use `noop` when no visible action is needed.
