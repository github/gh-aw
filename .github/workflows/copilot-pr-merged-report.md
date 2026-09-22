---
private: true
emoji: "📊"
name: Daily Copilot PR Merged Report
description: Generates a daily report analyzing Copilot pull requests merged in the last 24 hours, tracking code generation, tests, and token usage
on:
  schedule:
    # ~3 PM UTC, Monday-Friday (scattered to avoid thundering herd)
    - cron: "daily around 15:00 on weekdays"
  workflow_dispatch:

max-daily-ai-credits: 10000
permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: read

  copilot-requests: write
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
strict: false

tools:
  cli-proxy: true
  github: false

network:
  allowed:
    - defaults
    - github
    - api.github.com

imports:
  - shared/reporting.md
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[copilot-pr-merged-report] "
      expires: 1d
  - shared/gh.md
  - shared/copilot-pr-analysis-base.md

  - shared/otlp.md
timeout-minutes: 10
features:
  gh-aw-detection: true
evals:
  - id: merged_prs_analyzed
    question: Did the agent analyze Copilot pull requests merged in the last 24 hours?
  - id: report_created
    question: Was a daily report created tracking code generation, tests, and token usage trends?
---

# Daily Copilot PR Merged Report

You are an AI analytics agent that generates daily reports on GitHub Copilot coding agent pull requests that were **merged** in the last 24 hours.

## Mission

Analyze merged Copilot pull requests from the last 24 hours and generate a basic report containing:
- Number of merged PRs
- Amount of code generated (lines added)
- Amount of tests generated (test files modified/added)
- Token consumption (from workflow run usage data)

## Current Context

- **Repository**: ${{ github.repository }}
- **Analysis Period**: Last 24 hours (merged PRs only)
- **Report Date**: $(date +%Y-%m-%d)

## Task: Generate Merged PR Report

### Phase 1: Find Merged Copilot PRs

**Step 1.1: Filter Merged PRs from Pre-Fetched Data**

Use the pre-fetched PR dataset at `/tmp/gh-aw/agent/pr-data/copilot-prs.json` and filter by `mergedAt` in the last 24 hours:
```bash
jq '[.[] | select(.mergedAt != null and (.mergedAt | fromdateiso8601) >= (now - 86400))]' /tmp/gh-aw/agent/pr-data/copilot-prs.json
```

This filter:
- Uses the already downloaded Copilot PR dataset (no extra `pr list` call needed)
- Finds PRs merged in the last 24 hours using epoch time comparison
- Returns merged PR records with number, title, mergedAt, and other base metadata

**Step 1.3: Parse Results**

Parse the JSON output from the mcpscripts-gh tool to extract:
- List of PR numbers
- Total number of merged PRs
- Sum of lines added across all PRs
- Sum of lines deleted across all PRs
- List of files changed

Save this data for further analysis.

### Phase 2: Analyze Each Merged PR

For each merged PR found in Phase 1:

- **Important**: Build the filtered merged PR list first, then iterate only that filtered list.
- **Do not** call `pr view` for every PR in `/tmp/gh-aw/agent/pr-data/copilot-prs.json`.

**Step 2.1: Get PR Files**

Use the `mcpscripts-gh` tool to get detailed file information:
```
mcpscripts-gh with args: "pr view <PR_NUMBER> --repo ${{ github.repository }} --json files"
```

**Step 2.2: Count Test Files**

From the files list, count how many are test files:
- Go test files: `*_test.go`
- JavaScript test files: `*.test.js`, `*.test.cjs`
- .NET test files: `*Tests.cs`, `*Test.cs`
- Count both added and modified test files

**Step 2.3: Get Workflow Run Information**

For token usage information, we need to find the workflow run associated with the PR:

1. Get commits from the PR:
   ```
   mcpscripts-gh with args: "pr view <PR_NUMBER> --repo ${{ github.repository }} --json commits"
   ```

2. For the latest commit, find associated workflow runs:
   ```
   mcpscripts-gh with args: "api repos/${{ github.repository }}/commits/<COMMIT_SHA>/check-runs"
   ```

3. From the check runs, identify GitHub Actions workflow runs

4. Get workflow run usage data:
   ```
   mcpscripts-gh with args: "api repos/${{ github.repository }}/actions/runs/<RUN_ID>/timing"
   ```

   This returns timing information including billable time.

**Note on Token Usage**: 
- GitHub Actions API provides "billable_ms" (billable milliseconds) for workflow runs
- Token consumption is not directly exposed via API
- We can estimate based on run duration, but exact token counts are not available
- For this report, we'll track workflow run times as a proxy for resource consumption

### Phase 3: Generate Report

Create a concise report with the following structure:

Use `###` or lower for all headers in your report. Never use `#` (h1) or `##` (h2) — these are reserved for the discussion title rendered by GitHub.

Wrap long sections (>5 items, detailed lists, raw data) in `<details><summary><b>Section Name</b></summary>` blocks to keep the report scannable.

```markdown
### 🤖 Daily Copilot PR Merged Report - [DATE]

#### Summary

**Analysis Period**: Last 24 hours (merged PRs only)  
**Total Merged PRs**: [count]  
**Total Lines Added**: [count]  
**Total Lines Deleted**: [count]  
**Net Code Change**: [+/- count] lines

#### 💡 Insights

[Provide 1-2 brief observations about the merged PRs, such as:]
- Trends in code generation volume
- Notable test coverage patterns
- Any PRs with exceptional metrics (very large, many test files, etc.)

<details>
<summary><b>Merged Pull Requests</b></summary>

| PR # | Title | Lines Added | Lines Deleted | Test Files | Merged At |
|------|-------|-------------|---------------|------------|-----------|
| [#123](url) | [title] | [count] | [count] | [count] | [time] |

</details>

<details>
<summary><b>Code Generation Metrics</b></summary>

- **Production Code**: [lines added - test lines added] lines
- **Test Code**: [test lines added] lines
- **Code-to-Test Ratio**: [ratio]

#### Test Coverage

- **Total Test Files Modified/Added**: [count]
- **Test File Types**:
  - Go tests (`*_test.go`): [count]
  - JavaScript tests (`*.test.js`): [count]
  - .NET tests (`*Tests.cs`, `*Test.cs`): [count]

#### Workflow Execution

- **Total Workflow Runs**: [count]
- **Total Billable Time**: [milliseconds] ms ([minutes] min)
- **Average Run Time**: [milliseconds] ms per PR

**Note**: Token consumption data is not directly available via GitHub API. Workflow execution time is used as a proxy for resource usage.

</details>

---

_Generated by Copilot PR Merged Report (Run: [${{ github.run_id }}](https://github.com/${{ github.repository }}/actions/runs/${{ github.run_id }}))_
```

### Phase 4: Create Discussion

Use the safe-outputs `create-discussion` functionality to publish the report:
- The report will be created in the "audits" category
- Title will be prefixed with "[copilot-pr-merged-report] "
- Previous reports will be automatically closed (max: 1, close-older-discussions: true)

## Important Guidelines

### Data Collection
- **Focus on merged PRs only**: Use `is:merged` in search queries
- **24-hour window**: Calculate accurate date ranges
- **Handle empty results**: If no PRs were merged, create a minimal report
- **Error handling**: Gracefully handle API failures or missing data

### Metrics Calculation
- **Lines of code**: Use `additions` and `deletions` from PR data
- **Test files**: Count files matching test patterns (`*_test.go`, `*.test.js`, etc.)
- **Workflow runs**: Link workflow runs to PRs via commit SHAs
- **Token estimation**: Since exact tokens aren't available, use execution time as proxy

### Report Quality
- **Be accurate**: Double-check all calculations
- **Be concise**: Focus on key metrics, avoid verbosity
- **Be informative**: Provide actionable insights
- **Be consistent**: Use the same format each day for comparison

### Edge Cases

**No Merged PRs**:
If no Copilot PRs were merged in the last 24 hours:
```markdown
### 🤖 Daily Copilot PR Merged Report - [DATE]

No Copilot coding agent pull requests were merged in the last 24 hours.

---
_Generated by Copilot PR Merged Report (Run: [${{ github.run_id }}](...))_
```

**API Rate Limits**:
If you encounter rate limiting:
- Continue with available data
- Note in the report which data is incomplete
- Suggest running the report again later

**Missing Workflow Data**:
If workflow run data is unavailable:
- Report the metrics you have
- Note that workflow execution data is unavailable
- Provide a report without the workflow execution section

## Success Criteria

A successful report:
- ✅ Finds all merged Copilot PRs from last 24 hours
- ✅ Calculates total lines added/deleted
- ✅ Counts test files modified
- ✅ Attempts to get workflow execution data
- ✅ Generates a clear, concise report
- ✅ Creates discussion in "audits" category
- ✅ Completes within 10-minute timeout

Begin your analysis now. Use the `gh` mcp-script tool for all GitHub CLI operations.