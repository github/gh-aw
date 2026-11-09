---
name: developer
description: Core developer rules and coding conventions for gh-aw changes.
---


# Developer Instructions

Use this reference for gh-aw technical specs and development guidelines across code organization, validation, security, and implementation patterns.

## Table of Contents

- [Operational Command Playbook](#operational-command-playbook)
- [Capitalization Guidelines](#capitalization-guidelines)
- [Sub-Skills](#sub-skills)


## Operational Command Playbook

Use this section for the detailed day-to-day command flow that was intentionally removed from `AGENTS.md` to keep first-run ambient context small.

### Validation checkpoints

Run validation in tiers. Keep formatting and focused tests in the inner loop;
defer package-wide Go linting and workflow drift to the final pass.

1. **After each significant code edit** (format changed files, then run one focused check)
   ```bash
   make fmt
   go test ./pkg/cli -run '^TestRelevantBehavior' -count=1
   ```
   Replace the package and test pattern with the behavior being changed. This
   compiles the affected package too; do not add a CLI build unless the next
   command needs `./gh-aw`. First builds after Go edits can exceed the warm-cache
   budget because Go compiles the entire package, including its test files.
2. **Before every intermediate `report_progress` call** (fast, no test bodies)
   ```bash
   make agent-report-progress-no-test
   ```
   This formats branch changes and compiles/vets changed Go packages. It does
   not link the CLI or run package-wide Go linters or workflow recompilation.
   Schema changes still rebuild the CLI; stale-lock and local-permission guards
   still run. JavaScript/action-shell checks remain change-scoped.
3. **Before the FINAL `report_progress` call** (build, lint, impacted tests, and workflow drift)
   ```bash
   make agent-report-progress
   ```
   This is the full pre-PR gate. Run it once, not after every focused test.
   `make agent-finish` is an explicit repository-wide maintenance sweep, not an
   inner-loop or routine handoff command.

> **Key rule:** Run `test-unit` only before the **final** `report_progress` call, not before intermediate saves. Intermediate saves defer full Go lint and workflow drift; they do not replace the final gate.

> **Timeout budget:** Aim for warm-cache intermediate validation under 5 seconds. The 30-second `test-unit` budget covers selected test runtime, not compilation, linting, or full workflow drift; the final gate can take minutes. Set `TEST_UNIT_RUN_FULL=1` only when the full Go suite is required.

### Change-type command matrix

- Formatting: `make fmt` formats branch changes (including staged, unstaged, and untracked files). Go-only edits do not need npm. Use `make fmt-all` only for an explicit full-repository formatting sweep.
- Workflow markdown changes: `make recompile`
- JavaScript (`*.cjs`) changes: `make fmt`, then a focused runtime test. The intermediate gate runs changed-file JavaScript lint.

### Common focused checks from recent repository work

- `pkg/workflow/` edits: run `go test ./pkg/workflow -count=1` during iteration; narrow with `-run` when only one workflow behavior is under active change.
- `actions/setup/js/` edits: after `make fmt`, run the targeted `actions/setup/js/*.test.cjs` suites for the files you touched. Gateway changes commonly validate with `npx vitest run actions/setup/js/start_mcp_gateway.test.cjs`.
- Workflow source changes that also touch compiler or runtime code: run `make recompile`, then rerun the affected focused Go or JavaScript checks before the final `make agent-report-progress`.

### Merge-main playbook

When explicitly asked to merge main:

1. Run `make merge-main`.
2. If conflicts exist in `.go` or `.cjs`, resolve and stage files.
3. Run:
   ```bash
   make build
   make recompile
   git commit
   make fmt
   ```

## Capitalization Guidelines

The gh-aw CLI follows context-based capitalization to distinguish between the product name and generic workflow references.

### Capitalization Rules

| Context | Format | Example |
|---------|--------|---------|
| Product name | **Capitalized** | "GitHub Agentic Workflows CLI from GitHub Next" |
| Generic workflows | **Lowercase** | "Enable agentic workflows" |
| Technical terms | **Capitalized** | "Compile Markdown workflows to GitHub Actions YAML" |

This convention distinguishes between the product name (GitHub Agentic Workflows) and the concept (agentic workflows), following industry standards similar to "GitHub Actions" vs. "actions".

### Implementation

The capitalization rules are enforced through automated tests in `cmd/gh-aw/capitalization_test.go` that run as part of the standard test suite.



## Sub-Skills

The following sub-skills cover specific areas of the codebase. Load them lazily when the task requires the specific domain:

| Sub-skill | When to use |
|-----------|-------------|
| `.github/skills/developer-code-organization/SKILL.md` | Creating new files, refactoring, WASM stubs, file size decisions |
| `.github/skills/developer-security/SKILL.md` | Implementing new features, reviewing for security, template injection concerns |
| `.github/skills/developer-internals/SKILL.md` | Working on compiler internals, validation, safe outputs, MCP server, schema changes |
| `.github/skills/developer-release/SKILL.md` | Creating a release, evaluating breaking changes, firewall log analysis |
