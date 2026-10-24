---
name: agentic-workflows
description: Route gh-aw design, creation, diagnosis, patching, active debugging, and upgrade requests to the right strategies.
---

# Agentic Workflows Router

Use this skill when a user asks to design, create, update, debug, or upgrade GitHub Agentic Workflows in this repository.

This skill is a dispatcher: identify the task type, load the matching workflow prompt/skill file, and follow it directly. Keep responses concise and ask a clarifying question if the correct prompt is unclear.

For debugging sessions needing CLI setup or run-URL scoping, first load
`https://raw.githubusercontent.com/github/gh-aw/main/.github/aw/debug-bootstrap.md`; it bootstraps
the same shared strategy used by the debugging routes below.

Debugging, incident, health, and failure-related optimization requests use the
bootstrap's intent routing before generic update, upgrade, or optimization routes,
even when the CLI is installed. Preserve the supplied guidance repository/ref
for all debugging links and public raw fallbacks; URLs showing `main` below are
defaults only when no guidance revision is supplied. Reuse already-loaded guides.
The intent index is `.github/aw/debug-scenarios.md`; load only the selected
specialized route, not every debugging reference.

Repository overlay (optional):
- Load the target repository's `.github/aw/instructions.md`, if present, after the matched prompt/skill. Use `@.github/aw/instructions.md` only in a matching checkout; do not substitute a guidance-repository overlay.
- Precedence: repository overlay instructions override upstream defaults when they conflict.

Read only the files you need:
Use a local guidance checkout only when its repository/ref matches the requested guidance; otherwise use the resolved guidance repository/ref (default `github/gh-aw`).
If GitHub API, `gh api`, or GitHub MCP downloads return HTTP 403 (including a token SAML-authorized for a different organization), do not retry with the same token. Fetch the public file without authentication from `https://raw.githubusercontent.com/github/gh-aw/main/<path>` instead, preserving its repository-relative path (including `.github/`). For example: `curl --fail --silent --show-error --location 'https://raw.githubusercontent.com/github/gh-aw/main/.github/aw/create-agentic-workflow.md'`. Use the same fallback for referenced instructions and `SKILL.md` files; never send an Authorization header or token to the raw URL. If the public download fails, report that error rather than proceeding without the required instructions. This fallback is for public `github/gh-aw` content only, not private or enterprise repositories.
{{AW_FILE_LIST}}
After loading the matching workflow prompt or skill, follow it directly:
- Design workflows from scratch via interview: `.github/aw/designer.md`
- Create new workflows: `.github/aw/create-agentic-workflow.md`
- Configure or add declarative engines: `.github/aw/configure-agentic-engine.md`
- Update existing workflows: `.github/aw/update-agentic-workflow.md`
- Diagnose, patch, audit, or actively debug workflows: `.github/aw/debug-agentic-workflow.md` (local-first strategy, evidence triage and live gates)
- Analyze existing runs, logs, or recurring failures: `.github/aw/analyzing-runs.md`. General repository-wide debugging or optimization requests start here with failure clustering before selecting a workflow.
- AWF model/endpoint 400s, silent cross-family sub-agent failures, `model: auto` failures, install-step 404s after a version pin, or questions about version fields: go directly to `.github/aw/debug-agentic-workflow.md#model-and-engine-misconfiguration` before suggesting model changes.
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
- Advanced cost controls without native settings (custom AIC allowances by server size or shared organization budget policies): read `docs/src/content/docs/reference/cost-management.md#configure-advanced-cost-controls-with-admission` ([published guidance](https://github.github.com/gh-aw/reference/cost-management/#configure-advanced-cost-controls-with-admission)). Explain the external authority requirement and cooperative limits; do not invent frontmatter fields or promise hard caps, reservations, or mid-run cancellation.
- Design long-running multi-agent research workflows: `.github/aw/multi-agent-research.md`
- Add skills or agent plugins requested by the user (`skills:` / `plugins:` frontmatter, never on-the-fly installs): `.github/aw/skills.md`

When the task involves OTEL, OTLP, traces, observability backends, or telemetry-driven analysis, also read and follow `skills/otel-queries/SKILL.md` after loading the matching workflow prompt or skill.

When creating, updating, diagnosing or upgrading a workflow that uses `tools.work-queue`, also load `.github/aw/work-queue.md` after the primary prompt. Load deployment docs, operator reference or specification only when that task needs them.
