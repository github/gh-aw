---
description: Diagnose gh-aw failures using logs and audits; follow the shared strategy for patches and permitted active debug loops.
disable-model-invocation: true
---


# Workflow Diagnosis and Debugging Evidence

Use this reference to diagnose workflows: download/analyze existing logs, audit
runs, and trace failures. These reads are not an active debug loop.

For a fresh session, start with [the debugging bootstrap](debug-bootstrap.md) for CLI setup and
run-URL scoping. Read the complete
[shared local-first strategy](debug-agentic-workflow.md) before using
the commands below. This page supplies evidence and CLI reference, not a
separate execution policy. Never dispatch for evidence.

The strategy owns mode selection, MCP startup review, credential triage,
compilation, and live gates. Follow its
[branch-testing path](debug-agentic-workflow.md#test-workflow-changes-from-a-branch)
and the security reference's
[single-reviewer](debug-security-review.md#single-reviewer),
[annotated overview](debug-security-review.md#annotated-markdown-change-overview),
[session authorization](debug-security-review.md#session-authorization),
and [dry-run dangerous-feature gate](debug-security-review.md#dry-run-dangerous-features)
when those stages apply. Do not substitute this reference's examples for those gates.

Report separate [security-review results](debug-security-review.md#user-visible-result)
and [dry-run results](debug-compilation.md#user-visible-dry-run-result),
and follow the [blocked-result requirements](debug-security-review.md#explain-blocked-or-unavailable-debugging)
when evidence or authorization is unavailable. For workflows using `tools.work-queue`,
also load the [queue protocol](work-queue.md).

## Table of Contents

- [Downloading Workflow Logs](#downloading-workflow-logs)
- [Auditing Specific Runs](#auditing-specific-runs)
- [How Agentic Workflows Work](#how-agentic-workflows-work)
- [Common Issues and Solutions](#common-issues-and-solutions)
- [Advanced Debugging Techniques](#advanced-debugging-techniques)
- [Reference Commands](#reference-commands)

## Downloading Workflow Logs

`gh aw logs` collects existing GitHub Actions artifacts and logs, not new runs.
Keep supplied run/job/step URLs intact. For every bare run ID in the examples
below, add `--repo HOST/OWNER/REPO`; never infer the target from an unrelated checkout.
Workflow-name and fleet reads also use that explicit repository.

### Basic Usage

```bash
gh aw logs --repo HOST/OWNER/REPO -o EVIDENCE_DIR
```

Default: last 10 runs. Insert a workflow name to narrow scope.
Set `EVIDENCE_DIR` to a private session directory, not a tracked source path.

### Filter Options

Add filters to the scoped command above:

| Filter | Flags |
| --- | --- |
| Date | `--start-date DATE --end-date DATE`; relative starts include `-1d`, `-1w`, `-1mo` |
| Engine | `--engine copilot`, `claude`, or `codex` |
| Count | `-c 5` |
| Branch/tag | `--ref TARGET_REF` |
| Run IDs | `--after-run-id 1000 --before-run-id 2000` |
| Firewall mode | `--firewall` or `--no-firewall` |

### Output Options

| Output | Flag |
| --- | --- |
| JSON summary | `--json` |
| Parsed Markdown reports | `--parse` |
| Mermaid tool sequence graph | `--tool-graph` |
| Five-minute download limit | `--timeout 5` (minutes) |

### Downloaded Artifacts

Inspect actual downloaded names and completeness; artifacts may be absent:

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
gh aw logs --repo HOST/OWNER/REPO --start-date -1w -o EVIDENCE_DIR
jq '.runs[] | select(.conclusion == "failure")' EVIDENCE_DIR/summary.json
```

## Auditing Specific Runs

`gh aw audit` downloads a run's evidence and reports findings, not proven causes.

### Basic Usage

```bash
gh aw audit 1234567890 --repo HOST/OWNER/REPO
gh aw audit https://github.com/owner/repo/actions/runs/1234567890/job/9876543210#step:7:1
```

Keep supplied URLs intact. A job URL without a step selects its first failing step.

### Output Options

Add `--json` for structured output, `--parse` for Markdown, `-v` for verbosity,
or `-o EVIDENCE_DIR` for a private, reusable evidence directory.

### Check Whether an Error Recurs

```bash
gh aw audit 1234567890 1234567891 1234567892 --repo HOST/OWNER/REPO --group --json
```

Group per-run finding codes/counts; inspect cached reports for exact signatures
rather than plain metrics/firewall/tool diffs. Match first failing boundaries
and normalized error/tool/status signatures across comparable
workflow/revision/trigger/input/configuration contexts, counting each run once.
Missing/skipped evidence remains unknown; repeated HTTP 403 alone does not
prove a common cause.

### Search and Cluster Session Evidence

```bash
gh aw audit 1234567890 --repo HOST/OWNER/REPO --search "api.example.com ERR_AUTH_401" --search-limit 5 --json
```

Read `evidence_search` references, neighboring context and occurrence counts in
the actual redacted unified session. Single-run CLI search does not replace
audit findings, `--group` recurrence checks or fleet failure clustering; zero
hits do not clear findings or prove safety. Use
[retrieval guidance](analyzing-runs.md#retrieve-and-cluster-session-evidence)
for grouping/ranking semantics, omitted fields, bounds and provenance.

### Audit Report Contents

Inspect errors/warnings, MCP usage/missing tools, duration/tokens/cost, and
attempted safe-output operations. Missing fields do not prove success.

### Example: Investigate a Failed Run

```bash
gh aw audit 1234567890 --repo HOST/OWNER/REPO --json > audit.json
jq '{status: .overview.status, conclusion: .overview.conclusion, errors, missing_tools, tool_usage}' audit.json
```

## How Agentic Workflows Work

Locate the first failing boundary, not just the final job conclusion.

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

Trigger -> activation (permissions, mcp-scripts, sanitized context) ->
agent (MCP/tools, prompt, calls, output) -> safe outputs (validated GitHub writes) ->
completion (summary, artifacts). A failure in one stage can cascade.

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
# Test changes by compiling a diagnostic GitHub Actions workflow
gh aw compile <workflow-name> --dry-run

# Diagnostic artifact: .github/workflows/<name>.lock.yml
```

Recommend dry-run compilation first when testing workflow changes. It does not
execute the workflow; preserve the diagnostic lock separately rather than
publishing it as a production replacement.

## Common Issues and Solutions

### Missing Tool Errors

**Symptoms**: "Tool 'github:read_issue' not found" or unavailable GitHub APIs.

**Solution**: Compare the requested name with cached registration/startup evidence
before changing tools or permissions. Only if source inspection confirms missing
GitHub MCP configuration, add it and dry-run compile the source change:

```yaml
tools:
  github:
    mode: remote
    toolsets: [default]
```

### Permission Errors

**Symptoms**: HTTP 403 or "Resource not accessible".

**Solution**: First distinguish SAML/token-source denial from missing permissions
using the shared credential triage. Change only confirmed missing read permissions;
configure intended writes through safe outputs. Keep debugging outputs staged;
inspect individual job/token permissions rather than adding write permissions
to the agent. The [workflow example](#workflow-structure) uses read-only agent
permissions and staged safe outputs.

### Safe-Input Errors

**Symptoms**: "missing tool configuration for mcpscripts-gh" or missing environment variables.

**Solution when confirmed**: Compare the failed call with declared mcp-scripts
and environment-variable names before changing source. Do not retrieve values:

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

**Symptoms**: Failed resource creation or "Safe output not enabled".

**Solution when confirmed**: Compare the failed message with declared output names
and validation evidence. Add only the intended missing output, staged for debugging:

```yaml
safe-outputs:
  staged: true  # Preview safe outputs while debugging
  create-issue:
    labels: [ai-generated]
```

### Cascading Safe-Output Message Failures (Process Safe Outputs step)

Classify each failed message by tool/action, HTTP status and time.
An `update_pull_request` workflows-permission 403 can be expected/non-fatal;
an `add_comment` `Bad credentials` 401 is a separate authentication failure.
Compare reported message counts with individual failures; do not merge causes.

```bash
gh aw audit <run-id> --repo HOST/OWNER/REPO
gh aw audit <run-id> --repo HOST/OWNER/REPO --artifacts usage,github-api,mcp,agent
gh run view <run-id> --repo HOST/OWNER/REPO --job=<job-id> --log
```

Use `--artifacts all` only when targeted evidence is insufficient.
Inspect effective scopes for permission errors and token source/freshness metadata
for concurrent authentication failures; never retrieve credentials.

### Network Access Errors

**Symptoms**: Firewall denials or URLs shown as "(redacted)".

**Solution when confirmed**: Match an actual denial to an intended, authorized
destination before changing the smallest allow-list entry. A redacted URL alone
does not prove blocking. Keep the firewall enabled:

```yaml
network:
  allowed:
    - defaults
    - python    # For PyPI
    - node      # For npm
    - "api.example.com"  # Custom domains
```

### Timeout Errors

Timeouts, loops and hangs are symptoms. Compare failing runs' `tool_usage`,
`metrics.turns`, `agentic_assessments` (for example `resource_heavy_for_domain`)
and actual `agent-session.jsonl` calls. Check:

- Trigger `paths`/`paths-ignore` versus the workflow/skill's applicable domain.
- Missing/stale prefetched context causing repeated `gh`/`grep` reads; batch with DataOps.
- Unbounded exploration or code tracing.

Measure, diagnose and trim using the `optimize-agentic-workflow` skill.
Increase `timeout-minutes` only as a justified, bounded safety margin alongside
an efficiency fix, never as the first or sole remedy. Live validation retains
the shared authorization gates.

## Advanced Debugging Techniques

### Polling In-Progress Runs

Classify command exit separately from workflow outcome. A nonzero audit exit may
mean artifacts are not ready: confirm the same run with
`gh run view <run-id> --repo HOST/OWNER/REPO --json status,headSha,conclusion`, then poll within the
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

### Other Useful Commands

```bash
gh aw status --repo HOST/OWNER/REPO                                 # Fleet status
gh run download <run-id> --repo HOST/OWNER/REPO -n agent-stdio.log   # One artifact
gh run view --repo HOST/OWNER/REPO --job <job-id> --log              # One job's logs
gh aw logs --repo HOST/OWNER/REPO --parse --firewall                 # Parsed firewall logs
```

### Diagnostic and Development Compilation

Use the [compilation guide](debug-compilation.md) for required checks, optional
Docker scanners, severity rules, diagnostic preservation and result reporting.
This heading preserves existing links; it defines no separate compilation gate.

## Reference Commands

Full flag/option tables: [logs](cli-commands-reference.md#gh-aw-logs) (scoped to the
identified target repository), [audit](cli-commands-reference.md#gh-aw-audit) (preserve
supplied URLs; qualify bare IDs with `--repo HOST/OWNER/REPO`),
[mcp inspect](cli-commands-reference.md#gh-aw-mcp-inspect) (after the shared startup
preflight — a listing is not startup authorization), and
[status](cli-commands-reference.md#gh-aw-status) (compilation belongs to the
[compilation guide](debug-compilation.md), not a status check).

### Active Debugging Commands (Permitted Contexts Only)

Follow the [shared live debug loop](debug-agentic-workflow.md#live-debug-loop)
for dispatch, authorization, monitoring and stopping rules. No example here
grants live permission.

## Additional Resources

- [Workflow Health Monitoring Runbook](runbooks/workflow-health.md) - Step-by-step investigation procedures
- [Common Issues Reference](../../docs/src/content/docs/troubleshooting/common-issues.md) - Frequently encountered issues
- [Error Reference](../../docs/src/content/docs/troubleshooting/errors.md) - Error codes and solutions
- [GitHub MCP Server Documentation](../skills/github-mcp-server/SKILL.md) - Tool configuration reference
