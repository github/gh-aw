---
description: Runs Markdown quality checks using Super Linter and creates issues for violations
on:
  workflow_dispatch:
  schedule:
    - cron: "0 14 * * 1-5" # 2 PM UTC, weekdays only
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[linter] "
    labels: [automation, code-quality, cookie]
engine: copilot
name: Super Linter Report
timeout-minutes: 15
imports:
  - shared/mood.md
  - shared/reporting.md
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
        uses: actions/checkout@v6
        with:
          # super-linter needs the full git history to get the
          # list of files that changed across commits
          fetch-depth: 0
          persist-credentials: false
      
      - name: Super-linter
        uses: super-linter/super-linter@v8.2.1 # x-release-please-version
        id: super-linter
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          CREATE_LOG_FILE: "true"
          LOG_FILE: super-linter.log
          DEFAULT_BRANCH: main
          ENABLE_GITHUB_ACTIONS_STEP_SUMMARY: "true"
          # Only validate Markdown - other linters (Go, JS, YAML, Shell) run in CI
          VALIDATE_MARKDOWN: "true"
          # Disable all other linters to improve performance
          VALIDATE_ALL_CODEBASE: "false"
      
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
      
      - name: Upload super-linter log
        if: always()
        uses: actions/upload-artifact@v6
        with:
          name: super-linter-log
          path: super-linter.log
          retention-days: 7
steps:
  - name: Download super-linter log
    uses: actions/download-artifact@v6
    with:
      name: super-linter-log
      path: /tmp/gh-aw/
tools:
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

1. **Read the linter output** from `/tmp/gh-aw/super-linter.log` using the bash tool
2. **Analyze the findings**:
   - Categorize errors by severity (critical, high, medium, low)
   - Identify patterns in the errors
   - Determine which errors are most important to fix first
   - Note: This workflow only validates Markdown files. Other linters (Go, JavaScript, YAML, Shell, etc.) are handled by separate CI jobs
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
- **Remember**: This workflow only validates Markdown files. Other file types (Go, JavaScript, YAML, Shell, GitHub Actions) are handled by separate CI workflows

## Validating Fixes with Super Linter

When suggesting fixes for linting errors, you can provide instructions for running super-linter locally to validate the fixes before committing. Include this section in your issue report when relevant:

### Running Super Linter Locally

To validate your fixes locally before committing, run super-linter using Docker:

```bash
# Run super-linter with the same configuration as the workflow
docker run --rm \
  -e DEFAULT_BRANCH=main \
  -e RUN_LOCAL=true \
  -e VALIDATE_MARKDOWN=true \
  -v $(pwd):/tmp/lint \
  ghcr.io/super-linter/super-linter:slim-v8

# Run super-linter on specific file types only
# For example, to validate only Markdown files:
docker run --rm \
  -e RUN_LOCAL=true \
  -e VALIDATE_MARKDOWN=true \
  -v $(pwd):/tmp/lint \
  ghcr.io/super-linter/super-linter:slim-v8
```

**Note**: The Docker command uses the same super-linter configuration as this workflow. Files are mounted from your current directory to `/tmp/lint` in the container.

## Security Note

Treat linter output as potentially sensitive. Do not expose credentials, API keys, or other secrets that might appear in file paths or error messages.
