---
title: Debugging Workflows
description: How to run, debug, and investigate agentic workflow failures using the Copilot CLI, gh aw audit, and log analysis.
sidebar:
  order: 250
---

This guide shows you how to debug agentic workflow failures on **github.com** using the Copilot CLI, the `gh aw` debugging commands, and manual investigation techniques.

All agent entry points use the same [debugging bootstrap and inventory](https://github.com/github/gh-aw/blob/main/.github/aw/debug-bootstrap.md)
and [shared debugging strategy](https://github.com/github/gh-aw/blob/main/.github/aw/debug-agentic-workflow.md).
Start with existing evidence; diagnosis does not authorize dispatch. The shared
strategy owns local execution, MCP startup review, compilation, and live-test
authorization. For enterprise host configuration, retain the
[GHE-specific guide](/gh-aw/troubleshooting/debug-ghe/).

> [!TIP]
> Share the bootstrap and failing run URL with your existing coding agent.
> Copilot CLI is one option for a new assistant session, not a prerequisite.

## Debugging with the Copilot CLI

Use this section only when choosing a new local Copilot assistant session.
Otherwise continue with your existing agent and available evidence tools.

Launch `copilot` and invoke **agentic-workflows** to load the debugging
instructions. Skills do not grant tool access; CLI/MCP capabilities depend on
the client configuration and authorization. Then paste the failing run URL:

```text
Debug this workflow run: https://github.com/OWNER/REPO/actions/runs/RUN_ID
```

With authorized access, Copilot can analyze existing logs and suggest a cause
or fix. Cached/redacted evidence also works. Publication requires the user's
scope and repository rules; a debugging request does not automatically open a PR.

### Alternative entry points

- **Copilot Chat on GitHub.com**: in repositories configured for [agentic authoring](/gh-aw/setup/working-with-workflows/#configuring-your-repository-for-agentic-authoring), use `agentic-workflows debug <run-url>`.
- **Any coding agent**: paste this prompt to use available tools or cached evidence without mandatory installation:

  ```text
  Debug this workflow run using https://raw.githubusercontent.com/github/gh-aw/main/debug.md

  The failed workflow run is at https://github.com/OWNER/REPO/actions/runs/RUN_ID
  ```

## Debugging with CLI Commands

Use a matching target checkout; otherwise pass `--repo OWNER/REPO` for this
github.com guide. Never substitute an unrelated checkout for the supplied target.

### Auditing a Specific Run

`gh aw audit` breaks down a single run — failure analysis and root cause, behavior fingerprint (network/tool/cost profile), tool usage, MCP server status, firewall analysis, token/cost metrics, and safe-outputs. Accepts a run ID, run URL, job URL (extracts first failing step), or step URL.

```bash
gh aw audit https://github.com/OWNER/REPO/actions/runs/12345678 --no-baseline --timeout 2m
gh aw audit https://github.com/OWNER/REPO/actions/runs/123/job/456#step:7:1
gh aw audit 12345678 --repo OWNER/REPO --parse         # parse to markdown
gh aw audit 12345678 12345679 --repo OWNER/REPO        # compare two runs
gh aw audit 12345678 12345679 --repo OWNER/REPO --group --json  # recurrence findings
```

For security and performance trends across multiple runs, use `gh aw logs --format`:

```bash
gh aw logs my-workflow --format markdown --count 10
gh aw logs my-workflow --format markdown --last 5 --json
```

See [Audit Commands](/gh-aw/reference/audit/) for complete flag documentation.

### Analyzing Workflow Logs

`gh aw logs` downloads and analyzes logs across multiple runs (tool usage, network patterns, errors). Results are cached for 10–100× speedup on later runs.

```bash
gh aw logs my-workflow
gh aw logs my-workflow -c 10 --start-date -1w   # filter by count and date
gh aw logs my-workflow --firewall               # include firewall analysis
gh aw logs my-workflow --safe-output            # include safe-output details
gh aw logs my-workflow --json                   # JSON for scripting
```

Logs reports group failed, timed-out, cancelled, and startup-failure runs into **failure investigation clusters** using failed jobs/steps, MCP failures, missing tools/data, and blocked domains from the downloaded metadata. Clusters describe shared symptoms, not proven root causes, and can overlap, so do not sum their counts into a failure rate. Intentional failures and different failure kinds stay separate.

Start with `[failure_investigation]`: unique failure/evidence counts and a deduplicated plan of up to five runs. Selection prioritizes specific failure boundaries, then maximizes new cluster coverage and affected-run reach, preferring richer/newer diagnostics on ties. Generic failure-handler `aftermath` ranks last, and intentional failures are excluded. Remaining cluster counts show the plan is not exhaustive.

The default `[failure_clusters]` output is a compact overview with representative IDs. Use `-v` for text membership, or project `failure_investigation` from `--json`. JSON and summary files keep full `failure_clusters`, per-run `failure_cluster_ids`, typed `signal.job`/`signal.step`, repository/workflow/engine/model facets, and first/last-seen timestamps. Up to three representatives per cluster prefer distinct contexts and include cached `audit_path`/`logs_path`; read cached evidence before another audit. Stable cluster IDs identify the same symptom across queries; membership reflects only the returned runs.

No extra downloads or model calls are needed. Without detailed evidence (including older cache records without signals), a `metadata_only` cluster groups runs in the same workflow and repository without asserting a shared cause.

Each cluster's `statistics` exposes its `failed_population`, observed `share`, and Wilson 95% bounds (`wilson_lower_95`/`wilson_upper_95`). The denominator includes unique returned failures, including metadata-only runs; intentional failures form a separate population. These are symptom shares, not workflow failure rates. Correlated retries, incomplete collection, and missing diagnostics limit interpretation, so do not use them as causal confidence, a significance test, or a forecast.

### Checking Workflow Health

`gh aw health` reports workflow status across all workflows in a repository.
It walks backward independently for each active AW until it has five completed, first-attempt runs for the current revision, encounters a revision boundary, or exhausts the maximum horizon (`--days 7`, `30`, or `90`; default `7`). Rates, trends, durations, and token averages describe the recent current-revision sample, not the entire period. Sparse workflows and workflows with no recent runs remain visible as insufficient evidence.

Revision identity uses `frontmatter_hash`, `body_hash`, and compiler version from `gh-aw-metadata` at each run's immutable commit, not repository SHA changes alone. A branch-head metadata check detects pushed revisions with no runs yet. JSON `workflows[].revisions` (or `revisions` for one workflow) separates branch/event cohorts and includes current/previous hashes, counts, and run IDs. `recovery_observed` requires five successful first-attempt runs after a changed revision whose sampled predecessor failed and finished before those runs began. This is observed recovery, not proof that a particular commit fixed the cause. Missing hashes and triggers whose head SHA does not identify the executing workflow, including `workflow_run` and `pull_request`, remain `revision_unknown`.

`failure_clusters` and `failure_investigation` describe current-revision failures using the same implementation as `logs`; historical failures remain in revision comparisons, not the current audit plan. Health reuses fresh summaries from `.github/aw/logs` (override with `--logs-dir PATH`) without downloading artifacts. Missing or stale summaries produce `metadata_only` clusters. Cluster shares are not workflow failure rates.

Health checks the target host's core API quota, reuses immutable revision lookups, and stops rather than sleeping or retrying on primary or secondary rate limits. `collection` records quota, request counts, and partial results. Collection is bounded to 1,000 data requests and 20 pages per workflow; quota, request, or pagination limits are explicit. Incomplete evidence never counts as healthy. Use `logs` for a wider historical population and `audit` for targeted comparisons.

### Inspecting MCP Configuration

If you suspect MCP server issues, first review declarations, startup commands,
destinations, and credential references under the shared strategy. Commands that
start or connect to a server require its MCP startup gate; use cached schemas
when isolated test bindings are unavailable. Then inspect the configuration:

```bash
gh aw mcp list                              # list workflows with MCP servers
gh aw mcp inspect my-workflow               # inspect a workflow
gh aw mcp inspect my-workflow --inspector   # web-based inspector
```

## Common Errors

### "Authentication failed"

Compare the failed operation's host, declared credential source and existing
authentication logs before choosing a fix. `gh auth status` reports CLI
authentication metadata, not proof that the workflow's Copilot token or
entitlement is valid. Never retrieve token values or swap credentials after
a denial. See [Authentication Reference](/gh-aw/reference/auth/).

### "Tool not found" or Missing Tool Calls

Compare the requested name with cached registration/startup evidence and the
failed revision's `tools:` declarations. A naming mismatch does not prove missing
permissions or configuration. Follow the [finding-code playbook](https://github.com/github/gh-aw/blob/main/.github/aw/debug-playbook.md)
and MCP startup gate before any command that starts or connects servers.

### Shell Commands Missing Inside the Agent Sandbox

`command not found` for `jq` or `safeoutputs` is different from an unavailable MCP tool. The setup message `Successfully copied ... safe-outputs files` confirms that server scripts were copied to the runner. It does not confirm that the CLI wrapper was mounted or that its interpreter and directory are available on the agent's `PATH`.

For intermittent failures, compare one successful and one failing run from the same workflow: run URLs, gh-aw and engine versions, the `sandbox:` block, and the lock file's image, mounts, and `PATH`. Different self-hosted runner images, custom sandbox images, missing mounts, or shell startup files that reset `PATH` are possible causes, not proof of a provisioning race.

Check `command -v safeoutputs` in the **agent's shell** when permitted, not just in a runner setup step. If the CLI is unavailable, use direct safe-output MCP tools only when the engine exposes them. CLI payloads take one single-quoted JSON object (encode newlines as `\n`, double quotes as `\"`, backslashes as `\\`, apostrophes as `\u0027`), or stdin via `safeoutputs <tool_name> .`:

```bash
safeoutputs add_comment '{"item_number":42,"body":"### Result\n\nIt\u0027s ready.\nQuoted text: \"hello\".\nLiteral path: C:\\temp\\file.txt"}'
```

Quoting cannot guarantee acceptance by the engine's shell guard. Do not repeat a denied command or bypass it with an unapproved workaround. Report unavailable functionality with [`report_missing_tool`](/gh-aw/reference/safe-outputs/) when reachable (`report_incomplete` is for failed write attempts); otherwise state the missing command in the final response without claiming a write was emitted.

Collect diagnostics with the repository's [debug.md guidance](https://github.com/github/gh-aw/blob/main/debug.md). Redact secrets from logs and configuration; do not dump the full environment or make a real safe-output call merely to test availability.

### Network / Firewall Blocks

```text
DENIED CONNECT registry.npmjs.org:443
```

The agent reached a domain outside the firewall allow-list. First verify that
the destination is intended and authorized; then add only the necessary domain
or ecosystem entry, keeping the firewall enabled:

```aw
network:
  allowed:
    - defaults
    - node        # npm, yarn, pnpm registries
    - python      # PyPI, conda registries
    - registry.npmjs.org
```

See [Network Configuration](/gh-aw/guides/network-configuration/) for common domain configurations.

### Safe-Outputs Not Creating Issues / Comments

The safe-outputs job failed, the agent didn't produce the expected output, or permissions are missing. Inspect the safe-outputs section in `gh aw audit <run-id>` and review the [Safe Outputs Reference](/gh-aw/reference/safe-outputs/).

### Silent Long-running Commands After a Result

If the harness log says `post-result watchdog terminating idle process`, the agent already emitted a terminal safe output and then the child process went quiet. The watchdog is dormant until a terminal safe output such as `noop`, a comment, label, push, or pull request creation is observed; diagnostic outputs such as `report_missing_tool`, `report_missing_data`, and `report_incomplete` do not arm it. After arming, stdout or stderr activity resets the clock, so quiet builds, tests, or monorepo scans can be terminated even while they are still doing useful CPU or I/O work.

Increase `engine.harness.watchdog-timeout` for literal frontmatter values, or set `GH_AW_HARNESS_WATCHDOG_TIMEOUT_MS` when you need a millisecond environment-variable override. See [Harness Settings and Runtime Tuning Variables](/gh-aw/reference/environment-variables/#harness-settings-and-runtime-tuning-variables).

### Compilation Errors

Inspect the schema error or unsupported field, then follow the
[compilation guide](https://github.com/github/gh-aw/blob/main/.github/aw/debug-compilation.md)
for diagnostic emission and preservation. `validate` does not emit locks;
`compile --validate` can emit them. Apply codemods only for confirmed issues
within the authorized edit scope. See [Error Reference](/gh-aw/troubleshooting/errors/).

```bash
gh aw compile my-workflow --dry-run --verbose
gh aw validate my-workflow  # Additional source checks; no emitted lock or scanners
```

## Advanced Debugging

### Enable Debug Logging

`DEBUG` enables detailed internal logging for any `gh aw` command. Output goes to `stderr` — capture it with `2>&1 | tee debug.log`.

```bash
DEBUG=* gh aw compile my-workflow --dry-run     # all logs
DEBUG=cli:* gh aw audit 12345678 --repo OWNER/REPO  # CLI-specific
DEBUG=workflow:*,cli:* gh aw compile my-workflow --dry-run  # multiple packages
```

### Enable GitHub API Request Logging

`gh aw` uses go-gh's native REST/GraphQL clients for many GitHub API calls. Setting `GH_DEBUG=api` prints verbose request/response details for calls made through those go-gh clients, with no extra flags needed:

```bash
GH_DEBUG=api gh aw compile my-workflow --dry-run
```

Keep captured logs private and redact sensitive headers, payloads and URLs
before sharing them.

### Enable GitHub Actions Debug Logging

`ACTIONS_STEP_DEBUG=true` enables verbose step-level logging in the Actions UI.
Changing repository secrets or rerunning a workflow is not part of read-only
diagnosis; use the shared strategy's authorization and live gates before either.

### Inspecting Firewall Logs

Workflow run artifacts include `sandbox/firewall/logs/access.log`. Each line shows allowed (`TCP_TUNNEL`) or blocked (`DENIED`) traffic:

```text
TCP_TUNNEL/200 api.github.com:443
DENIED CONNECT blocked-domain.com:443
```

Or use the CLI: `gh aw logs my-workflow --firewall`, or `gh aw audit <run-id>` for combined firewall analysis.

### Inspecting Artifacts

Workflow runs produce several artifacts useful for debugging:

| Artifact | Location | Contents |
|----------|----------|----------|
| `prompt.txt` | `/tmp/gh-aw/aw-prompts/` | Full prompt sent to the AI agent |
| `agent_output.json` | `/tmp/gh-aw/safeoutputs/` | Structured safe-output data |
| `agent-stdio.log` | `/tmp/gh-aw/` | Raw agent stdin/stdout log |
| `firewall-logs/` | `/tmp/gh-aw/firewall-logs/` | Network access logs |

Download from the Actions run page or `gh run download <run-id> --repo OWNER/REPO`.

### Recompiling for a Quick Fix

After editing the `.md` source, use the shared strategy's review and development
compilation gates:

```bash
gh aw compile my-workflow --dry-run
```

This emits a diagnostic lock, not a live run or a normal production replacement.
Preserve evidence and user changes using the strategy's snapshot/restore rules.
Publication and live testing follow its feature-branch and exact-revision gates;
do not infer permission to push or rerun from a successful compile.
