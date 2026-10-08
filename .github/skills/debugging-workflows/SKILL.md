---
name: debugging-workflows
description: Diagnose gh-aw failures using logs and audits; follow the shared strategy for patches and permitted active debug loops.
---


# Workflow Diagnosis and Debugging Evidence

Use this reference to diagnose workflows: download/analyze existing logs, audit
runs, and trace failures. These reads are not an active debug loop.

Follow the [shared local-first strategy](../../aw/debug-agentic-workflow.md) for all
reproduction, fixes, uploads, and live tests. This page is an evidence and CLI
reference, not a separate execution policy. Respect explicit no-dispatch contexts.
Apply its live-outcome table, credential triage and untrusted-evidence rules.
Without accessible existing logs, use source/fixtures; never dispatch for evidence.

Workflow registration/activation and secret presence, validity, or expiry are
runtime readiness checks, not pre-dispatch prerequisites. Do not require workflow
or secret inventories or organization-admin metadata access. An otherwise
authorized dispatch and the workflow's startup/authentication checks determine
readiness; report their failures without automatic enabling, provisioning, or
dispatch retries. Missing readiness metadata alone is not a safety finding.
Declared credential flows, authorized destinations, permissions, source/lock
review, and human live-validation gates remain in scope.

The user may grant explicit [session authorization](../../aw/debug-security-review.md#session-authorization)
for bounded in-scope debug iterations without repeated approval. Re-review every
changed source/lock revision; any compiler security warning invalidates the grant
and requires fresh authorization after resolution.

Always provide separate short user-visible result sentences for each security
review and dry-run attempt, including failed, blocked, or unavailable outcomes.
Name the artifact/scope, checks actually performed, and material findings or
coverage gaps; do not leave the result only in logs or artifacts or imply live
execution. Follow the shared [review result](../../aw/debug-security-review.md#user-visible-result)
and [dry-run result](../../aw/debug-agentic-workflow.md#user-visible-dry-run-result) rules.
When debugging is refused, follow the
[blocked-result requirements](../../aw/debug-security-review.md#explain-blocked-or-unavailable-debugging):
explain each concrete cause, link the reviewed source and lines, distinguish
confirmed defects from incomplete evidence or authorization, and name the
necessary resolution and its owner. Do not report disagreement alone as a code
vulnerability.
In dry-run mode, reject enabled `dangerously-*` entries in workflow Markdown
configuration, including imported configuration,
under the [strict security gate](../../aw/debug-security-review.md#dry-run-dangerous-features).
This filter does not reject implementation-internal flags used by trusted
built-in engines. Review their runtime isolation separately. Link the authored
field responsible for a refusal; do not claim compiler rejection when none occurred.

## Table of Contents

- [Quick Start](#quick-start)
- [Downloading Workflow Logs](#downloading-workflow-logs)
- [Auditing Specific Runs](#auditing-specific-runs)
- [How Agentic Workflows Work](#how-agentic-workflows-work)
- [Common Issues and Solutions](#common-issues-and-solutions)
- [Advanced Debugging Techniques](#advanced-debugging-techniques)
- [Reference Commands](#reference-commands)

## Quick Start

### Download Logs from Recent Runs

```bash
# Download logs from the last 24 hours
gh aw logs --start-date -1d -o .github/aw/logs/recent

# Download logs for a specific workflow
gh aw logs weekly-research --start-date -1d

# Download logs with JSON output for programmatic analysis
gh aw logs --json
```

### Audit a Specific Run

```bash
# Audit by run ID
gh aw audit 1234567890

# Audit from a GitHub Actions URL
gh aw audit https://github.com/owner/repo/actions/runs/1234567890

# Audit with JSON output
gh aw audit 1234567890 --json
```

## Downloading Workflow Logs

The `gh aw logs` command downloads workflow run artifacts and logs from GitHub Actions for analysis.

### Basic Usage

```bash
# Download logs for all workflows (last 10 runs)
gh aw logs

# Download logs for a specific workflow
gh aw logs <workflow-name>

# Download with custom output directory
gh aw logs -o .github/aw/logs/custom
```

### Filter Options

```bash
# Filter by date range
gh aw logs --start-date 2024-01-01 --end-date 2024-01-31
gh aw logs --start-date -1w                    # Last week
gh aw logs --start-date -1mo                   # Last month

# Filter by AI engine
gh aw logs --engine copilot
gh aw logs --engine claude
gh aw logs --engine codex

# Filter by count
gh aw logs -c 5                                # Last 5 runs

# Filter by branch/tag
gh aw logs --ref main
gh aw logs --ref feature-xyz

# Filter by run ID range
gh aw logs --after-run-id 1000 --before-run-id 2000

# Filter firewall-enabled runs
gh aw logs --firewall                          # Only firewall-enabled
gh aw logs --no-firewall                       # Only non-firewall
```

### Output Options

```bash
# Generate JSON summary
gh aw logs --json

# Parse agent logs and generate Markdown reports
gh aw logs --parse

# Generate Mermaid tool sequence graph
gh aw logs --tool-graph

# Set download timeout
gh aw logs --timeout 300                       # 5 minute timeout
```

### Downloaded Artifacts

When you run `gh aw logs`, the following artifacts are downloaded for each run:

| File | Description |
|------|-------------|
| `aw_info.json` | Engine configuration and workflow metadata |
| `safe_output.jsonl` | Agent's final output content (when non-empty) |
| `agent_output/` | Agent logs directory |
| `agent-stdio.log` | Agent standard output/error logs |
| `aw.patch` | Git patch of changes made during execution |
| `workflow-logs/` | GitHub Actions job logs (organized by job) |
| `summary.json` | Complete metrics and run data for all runs |

### Example: Analyze Recent Failures

```bash
# Download failed runs from last week
gh aw logs --start-date -1w -o .github/aw/logs/debug

# Check the summary for patterns
cat .github/aw/logs/debug/summary.json | jq '.runs[] | select(.conclusion == "failure")'
```

## Auditing Specific Runs

The `gh aw audit` command investigates a single workflow run in detail, downloading artifacts, detecting errors, and generating a report.

### Basic Usage

```bash
# Audit by numeric run ID
gh aw audit 1234567890

# Audit from GitHub Actions URL
gh aw audit https://github.com/owner/repo/actions/runs/1234567890

# Audit from job URL (extracts first failing step)
gh aw audit https://github.com/owner/repo/actions/runs/1234567890/job/9876543210

# Audit from job URL with specific step
gh aw audit https://github.com/owner/repo/actions/runs/1234567890/job/9876543210#step:7:1
```

### Output Options

```bash
# JSON output for programmatic analysis
gh aw audit 1234567890 --json

# Custom output directory
gh aw audit 1234567890 -o ./audit-reports

# Parse agent logs and firewall logs
gh aw audit 1234567890 --parse

# Verbose output
gh aw audit 1234567890 -v
```

### Check Whether an Error Recurs

```bash
# Compare existing runs, without dispatching new ones
gh aw audit 1234567890 1234567891 1234567892 --group --json
```

Use grouped per-run finding codes/counts, then cached individual reports/logs
for exact signatures. Plain multi-run diffs focus on metrics/firewall/tools;
absent findings or skipped runs do not prove the error disappeared.
Match the first failing boundary and normalized error/tool/status signature
across comparable workflows, revisions, triggers/inputs and configurations. Count each
matching run once, report matching/inspectable runs and IDs, and keep missing
evidence unknown. Repeated HTTP 403 alone does not establish one root cause.

### Audit Report Contents

The audit command provides:

- **Error Detection**: Errors and warnings from workflow logs
- **MCP Tool Usage**: Statistics on tool calls by the AI agent
- **Missing Tools**: Tools the agent tried to use but weren't available
- **Execution Metrics**: Duration, token usage, and cost information
- **Safe Output Analysis**: What GitHub operations were attempted

### Example: Investigate a Failed Run

```bash
# Get detailed audit report
gh aw audit 1234567890 --json > audit.json

# Extract key information
cat audit.json | jq '{
  status: .overview.status,
  conclusion: .overview.conclusion,
  errors: .errors,
  missing_tools: .missing_tools,
  tool_usage: .tool_usage
}'
```

## How Agentic Workflows Work

Understanding the workflow architecture helps in debugging.

### Workflow Structure

Agentic workflows use a **markdown + YAML frontmatter** format:

```markdown
---
on:
  issues:
    types: [opened]
permissions:
  contents: read
timeout-minutes: 10
engine: copilot
tools:
  github:
    mode: remote
    toolsets: [default]
safe-outputs:
  staged: true
  create-issue:
    labels: [ai-generated]
---

# Workflow Title

Natural language instructions for the AI agent.

Use GitHub context like ${{ github.event.issue.number }}.
```

### Execution Flow

```
1. Trigger Event (issue opened, PR created, schedule, etc.)
     ↓
2. Activation Job
   - Validates permissions
   - Processes mcp-scripts
   - Sanitizes context
     ↓
3. AI Agent Job
   - Loads MCP servers and tools
   - Executes AI agent with prompt
   - Agent makes tool calls
   - Agent produces output
     ↓
4. Safe Outputs Job
   - Processes agent output
   - Creates GitHub resources (issues, PRs, etc.)
   - Applies labels, comments
     ↓
5. Completion
   - Workflow summary generated
   - Artifacts uploaded
```

### Key Components

| Component | Purpose | Configuration |
|-----------|---------|---------------|
| **Engine** | AI model to use | `engine: copilot`, `claude`, `codex` |
| **Tools** | APIs available to agent | `tools:` section with MCP servers |
| **MCP Scripts** | Context passed to agent | `mcp-scripts:` with GitHub expressions |
| **Safe-Outputs** | Resources agent can create | `safe-outputs:` with allowed operations |
| **Permissions** | GitHub token permissions | `permissions:` block |
| **Network** | Allowed network access | `network:` with domain/ecosystem lists |

### Compilation Process

```bash
# Compile workflow to GitHub Actions YAML
gh aw compile <workflow-name>

# Result: .github/workflows/<name>.md → .github/workflows/<name>.lock.yml
```

The `.lock.yml` file is the actual GitHub Actions workflow that runs.

## Common Issues and Solutions

### Missing Tool Errors

**Symptoms**:
- Error: "Tool 'github:read_issue' not found"
- Agent cannot access GitHub APIs

**Solution**: Add GitHub MCP server configuration:

```yaml
tools:
  github:
    mode: remote
    toolsets: [default]
```

### Permission Errors

**Symptoms**:
- HTTP 403 (Forbidden) errors
- "Resource not accessible" errors

**Solution**: First distinguish SAML/token-source denial from missing permissions
using the shared credential triage. Grant required read permissions to the agent and configure writes
through safe outputs. Keep debugging outputs staged; inspect individual job/token
permissions rather than adding write permissions to the agent:

```yaml
permissions:
  contents: read
safe-outputs:
  staged: true
  create-issue: {}
```

### Safe-Input Errors

**Symptoms**:
- "missing tool configuration for mcpscripts-gh"
- Environment variable not available

**Solution**: Configure mcp-scripts:

```yaml
mcp-scripts:
  issue:
    script: |
      return { title: process.env.ISSUE_TITLE, body: process.env.ISSUE_BODY };
    env:
      ISSUE_TITLE: ${{ github.event.issue.title }}
      ISSUE_BODY: ${{ github.event.issue.body }}
```

### Safe-Output Errors

**Symptoms**:
- Agent tries to create resources but fails
- "Safe output not enabled" errors

**Solution**: Enable safe-outputs:

```yaml
safe-outputs:
  staged: true  # Preview safe outputs while debugging
  create-issue:
    labels: [ai-generated]
```

### Cascading Safe-Output Message Failures (Process Safe Outputs step)

**Symptoms**:
- `Process Safe Outputs` reports multiple failed messages in one run
- One failed `update_pull_request` message includes a 403 workflows-permission warning
- Other failed messages (for example `add_comment`) include `Bad credentials`

**What this means**:
- Do not assume all safe-output failures share one root cause.
- A 403 workflows-permission error on `update_pull_request` can be expected/non-fatal in some workflows.
- A 401-style `Bad credentials` error on other messages is a separate authentication failure that needs its own fix.

**Diagnostic steps**:

```bash
# Summarize failed safe-output messages and types
gh aw audit <run-id>

# Include additional artifacts when diagnosis needs more context
gh aw audit <run-id> --artifacts usage,github-api,mcp,agent

# Escalate to full artifact collection for hard-to-classify failures
gh aw audit <run-id> --artifacts all

# Inspect full failing job logs to classify each message failure
gh run view <run-id> --job=<job-id> --log
```

- Triage each failed message by its own HTTP status code and tool/action name.
- Check `permissions:` for missing scopes when 403 errors appear.
- Compare the "failed message count" against the individual failed message lines to confirm whether there are multiple independent failures.
- If several credential failures cluster together in time, investigate token freshness/expiry and token source for the run.

### Network Access Errors

**Symptoms**:
- Firewall denials
- URLs appearing as "(redacted)"

**Solution**: Configure network access:

```yaml
network:
  allowed:
    - defaults
    - python    # For PyPI
    - node      # For npm
    - "api.example.com"  # Custom domains
```

### Timeout Errors

**Symptoms**:
- Workflow exceeds time limit
- Agent loops or hangs

**Solution**: Increase timeout or optimize prompt:

```yaml
timeout-minutes: 30  # Increase from default
```

## Advanced Debugging Techniques

### Polling In-Progress Runs

Classify command exit separately from workflow outcome. A nonzero audit exit may
mean artifacts are not ready: confirm the same run with
`gh run view <run-id> --json status,headSha,conclusion`, then poll within the
approved interval/deadline. Audit/log permission denial blocks further live
iteration; report evidence unavailable, not workflow failure. Follow the shared
outcome table for dispatch timeouts and SHA mismatches; never redispatch for logs.

### Inspecting MCP Configuration

Preflight declarations, startup effects and isolated test bindings using the
shared strategy before commands that can start/connect servers.

```bash
# Inspect MCP servers for a workflow
gh aw mcp inspect <workflow-name>

# List all workflows with MCP servers
gh aw mcp list
```

### Checking Workflow Status

```bash
# Show status of all agentic workflows
gh aw status
```

### Downloading Specific Artifacts

```bash
# Download only the agent log artifact
GH_REPO=owner/repo gh run download <run-id> -n agent-stdio.log
```

### Inspecting Job Logs

```bash
# View specific job logs
gh run view <run-id>
gh run view --job <job-id> --log
```

### Analyzing Firewall Logs

```bash
# Parse firewall logs for network issues
gh aw logs --parse

# Check firewall-enabled runs
gh aw logs --firewall
```

### Diagnostic and Development Compilation

```bash
# Strict/staged development compilation; Docker-based checks are optional
gh aw compile <workflow> --dry-run

# Recommended when using a reviewed test environment
gh aw compile <workflow> --dry-run --environment gh-aw-debug

# Additional source validation; does not run scanners
gh aw validate <workflow>

# Run those scanners on the emitted dry-run lock file when possible
gh aw compile <workflow> --dry-run --zizmor --actionlint --poutine
```

Docker unavailability does not block the dry-run gate. Use native `shellcheck`
for required run-step linting without Docker, and report unavailable optional
checks as unverified.

When Docker is available, recommend both commands: `validate` checks source
without emitting files, while the scanner-enabled compile command runs zizmor,
actionlint and poutine. A successful `validate` does not establish scanner coverage.

## Reference Commands

### Log Analysis Commands

| Command | Description |
|---------|-------------|
| `gh aw logs` | Download logs for all workflows |
| `gh aw logs <workflow>` | Download logs for specific workflow |
| `gh aw logs --json` | Output as JSON |
| `gh aw logs --start-date -1d` | Filter by date |
| `gh aw logs --engine copilot` | Filter by engine |
| `gh aw logs --parse` | Generate Markdown reports |

### Audit Commands

| Command | Description |
|---------|-------------|
| `gh aw audit <run-id>` | Audit specific run |
| `gh aw audit <url>` | Audit from GitHub URL |
| `gh aw audit <run-id> --json` | Output as JSON |
| `gh aw audit <run-id> --parse` | Parse logs to Markdown |
| `gh aw audit <id1> <id2> ... --group --json` | Group existing-run findings for recurrence |

### MCP Commands

| Command | Description |
|---------|-------------|
| `gh aw mcp list` | List workflows with MCP servers |
| `gh aw mcp inspect <workflow>` | Inspect MCP configuration |

### Status Commands

| Command | Description |
|---------|-------------|
| `gh aw status` | Show all workflow status |
| `gh aw compile` | Compile all workflows |
| `gh aw compile <workflow>` | Compile specific workflow |
| `gh aw compile <workflow> --dry-run` | Enforce shared development-testing checks |

### Active Debugging Commands (Permitted Contexts Only)

| Command | Description |
|---------|-------------|
| `gh aw run <workflow> --ref <reviewed-ref>` | Only after shared human-validation gates; explicit no-dispatch rules take precedence |
| `gh run view <run-id> --json status,headSha,conclusion` | Same-run monitoring within approved bounds |

## Additional Resources

- [Workflow Health Monitoring Runbook](../../aw/runbooks/workflow-health.md) - Step-by-step investigation procedures
- [Common Issues Reference](../../../docs/src/content/docs/troubleshooting/common-issues.md) - Frequently encountered issues
- [Error Reference](../../../docs/src/content/docs/troubleshooting/errors.md) - Error codes and solutions
- [GitHub MCP Server Documentation](../github-mcp-server/SKILL.md) - Tool configuration reference
