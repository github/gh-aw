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
- [release-workflow.md](release-workflow.md) whenever a workflow creates or updates release notes
- [documentation-workflows.md](documentation-workflows.md) for documentation agents, required docs checks, and post-merge page/index upkeep
- [agent-runtime-instructions.md](agent-runtime-instructions.md) for changes involving Docker, Cloud Hypervisor, ARC DinD, or self-hosted runners
- [skills.md](skills.md) when the user asks to add specific skills or agent plugins
- [agentic-engine.md](agentic-engine.md) when changing engines; honor developer
  harness preferences and preserve the engine during unrelated updates
- [operational-value-designer](../skills/operational-value-designer/SKILL.md) when updating graders, their evidence contract, or legacy evaluator syntax

## Scope

This prompt is for **updating existing workflows only**. For new workflows, use the creator prompt.

## Start the Conversation

1. Ask which workflow to update.
2. Ask what change is needed.
3. Then inspect the existing file, including its `intent:`, before proposing edits.

## First Decision: Frontmatter or Prompt Body?

Use [workflow-editing.md](workflow-editing.md) as the source of truth for the frontmatter-vs-body recompilation taxonomy and the `.lock.yml` review. Always compile after any edit, even for body-only changes.

Apply the [browser decision](playwright.md#browser-decision-and-readiness) when
the outcome or validation requirements change. Inspect existing tools, scripts,
and tests; configure supporting lifecycle permissions, not just Playwright.

## Update Rules

- make the smallest possible change
- preserve existing style and structure unless reorganization is required
- do not rewrite unrelated frontmatter sections
- preserve the existing `intent:` for implementation-only changes, including trigger or output-channel redesigns
- when the requested outcome materially expands, contracts, or changes, update `intent:` and re-derive its applicability, required effects, no-op conditions, architecture, and evals using [intent.md](intent.md)
- when an implementation-only change selects a different architecture, revalidate activation conditions, evidence window, deduplication or previous-result strategy, no-op behavior, and evals so event-specific rules do not survive an incompatible redesign
- for Copilot inference, recommend `permissions: { copilot-requests: write }` (scope and caveats: [syntax-core.md](syntax-core.md))
- prefer `toolsets:` for GitHub tools
- require `update-release` for agent-generated release notes or release-description changes; never use direct `gh`, API, or GitHub write-tool mutations from the agent
- when the user asks for specific skills or agent plugins, add them to the top-level `skills:` / `plugins:` frontmatter fields; never add on-the-fly install steps or prompt instructions to install them at run time (see [skills.md](skills.md))
- migrate run-scoped outcome grading to `graders.operational-value.script`: synchronous restricted JavaScript over immutable `session`, `config`, and `helpers`; return a nonempty ordered array of unique `{id,value}` metrics whose first value is primary
- use `graders.<distinct-id>.script` for other custom metrics; return one finite number, `null`, or an object with `value`
- reject removed `graders.operational-value.run` declarations with migration guidance; never ignore or translate shell automatically or wrap file/API behavior in JavaScript

See [workflow-constraints.md](workflow-constraints.md) for the read-only security posture (keep the agent job read-only, route writes through `safe-outputs:`).

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
- keep the workflow valid at every step

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
