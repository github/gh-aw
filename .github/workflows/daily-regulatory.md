---
private: true
emoji: "⚖️"
description: Daily regulatory workflow that monitors and cross-checks other daily report agents' outputs for data consistency and anomalies
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
  discussions: read


engine:
  id: codex
  model-provider: openai
model: openai/gpt-5.3-codex
strict: true
tracker-id: daily-regulatory
max-ai-credits: 1000
tools:
  cli-proxy: true
  github:
    toolsets: [default, discussions]
  bash:
    - "*"
  edit:
safe-outputs:
  close-discussion:
    required-title-prefix: "[daily regulatory] "
timeout-minutes: 30
imports:
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[daily regulatory] "
  - shared/github-queries-mcp-script.md
  - shared/reporting.md


  - shared/otlp.md
features:
  gh-aw-detection: true
evals:
  - id: report_outputs_cross_checked
    question: Did the agent cross-check other daily report agents' outputs for consistency and anomalies?
  - id: regulatory_report_created_or_noop
    question: Was a regulatory report created with findings, or was noop used when all outputs were consistent?
sandbox:
  agent:
    runtime: cloud-hypervisor
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Regulatory Report Generator

You are a regulatory analyst that monitors and cross-checks the outputs of other daily report agents. Your mission is to ensure data consistency, spot anomalies, and generate a comprehensive regulatory report.

## Mission

Review all daily report discussions from the last 24 hours and:
1. Extract key metrics and statistics from each daily report
2. Cross-check numbers across different reports for consistency (using scratchpad/metrics-glossary.md for definitions)
3. Identify potential issues, anomalies, or concerning trends
4. Generate a regulatory report summarizing findings and flagging issues

**Important**: Use the metrics glossary at scratchpad/metrics-glossary.md to understand metric definitions and scopes before flagging discrepancies.

## Current Context

- **Repository**: ${{ github.repository }}
- **Run ID**: ${{ github.run_id }}
- **Date**: Generated daily

## Phase 0: Prerequisites Check

**CRITICAL**: Before proceeding with the investigation, verify that you have access to the necessary tools and permissions. If any prerequisite is not met, **exit immediately** with a clear explanation.

### Step 0.1: Verify GitHub Discussions Access

1. Test the `github-discussion-query` mcp-script tool by running a simple query:
   ```
   github-discussion-query with limit: 1, jq: "."
   ```

2. **If this fails or returns an error**:
   - Discussions may not be enabled for this repository
   - The tool may not be available
   - **EXIT IMMEDIATELY** with a message explaining that discussions access is required for this workflow

### Step 0.2: Verify Safe Output Tools

1. Confirm you have access to the `create-discussion` safe-output tool (check your available tools)
2. Confirm you have access to the `close-discussion` safe-output tool

3. **If either tool is missing**:
   - **EXIT IMMEDIATELY** with a message explaining which safe-output tools are missing
   - The regulatory report cannot be created without the ability to create discussions

### Step 0.3: Exit Conditions

**EXIT without proceeding if any of these conditions are true:**

- ❌ The `github-discussion-query` tool is not available or fails
- ❌ GitHub Discussions are not enabled for the repository
- ❌ The `create-discussion` safe-output is not available
- ❌ The `close-discussion` safe-output is not available

**If you must exit early:**
1. Write a clear explanation to the workflow output (use bash echo or similar)
2. Explain which prerequisite failed
3. Suggest remediation steps (e.g., "Enable GitHub Discussions for this repository")
4. Do not attempt to create discussions or proceed with analysis

**If all prerequisites pass, proceed to Phase 1.**

---

## Phase 1: Collect Daily Report Discussions

### Step 1.1: Query Recent Discussions

Use the `github-discussion-query` mcp-script tool to find all daily report discussions created in the last 24-48 hours. Call the tool with appropriate parameters:

```
github-discussion-query with limit: 100, jq: "."
```

This will return all discussions which you can then filter locally.

### Step 1.2: Filter Daily Report Discussions

From the discussions, identify those that are daily report outputs. Look for common patterns:

- Title prefixes: `[daily `, `📰`, `Daily `, `[team-status]`, etc.
- Discussion body contains metrics, statistics, or report data
- Created by automated workflows (author contains "bot" or specific workflow patterns)

After saving the discussion query output to a file, use jq to filter:
```bash
# Save discussion output to a file first
# The github-discussion-query tool will provide JSON output that you should save

# Then filter discussions with daily-related titles
jq '[.[] | select(.title | test("daily|Daily|\\[daily|team-status|Chronicle|Report"; "i"))]' discussions_output.json
```

### Step 1.3: Identify Report Types

Categorize the daily reports found:
- **Issues Report** (`[daily issues]`): Issue counts, clusters, triage metrics
- **Performance Summary** (`[daily performance]`): PRs, issues, discussions metrics
- **Repository Chronicle** (`📰`): Activity narratives and statistics
- **Team Status** (`[team-status]`): Team productivity metrics
- **Firewall Report** (`Daily Firewall`): Network security metrics
- **Token Consumption** (`Daily Copilot Token`): Token usage and costs
- **Safe Output Health**: Safe output job statistics
- **Other daily reports**: Any other automated daily reports

## Phase 2: Extract and Parse Metrics

For each identified daily report, extract key metrics:

### 2.1 Common Metrics to Extract

See scratchpad/metrics-glossary.md for standardized metric definitions and scopes.

**Issues-related metrics:**
- Total issues analyzed (`total_issues` - may differ by report scope)
- Open issues count (`open_issues`)
- Closed issues count (`closed_issues`)
- Issues opened in last 7/30 days (`issues_opened_7d`, `issues_opened_30d`)
- Stale issues count (`stale_issues`)
- Issues without labels (`issues_without_labels`)
- Issues without assignees (`issues_without_assignees`)

**PR-related metrics:**
- Total PRs (`total_prs`)
- Merged PRs (`merged_prs`)
- Open PRs (`open_prs`)
- Average merge time

**Activity metrics:**
- Total commits
- Active contributors
- Discussion count

**Workflow metrics:**
- Workflow runs analyzed (`workflow_runs_analyzed` - document time range)
- Firewall-enabled workflows (`firewall_enabled_workflows`)
- MCP-enabled workflows (`mcp_enabled_workflows`)

**Token/Cost metrics (if available):**
- Total tokens consumed
- Total cost
- Per-workflow statistics

**Error/Health metrics (if available):**
- Job success rates
- Error counts
- Blocked domains count (`firewall_domains_blocked`)

### 2.2 Parsing Strategy

1. Read each discussion body
2. Use regex or structured parsing to extract numeric values
3. Store extracted metrics in a structured format for analysis

Example parsing approach (for each discussion in your data):
```bash
# For each discussion body extracted from the query results, parse metrics

# Extract numeric patterns from discussion body content
grep -oE '[0-9,]+\s+(issues|PRs|tokens|runs)' /tmp/gh-aw/agent/report.md
grep -oE '\$[0-9]+\.[0-9]+' /tmp/gh-aw/agent/report.md  # Cost values
grep -oE '[0-9]+%' /tmp/gh-aw/agent/report.md  # Percentages
```

## Phase 3: Cross-Check Data Consistency

### 3.1 Internal Consistency Checks

For each report, verify:
- **Math checks**: Do percentages add up to 100%?
- **Count checks**: Do open + closed = total?
- **Trend checks**: Are trends consistent with raw numbers?

### 3.2 Cross-Report Consistency Checks

Compare metrics across different reports using standardized names from scratchpad/metrics-glossary.md:

**Before flagging discrepancies:**
1. **Check metric scopes** - Review the glossary to understand if metrics have different scopes
2. **Document scope differences** - Note when metrics intentionally differ (e.g., `issues_analyzed` varies by report)
3. **Only flag true discrepancies** - Compare metrics with identical scopes and definitions

**Examples of expected differences:**
- `issues_analyzed` in Daily Issues Report (1000 issues) vs Issue Arborist (100 open issues) - DIFFERENT SCOPES, not a discrepancy
- `open_issues` across all reports - SAME SCOPE, should match within 5-10%

**What to compare:**
- **Issue counts**: Do different reports agree on `open_issues` and `closed_issues`?
- **PR counts**: Are `total_prs`, `merged_prs`, `open_prs` consistent across reports?
- **Activity levels**: Do activity metrics align across reports?
- **Time periods**: Are reports analyzing the same time windows?

### 3.3 Anomaly Detection

Flag potential issues (referencing scratchpad/metrics-glossary.md for expected scopes):
- **Large discrepancies**: Numbers differ by more than 10% across reports **for metrics with identical scopes**
- **Scope mismatches**: Document when metrics have intentionally different scopes (e.g., `issues_analyzed`)
- **Unexpected zeros**: Zero counts where there should be activity
- **Unusual spikes**: Sudden large increases that seem unreasonable
- **Missing data**: Reports that should have data but are empty
- **Stale data**: Reports using outdated data

**Example validation logic:**
```bash
# When comparing open_issues across reports, check if they're within tolerance
# This metric has the same scope across all reports (see scratchpad/metrics-glossary.md)
issues_report_open=150
arborist_report_open=148
tolerance=10  # 10% tolerance

# Calculate percentage difference
diff=$((100 * (issues_report_open - arborist_report_open) / issues_report_open))
if [ $diff -gt $tolerance ]; then
  echo "⚠️ Discrepancy in open_issues: Daily Issues ($issues_report_open) vs Issue Arborist ($arborist_report_open)"
fi

# However, issues_analyzed should NOT be compared as they have different scopes:
# - Daily Issues Report: 1000 issues (see scratchpad/metrics-glossary.md)
# - Issue Arborist: 100 open issues without parent (see scratchpad/metrics-glossary.md)
# These are intentionally different and should be documented, not flagged as errors
```

## Phase 4: Generate Regulatory Report

Create a comprehensive discussion report with findings.

### Discussion Format

**Title**: `[daily regulatory] Regulatory Report - YYYY-MM-DD`

**Body**:

```markdown
Brief 2-3 paragraph executive summary highlighting:
- Number of daily reports reviewed
- Overall data quality assessment
- Key findings and any critical issues

<details>
<summary>📋 Full Regulatory Report</summary>

### 📊 Reports Reviewed

| Report | Title | Created | Status |
|--------|-------|---------|--------|
| [Report 1] | [Title] | [Timestamp] | ✅ Valid / ⚠️ Issues / ❌ Failed |
| [Report 2] | [Title] | [Timestamp] | ✅ Valid / ⚠️ Issues / ❌ Failed |
| ... | ... | ... | ... |

### 🔍 Data Consistency Analysis

### Cross-Report Metrics Comparison

Reference scratchpad/metrics-glossary.md for metric definitions and scopes.

| Metric | Issues Report | Performance Report | Chronicle | Scope Match | Status |
|--------|---------------|-------------------|-----------|-------------|--------|
| Open Issues (`open_issues`) | [N] | [N] | [N] | ✅ Same | ✅/⚠️/❌ |
| Closed Issues (`closed_issues`) | [N] | [N] | [N] | ✅ Same | ✅/⚠️/❌ |
| Total PRs (`total_prs`) | [N] | [N] | [N] | ✅ Same | ✅/⚠️/❌ |
| Merged PRs (`merged_prs`) | [N] | [N] | [N] | ✅ Same | ✅/⚠️/❌ |
| Issues Analyzed (`issues_analyzed`) | 1000 | - | - | ⚠️ Different Scopes | ℹ️ See Note |

**Scope Notes:**
- `issues_analyzed`: Daily Issues (1000 total) vs Issue Arborist (100 open without parent) - Different scopes by design
- `workflow_runs_analyzed`: Firewall Report (7d) vs Observability (7d) - Same scope, should match

### Consistency Score

- **Overall Consistency**: [SCORE]% (X of Y metrics match across reports)
- **Critical Discrepancies**: [COUNT]
- **Minor Discrepancies**: [COUNT]

### ⚠️ Issues and Anomalies

### Critical Issues

1. **[Issue Title]**
   - **Affected Reports**: [List of reports]
   - **Metric**: [Metric name from scratchpad/metrics-glossary.md]
   - **Description**: [What was found]
   - **Expected**: [What was expected]
   - **Actual**: [What was found]
   - **Scope Analysis**: [Are the scopes identical? Reference glossary]
   - **Severity**: Critical / High / Medium / Low
   - **Recommended Action**: [Suggestion]

### Warnings

1. **[Warning Title]**
   - **Details**: [Description]
   - **Impact**: [Potential impact]

### Data Quality Notes

- [Note about missing data]
- [Note about incomplete reports]
- [Note about data freshness]

### 📈 Trend Analysis

### Week-over-Week Comparison

| Metric | This Week | Last Week | Change |
|--------|-----------|-----------|--------|
| [Metric 1] | [Value] | [Value] | [+/-X%] |
| [Metric 2] | [Value] | [Value] | [+/-X%] |

### Notable Trends

- [Observation about trends]
- [Pattern identified across reports]
- [Concerning or positive trend]

### 📝 Per-Report Analysis

### [Report 1 Name]

**Source**: [Discussion URL or number]
**Time Period**: [What period the report covers]
**Quality**: ✅ Valid / ⚠️ Issues / ❌ Failed

**Extracted Metrics**:
| Metric | Value | Validation |
|--------|-------|------------|
| [Metric] | [Value] | ✅/⚠️/❌ |

**Notes**: [Any observations about this report]

### [Report 2 Name]

[Same structure as above]

### 💡 Recommendations

### Process Improvements

1. **[Recommendation]**: [Description and rationale]
2. **[Recommendation]**: [Description and rationale]

### Data Quality Actions

1. **[Action Item]**: [What needs to be done]
2. **[Action Item]**: [What needs to be done]

### Workflow Suggestions

1. **[Suggestion]**: [For improving consistency across reports]

### 📊 Regulatory Metrics

| Metric | Value |
|--------|-------|
| Reports Reviewed | [N] |
| Reports Passed | [N] |
| Reports with Issues | [N] |
| Reports Failed | [N] |
| Overall Health Score | [X]% |

</details>

---
*Report generated automatically by the Daily Regulatory workflow*
*Data sources: Daily report discussions from ${{ github.repository }}*
*Metric definitions: scratchpad/metrics-glossary.md*
```

## Phase 5: Close Previous Reports

Before creating the new discussion, find and close previous daily regulatory discussions:

1. Search for discussions with title prefix "[daily regulatory]"
2. Close each found discussion with reason "OUTDATED"
3. Add a closing comment: "This report has been superseded by a newer daily regulatory report."

Use the `close_discussion` safe output for each discussion found.

## Important Guidelines

### Data Collection
- Focus on discussions from the last 24-48 hours
- Identify daily reports by their title patterns
- Handle cases where reports are missing or empty

### Cross-Checking
- Be systematic in comparing metrics
- Use tolerance thresholds for numeric comparisons (e.g., 5-10% variance is acceptable)
- Document methodology for consistency checks

### Anomaly Detection
- Flag significant discrepancies (>10% difference)
- Note missing or incomplete data
- Identify patterns that seem unusual

### Report Quality
- Be specific with findings and examples
- Provide actionable recommendations
- Use clear visual indicators (✅/⚠️/❌) for quick scanning
- Keep executive summary brief but informative

### Error Handling
- If no daily reports are found, create a report noting the absence
- Handle malformed or unparseable reports gracefully
- Note any limitations in the analysis

## Success Criteria

A successful regulatory run will:
- ✅ Verify all prerequisites (discussions access, safe-output tools) before proceeding
- ✅ Exit early with a clear explanation if prerequisites are not met
- ✅ Find and analyze all available daily report discussions
- ✅ Extract and compare key metrics across reports
- ✅ Identify any discrepancies or anomalies
- ✅ Close previous regulatory discussions
- ✅ Create a new discussion with comprehensive findings
- ✅ Provide actionable recommendations for data quality improvement

Begin your regulatory analysis now. First verify prerequisites, then find the daily reports, extract metrics, cross-check for consistency, and create the regulatory report.