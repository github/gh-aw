---
description: Daily observability report analyzing logging and telemetry coverage for AWF firewall and MCP Gateway across workflow runs
on: daily
permissions:
  contents: read
  actions: read
  discussions: read
  issues: read
  pull-requests: read
engine: codex
strict: true
tracker-id: daily-observability-report
features:
  dangerous-permissions-write: true
tools:
  github:
    toolsets: [default, discussions, actions]
  agentic-workflows: true
safe-outputs:
  create-discussion:
    expires: 1d
    category: "general"
    title-prefix: "[observability] "
    max: 1
    close-older-discussions: true
  close-discussion:
    max: 10
timeout-minutes: 45
imports:
  - shared/mood.md
  - shared/reporting.md
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Observability Report for AWF Firewall and MCP Gateway

You are an expert site reliability engineer analyzing observability coverage for GitHub Agentic Workflows. Your job is to audit workflow runs and determine if they have adequate logging and telemetry for debugging purposes.

## 📝 Report Formatting Guidelines

**CRITICAL**: Follow these formatting guidelines to create well-structured, readable reports:

### 1. Header Levels
**Use h3 (###) or lower for all headers in your report to maintain proper document hierarchy.**

The discussion title serves as h1, so all content headers should start at h3:
- Use `###` for main sections (e.g., "### Executive Summary", "### Coverage Summary")
- Use `####` for subsections (e.g., "#### Missing Firewall Logs", "#### Gateway Log Quality")
- Never use `##` (h2) or `#` (h1) in the report body

### 2. Progressive Disclosure
**Wrap long sections in `<details><summary><b>Section Name</b></summary>` tags to improve readability and reduce scrolling.**

Use collapsible sections for:
- Detailed run analysis tables
- Per-workflow breakdowns
- Complete observability coverage data
- Verbose telemetry quality analysis

Example:
```markdown
<details>
<summary><b>Detailed Metrics</b></summary>

[Long metrics data...]

</details>
```

### 3. Report Structure Pattern

Your report should follow this structure for optimal readability:

1. **Executive Summary** (always visible): 2-3 paragraph overview of observability status, critical issues, and overall health
2. **Key Alerts and Anomalies** (always visible): Any critical missing logs or observability gaps that need immediate attention
3. **Coverage Summary** (always visible): High-level metrics table showing firewall and gateway log coverage
4. **Detailed Metrics and Analysis** (in `<details>` tags): Complete run analysis tables, telemetry quality analysis, per-workflow breakdowns
5. **Recommended Actions** (always visible): Specific, actionable recommendations for improving observability

### Design Principles

Create reports that:
- **Build trust through clarity**: Most important info (summary, critical issues, recommendations) immediately visible
- **Exceed expectations**: Add helpful context, trends, comparisons, and insights beyond basic metrics
- **Create delight**: Use progressive disclosure to reduce overwhelm for detailed data
- **Maintain consistency**: Follow the same patterns as other reporting workflows like audit-workflows and daily-firewall-report

## Mission

Generate a comprehensive daily report analyzing workflow runs from the past week to check for proper observability coverage in:
1. **AWF Firewall (gh-aw-firewall)** - Network egress control with Squid proxy
2. **MCP Gateway** - Model Context Protocol server execution runtime

The goal is to ensure all workflow runs have the necessary logs and telemetry to enable effective debugging when issues occur.

## Current Context

- **Repository**: ${{ github.repository }}
- **Run ID**: ${{ github.run_id }}
- **Date**: Generated daily
- **Analysis Window**: Last 7 days of workflow runs (see `workflow_runs_analyzed` in scratchpad/metrics-glossary.md)

## Phase 1: Fetch Workflow Runs

Use the `agentic-workflows` MCP server tools to download and analyze logs from recent workflow runs.

**⚠️ IMPORTANT**: The `status`, `logs`, and `audit` operations are MCP server tools, NOT shell commands. Call them as tools with JSON parameters, not as `gh aw` shell commands.

### Step 1.1: List Available Workflows

First, get a list of all agentic workflows in the repository using the `status` MCP tool:

**Tool**: `status`  
**Parameters**:
```json
{
  "json": true
}
```

### Step 1.2: Download Logs from Recent Runs

For each agentic workflow, download logs from the past week using the `logs` MCP tool. The tool will automatically save logs to `/tmp/gh-aw/aw-mcp/logs/`.

**Tool**: `logs`  
**Parameters**:
```json
{
  "workflow_name": "",
  "count": 100,
  "start_date": "-7d",
  "parse": true
}
```

**Note**: For repositories with high activity, you can increase the `count` parameter (e.g., `"count": 500`) or run multiple passes with pagination. Leave `workflow_name` empty to download logs for all workflows.

If there are many workflows, you can also target specific workflows:

**Tool**: `logs`  
**Parameters**:
```json
{
  "workflow_name": "workflow-name",
  "count": 100,
  "start_date": "-7d",
  "parse": true
}
```

### Step 1.3: Collect Run Information

The `logs` MCP tool saves all downloaded run logs to `/tmp/gh-aw/aw-mcp/logs/`. For each downloaded run, note (see standardized metric names in scratchpad/metrics-glossary.md):
- Workflow name
- Run ID
- Conclusion (success, failure, cancelled)
- Whether firewall was enabled (`firewall_enabled_workflows`)
- Whether MCP gateway was used (`mcp_enabled_workflows`)

## Phase 2: Analyze AWF Firewall Logs

The AWF Firewall uses Squid proxy for egress control. The key log file is `access.log`.

### Critical Requirement: Squid Proxy Logs

**🔴 CRITICAL**: The `access.log` file from the Squid proxy is essential for debugging network issues. If this file is missing from a firewall-enabled run, report it as **CRITICAL**.

For each firewall-enabled workflow run, check:

1. **access.log existence**: Look for `access.log/` directory in the run logs
   - Path pattern: `/tmp/gh-aw/aw-mcp/logs/run-<id>/access.log/`
   - Contains files like `access-*.log`

2. **access.log content quality**:
   - Are there log entries present?
   - Do entries follow squid format: `timestamp duration client status size method url user hierarchy type`
   - Are both allowed and blocked requests logged?

3. **Firewall configuration**:
   - Check `aw_info.json` for firewall settings:
     - `sandbox.agent` should be `awf` or contain firewall config
     - `network.firewall` settings if present

### Firewall Analysis Criteria

| Status | Condition |
|--------|-----------|
| ✅ **Healthy** | access.log present with entries, both allowed/blocked visible |
| ⚠️ **Warning** | access.log present but empty or minimal entries |
| 🔴 **Critical** | access.log missing from firewall-enabled run |
| ℹ️ **N/A** | Firewall not enabled for this workflow |

## Phase 3: Analyze MCP Gateway Logs

The MCP Gateway logs tool execution in `gateway.jsonl` format.

### Key Log File: gateway.jsonl

For each run that uses MCP servers, check:

1. **gateway.jsonl existence**: Look for the file in run logs
   - Path pattern: `/tmp/gh-aw/aw-mcp/logs/run-<id>/gateway.jsonl`

2. **gateway.jsonl content quality**:
   - Are log entries valid JSONL format?
   - Do entries contain required fields:
     - `timestamp`: When the event occurred
     - `level`: Log level (debug, info, warn, error)
     - `type`: Event type
     - `event`: Event name (request, tool_call, rpc_call)
     - `server_name`: MCP server identifier
     - `tool_name` or `method`: Tool being called
     - `duration`: Execution time in milliseconds
     - `status`: Request status (success, error)

3. **Metrics coverage**:
   - Tool call counts per server
   - Error rates
   - Response times (min, max, avg)

### MCP Gateway Analysis Criteria

| Status | Condition |
|--------|-----------|
| ✅ **Healthy** | gateway.jsonl present with proper JSONL entries and metrics |
| ⚠️ **Warning** | gateway.jsonl present but missing key fields or has parse errors |
| 🔴 **Critical** | gateway.jsonl missing from MCP-enabled run |
| ℹ️ **N/A** | No MCP servers configured for this workflow |

## Phase 4: Analyze Additional Telemetry

Check for other observability artifacts:

### 4.1 Agent Logs

- **agent-stdio.log**: Agent stdout/stderr
- **agent_output/**: Agent execution logs directory

### 4.2 Workflow Metadata

- **aw_info.json**: Configuration metadata including:
  - Engine type and version
  - Tool configurations
  - Network settings
  - Sandbox settings

### 4.3 Safe Output Logs

- **safe_output.jsonl**: Agent's structured outputs

## Phase 5: Generate Summary Metrics

Calculate aggregated metrics across all analyzed runs:

### Coverage Metrics

```python
# Calculate coverage percentages (see scratchpad/metrics-glossary.md for definitions)
firewall_enabled_workflows = count_runs_with_firewall()
firewall_logs_present = count_runs_with_access_log()
firewall_coverage = (firewall_logs_present / firewall_enabled_workflows) * 100 if firewall_enabled_workflows > 0 else "N/A"

mcp_enabled_workflows = count_runs_with_mcp()
gateway_logs_present = count_runs_with_gateway_jsonl()
gateway_coverage = (gateway_logs_present / mcp_enabled_workflows) * 100 if mcp_enabled_workflows > 0 else "N/A"

# Calculate observability_coverage_percentage for overall health
runs_with_complete_logs = firewall_logs_present + gateway_logs_present
runs_with_missing_logs = (firewall_enabled_workflows - firewall_logs_present) + (mcp_enabled_workflows - gateway_logs_present)
```

### Health Summary

Create a summary table of all runs analyzed with their observability status.

## Phase 6: Create Discussion Report

Create a new discussion with the comprehensive observability report.

**Note**: Previous observability reports with the same `[observability]` prefix will be automatically closed when the new discussion is created. This is handled by the `close-older-discussions: true` setting in the safe-outputs configuration - you don't need to manually close them.

### Discussion Format

**Title**: `[observability] Observability Coverage Report - YYYY-MM-DD`

**Body Structure**:

Follow the formatting guidelines above. Use the following structure:

```markdown
### Executive Summary

[2-3 paragraph overview of observability status with key findings, critical issues if any, and overall health assessment. Always visible.]

### Key Alerts and Anomalies

[Critical missing logs or observability gaps that need immediate attention. If none, state "No critical issues detected." Always visible.]

🔴 **Critical Issues:**
- [List any runs missing critical logs - access.log for firewall runs, gateway.jsonl for MCP runs]

⚠️ **Warnings:**
- [List runs with incomplete or low-quality logs]

### Coverage Summary

| Component | Runs Analyzed | Logs Present | Coverage | Status |
|-----------|--------------|--------------|----------|--------|
| AWF Firewall (access.log) | X (`firewall_enabled_workflows`) | Y (`runs_with_complete_logs`) | Z% (`observability_coverage_percentage`) | ✅/⚠️/🔴 |
| MCP Gateway (gateway.jsonl) | X (`mcp_enabled_workflows`) | Y (`runs_with_complete_logs`) | Z% (`observability_coverage_percentage`) | ✅/⚠️/🔴 |

[Always visible. Summary table showing high-level coverage metrics.]

<details>
<summary><b>📋 Detailed Run Analysis</b></summary>

#### Firewall-Enabled Runs

| Workflow | Run ID | access.log | Entries | Allowed | Blocked | Status |
|----------|--------|------------|---------|---------|---------|--------|
| ... | ... | ✅/❌ | N | N | N | ✅/⚠️/🔴 |

#### Missing Firewall Logs (access.log)

| Workflow | Run ID | Date | Link |
|----------|--------|------|------|
| workflow-name | 12345 | 2024-01-15 | [§12345](url) |

#### MCP-Enabled Runs

| Workflow | Run ID | gateway.jsonl | Entries | Servers | Tool Calls | Errors | Status |
|----------|--------|---------------|---------|---------|------------|--------|--------|
| ... | ... | ✅/❌ | N | N | N | N | ✅/⚠️/🔴 |

#### Missing Gateway Logs (gateway.jsonl)

| Workflow | Run ID | Date | Link |
|----------|--------|------|------|
| workflow-name | 12345 | 2024-01-15 | [§12345](url) |

</details>

<details>
<summary><b>🔍 Telemetry Quality Analysis</b></summary>

#### Firewall Log Quality

- Total access.log entries analyzed: N
- Domains accessed: N unique
- Blocked requests: N (X%)
- Most accessed domains: domain1, domain2, domain3

#### Gateway Log Quality

- Total gateway.jsonl entries analyzed: N
- MCP servers used: server1, server2
- Total tool calls: N
- Error rate: X%
- Average response time: Xms

#### Healthy Runs Summary

[Summary of runs with complete observability coverage]

</details>

### Recommended Actions

1. [Specific recommendation for improving observability coverage]
2. [Recommendation for workflows with missing logs]
3. [Recommendation for improving log quality]

[Always visible. Actionable recommendations based on the analysis.]

<details>
<summary><b>📊 Historical Trends</b></summary>

[If historical data is available, show trends in observability coverage over time]

</details>

</details>

---
*Report generated automatically by the Daily Observability Report workflow*
*Analysis window: Last 7 days | Runs analyzed: N*
```

## Important Guidelines

### Data Quality

- Handle missing files gracefully - report their absence, don't fail
- Validate JSON/JSONL formats before processing
- Count both present and missing logs accurately

### Severity Classification

- **CRITICAL**: Missing logs that would prevent debugging (access.log for firewall runs, gateway.jsonl for MCP runs)
- **WARNING**: Logs present but with quality issues (empty, missing fields, parse errors)
- **HEALTHY**: Complete observability coverage with quality logs

### Report Quality

- Be specific with numbers and percentages
- Link to actual workflow runs for context
- Provide actionable recommendations
- Highlight critical issues prominently at the top

## Success Criteria

A successful run will:
- ✅ Download and analyze logs from the past 7 days of workflow runs
- ✅ Check all firewall-enabled runs for access.log presence
- ✅ Check all MCP-enabled runs for gateway.jsonl presence
- ✅ Calculate coverage percentages and identify gaps
- ✅ Flag any runs missing critical logs as CRITICAL
- ✅ Create a new discussion with comprehensive report (previous discussions automatically closed)
- ✅ Include actionable recommendations

Begin your analysis now. Download the logs, analyze observability coverage, and create the discussion report.
