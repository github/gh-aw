---
description: GitHub Agentic Workflows
applyTo: ".github/workflows/*.md,.github/workflows/**/*.md"
---

# GitHub Agentic Workflows

## Persona-to-Pattern Quick Matrix

Persona-lens view of the same facts as the canonical [Decision Matrix](triggers.md#decision-matrix) in triggers.md; update both when a trigger/tool/output mapping changes.

| Persona | Preferred trigger and scope | Typical read tools | Typical write path | Explicit `noop` rule |
|---|---|---|---|---|
| Backend Engineer | `pull_request` with `paths:` scoped to migrations, schema, and API contracts | `github` (`gh-proxy`) | `add-comment` for PR-local findings; `create-issue` only for cross-cutting incidents | `noop` when no backend contract files changed |
| Frontend Developer | `pull_request` with `paths:` scoped to UI, design-token, and asset files | `github` (`gh-proxy`), optional `playwright`, optional `cache-memory` for baselines | `add-comment` | `noop` when no UI/token files changed or no actionable visual/token issues were found |
| DevOps Engineer | `workflow_run` for GitHub Actions failures, `deployment_status` for external deployment failures | `github` (`gh-proxy`) with `actions: read` or `deployments: read` | `create-issue` with stable dedup key | `noop` when status is non-terminal, self-recovered, or an open incident already exists for the same dedup key |
| Program Manager | `schedule` (+ `workflow_dispatch` for previews and backfills) | `github` (`gh-proxy`) | `create-issue` with `close-older-issues: true` for recurring digests | `noop` when the reporting window contains no qualifying updates |
| Designer | `pull_request` with `paths:` scoped to UI, design-token, copy, and asset files | `github` (`gh-proxy`); optional `playwright` for visual checks | `add-comment` on the PR | `noop` when scoped paths are unchanged or no actionable design/token issue is found |
| Legal / Compliance | `pull_request` with `paths:` scoped to dependency manifests or policy docs for PR reviews; `schedule` for recurring audits | `github` (`gh-proxy`) | `add-comment` for findings; `create-issue` only for violations requiring team-wide follow-up | `noop` when no in-scope files changed or all findings are in the allowed tier; always search for an existing open issue before escalating |

## Persona-to-Toolset Matrix

| Persona | Default toolset is enough when... | Name optional tools when... |
|---|---|---|
| Program Manager | Digest/report uses GitHub data only (`tools.github.toolsets: [default]`) | Add `cache-memory` only when trend baselines/deltas must persist across runs |
| Designer | PR review is metadata/content-aware via GitHub reads only | Add `playwright` for screenshot/visual checks; add `cache-memory` when baselines or snapshot history are required |
| Legal / Compliance | Policy/dependency review is repo-state and metadata driven | Add `cache-memory` when recurring audits need prior-run evidence/comparison state |

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
- For visual regression workflows, explicitly name the baseline source (for example `cache-memory` key, artifact, or branch path). See [visual-regression.md](visual-regression.md).

See [workflow-constraints.md](workflow-constraints.md) for the security posture (read-only job, safe-outputs routing, gh-proxy/cli-proxy, network constraints, sanitized text), safer-alternatives pattern, and common risk areas.

## Repository-Specific Instructions

Use `@.github/aw/instructions.md` as the canonical repository-local overlay for workflow authoring standards.

- This file is optional and repository-owned.
- Installed gh-aw agents should load and apply it automatically when present.
- Precedence: apply upstream defaults first, then apply repository overlay rules; when they conflict, repository overlay rules win.

## Trigger Selection

Use the smallest trigger that matches the requested automation. See the [Decision Matrix](triggers.md#decision-matrix) in triggers.md for the canonical trigger-to-use-case mapping, and [workflow-constraints.md](workflow-constraints.md) for the security posture.

## Ad Hoc Scenario Evaluation

Installed gh-aw agents should support scenario evaluation requests that do not create workflow files.

- Treat prompts such as `agentic-workflows evaluate this scenario without creating files` as ad hoc evaluation mode.
- For explicit research/evaluation requests, invoke with wording such as `agentic-workflows evaluate this scenario without creating files` or `agentic-workflows research this workflow pattern and return recommendations only`.
- Return a compact design recommendation covering trigger, scope, tools, permissions, safe outputs, `noop` behavior, and any report window / grouping / deduplication requirements.
- Offer to turn the recommendation into `.github/workflows/<workflow-id>.md` only if the user asks to proceed.

### Supported Invocation Surface

Ad hoc scenario evaluation is a **conversation-mode capability** of the installed `agentic-workflows` custom agent, not a CLI flag or MCP tool parameter — no `gh aw` CLI/MCP command accepts a freeform `prompt`/`scenario`/`query` parameter. See [Invocation Surface](create-agentic-workflow.md#ad-hoc-evaluation-mode) in create-agentic-workflow.md for the full explanation and recovery steps if a tool call returns `Unknown parameter`.

### Program Manager digest example

```yaml
on:
  schedule:
    - cron: "0 9 * * 1" # weekly, Monday 09:00
  workflow_dispatch:
permissions:
  contents: read
  issues: write
tools:
  github:
    mode: gh-proxy
    toolsets: [default]
safe-outputs:
  create-issue:
    close-older-issues: true
```

- Reporting window: 7 days, ending at run time; use `workflow_dispatch` for previews, reruns, or backfills of a prior window.
- Grouping dimensions: group items by owning team or repository, then by status (in-progress, blocked, at-risk).
- Dedup key example: `pm-digest:<window-id>` (for example `pm-digest:2026-W33`); combine with `close-older-issues: true` so each run supersedes the previous digest issue instead of accumulating duplicates.
- Call `noop` when the reporting window has no qualifying updates.

### Non-technical persona examples

Trigger and write-path are the same as the [Persona-to-Pattern Quick Matrix](#persona-to-pattern-quick-matrix) above. For ad hoc evaluation, also gather:

| Persona | Key prompt details |
|---|---|
| Program Manager | Report window, grouping dimensions, stable dedup key, and `noop` for empty windows |
| Designer | Review rubric (accessibility, token consistency, asset policy); `noop` when scoped files unchanged |
| Legal / Compliance | Classify against policy tiers; dedup before escalating; `noop` when no in-scope change or violation |

## PR Checks with Linked References

When a PR analysis requires verifying or attaching a linked artifact (design doc, policy link, architecture decision record, or approval), follow this compact pattern:

1. **Read the linked reference** from the PR body or comments (for example, a URL, a markdown link, or an ADR reference token like `ADR-NN`) using `gh pr view`.
2. **Validate the link** — confirm the document exists and is accessible before assessing compliance.
3. **Classify the result**:
   - Link present and satisfies requirement → `add-comment` with a ✅ summary
   - Link present but does not satisfy requirement → `add-comment` flagging the specific gap
   - Link missing → `add-comment` requesting it, or `create-issue` if policy requires a blocking escalation
4. **Call `noop`** when the PR is not in scope (for example `paths:` guard excludes all changed files).

Permissions: `pull-requests: read` only; all writes route through `add-comment` safe output.

For the full dependency-license/compliance review pattern (paths scoping, license-tier classification, escalation table), see [Compliance review guidance](create-agentic-workflow-trigger-details.md#compliance-review-guidance).

## Reference Files

| Topic | File |
|---|---|
| Editing and recompilation rules | [workflow-editing.md](workflow-editing.md) |
| Architectural and security constraints | [workflow-constraints.md](workflow-constraints.md) |
| Common design patterns | [workflow-patterns.md](workflow-patterns.md) |
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

## Compile Commands

```bash
gh aw compile
gh aw compile <workflow-id>
gh aw compile --purge
gh aw compile --strict
```
