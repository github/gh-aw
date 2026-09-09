---
emoji: "🔍"
description: Investigates [aw] failures from the last 6 hours, correlates with open agentic-workflows issues, closes consolidated failures as duplicates, and opens focused fix sub-issues when needed
on:
  schedule:
    - cron: "every 30m"
  cooldown: 6h
  workflow_dispatch:
max-daily-ai-credits: 10000
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
tracker-id: aw-failure-investigator
engine: claude
experiments:
  tone_variant:
    variants: [clinical, assertive, narrative]
    description: "Tests whether report tone (clinical/assertive/narrative) affects output efficiency and engineer engagement on failure investigation issues"
    hypothesis: "H0: no change in output_length_chars across tone variants. H1: assertive tone produces shorter, more actionable outputs than clinical or narrative, with equivalent or better sub-issue quality."
    metric: output_length_chars
    secondary_metrics: [issue_creation_rate, sub_issue_link_count, run_duration_seconds]
    guardrail_metrics:
      - name: run_success_rate
        threshold: ">=0.85"
    min_samples: 50
    weight: [34, 33, 33]
    start_date: "2026-05-31"
    analysis_type: mann_whitney
    tags: [tone, output-quality, triage]
    issue: 36105
sandbox:
  agent:
    runtime: cloud-hypervisor
tools:
  cli-proxy: true
  github:
    mode: local
    toolsets: [actions, issues, pull_requests]
  bash: ["*"]
cache:
  - key: aw-failure-investigator-prefetch-${{ github.run_id }}
    name: Failure investigator prefetch
    path: /tmp/gh-aw/agent/failure-investigator
safe-outputs:
  create-issue:
    expires: 7d
    title-prefix: "[aw-failures] "
    labels: [agentic-workflows, automation, cookie]
    max: 2
    group: true
  close-issue:
    target: "*"
    required-labels: [agentic-workflows]
    required-title-prefix: "[aw]"
    state-reason: duplicate
    max: 100
  update-issue:
    target: "*"
    max: 10
  link-sub-issue:
    max: 10
  noop:
timeout-minutes: 60
imports:
  - uses: shared/meta-analysis-base.md
    with:
      toolsets: [default, actions]
  - shared/reporting.md

  - shared/otlp.md
  - shared/default-ai-credits-pricing.md
  - shared/graders.md
steps:
  - name: Deterministic pre-fetch for failure analysis
    uses: actions/github-script@v9.0.0
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    with:
      github-token: ${{ secrets.GITHUB_TOKEN }}
      script: |
        const fs = require('fs');
        const path = require('path');
        const { execFileSync } = require('child_process');

        const REPO = process.env.GITHUB_REPOSITORY;
        const OUT = '/tmp/gh-aw/agent/failure-investigator/prefetch.json';
        const TRACKER_ID = 'aw-failure-investigator';
        const LOOKBACK_HOURS = 6;
        const FAILURE_CONCLUSIONS = new Set(['failure', 'timed_out', 'startup_failure']);
        const MAX_DISCOVERY_PAGES = 20;
        const MAX_LOG_TAIL_LINES = 50;
        const FAULT_MARKER = /\b(?:error|panic|exception|traceback|fatal|abort|segfault|coredump)\b|(?:process|command).*(?:failed|exit code)|(?:exit code|non-zero exit)/i;
        const MAX_FAILURES_TO_DETAIL = 5;
        const workflowsDir = '.github/workflows';
        const AGENTIC_WORKFLOW_PATHS = fs.existsSync(workflowsDir)
          ? new Set(
              fs
                .readdirSync(workflowsDir)
                .filter((name) => name.endsWith('.lock.yml'))
                .map((name) => `.github/workflows/${name}`),
            )
          : new Set();

        function cmdDisplay(args) {
          return ['gh', ...args].join(' ');
        }

        function commandOutput(error) {
          const stdout = Buffer.isBuffer(error?.stdout) ? error.stdout.toString('utf8') : error?.stdout || '';
          const stderr = Buffer.isBuffer(error?.stderr) ? error.stderr.toString('utf8') : error?.stderr || '';
          return `${stdout}${stderr}`.trim();
        }

        function runJson(args) {
          let out;
          try {
            out = execFileSync('gh', args, { encoding: 'utf8' });
          } catch (error) {
            core.warning(`Command failed: ${cmdDisplay(args)}`);
            const output = commandOutput(error);
            if (output) core.warning(output);
            return null;
          }
          try {
            return JSON.parse(out);
          } catch (error) {
            core.warning(`Non-JSON output from command: ${cmdDisplay(args)} (${error.message})`);
            return null;
          }
        }

        function runText(args) {
          try {
            return execFileSync('gh', args, { encoding: 'utf8' });
          } catch (error) {
            core.warning(`Command failed: ${cmdDisplay(args)}`);
            const output = commandOutput(error);
            if (output) core.warning(output);
            return '';
          }
        }

        function runApiJson(endpoint, params) {
          const query = new URLSearchParams(params).toString();
          return runJson(['api', `${endpoint}?${query}`]);
        }

        function isFailureConclusion(conclusion) {
          return FAILURE_CONCLUSIONS.has(String(conclusion || '').toLowerCase());
        }

        function normalizeWorkflowPath(workflowPath) {
          return String(workflowPath || '').split('@', 1)[0];
        }

        function isAgenticWorkflowPath(workflowPath) {
          const normalizedPath = normalizeWorkflowPath(workflowPath);
          if (AGENTIC_WORKFLOW_PATHS.size > 0) {
            return AGENTIC_WORKFLOW_PATHS.has(normalizedPath);
          }
          core.warning('No local .lock.yml workflows found; falling back to workflow path suffix matching');
          return normalizedPath.endsWith('.lock.yml');
        }

        function captureErrorWindow(logText) {
          const lines = logText.split(/\r?\n/);
          let markerIndex = null;
          for (let index = lines.length - 1; index >= 0; index -= 1) {
            if (FAULT_MARKER.test(lines[index])) {
              markerIndex = index;
              break;
            }
          }

          let capturedLines;
          if (markerIndex === null) {
            capturedLines = lines.slice(-MAX_LOG_TAIL_LINES);
          } else {
            const start = Math.max(0, Math.min(markerIndex - Math.floor(MAX_LOG_TAIL_LINES / 2), lines.length - MAX_LOG_TAIL_LINES));
            const end = Math.min(lines.length, start + MAX_LOG_TAIL_LINES);
            capturedLines = lines.slice(start, end);
          }

          const hasFaultMarker = capturedLines.some((line) => FAULT_MARKER.test(line));
          return { capturedLines, hasFaultMarker };
        }

        function isoformatZ(date) {
          return date.toISOString().replace(/\.\d{3}Z$/, 'Z');
        }

        function listFailedAgenticRuns() {
          const createdSince = isoformatZ(new Date(Date.now() - LOOKBACK_HOURS * 60 * 60 * 1000));
          let page = 1;
          const failedRuns = [];

          while (true) {
            const response =
              runApiJson(`repos/${REPO}/actions/runs`, {
                exclude_pull_requests: 'true',
                status: 'completed',
                created: `>=${createdSince}`,
                per_page: '100',
                page: String(page),
              }) || {};
            const workflowRuns = response.workflow_runs || [];
            if (workflowRuns.length === 0) {
              break;
            }

            for (const run of workflowRuns) {
              const workflowPath = normalizeWorkflowPath(run.path);
              if (!isAgenticWorkflowPath(workflowPath)) continue;
              if (!isFailureConclusion(run.conclusion)) continue;

              failedRuns.push({
                run_id: run.id,
                workflow_name: run.name,
                workflow_path: workflowPath,
                created_at: run.created_at,
                status: run.status,
                conclusion: run.conclusion,
                url: run.html_url,
              });
            }

            if (workflowRuns.length < 100) break;
            if (page >= MAX_DISCOVERY_PAGES) {
              core.warning(`Reached pagination cap (${MAX_DISCOVERY_PAGES} pages) while listing workflow runs`);
              break;
            }
            page += 1;
          }

          failedRuns.sort((a, b) => String(b.created_at || '').localeCompare(String(a.created_at || '')));
          return failedRuns;
        }

        const failedRuns = listFailedAgenticRuns();

        const failureDetails = [];
        for (const run of failedRuns.slice(0, MAX_FAILURES_TO_DETAIL)) {
          const runId = run.run_id;
          if (!runId) continue;

          const runView = runJson([
            'run',
            'view',
            String(runId),
            '--repo',
            REPO,
            '--json',
            'databaseId,url,name,workflowName,createdAt,conclusion,status,jobs',
          ]);
          if (!runView) continue;

          const failedJobNames = [];
          const failedSteps = [];
          const truncatedErrorLogs = [];
          let agentJobConclusion = null;

          for (const job of runView.jobs || []) {
            const jobName = job.name;
            const jobConclusion = String(job.conclusion || '').toLowerCase();
            if (String(jobName || '').toLowerCase() === 'agent') {
              agentJobConclusion = jobConclusion || null;
            }

            if (isFailureConclusion(jobConclusion)) {
              if (jobName) failedJobNames.push(jobName);

              for (const step of job.steps || []) {
                if (isFailureConclusion(step.conclusion)) {
                  failedSteps.push({
                    job_id: job.databaseId,
                    job_name: jobName,
                    step_name: step.name,
                  });
                }
              }

              const jobId = job.databaseId;
              if (jobId) {
                const logText = runText([
                  'run',
                  'view',
                  String(runId),
                  '--repo',
                  REPO,
                  '--job',
                  String(jobId),
                  '--log',
                ]);
                if (logText) {
                  const { capturedLines, hasFaultMarker } = captureErrorWindow(logText);
                  truncatedErrorLogs.push({
                    job_id: jobId,
                    job_name: jobName,
                    line_count: capturedLines.length,
                    tail_lines: capturedLines.join('\n'),
                    capture_likely_missed_fault: !hasFaultMarker,
                  });
                }
              }
            }
          }

          failureDetails.push({
            run_id: runId,
            workflow_name: runView.workflowName || runView.name,
            workflow_path: run.workflow_path,
            url: runView.url,
            created_at: runView.createdAt,
            status: runView.status,
            conclusion: runView.conclusion,
            failed_job_names: [...new Set(failedJobNames)].sort(),
            agent_job_conclusion: agentJobConclusion,
            failed_steps: failedSteps,
            truncated_error_logs: truncatedErrorLogs,
          });
        }

        const existingTrackingIssues =
          runJson([
            'issue',
            'list',
            '--repo',
            REPO,
            '--state',
            'open',
            '--search',
            `gh-aw-tracker-id: ${TRACKER_ID}`,
            '--limit',
            '100',
            '--json',
            'number,title,state,url,labels,createdAt,updatedAt',
          ]) || [];

        const payload = {
          generated_at: new Date().toISOString(),
          repository: REPO,
          lookback_window: `${LOOKBACK_HOURS}h`,
          failed_run_ids: failedRuns.map((run) => run.run_id).filter(Boolean),
          failures: failureDetails,
          existing_tracking_issues: existingTrackingIssues,
        };

        fs.mkdirSync(path.dirname(OUT), { recursive: true });
        fs.writeFileSync(OUT, `${JSON.stringify(payload, null, 2)}\n`, 'utf8');

        core.info(`Wrote deterministic prefetch payload to ${OUT}`);
        core.info(`Failed runs in payload: ${payload.failed_run_ids.length}`);
        core.info(`Existing tracking issues in payload: ${existingTrackingIssues.length}`);
features:
  gh-aw-detection: true
evals:
  - id: failures_investigated
    question: Did the agent investigate agentic workflow failures from the last 6 hours and produce findings?
  - id: issues_created_or_closed
    question: Were fix sub-issues created for unresolved failures, or were resolved tracking issues closed?
  - id: consolidated_failures_closed
    question: When failures were consolidated into or matched against an existing issue, were all corresponding source failure issues closed as duplicates with comments referencing that issue?

---

# [aw] Failure Investigator (6h)

Investigate agentic workflow failures from the last 6 hours and produce actionable issue tracking with sub-issues.

## Scope

- **Repository**: `${{ github.repository }}`
- **Lookback window**: last 6 hours
- **Issue query to inspect first**: <https://github.com/github/gh-aw/issues?q=is%3Aissue%20state%3Aopen%20label%3Aagentic-workflows>
- **Deterministic pre-fetch payload**: `/tmp/gh-aw/agent/failure-investigator/prefetch.json`

## Mission

1. Find recent failures from agentic workflows in the last 6 hours.
2. Correlate findings with currently open `agentic-workflows` issues.
3. Perform large-scale failure analysis using logs + audit + audit-diff.
4. When repeated failures are already tracked by an open `agentic-workflows` issue, do not open a new issue for them — close the new duplicate source failure issues and associate them with the existing issue. Close fixed/stale issues first, then create only the minimum necessary linked fix sub-issues for genuinely uncovered failures, and close every source failure issue represented by a consolidated issue.

## Required Investigation Steps

### 0) Read deterministic pre-fetch payload first (required)

Read `failed_run_ids`, `failures`, and `existing_tracking_issues` **once** from `/tmp/gh-aw/agent/failure-investigator/prefetch.json`.
Do not re-read this file; keep the parsed data in context for all subsequent steps.
Use this payload as the primary discovery dataset and build clustered failure rows with representative + comparator run IDs.
Definitions for step 0 clustering:
- representative run ID: failed run that best captures the dominant signature in a cluster
- comparator run ID: nearest successful run of the same workflow when available, otherwise nearest prior failed run
Only call additional logs/list APIs when a required field is missing or stale.

**Early exit**: If `failed_run_ids` is empty, call `noop` immediately with a brief explanation and stop. If every failure signature is already covered by an open issue in `existing_tracking_issues`, skip steps 1-3 (no new classification or analysis needed) and go straight to step 4 to close every covered source failure issue as a duplicate of its existing tracking issue.

### 1) Classify failures and correlate with existing issues

Use the `failure-classifier` agent, passing the full `failures` array (including `truncated_error_logs`) from the prefetch payload.
It returns compact JSON with severity-ranked clusters (id, severity, representative_run_id, comparator_run_id, workflows, error_signature).

Then use the `issue-matcher` agent, passing the cluster summaries and `existing_tracking_issues` from the prefetch payload.
It returns which clusters are already tracked (matched) and which are new gaps.

Keep the combined cluster + tracking mapping in context for steps 2-4.

**Early exit**: If all untracked clusters from `issue-matcher` are P2 severity (no P0 or P1 gaps), skip steps 2-3 (no deepened evidence or audit-diff needed) but still continue to step 4: matched clusters still need their represented source failure issues closed as duplicates, and any P2 gaps still need to be reflected in the existing coverage. Only call `noop` at this point if there are also no `matched` clusters with open source failure issues to close.

### 2) Deepen evidence for untracked clusters

Use the `cluster-evidence-extractor` agent for untracked P0 and P1 clusters identified in step 1 (at most 3 clusters). The agent prefers pre-fetched `truncated_error_logs` and only calls `audit` for clusters whose logs are too sparse — capping total `audit` MCP calls at 2 across all clusters.

### 3) Compare behavior with `audit-diff`

Use `agentic-workflows` MCP `audit-diff` to compare **the single highest-severity cluster only** (1 comparison maximum):
- failed run vs nearest successful run of the same workflow, or
- failed run vs prior failed run to detect drift

Identify regressions and deltas (metrics/tooling/firewall/MCP behavior) that support fix recommendations.

### 4) Close fixed issues, add focused sub-issues, then close consolidated failures

First, identify currently open `agentic-workflows` issues that are now fixed, stale, or no longer actionable based on fresh evidence, and close them using `update-issue`.

Before closing a tool-denial-limit issue (for example, exceeded denial/guardrail failures), verify there is at least one
linked commit after the issue was opened that touches the affected workflow `.md` or `.lock.yml` path. If no such commit
exists, do **not** close the issue as completed; keep it open and add/update a tracking comment with the missing workflow
fix evidence.

Then, if new uncovered work remains, add **sub-issues** for concrete fixes to the **most recent open parent report issue** instead of creating a new parent by default.

Only create a new parent report issue when **P0 failures have no existing tracking coverage**.

Each new sub-issue must include:
- clear problem statement
- affected workflows and run IDs
- probable root cause
- specific proposed remediation
- success criteria / verification

For every cluster the `issue-matcher` agent already matched to an existing open `agentic-workflows` issue, and for any consolidated parent report or fix issue selected or filed above, identify every open source failure issue represented by that existing or consolidated issue. Source failure issues are the automated `[aw]` issues for the included workflow failures, such as issues reporting that a workflow failed or produced an incomplete result.

Close **every** represented source failure issue with `close_issue`:
- set `issue_number` to the source failure issue
- set `duplicate_of` to the existing or consolidated issue number
- set `body` to `Consolidated into #<existing or consolidated issue number>.`

The configured close reason marks these issues as duplicates. Use the actual issue number in both `duplicate_of` and the comment. Do not close the existing/consolidated issue itself, its remediation sub-issues, or source failure issues that were not included in it.

## Tone Variant Instructions

{{#if experiments.tone_variant == 'assertive'}}
Tone instruction: Write in assertive, action-first style. Open every section with a direct imperative recommendation (e.g., "Fix the retry loop in workflow X — it causes 40% of P0 failures"). Keep rationale to one sentence. Prioritize brevity and actionability over completeness.
{{#elseif experiments.tone_variant == 'narrative'}}
Tone instruction: Write in narrative style. Use flowing prose paragraphs to explain what happened, why it matters, and what the broader context is. Readers should finish each section with a clear mental model of the failure, not just a list of facts.
{{#else}}
Tone instruction: Write in clinical, neutral style. Use numbered lists, avoid editorializing, and anchor every claim to a metric or log reference. This is the baseline behavior.
{{/if}}

## Output Requirements

Follow `shared/reporting.md` for header levels and progressive disclosure formatting.
When creating a parent report issue, include: executive summary, failure cluster table, evidence, existing issue correlation, fix roadmap (P0/P1/P2), and sub-issues created.
For sub-issues, prioritize high-quality actionable items, avoid duplicates unless scope changed, and reference the parent issue and analyzed run IDs.

**Important**: If no action is needed after completing your analysis, you **MUST** call the `noop` safe-output tool with a brief explanation.

## agent: `failure-classifier`
---
description: Groups pre-fetched failure runs into severity-ranked clusters by error signature and workflow
model: small
---
You receive a JSON array of `failures` from the pre-fetch payload. Each entry has `run_id`, `workflow_name`, `workflow_path`, `conclusion`, `failed_job_names`, `failed_steps`, and `truncated_error_logs`. Treat a `truncated_error_logs` entry with `capture_likely_missed_fault: true` as insufficient evidence, never as a failure signature.

Group failures into clusters:
1. Cluster by dominant error signature extracted from `truncated_error_logs[].tail_lines`; group failures from the same workflow with matching signatures together.
2. Assign severity — P0: agent/infra crash, data loss risk, or startup_failure; P1: persistent failure pattern across ≥2 runs; P2: isolated or transient.
3. Pick `representative_run_id` (run that best illustrates the cluster) and `comparator_run_id` (nearest run_id not in the cluster, for diff).
4. Copy `truncated_error_logs` from the representative run only into the output cluster.

Return only JSON — no prose:
```json
{"clusters":[{"id":"cluster-1","severity":"P0|P1|P2","representative_run_id":123,"comparator_run_id":456,"workflows":["workflow-name"],"error_signature":"one-line dominant error","run_ids":[123,789],"truncated_error_logs":[]}]}
```

## agent: `issue-matcher`
---
description: Matches failure clusters to existing open tracking issues to identify coverage gaps
model: small
---
You receive:
- `clusters`: array of failure clusters (id, severity, workflows, error_signature)
- `existing_tracking_issues`: array of open issues (number, title, labels, url)

For each cluster, determine whether an existing issue already tracks it. Match by error_signature similarity and workflow name overlap.

Return only JSON — no prose:
```json
{"matched":[{"cluster_id":"cluster-1","issue_number":42,"confidence":"high|medium|low"}],"gaps":[{"cluster_id":"cluster-2","reason":"no existing issue covers this signature"}]}
```

## agent: `cluster-evidence-extractor`
---
description: Extracts per-cluster audit evidence including dominant errors, tool patterns, anomalies, and failure class
model: small
---
Given failure clusters with their `truncated_error_logs` from the prefetch payload:
1. If a cluster has ≥10 lines of pre-fetched error logs and none has `capture_likely_missed_fault: true`, extract evidence directly from those logs — do **not** call `audit`.
2. Only call `agentic-workflows` MCP `audit` when pre-fetched logs are missing, fewer than 5 lines, or `capture_likely_missed_fault: true`. Cap total `audit` calls at **2** across all clusters.
3. When calling `audit`, request only `artifacts: ["usage", "agent"]` to limit download size.

Extract dominant error, tool-failure pattern, anomalies, and failure class.

Return only JSON:
```json
{"cluster_evidence":[{"cluster_id":"cluster-1","dominant_error":"one-line dominant error","tool_failure_pattern":"tool_name + failing step pattern","anomalies":[],"failure_class":"infra|tool|data|policy|unknown","evidence_run_ids":[123,789]}]}
```
