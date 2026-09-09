---
private: true
emoji: "🔍"
description: Runs Go module quality checks using Super Linter and creates issues for violations
on:
  workflow_dispatch:
  schedule:
    - cron: "daily around 14:00 on weekdays" # ~2 PM UTC, weekdays only
permissions:
  copilot-requests: write
  contents: read
  actions: read
  issues: read
  pull-requests: read
safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[linter] "
    labels: [automation, code-quality, cookie]
engine: pi
model: copilot/auto
name: Super Linter Report
timeout-minutes: 15
imports:
  - shared/reporting.md
  - shared/otlp.md
jobs:
  super_linter:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: read
      # To report GitHub Actions status checks
      statuses: write
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7.0.1
        with:
          # super-linter needs the full git history to get the
          # list of files that changed across commits
          fetch-depth: 0
          persist-credentials: false
      
      - name: Run super-linter
        # zizmor: ignore[github_action_from_unverified_creator_used]
        uses: super-linter/super-linter@v8.7.0 # x-release-please-version
        id: super-linter
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          CREATE_LOG_FILE: "true"
          LOG_FILE: super-linter.log
          DEFAULT_BRANCH: main
          # Only validate Go modules - other linters run in CI
          VALIDATE_GO_MODULES: "true"
          # Scheduled runs have no diff, so they must validate the full codebase.
          VALIDATE_ALL_CODEBASE: ${{ github.event_name == 'schedule' && 'true' || 'false' }}
          # Avoid false-negative failures from super-linter summary formatter
          ENABLE_GITHUB_ACTIONS_STEP_SUMMARY: "false"
          ENABLE_GITHUB_PULL_REQUEST_SUMMARY_COMMENT: "false"
          FILTER_REGEX_EXCLUDE: "(^|.*/)super-linter-output/.*"
      
      - name: Check for linting issues
        id: check-results
        run: |
          if [ -f "super-linter.log" ] && [ -s "super-linter.log" ]; then
            # Check if there are actual errors (not just the header)
            if grep -qE "ERROR|WARN|FAIL" super-linter.log; then
              echo "needs-linting=true" >> "$GITHUB_OUTPUT"
            else
              echo "needs-linting=false" >> "$GITHUB_OUTPUT"
            fi
          else
            echo "needs-linting=false" >> "$GITHUB_OUTPUT"
          fi

      - name: Prepare super-linter log for upload
        if: always()
        run: |
          if [ -f "super-linter.log" ]; then
            if sudo cp super-linter.log /tmp/super-linter.log 2>/dev/null || cp super-linter.log /tmp/super-linter.log; then
              if ! (sudo chmod 644 /tmp/super-linter.log 2>/dev/null || chmod 644 /tmp/super-linter.log); then
                echo "::warning::Unable to set permissions on /tmp/super-linter.log"
              fi
            else
              echo "::warning::Unable to copy super-linter.log to /tmp for artifact upload"
            fi
          fi
      
      - name: Upload super-linter log
        if: always()
        uses: actions/upload-artifact@v7.0.1
        with:
          name: super-linter-log
          path: /tmp/super-linter.log
          if-no-files-found: ignore
          retention-days: 7
steps:
  - name: Download super-linter log
    uses: actions/download-artifact@v8.0.1
    with:
      name: super-linter-log
      path: /tmp/gh-aw/agent/
tools:
  cli-proxy: true
  cache-memory: true
  edit:
  bash:
    - "*"
---

# Super Linter Analysis Report

You are an expert code quality analyst for a Go-based GitHub CLI extension project. Your task is to analyze the super-linter output and create a comprehensive issue report.

## Context

- **Repository**: ${{ github.repository }}
- **Project Type**: Go CLI tool (GitHub Agentic Workflows extension)
- **Triggered by**: @${{ github.actor }}
- **Run ID**: ${{ github.run_id }}

## Your Task

1. **Read the linter output** from `/tmp/gh-aw/agent/super-linter.log` using the bash tool
2. **Analyze the findings**:
   - Categorize errors by severity (critical, high, medium, low)
   - Identify patterns in the errors
   - Determine which errors are most important to fix first
   - Note: This workflow only validates Go modules. Other linters are handled by separate CI jobs
3. **Create a detailed issue** with the following structure:

### Issue Title
Use format: "Code Quality Report - [Date] - [X] issues found"

### Issue Body Structure

```markdown
## 🔍 Super Linter Analysis Summary

**Date**: [Current date]
**Total Issues Found**: [Number]
**Run ID**: ${{ github.run_id }}

## 📊 Breakdown by Severity

- **Critical**: [Count and brief description]
- **High**: [Count and brief description]  
- **Medium**: [Count and brief description]
- **Low**: [Count and brief description]

## 📁 Issues by Category

### [Category/Linter Name]
- **File**: `path/to/file`
  - Line [X]: [Error description]
  - Impact: [Why this matters]
  - Suggested fix: [How to resolve]

[Repeat for other categories]

## 🎯 Priority Recommendations

1. [Most critical issue to address first]
2. [Second priority]
3. [Third priority]

## 📋 Full Linter Output

<details>
<summary>Click to expand complete linter log</summary>

```
[Include the full linter output here]
```

</details>

## 🔗 References

- [Link to workflow run](${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }})
- [Super Linter Documentation](https://github.com/super-linter/super-linter)
- [Project CI Configuration](${{ github.server_url }}/${{ github.repository }}/blob/main/.github/workflows/ci.yml)
```

## Important Guidelines

- **Be concise but thorough**: Focus on actionable insights
- **Prioritize issues**: Not all linting errors are equal
- **Provide context**: Explain why each type of error matters for a CLI tool project
- **Suggest fixes**: Give practical recommendations
- **Use proper formatting**: Make the issue easy to read and navigate
- **If no errors found**: Create a positive report celebrating clean code
- **Remember**: This workflow only validates Go modules. Other file types are handled by separate CI workflows

## Validating Fixes with Super Linter

When suggesting fixes for linting errors, you can provide instructions for running super-linter locally to validate the fixes before committing. Include this section in your issue report when relevant:

### Running Super Linter Locally

To validate your fixes locally before committing, run super-linter using Docker:

```bash
# Run super-linter with the same configuration as the workflow
docker run --rm \
  -e DEFAULT_BRANCH=main \
  -e RUN_LOCAL=true \
  -e VALIDATE_GO_MODULES=true \
  -v $(pwd):/tmp/lint \
  ghcr.io/super-linter/super-linter:v8.7.0

# Run super-linter on specific file types only
# For example, to validate only Go modules:
docker run --rm \
  -e RUN_LOCAL=true \
  -e VALIDATE_GO_MODULES=true \
  -v $(pwd):/tmp/lint \
  ghcr.io/super-linter/super-linter:v8.7.0
```

**Note**: The Docker command uses the same super-linter configuration as this workflow. Files are mounted from your current directory to `/tmp/lint` in the container.

## Security Note

Treat linter output as potentially sensitive. Do not expose credentials, API keys, or other secrets that might appear in file paths or error messages.