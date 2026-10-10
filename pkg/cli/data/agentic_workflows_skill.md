---
name: agentic-workflows
description: Route gh-aw design, creation, diagnosis, patching, active debugging, and upgrade requests to the right strategies.
---

# Agentic Workflows Router

Use this skill when a user asks to design, create, update, debug, or upgrade GitHub Agentic Workflows in this repository.

This skill is a dispatcher: identify the task type, load the matching workflow prompt/skill file, and follow it directly. Keep responses concise and ask a clarifying question if the correct prompt is unclear.

Repository overlay (optional):
- If `.github/aw/instructions.md` exists, load it with `@.github/aw/instructions.md` after loading the matched prompt/skill.
- Precedence: repository overlay instructions override upstream defaults when they conflict.

Read only the files you need:
Load these files from `github/gh-aw` (they are not available locally).
{{AW_FILE_LIST}}
After loading the matching workflow prompt or skill, follow it directly:
- Design workflows from scratch via interview: `.github/aw/designer.md`
- Create new workflows: `.github/aw/create-agentic-workflow.md`
- Configure or add declarative engines: `.github/aw/configure-agentic-engine.md`
- Update existing workflows: `.github/aw/update-agentic-workflow.md`
- Diagnose, patch, audit, or actively debug workflows: `.github/aw/debug-agentic-workflow.md` (local-first strategy, evidence triage and live gates)
- AWF model/endpoint 400s, silent cross-family sub-agent failures, `model: auto` failures, install-step 404s after a version pin, or questions about version fields: go directly to [Model and engine misconfiguration](../../aw/debug-agentic-workflow.md#model-and-engine-misconfiguration) before suggesting model changes.
- Upgrade workflows and fix deprecations: `.github/aw/upgrade-agentic-workflows.md`
- Create shared components or MCP wrappers: `.github/aw/create-shared-agentic-workflow.md`
- Create report-generating workflows: `.github/aw/report.md`
- Fix Dependabot manifest PRs: `.github/aw/dependabot.md`
- Analyze coverage workflows: `.github/aw/test-coverage.md`
- Render compact markdown charts: `.github/aw/asciicharts.md`
- Map CLI commands to MCP usage: `.github/aw/cli-commands.md`
- Choose workflow architecture and patterns: `.github/aw/patterns.md`
- Design, deploy, inspect or recover a Git-backed work queue: `.github/aw/work-queue.md`
- Optimize token usage and cost: `.github/aw/token-optimization.md`
- Design long-running multi-agent research workflows: `.github/aw/multi-agent-research.md`
- Add skills or agent plugins requested by the user (`skills:` / `plugins:` frontmatter, never on-the-fly installs): `.github/aw/skills.md`

When the task involves OTEL, OTLP, traces, observability backends, or telemetry-driven analysis, also read and follow `skills/otel-queries/SKILL.md` after loading the matching workflow prompt or skill.

When creating, updating, diagnosing or upgrading a workflow that uses `tools.work-queue`, also load `.github/aw/work-queue.md` after the primary prompt. Load deployment docs, operator reference or specification only when that task needs them.
