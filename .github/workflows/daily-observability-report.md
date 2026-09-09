---
private: true
emoji: "📊"
description: Daily observability report analyzing logging and telemetry coverage for AWF firewall and MCP Gateway across workflow runs
on: daily
permissions:
  contents: read
  actions: read
  discussions: read
  issues: read
  pull-requests: read
model: gpt-5.3-codex
engine:
  id: codex
strict: true
tracker-id: daily-observability-report
tools:
  agentic-workflows: true
timeout-minutes: 45
# Default AI credit budget for this workflow.
max-ai-credits: 1500
imports:
  - uses: shared/meta-analysis-base.md
    with:
      toolsets: [default, discussions, actions]
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[observability] "
      expires: 1d
  - shared/reporting.md


  - shared/otlp.md
features:
  gh-aw-detection: true
sandbox:
  agent:
    id: awf
evals:
  - id: observability_data_analyzed
    question: Did the agent analyze logging and telemetry coverage for AWF firewall and MCP Gateway?
  - id: report_created
    question: Was an observability report created identifying gaps and trends?
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Observability Report for AWF Firewall and MCP Gateway

You are an expert site reliability engineer analyzing observability coverage for GitHub Agentic Workflows. Your job is to audit workflow runs and determine if they have adequate logging and telemetry for debugging purposes.

## Mission

Generate a daily report analyzing a representative, capped set of workflow runs from the past week to check for proper observability coverage in:
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

### Step 1.1: Download Logs from Recent Runs

Start with a single broad `logs` MCP tool call. The tool will automatically save logs to `/tmp/gh-aw/aw-mcp/logs/`.

Using `count: 30` gives a recent, representative cross-workflow sample without forcing the agent to download and compare every run from the full week.

**Tool**: `logs`  
**Parameters**:
```json
{
  "workflow_name": "",
  "count": 30,
  "start_date": "-7d",
  "artifacts": ["usage", "agent", "detection", "firewall", "mcp"]
}
```

Do **not** list every workflow first and do **not** paginate broadly. One repository-wide fetch is the default path.

### Step 1.2: Narrow Follow-up Only When Needed

Only if the broad fetch is missing an important class of run (for example, no recent firewall-enabled run or no recent MCP-enabled run), do a small number of targeted follow-up `logs` calls:

**Tool**: `logs`  
**Parameters**:
```json
{
  "workflow_name": "workflow-name",
  "count": 100,
  "start_date": "-7d",
  "artifacts": ["usage", "agent", "detection", "firewall", "mcp"]
}
```

Keep targeted follow-up minimal:
- At most **5** targeted `logs` calls total
- At most **10** runs per targeted call
- Prefer the most recent failed or cancelled runs first, then successful runs
- These follow-up calls only expand the candidate pool; they do **not** override the total analysis cap below

### Step 1.3: Cap Analysis Scope

Analyze at most **20 runs total** across all fetched results.

Prioritize runs in this order:
1. Failed or cancelled runs
2. Runs with firewall enabled
3. Runs with MCP servers configured
4. Most recent successful runs needed to confirm healthy coverage

When multiple runs come from the same workflow, keep at most **2 runs per workflow** within the 20-run total cap unless a third run is needed to confirm a repeated critical gap. If you are near the 20-run cap, prefer breadth across workflows over extra runs from the same workflow.

### Step 1.4: Collect Run Information

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

1. **access.log existence**: Search recursively inside each run folder for firewall access logs
   - Canonical path (current AWF layout): `run-<id>/sandbox/firewall/logs/squid-logs/access.log`
   - Also accept the legacy path: `run-<id>/sandbox/firewall/logs/access.log` (older AWF layout)
   - Also accept equivalent paths nested under artifact-prefixed directories (workflow_call)
   - Do not assume a fixed top-level location; use recursive discovery

2. **access.log content quality**:
   - Are there log entries present?
   - Do entries follow AWF custom format: `timestamp client_ip:port domain dest_ip:port proto method status decision url user_agent`
   - Example entry: `1761332530.474 172.30.0.20:35288 api.github.com:443 140.82.112.22:443 1.1 CONNECT 200 TCP_TUNNEL:HIER_DIRECT api.github.com:443 "-"`
   - Are both allowed (TCP_TUNNEL) and blocked (TCP_DENIED) requests logged?

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

The MCP Gateway logs tool execution. Two log formats may be present depending on engine version:

- **`gateway.jsonl`**: Structured gateway log with per-event metrics (preferred format)
- **`mcp-logs/rpc-messages.jsonl`**: Raw JSON-RPC message log written by the Copilot CLI (canonical fallback)

### Key Log Files: gateway.jsonl or rpc-messages.jsonl

For each run that uses MCP servers, check in this order:

1. **gateway.jsonl existence** (preferred): Search recursively inside the run folder
   - Canonical path: `/tmp/gh-aw/aw-mcp/logs/run-<id>/mcp-logs/gateway.jsonl`
   - Also accept equivalent paths nested under artifact-prefixed directories (workflow_call)

2. **rpc-messages.jsonl existence** (canonical fallback): Check when gateway.jsonl is missing
   - Canonical path: `/tmp/gh-aw/aw-mcp/logs/run-<id>/mcp-logs/rpc-messages.jsonl`
   - Also accept equivalent paths nested under artifact-prefixed directories (workflow_call)
   - This file is written by the Copilot CLI and contains raw JSON-RPC protocol messages
   - A run with this file present has MCP telemetry and should NOT be reported as Critical

**Path normalization rule (required):**
- For each run, determine telemetry presence by recursively finding files named `gateway.jsonl` or `rpc-messages.jsonl` anywhere under `run-<id>/`.
- Record the exact discovered path(s) in your analysis notes before assigning status.
- Never classify a run as "missing MCP telemetry" based only on one hardcoded path check.

3. **gateway.jsonl content quality** (when present):
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

4. **rpc-messages.jsonl content quality** (when used as fallback):
   - Are entries valid JSONL format?
   - Do entries contain required fields:
     - `timestamp`: When the message was sent/received
     - `direction`: "IN" (from server) or "OUT" (to server)
     - `event` or `type`: Message kind. Real-world telemetry (schema `rpc-message/v2`) uses a
       top-level `event` field with values `rpc_request` / `rpc_response` (and `difc_filtered`
       for blocked events); a top-level `_schema` field (e.g. `"rpc-message/v2"`) marks this
       format. Older/synthetic files may instead use a top-level `type` field directly with
       values `REQUEST` / `RESPONSE` / `DIFC_FILTERED`. **Do not flag a run as unhealthy solely
       because entries lack a top-level `type` field** — check for `event` first.
     - `server_id`: MCP server identifier
     - `payload`: JSON-RPC payload with `method`, `params`, `result`, or `error`
   - Tool call count derived from outgoing `tools/call` requests

5. **Metrics coverage** (from whichever log is available):
   - Tool call counts per server
   - Error rates
   - Response times (min, max, avg) — available in gateway.jsonl; computed from request/response pairing in rpc-messages.jsonl

### MCP Gateway Analysis Criteria

| Status | Condition |
|--------|-----------|
| ✅ **Healthy** | gateway.jsonl present with proper JSONL entries and metrics |
| ✅ **Healthy** | rpc-messages.jsonl present (canonical fallback) with valid JSON-RPC entries |
| ⚠️ **Warning** | gateway.jsonl or rpc-messages.jsonl present but missing key fields or has parse errors |
| 🔴 **Critical** | Neither gateway.jsonl nor rpc-messages.jsonl found in MCP-enabled run |
| ℹ️ **N/A** | No MCP servers configured for this workflow |

**Important**: When reporting MCP telemetry coverage, treat a run as having MCP telemetry if **either** `gateway.jsonl` **or** `rpc-messages.jsonl` is present. Only flag as Critical when both files are absent.

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
# A run has MCP telemetry if gateway.jsonl OR rpc-messages.jsonl is present
gateway_logs_present = count_runs_with_gateway_jsonl_or_rpc_messages()
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
- [List any runs missing critical logs - access.log for firewall runs, gateway.jsonl AND rpc-messages.jsonl both absent for MCP runs]

⚠️ **Warnings:**
- [List runs with incomplete or low-quality logs]

### Coverage Summary

| Component | Runs Analyzed | Logs Present | Coverage | Status |
|-----------|--------------|--------------|----------|--------|
| AWF Firewall (access.log) | X (`firewall_enabled_workflows`) | Y (`runs_with_complete_logs`) | Z% (`observability_coverage_percentage`) | ✅/⚠️/🔴 |
| MCP Gateway (gateway.jsonl or rpc-messages.jsonl) | X (`mcp_enabled_workflows`) | Y (`runs_with_complete_logs`) | Z% (`observability_coverage_percentage`) | ✅/⚠️/🔴 |

[Always visible. Summary table showing high-level coverage metrics.]

<details>
<summary>📋 Detailed Run Analysis</summary>

#### Firewall-Enabled Runs

| Workflow | Run ID | access.log | Entries | Allowed | Blocked | Status |
|----------|--------|------------|---------|---------|---------|--------|
| ... | ... | ✅/❌ | N | N | N | ✅/⚠️/🔴 |

#### Missing Firewall Logs (access.log)

| Workflow | Run ID | Date | Link |
|----------|--------|------|------|
| workflow-name | 12345 | 2024-01-15 | [§12345](url) |

#### MCP-Enabled Runs

| Workflow | Run ID | Telemetry Source | Entries | Servers | Tool Calls | Errors | Status |
|----------|--------|-----------------|---------|---------|------------|--------|--------|
| ... | ... | gateway.jsonl / rpc-messages.jsonl / ❌ None | N | N | N | N | ✅/⚠️/🔴 |

#### Missing MCP Telemetry (no gateway.jsonl or rpc-messages.jsonl)

| Workflow | Run ID | Date | Link |
|----------|--------|------|------|
| workflow-name | 12345 | 2024-01-15 | [§12345](url) |

</details>

<details>
<summary>🔍 Telemetry Quality Analysis</summary>

#### Firewall Log Quality

- Total access.log entries analyzed: N
- Domains accessed: N unique
- Blocked requests: N (X%)
- Most accessed domains: domain1, domain2, domain3

#### Gateway Log Quality

- Telemetry source: gateway.jsonl (preferred) or rpc-messages.jsonl (canonical fallback)
- Total entries analyzed: N
- MCP servers used: server1, server2
- Total tool calls: N
- Error rate: X%
- Average response time: Xms (N/A when derived from rpc-messages.jsonl without duration pairing)

#### Healthy Runs Summary

[Summary of runs with complete observability coverage]

</details>

### Recommended Actions

1. [Specific recommendation for improving observability coverage]
2. [Recommendation for workflows with missing logs]
3. [Recommendation for improving log quality]

[Always visible. Actionable recommendations based on the analysis.]

<details>
<summary>📊 Historical Trends</summary>

[If historical data is available, show trends in observability coverage over time]

</details>

</details>

---
*Report generated automatically by the Daily Observability Report workflow*
*Analysis window: Last 7 days | Runs analyzed: N*
```

## Important Guidelines

## Token Budget Guidelines

This workflow uses Codex, so prompt discipline is the main budget control.

- **Start with one broad `logs` call** and avoid repository-wide workflow enumeration
- **Stay within the 20-run cap** and do not inspect extra runs once coverage status is clear
- **Use targeted follow-up only to fill a missing category**; do not fan out across every workflow
- **Summarize, don't exhaustively transcribe** — visible sections should cover only the most important findings, with detailed tables limited to the analyzed run set
- **Stop immediately after `create_discussion` or `noop`** — no extra tool calls or post-report analysis

### Data Quality

- Handle missing files gracefully - report their absence, don't fail
- Validate JSON/JSONL formats before processing
- Count both present and missing logs accurately

### Severity Classification

- **CRITICAL**: Missing logs that would prevent debugging (access.log for firewall runs; **both** gateway.jsonl and rpc-messages.jsonl absent for MCP runs)
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
- ✅ Check all MCP-enabled runs for gateway.jsonl **or** rpc-messages.jsonl presence
- ✅ Calculate coverage percentages and identify gaps
- ✅ Flag any runs missing **all** MCP telemetry (neither gateway.jsonl nor rpc-messages.jsonl) as CRITICAL
- ✅ Create a new discussion with comprehensive report (previous discussions automatically closed)
- ✅ Include actionable recommendations

Begin your analysis now. Download the logs, analyze observability coverage, and create the discussion report.