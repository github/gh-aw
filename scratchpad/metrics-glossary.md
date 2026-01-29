# Daily Reports Metrics Glossary

This document defines standardized metric names and scopes used across all daily report workflows in the gh-aw repository. Using consistent terminology enables accurate cross-report comparisons and reduces confusion when analyzing data from multiple sources.

## Purpose

Each daily report workflow was originally developed independently, leading to inconsistent metric naming. This glossary establishes canonical names and definitions for common metrics to:

- Enable accurate cross-report comparisons
- Reduce ambiguity in metric interpretation
- Provide clear scope documentation for each metric
- Support regulatory validation and trend analysis

## How to Use This Glossary

When implementing or updating daily report workflows:

1. **Use standardized metric names** from this glossary
2. **Document scope with comments** in Python/shell scripts
3. **Reference this glossary** when comparing metrics across reports
4. **Add new metrics** to this glossary when introducing novel measurements

## Issue Metrics

### total_issues

**Definition**: Total count of issues in the repository across all states (open, closed, etc.)

**Scope**: All issues in repository, no filters applied

**Used By**: 
- Daily Issues Report
- Issue Arborist Report

**Python Variable**: `total_issues`

**Notes**: This is the absolute count. For subsets, use `issues_analyzed` or `open_issues`.

---

### open_issues

**Definition**: Count of issues where `state = "open"` at the time of report generation

**Scope**: All open issues in repository, no time filter

**Used By**:
- Daily Issues Report
- Issue Arborist Report
- Daily Regulatory Report

**Python Variable**: `open_issues`

**Notes**: This is a snapshot count at report time. May differ from `issues_analyzed` which applies additional filters.

---

### closed_issues

**Definition**: Count of issues where `state = "closed"` at the time of report generation

**Scope**: All closed issues in repository, no time filter

**Used By**:
- Daily Issues Report
- Daily Code Metrics Report

**Python Variable**: `closed_issues`

**Notes**: Includes issues closed at any time in the past.

---

### issues_analyzed

**Definition**: Subset of issues included in the current report's analysis based on specific criteria

**Scope**: Filtered subset - varies by report (see report-specific documentation)

**Used By**:
- Daily Issues Report (last 1000 issues)
- Issue Arborist Report (last 100 open issues without parent)

**Python Variable**: `issues_analyzed`

**Notes**: **IMPORTANT** - This count may differ across reports due to different filtering criteria. Always document the specific filters applied in comments.

**Example Scopes**:
- Daily Issues Report: Last 1000 issues sorted by updated date
- Issue Arborist Report: Last 100 open issues excluding those with parent issues

---

### issues_opened_7d / issues_7d

**Definition**: Count of issues created in the last 7 days

**Scope**: Issues where `createdAt > now() - 7 days`

**Used By**:
- Daily Issues Report

**Python Variable**: `issues_7d` or `issues_opened_7d`

**Standardized Name**: Use `issues_opened_7d` for clarity

---

### issues_opened_14d / issues_14d

**Definition**: Count of issues created in the last 14 days

**Scope**: Issues where `createdAt > now() - 14 days`

**Used By**:
- Daily Issues Report

**Python Variable**: `issues_14d` or `issues_opened_14d`

**Standardized Name**: Use `issues_opened_14d` for clarity

---

### issues_opened_30d / issues_30d

**Definition**: Count of issues created in the last 30 days

**Scope**: Issues where `createdAt > now() - 30 days`

**Used By**:
- Daily Issues Report
- Daily Code Metrics Report

**Python Variable**: `issues_30d` or `issues_opened_30d`

**Standardized Name**: Use `issues_opened_30d` for clarity

---

### issues_closed_30d / closed_last_30d

**Definition**: Count of issues closed in the last 30 days

**Scope**: Issues where `closedAt > now() - 30 days`

**Used By**:
- Daily Issues Report

**Python Variable**: `closed_last_30d` or `issues_closed_30d`

**Standardized Name**: Use `issues_closed_30d` for clarity

---

### stale_issues

**Definition**: Count of open issues with no activity (comments, updates) in the last 30 days

**Scope**: Open issues where `updatedAt < now() - 30 days` and `state = "open"`

**Used By**:
- Daily Issues Report
- Daily Code Metrics Report

**Python Variable**: `stale_issues`

**Notes**: Used to identify issues that may need attention or closure.

---

### issues_without_labels / unlabeled_issues

**Definition**: Count of issues that have no labels assigned

**Scope**: Issues where `labels` array is empty

**Used By**:
- Daily Issues Report

**Python Variable**: `unlabeled_issues` or `issues_without_labels`

**Standardized Name**: Use `issues_without_labels` for clarity

---

### issues_without_assignees / unassigned_issues

**Definition**: Count of issues that have no assignees

**Scope**: Issues where `assignees` array is empty

**Used By**:
- Daily Issues Report

**Python Variable**: `unassigned_issues` or `issues_without_assignees`

**Standardized Name**: Use `issues_without_assignees` for clarity

---

## Pull Request Metrics

### total_prs

**Definition**: Total count of pull requests in the repository across all states

**Scope**: All PRs in repository, no filters applied

**Used By**:
- Copilot Agent Analysis
- Daily Performance Summary

**Python Variable**: `total_prs`

---

### open_prs

**Definition**: Count of pull requests where `state = "open"`

**Scope**: All open PRs in repository

**Used By**:
- Copilot Agent Analysis
- Daily Performance Summary

**Python Variable**: `open_prs`

---

### merged_prs

**Definition**: Count of pull requests that have been merged

**Scope**: PRs where `merged = true`

**Used By**:
- Copilot Agent Analysis
- Daily Performance Summary

**Python Variable**: `merged_prs`

---

### closed_prs

**Definition**: Count of pull requests that were closed without merging

**Scope**: PRs where `state = "closed"` and `merged = false`

**Used By**:
- Copilot Agent Analysis

**Python Variable**: `closed_prs`

---

## Workflow Metrics

### total_workflows

**Definition**: Total count of agentic workflow source files in `.github/workflows/`

**Scope**: All `.md` workflow source files (agentic workflows, not standard GitHub Actions YAML)

**Used By**:
- Daily Code Metrics Report

**Python Variable**: `total_workflows`

**Notes**: This counts `.md` files which are agentic workflow sources that compile to `.lock.yml` files. Standard GitHub Actions workflows (`.yml`) are not included in this count.

---

### workflows_analyzed / workflow_runs_analyzed

**Definition**: Number of workflow runs analyzed in the report period

**Scope**: Varies by report - document specific time range

**Used By**:
- Daily Firewall Report (last 7 days)
- Daily Observability Report (last 7 days)

**Python Variable**: `workflow_runs_analyzed`

**Standardized Name**: Use `workflow_runs_analyzed` for clarity

**Notes**: Always specify the time range in comments (e.g., "last 7 days")

---

### workflows_with_firewall / firewall_enabled_workflows

**Definition**: Count of workflow runs where firewall was enabled

**Scope**: Workflow runs with `steps.firewall` field present in `aw_info.json`

**Used By**:
- Daily Firewall Report
- Daily Observability Report

**Python Variable**: `firewall_enabled_workflows`

**Standardized Name**: Use `firewall_enabled_workflows` for clarity

---

### workflows_with_mcp / mcp_enabled_workflows

**Definition**: Count of workflow runs where MCP servers were configured

**Scope**: Workflow runs with MCP server configuration in frontmatter

**Used By**:
- Daily Observability Report

**Python Variable**: `mcp_enabled_workflows`

**Standardized Name**: Use `mcp_enabled_workflows` for clarity

---

## Firewall Metrics

### total_requests / firewall_requests_total

**Definition**: Total number of network requests made through the firewall

**Scope**: All requests logged in Squid access.log files

**Used By**:
- Daily Firewall Report

**Python Variable**: `firewall_requests_total`

**Standardized Name**: Use `firewall_requests_total` for clarity

---

### allowed_requests / firewall_requests_allowed

**Definition**: Count of network requests that were allowed by the firewall

**Scope**: Requests with HTTP 200-399 status codes in access.log

**Used By**:
- Daily Firewall Report

**Python Variable**: `firewall_requests_allowed`

**Standardized Name**: Use `firewall_requests_allowed` for clarity

---

### blocked_requests / firewall_requests_blocked

**Definition**: Count of network requests that were blocked by the firewall

**Scope**: Requests with HTTP 403 or 5xx status codes in access.log

**Used By**:
- Daily Firewall Report

**Python Variable**: `firewall_requests_blocked`

**Standardized Name**: Use `firewall_requests_blocked` for clarity

---

### blocked_domains / firewall_domains_blocked

**Definition**: Count of unique domains that were blocked by the firewall

**Scope**: Distinct domain names extracted from blocked requests

**Used By**:
- Daily Firewall Report

**Python Variable**: `firewall_domains_blocked`

**Standardized Name**: Use `firewall_domains_blocked` for clarity

---

## Code Quality Metrics

### total_loc / lines_of_code_total

**Definition**: Total lines of code in the repository across all languages

**Scope**: All source files, calculated using cloc or similar tool

**Used By**:
- Daily Code Metrics Report

**Python Variable**: `lines_of_code_total`

**Standardized Name**: Use `lines_of_code_total` for clarity

---

### test_loc / test_lines_of_code

**Definition**: Total lines of code in test files

**Scope**: Files matching test patterns (*_test.go, *.test.js, test_*.py, etc.)

**Used By**:
- Daily Code Metrics Report

**Python Variable**: `test_lines_of_code`

**Standardized Name**: Use `test_lines_of_code` for clarity

---

### test_to_source_ratio

**Definition**: Ratio of test code to source code (test LOC / source LOC)

**Scope**: Excludes test files from source LOC calculation

**Used By**:
- Daily Code Metrics Report

**Python Variable**: `test_to_source_ratio`

**Notes**: Values between 0.5-1.0 are considered healthy

---

## Observability Metrics

### runs_with_logs / runs_with_complete_logs

**Definition**: Count of workflow runs that have complete log coverage

**Scope**: Runs with all required log files present (access.log, gateway.jsonl, etc.)

**Used By**:
- Daily Observability Report

**Python Variable**: `runs_with_complete_logs`

**Standardized Name**: Use `runs_with_complete_logs` for clarity

---

### runs_missing_logs / runs_with_missing_logs

**Definition**: Count of workflow runs missing critical log files

**Scope**: Firewall-enabled runs without access.log or MCP runs without gateway.jsonl

**Used By**:
- Daily Observability Report

**Python Variable**: `runs_with_missing_logs`

**Standardized Name**: Use `runs_with_missing_logs` for clarity

---

### observability_coverage_percentage

**Definition**: Percentage of workflow runs with complete observability coverage

**Scope**: (runs_with_complete_logs / total_runs_analyzed) * 100

**Used By**:
- Daily Observability Report

**Python Variable**: `observability_coverage_percentage`

---

## Copilot Agent Metrics

### agent_prs_total

**Definition**: Total count of pull requests created by Copilot agent

**Scope**: PRs created by copilot-swe-agent (user.login == "copilot" or branch starts with "copilot/")

**Used By**:
- Copilot Agent Analysis

**Python Variable**: `agent_prs_total`

**Standardized Name**: Use `agent_prs_total` for clarity

---

### agent_prs_merged

**Definition**: Count of Copilot agent PRs that were successfully merged

**Scope**: Agent PRs where `merged = true`

**Used By**:
- Copilot Agent Analysis

**Python Variable**: `agent_prs_merged`

**Standardized Name**: Use `agent_prs_merged` for clarity

---

### agent_success_rate

**Definition**: Percentage of Copilot agent PRs that were merged

**Scope**: (agent_prs_merged / agent_prs_total) * 100

**Used By**:
- Copilot Agent Analysis

**Python Variable**: `agent_success_rate`

---

## Cross-Report Comparison Guidelines

### Understanding Scope Differences

When comparing metrics across reports, be aware that identical metric names may have different scopes:

**Example: `issues_analyzed`**
- **Daily Issues Report**: Last 1000 issues (all states)
- **Issue Arborist Report**: Last 100 open issues without parent

These are intentionally different scopes for different purposes. The regulatory report should acknowledge these differences rather than flag them as discrepancies.

### Scope Documentation in Code

Always document the scope of metrics in Python/shell scripts:

```python
# Scope: Open issues created in last 7 days
issues_opened_7d = len(df[
    (df['state'] == 'OPEN') & 
    (df['createdAt'] > now - timedelta(days=7))
])
```

```bash
# Scope: Workflow runs from last 7 days with firewall enabled
firewall_enabled_workflows=$(jq '[.[] | select(.firewall_enabled == true)] | length' runs.json)
```

### Validation Rules

The regulatory report should:

1. **Compare like-with-like**: Only compare metrics with identical scopes
2. **Document scope differences**: Note when metrics have different scopes and explain why
3. **Flag true discrepancies**: Report when metrics with same scope differ by >10%
4. **Reference this glossary**: Link to glossary entries when reporting discrepancies

## Adding New Metrics

When introducing new metrics to daily reports:

1. Add a definition to this glossary with:
   - Standardized name
   - Clear definition
   - Explicit scope documentation
   - Reports that use it
   - Python/shell variable name
2. Update relevant report workflows to use the standardized name
3. Add scope comments in the implementation code
4. Update the regulatory report if cross-comparison is needed

## Version History

- **2026-01-18**: Initial version - standardized metrics across 7+ daily reports
