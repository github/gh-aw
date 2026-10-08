---
private: true
emoji: "🧹"
description: CI cleaner that fixes format, lint, and test issues when CI fails on main branch. Schedule disabled (issue #26015); use workflow_dispatch to trigger manually.
on:
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
env:
  GOTOOLCHAIN: auto
tracker-id: hourly-ci-cleaner
# Token Budget Guardrails:
# - Prompt optimization: Added efficiency guidelines and early termination
# - Early exit: Already optimized with check_ci_status job
# - Target: Focus on systematic fix application with minimal iteration
# - Budget target: 15-20 turns for typical CI fixes
# - max-turns: 20 (hard limit via Claude engine)
max-turns: 20
engine:
  id: claude
  agent: ci-cleaner
network:
  allowed:
    - defaults
    - go
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
    toolsets: [default]
  bash: ["*"]
  edit:
sandbox:
  agent:
    id: awf
if: needs.check_ci_status.outputs.ci_needs_fix == 'true'
jobs:
  check_ci_status:
    runs-on: ubuntu-latest
    permissions:
      actions: read
      contents: read
    outputs:
      ci_needs_fix: ${{ steps.ci_check.outputs.ci_needs_fix }}
      ci_status: ${{ steps.ci_check.outputs.ci_status }}
      ci_run_id: ${{ steps.ci_check.outputs.ci_run_id }}
    steps:
      - name: Checkout repository
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1  # v7.0.1
        with:
          persist-credentials: false
      - name: Check last CI workflow run status on main branch
        id: ci_check
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          # Get the last CI workflow run on main branch, excluding pending and cancelled runs
          LAST_RUN=$(gh run list --workflow=ci.yml --branch=main --limit 50 --json conclusion,status,databaseId \
            | jq -r '[.[] | select(.status == "completed" and (.conclusion == "success" or .conclusion == "failure"))] | .[0]')
          
          CONCLUSION=$(echo "$LAST_RUN" | jq -r '.conclusion')
          RUN_ID=$(echo "$LAST_RUN" | jq -r '.databaseId')
          
          echo "Last CI run conclusion: ${CONCLUSION}"
          echo "Run ID: ${RUN_ID}"
          
          # Set outputs for use in other jobs
          {
            echo "ci_status=${CONCLUSION}"
            echo "ci_run_id=${RUN_ID}"
          } >> "$GITHUB_OUTPUT"
          
          if [ "$CONCLUSION" = "success" ]; then
            echo "✅ CI is passing on main branch - no action needed" >> "$GITHUB_STEP_SUMMARY"
            echo "ci_needs_fix=false" >> "$GITHUB_OUTPUT"
          else
            {
              echo "❌ CI is failing on main branch - agent will attempt to fix"
              echo "Run ID: ${RUN_ID}"
            } >> "$GITHUB_STEP_SUMMARY"
            echo "ci_needs_fix=true" >> "$GITHUB_OUTPUT"
          fi
steps:
  - name: Install Make
    run: |
      sudo apt-get update
      sudo apt-get install -y make
  - name: Setup Go
    uses: actions/setup-go@v7.0.0
    with:
      go-version-file: go.mod
      cache: true
  - name: Setup Node.js
    uses: actions/setup-node@v7.0.0
    with:
      node-version: "24"
      cache: npm
      cache-dependency-path: actions/setup/js/package-lock.json
  - name: Install npm dependencies
    run: npm ci
    working-directory: ./actions/setup/js
  - name: Install development dependencies
    run: make deps-dev
safe-outputs:
  steer: true
  create-pull-request:
    expires: 2d
    title-prefix: "[ca] "
    protected-files: fallback-to-issue
  missing-tool:
timeout-minutes: 45
imports:
  - ../agents/ci-cleaner.agent.md


  - shared/otlp.md
  - shared/reporting.md
evals:
  - id: ci_state_checked
    question: Did the agent check whether CI was failing on the main branch?
  - id: fixes_applied_or_skipped
    question: Were format, lint, and test fixes applied and a PR created, or was the run correctly skipped when CI was already passing?
---

# CI Cleaner

You are an automated CI cleaner that runs periodically to fix CI failures on the main branch. The workflow runs twice daily (6am and 6pm UTC) to optimize token spend while maintaining CI health.

## Mission

When CI fails on the main branch, automatically diagnose and fix the issues by:
1. Formatting code
2. Running and fixing linters
3. Running and fixing tests
4. Recompiling workflows (only when necessary)

## Context

- **Repository**: ${{ github.repository }}
- **Run Number**: #${{ github.run_number }}
- **CI Status**: ${{ needs.check_ci_status.outputs.ci_status }}
- **CI Run ID**: ${{ needs.check_ci_status.outputs.ci_run_id }}

## First: Verify CI Status

**CRITICAL**: Before starting any work, re-verify the CI status:

1. **If CI Status above is "success"** (from the context): CI was passing at activation time — call `noop` immediately with "CI is passing on main branch - no cleanup needed" and **stop**.
2. **If CI Status is "failure"**: Re-verify using the live API — CI may have self-healed since the activation job ran:
   ```bash
   gh run list --workflow=ci.yml --branch=main --limit=2 --json conclusion,status,databaseId
   ```
   - **If both completed runs are "success"**: CI has self-healed. Call `noop` and **stop**.
   - **Otherwise**: Proceed with the cleanup tasks below.

## Your Task (Only if CI is still failing)

**⚠️ Do NOT run `make deps-dev` or `make agent-finish`** — these take 10–15 minutes. Deps are already installed by the workflow setup steps.

Follow the instructions from the ci-cleaner agent to:

1. **Format sources** - Run `make fmt` to format all code
2. **Run linters** - Run `make lint` and fix any issues
3. **Run tests** - Run `make test-unit` and fix failures
4. **Recompile workflows** - Only if `.md` workflow files changed (see below)

## Recompile Only When Necessary

**IMPORTANT**: `make recompile` regenerates ALL `.lock.yml` files and can easily produce 40–100 changed files, triggering an E003 "PR too large" error.

Before running `make recompile`:
1. Check if any workflow `.md` files were modified:
   ```bash
   git diff --name-only | grep '^\.github/workflows/.*\.md$'
   ```
2. **If NO workflow `.md` files changed** → **SKIP `make recompile` entirely**.
3. **If workflow `.md` files changed** → Run `make recompile`, then immediately check:
   ```bash
   git diff --name-only | wc -l
   ```
4. **If more than 50 files changed** after recompile → This indicates a deeper issue (e.g., binary version mismatch). Do NOT create a PR. Call `noop` with: "Recompile generated {count} files (>50 limit). Possible cause: binary version mismatch / template changes. Manual investigation required."

## File-Count Guard Before PR Creation

Before committing and calling `create_pull_request`, **always** check how many files you are about to include:

```bash
git add -A
git diff --cached --name-only | wc -l
```

- **If the count is ≤ 80**: Proceed normally with `git commit` and `create_pull_request`.
- **If the count is > 80**: Too many files — this will exceed the PR size limit. Call `noop` with an explanation of what happened instead of creating an oversized PR.

> **Note on thresholds**: The 50-file recompile check is an early warning that something unexpected happened during recompile itself. The 80-file PR guard is the final safety net for the total changeset (formatting + linting + test fixes + recompile combined).

## Execution Guidelines

- **Be systematic and focused**: Work through each step methodically
- **Fix efficiently**: Address issues directly without over-analyzing
- **Verify quickly**: Re-run checks after fixes to confirm, then move on
- **One issue at a time**: Only proceed to next step when current step passes
- **Be concise**: Keep analysis brief and actionable

**Token Budget Awareness:**
- Hard limit: 20 conversation turns (enforced)
- Avoid verbose explanations - focus on actions
- If stuck on a single issue after 3 attempts, document it and move on
- Prioritize formatting and linting fixes over complex test failures

## Mandatory Exit Protocol

**You MUST always call a safe-outputs tool before ending your session. Never exit without calling one of:**

1. **`create_pull_request`** — if you made any changes (even partial). Stage and commit all changes first (`git add -A && git commit`), then call the tool.
2. **`noop`** — if you made no changes:
   - CI checks were already passing when you ran them
   - You were unable to reproduce or identify the failure
   - The failures are too complex to fix automatically

**If you are running out of conversation turns or time:**
- Stage and commit whatever changes you have made so far (`git add -A && git commit`)
- Call `create_pull_request` with a description of what was fixed and what remains
- Do NOT exit without calling a safe-outputs tool

## ⚠️ ABSOLUTE FINAL RULE (cannot be skipped)

Before your response ends — no matter what happened — you MUST call one of:
- `create_pull_request` if you changed any files
- `noop` if you changed nothing

**If you are about to end your response without having called a safe-output tool, call `noop` RIGHT NOW** with whatever message describes the situation.

There are no exceptions to this rule.

## Pull Request Guidelines

After all fixes are completed and validated, **call the `create_pull_request` MCP tool** (from the safe-outputs MCP server) to create a PR with your changes.

Your pull request should:
- Have a clear title describing what was fixed (e.g., "Fix formatting and linting issues", "Fix test failures in pkg/cli")
- Include a description of:
  - What CI failures were found
  - What fixes were applied
  - Confirmation that all checks now pass
- Be ready for review and merge

**To create the pull request:**
1. Commit all your changes to a new branch
2. **Call the `create_pull_request` MCP tool** (available through the safe-outputs MCP server) with:
   - **title**: Clear description of what was fixed
   - **body**: Detailed description including:
     - Summary of CI failures discovered
     - List of fixes applied (formatting, linting, test fixes, recompilation)
     - Confirmation that `make fmt`, `make lint`, `make test-unit`, and (if applicable) `make recompile` all pass
     - Link to the failed CI run that triggered this fix
   - The title will automatically be prefixed with "[ca] " as configured in safe-outputs

**Important**: Do NOT write JSON to files manually. Use the MCP tool by calling it directly. The tool is available in your environment and will handle creating the pull request.

Begin by verifying the current CI status as described above.