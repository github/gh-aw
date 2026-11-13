---
description: GitHub Agentic Workflows
applyTo: ".github/workflows/*.md,.github/workflows/**/*.md"
---

# GitHub Agentic Workflows

Use for workflow authoring. Start with the [creator](create-agentic-workflow.md),
[updater](update-agentic-workflow.md), or [debugger](debug-agentic-workflow.md).
Load only the topic references required by the task.

## Persona-to-Pattern Quick Matrix

Use the canonical [Decision Matrix](triggers.md#decision-matrix) and
[persona scenario map](create-agentic-workflow-trigger-details.md#persona-oriented-scenario-map)
for triggers, path scope, outputs and `noop` conditions.

## Persona-to-Toolset Matrix

Use GitHub's default toolset for GitHub-only reads. Add `playwright` for visual
checks; add `cache-memory` only for persistent baselines or comparisons.

## Replace Previous Results

When a recurring digest or PR-reactive update is superseded by the newest result, prefer replacing the prior output instead of accumulating stale results:

- For recurring issue digests, use `create-issue` with `close-older-issues: true`.
- For rolling PR comments, use `add-comment` with `hide-older-comments: true`.

Wait for the previous result when only one active issue or PR should exist; keep prior outputs when each result is intentionally distinct or history is required.

## File Format

Agentic workflows are markdown files with YAML frontmatter.

```markdown
---
emoji: 🧠
name: My Workflow
description: Short description
on:
  issues:
    types: [opened]
permissions:
  contents: read
  actions: read
strict: true
network:
  allowed: [defaults, github]
tools:
  github:
    mode: gh-proxy
    toolsets: [default]
safe-outputs:
  add-comment:
---

# Workflow Title

Natural language instructions for the AI agent.
```

## Recompilation Rule

See [workflow-editing.md](workflow-editing.md) for when `gh aw compile` is required.

## Core Rules

- Set `strict: true` for production workflows.
- Limit `bash` access to what the workflow actually needs.
- Keep agent-job permissions read-only when writes are handled solely by safe outputs. For example, `add-comment` needs `pull-requests: read` to inspect a PR, not `pull-requests: write`; `create-issue` needs `issues: read` to check existing issues, not `issues: write`. Safe-output writes run in separate jobs with their own permissions.
- For visual regression workflows, explicitly name the baseline source (for example `cache-memory` key, artifact, or branch path). See [visual-regression.md](visual-regression.md).

See [workflow-constraints.md](workflow-constraints.md) for the security posture (read-only job, safe-outputs routing, gh-proxy/cli-proxy, network constraints, sanitized text), safer-alternatives pattern, and common risk areas.

## Repository-Specific Instructions

Use `@.github/aw/instructions.md` as the canonical repository-local overlay for workflow authoring standards.

- This file is optional and repository-owned.
- Installed gh-aw agents should load and apply it automatically when present.
- Precedence: apply upstream defaults first, then apply repository overlay rules; when they conflict, repository overlay rules win.

## Trigger Selection

Use the smallest trigger that matches the requested automation. See the [Decision Matrix](triggers.md#decision-matrix) in triggers.md for the canonical trigger-to-use-case mapping, and [workflow-constraints.md](workflow-constraints.md) for the security posture.

Use `pull_request` with `paths:` for checks that need only changed files. When a PR check depends on an artifact produced by another Actions workflow, use `workflow_run` scoped to that workflow with `types: [completed]` and gate on a successful conclusion, rather than racing the artifact producer on `pull_request`. Use `schedule` (with `workflow_dispatch` for manual runs) when an audit must correlate results across runs or inspect drift independently of a single PR or CI run.

## Ad Hoc Scenario Evaluation

For exploration/persona requests, follow
[ad hoc evaluation](create-agentic-workflow.md#ad-hoc-evaluation-mode).
Do not create/edit workflows without an explicit implementation request.
Address the custom agent in conversation; CLI/MCP workflow-management tools
do not accept freeform `prompt`, `scenario` or `query` parameters.
Return trigger, scope, tools, permissions, safe outputs and `noop` criteria.
For recurring reports, state window, grouping dimensions and a stable dedup key;
replace superseded digests with `create-issue.close-older-issues: true`.
Name design/accessibility rubrics and policy tiers when relevant; deduplicate
before escalating compliance findings.

## PR Checks with Linked References

Read PR-body/comment references with `gh pr view`; verify access and content
before assessing compliance. Use `add-comment` for satisfied requirements,
specific gaps or missing links; escalate via `create-issue` only when policy
requires it. Call `noop` for out-of-scope PRs. Keep agent permissions read-only.
Use [compliance guidance](create-agentic-workflow-trigger-details.md#compliance-review-guidance)
for license tiers, path scope and escalation.

## Reference Files

| Topic | File |
|---|---|
| Editing and recompilation rules | [workflow-editing.md](workflow-editing.md) |
| Architectural and security constraints | [workflow-constraints.md](workflow-constraints.md) |
| Common design patterns | [workflow-patterns.md](workflow-patterns.md) |
| Git-backed work queue dispatchers, workers, and CLI inspection | [work-queue.md](work-queue.md) |
| Frontmatter schema index | [syntax.md](syntax.md) |
| Safe outputs index | [safe-outputs.md](safe-outputs.md) |
| Trigger patterns | [triggers.md](triggers.md) |
| Context expressions and `{{#if}}` templates | [context.md](context.md) |
| Declarative engine configuration | [configure-agentic-engine.md](configure-agentic-engine.md) |
| Agent runtime selection (Docker, Cloud Hypervisor, ARC DinD) | [agent-runtime-instructions.md](agent-runtime-instructions.md) |
| Private-repository enclaves (preview) | [enclaves.md](enclaves.md) |
| CLI commands and MCP equivalents | [cli-commands.md](cli-commands.md) |
| Network configuration | [network.md](network.md) |
| Memory and persistence | [memory.md](memory.md) |
| Drive memory (private preview) | [drive-memory.md](drive-memory.md) |
| Imports and shared components | [reuse.md](reuse.md) |
| Sub-agents | [subagents.md](subagents.md) |
| Skills | [skills.md](skills.md) |
| Token cost optimization | [token-optimization.md](token-optimization.md) |
| GitHub MCP server configuration | [github-mcp-server.md](github-mcp-server.md) |
| GitHub MCP server per-toolset tool reference | [github-mcp-server-tools.md](github-mcp-server-tools.md) |
| GitHub MCP server pagination limits | [github-mcp-server-pagination.md](github-mcp-server-pagination.md) |
| Compiler-generated jobs, credentials, and job graph | [jobs.md](jobs.md) |
| Campaign and KPI patterns | [campaign.md](campaign.md) |
| Experiments and A/B testing | [experiments.md](experiments.md) |
| Charts and Python data visualization | [charts.md](charts.md) |
| LLM API endpoint discovery | [llms.md](llms.md) |
| Local SDLC issue bootstrap | [squad.md](squad.md) |

## Compile Commands

```bash
gh aw compile
gh aw compile <workflow-id>
gh aw compile --purge
gh aw compile --strict
```
