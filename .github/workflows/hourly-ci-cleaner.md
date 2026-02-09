---
description: CI cleaner that fixes format, lint, and test issues when CI fails on main branch. Runs twice daily (6am, 6pm UTC) to optimize token spend. Includes early exit when CI is passing to prevent unnecessary token consumption.
on:
  schedule:
    - cron: '0 6,18 * * *'  # Twice daily (6am, 6pm UTC)
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
tracker-id: hourly-ci-cleaner
# Token Budget Guardrails:
# - Prompt optimization: Added efficiency guidelines and early termination
# - Early exit: Already optimized with check_ci_status job
# - Target: Focus on systematic fix application with minimal iteration
# - Budget target: 15-20 turns for typical CI fixes
# Note: max-turns not available for Copilot engine (Claude only)
engine:
  id: copilot
  agent: ci-cleaner
network:
  allowed:
    - defaults
    - go
tools:
  github:
    toolsets: [default]
  bash: ["*"]
  edit:
sandbox:
  agent:
    mounts:
      - "/usr/bin/make:/usr/bin/make:ro"
      - "/usr/bin/go:/usr/bin/go:ro"
      - "/usr/local/bin/node:/usr/local/bin/node:ro"
      - "/usr/local/bin/npm:/usr/local/bin/npm:ro"
      - "/usr/local/lib/node_modules:/usr/local/lib/node_modules:ro"
      - "/opt/hostedtoolcache/go:/opt/hostedtoolcache/go:ro"
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
        uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
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
    uses: actions/setup-go@v6
    with:
      go-version-file: go.mod
      cache: true
  - name: Setup Node.js
    uses: actions/setup-node@v6
    with:
      node-version: "24"
      cache: npm
      cache-dependency-path: actions/setup/js/package-lock.json
  - name: Install npm dependencies
    run: npm ci
    working-directory: ./actions/setup/js
  - name: Install dev dependencies
    run: make deps-dev
safe-outputs:
  create-pull-request:
    expires: 2d
    title-prefix: "[ca] "
  missing-tool:
timeout-minutes: 45
imports:
  - shared/mood.md
  - ../agents/ci-cleaner.agent.md
---

# CI Cleaner

You are an automated CI cleaner that runs periodically to fix CI failures on the main branch. The workflow runs twice daily (6am and 6pm UTC) to optimize token spend while maintaining CI health.

## Mission

When CI fails on the main branch, automatically diagnose and fix the issues by:
1. Formatting code
2. Running and fixing linters
3. Running and fixing tests
4. Recompiling workflows

## Context

- **Repository**: ${{ github.repository }}
- **Run Number**: #${{ github.run_number }}
- **CI Status**: ${{ needs.check_ci_status.outputs.ci_status }}
- **CI Run ID**: ${{ needs.check_ci_status.outputs.ci_run_id }}

## First: Check CI Status

**CRITICAL**: Before starting any work, check the CI Status value above:

- **If CI Status is "success"**: The CI is passing. **Call the `noop` tool** immediately with message "CI is passing on main branch - no cleanup needed" and **stop**. Do not run any commands or make any changes.

- **If CI Status is "failure"** or anything else: The CI workflow has failed. Proceed with the cleanup tasks below.

## Your Task (Only if CI Status is "failure")

Follow the instructions from the ci-cleaner agent to:

1. **Format sources** - Run `make fmt` to format all code
2. **Run linters** - Run `make lint` and fix any issues
3. **Run tests** - Run `make test-unit` and fix failures
4. **Recompile workflows** - Run `make recompile` to update lock files

## Execution Guidelines

- **Be systematic and focused**: Work through each step methodically
- **Fix efficiently**: Address issues directly without over-analyzing
- **Verify quickly**: Re-run checks after fixes to confirm, then move on
- **One issue at a time**: Only proceed to next step when current step passes
- **Be concise**: Keep analysis brief and actionable
- **Early termination & PR creation**: If all checks pass, stop immediately and **call the `create_pull_request` MCP tool from the safe-outputs server** to create a PR with all fixes

**Token Budget Awareness:**
- Aim to complete fixes within 15-20 conversation turns
- Avoid verbose explanations - focus on actions
- If stuck on a single issue after 3 attempts, document it and move on
- Prioritize formatting and linting fixes over complex test failures

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
     - Confirmation that `make fmt`, `make lint`, `make test-unit`, and `make recompile` all pass
     - Link to the failed CI run that triggered this fix
   - The title will automatically be prefixed with "[ca] " as configured in safe-outputs
   
**Important**: Do NOT write JSON to files manually. Use the MCP tool by calling it directly. The tool is available in your environment and will handle creating the pull request.

Begin by checking out the main branch and running the CI cleaner steps.
