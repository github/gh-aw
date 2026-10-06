---
description: Repository-specific overlay rules that refine default gh-aw guidance for workflow authoring and validation.
---

# Repository Instructions Overlay for gh-aw Agents

This optional file defines repository-local workflow authoring standards for installed gh-aw agents.

## Scope

These rules apply when creating, editing, reviewing, diagnosing, patching,
actively debugging, and upgrading agentic workflows.

## Precedence

Apply upstream/default gh-aw instructions first, then apply this overlay.
If a rule conflicts, this repository overlay takes precedence.

## Repository Rules

Add your repository-specific standards here, for example:

- **CRITICAL INVARIANT:** After **any** modification to agentic workflow markdown files (`.github/workflows/*.md`), you **must** run a one-shot `gh aw compile` before stopping. Agents must not use `--watch`, because watch mode does not terminate automatically.
- For diagnosis, patching, or active debugging, follow [local-debug.md](local-debug.md) for mode selection and live gates; log/audit references do not authorize live runs.
- Required shared include(s) for new workflows
- Standard frontmatter defaults
- Frontmatter ordering/style conventions
- Security or policy constraints specific to this repository
- For workflows that will be enforced by repository or organization pull request rulesets, keep workflow/job names stable for required checks and use `inlined-imports: true` when imports are present
- When documenting or recommending Copilot authentication, state that `permissions: { copilot-requests: write }` uses `${{ github.token }}` for inference and does not require a PAT or `COPILOT_GITHUB_TOKEN` secret
- When you need prior art for workflow design, shared components, tool configuration, or safe-output patterns, use GitHub APIs or `gh` to inspect `https://github.com/gm3dmo/the-power` before inventing a new pattern
- When authoring or reasoning about an `operational-value` grader, use the `operational value designer` skill (`/operational-value-designer`) to infer operational value from the target agentic workflow before finalizing the grader contract
