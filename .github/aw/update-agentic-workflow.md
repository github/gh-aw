---
description: Update existing agentic workflows using GitHub Agentic Workflows (gh-aw) with concise guidance on minimal changes and validation.
disable-model-invocation: true
---

# GitHub Agentic Workflow Updater

Update existing workflow files in `.github/workflows/`.

## Load These References First

- [github-agentic-workflows.md](github-agentic-workflows.md)
- [workflow-editing.md](workflow-editing.md)
- [workflow-constraints.md](workflow-constraints.md)
- [safe-outputs.md](safe-outputs.md)
- [syntax.md](syntax.md)
- [intent.md](intent.md) for preserving the outcome and re-deriving evals or operational value when it changes

Load these additional files only when relevant:

- [campaign.md](campaign.md)
- [experiments.md](experiments.md)
- [visual-regression.md](visual-regression.md)
- [serena-tool.md](serena-tool.md)
- [linter-workflows.md](linter-workflows.md)
- [agent-runtime-instructions.md](agent-runtime-instructions.md) for changes involving Docker, gVisor, Docker sbx, ARC DinD, self-hosted runners, or `sandbox.agent.runtime-install`
- [skills.md](skills.md) when the user asks to add specific skills or agent plugins

## Scope

This prompt is for **updating existing workflows only**. For new workflows, use the creator prompt.

## Start the Conversation

1. Ask which workflow to update.
2. Ask what change is needed.
3. Then inspect the existing file, including its `intent:`, before proposing edits.

## First Decision: Frontmatter or Prompt Body?

Use [workflow-editing.md](workflow-editing.md) as the source of truth for when recompilation is required. Always compile after any edit to keep `.lock.yml` in sync, even for body-only changes.

## Update Rules

- make the smallest possible change
- preserve existing style and structure unless reorganization is required
- do not rewrite unrelated frontmatter sections
- preserve the existing `intent:` for implementation-only changes, including trigger or output-channel redesigns
- when the requested outcome materially expands, contracts, or changes, update `intent:` and re-derive its applicability, required effects, no-op conditions, architecture, and evals using [intent.md](intent.md)
- when an implementation-only change selects a different architecture, revalidate activation conditions, evidence window, deduplication or previous-result strategy, no-op behavior, and evals so event-specific rules do not survive an incompatible redesign
- when targeting the Copilot coding agent, recommend `permissions: { copilot-requests: write }` for Copilot authentication
- prefer `toolsets:` for GitHub tools
- when the user asks for specific skills or agent plugins, add them to the top-level `skills:` / `plugins:` frontmatter fields; never add on-the-fly install steps or prompt instructions to install them at run time (see [skills.md](skills.md))

See [workflow-constraints.md](workflow-constraints.md) for the read-only security posture (keep the agent job read-only, route writes through `safe-outputs:`).

## Common Update Categories

See [workflow-editing.md](workflow-editing.md) for the full frontmatter-vs-body recompilation taxonomy and the field list that requires `gh aw compile <workflow-id>` plus a `.lock.yml` review.

## Cost-Oriented Update Checks

When refining existing workflows, keep edits minimal and confirm the design still follows the [High-Volume Triage and Escalation Pattern](workflow-patterns.md#high-volume-triage-and-escalation-pattern): cheap triage before escalation, `noop`/safe output for known/duplicate/stale cases, frontier reasoning reserved for high-value cases, and context pulled on demand. Keep sub-agent fan-out bounded (see [subagents.md](subagents.md)), then measure the change with `gh aw audit` and treat token or quality regressions as failures (see [token-optimization.md](token-optimization.md)).

## Security Rules

- never suggest GitHub mutation through raw GitHub tools when a safe output exists
- do not recommend `mode: remote` for GitHub tools unless explicitly required and properly configured
- do not replace `pull_request` with `pull_request_target` unless the user explicitly needs a `pull_request_target` design
- do not use `post-steps:` for agent-driven write behavior that belongs in a safe-output job

## Safer-Alternatives Pattern

Follow the "Safer Alternatives First" pattern in [workflow-constraints.md](workflow-constraints.md) when a requested change raises risk.

## Minimal Examples

### Add a GitHub toolset

```yaml
tools:
  github:
    toolsets: [default]
```

### Add a safe output

```yaml
safe-outputs:
  add-comment:
    max: 1
```

### Add network access

```yaml
network:
  allowed:
    - defaults
    - node
```

### Add a skill or agent plugin

```yaml
skills:
  - mattpocock/skills/tdd@801dca688564c529fa84f247f64472520d9ebe28
plugins:
  - octo-org/agent-plugin@v1
```

## Validation Flow

- always inspect the workflow before editing
- explicitly determine whether the existing intent is preserved or changed
- always compile after any change to keep `.lock.yml` in sync
- keep the workflow valid at every step
- summarize what changed and whether recompilation was needed

## Final Steps

1. compile with `gh aw compile <workflow-id>`
2. fix all compile errors
3. include the updated `.lock.yml` in the PR

## Final Message Rules

At the end, tell the user:

- what changed
- whether the change touched frontmatter or prompt body
- whether recompilation was required
- any next step they should take

Keep the summary short.
