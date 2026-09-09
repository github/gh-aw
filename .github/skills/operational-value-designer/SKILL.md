---
name: operational-value-designer
description: "Design and verify a deterministic operational-value grader for a GitHub Agentic Workflow. Use for per-run operational value, evidence attribution, maturation, baselines, and operational-value evaluators. Usage: /operational-value-designer OWNER/REPO WORKFLOW-NAME."
argument-hint: "OWNER/REPO WORKFLOW-NAME"
allowed-tools: bash jq gh
metadata:
  version: "1.0.0"
---

# Operational Value Grader

Design one deterministic `operational-value` grader that reports absolute operational attainment for each workflow run. Keep exactly one authoritative primary value, and declare any normalized diagnostic metrics separately.

Operational value is the degree to which the workflow's intended repository outcome is attained for the opportunity assigned to a run, demonstrated by accepted repository evidence under a frozen contract. It is not execution quality, output volume, safe-output creation, or an agent's assessment.

## Output

Create one executable evaluator at:

```text
.github/graders/WORKFLOW-NAME-operational-value.sh
```

Configure the workflow:

```yaml
graders:
  operational-value:
    run: .github/graders/WORKFLOW-NAME-operational-value.sh
```

The grader's primary operational value (`value`) is absolute attainment in `[0,1]`. A comparable frozen baseline may be reported separately as `baselineValue`; gh-aw derives `deltaFromBaseline`. Never define the primary operational value as a difference from baseline.

## Design Procedure

1. Validate `OWNER/REPO` and resolve `.github/workflows/WORKFLOW-NAME.md`. Do not infer inputs from the workspace or remotes.
2. Recover adoption-time intent from the workflow's first commit and first parent. Use only adoption-time workflow content and pre-adoption evidence to choose opportunities, accepted evidence, formulas, targets, or a baseline.
3. Define how every workflow run binds to one operational case:
   - produce a stable `opportunityKey`;
   - prevent overlapping ownership where possible;
   - preserve repeated keys when duplicate runs target the same opportunity so downstream analysis can cluster or deduplicate them;
   - treat reruns with the same GitHub run ID as the same subject.
4. Freeze accepted evidence, evidence repositories, matching rules, zero-versus-missing behavior, and `maturesAt` computation.
  - Declare only the workflow permission scopes required to collect that evidence. The evaluator receives `GH_TOKEN` with the agent job's declared permissions; gh-aw does not add evidence permissions automatically.
5. Choose exactly one direct primary metric in `[0,1]`. Higher must always mean greater attainment. Optional diagnostic metrics may provide normalized outcome context, but must remain separate rather than being combined into the primary value. Keep trace graders and activity counts separate.
6. If comparable pre-adoption evidence exists, score it with the same metric and freeze it under `baseline`. Otherwise use `attainment-only` with a null baseline value.
7. Implement the evaluator interface below and run:

   ```bash
  .github/skills/operational-value-designer/scripts/verify-operational-value-evaluator.sh .github/graders/WORKFLOW-NAME-operational-value.sh
   gh aw compile .github/workflows/WORKFLOW-NAME.md
   ```

## Evaluator Interface

The evaluator uses Bash 3.2-compatible Bash plus `jq` and supports:

- `--definition`: print the frozen schema-version 4 contract.
- `--metric`: read one evidence object on stdin and print a deterministic number in `[0,1]` or `null`.
- `--grade-run`: read a run request on stdin and print one operational-value observation.

`--grade-run` receives:

```json
{
  "schemaVersion": 1,
  "run": {
    "id": "12345",
    "attempt": 1,
    "repository": "OWNER/REPO",
    "workflow": "Workflow name",
    "ref": "refs/heads/main",
    "sha": "...",
    "eventName": "schedule",
    "createdAt": "2026-08-23T11:58:00Z"
  },
  "evidenceAt": "2026-08-23T12:00:00.000Z",
  "case": null,
  "event": null,
  "config": {}
}
```

It returns:

```json
{
  "value": 0.75,
  "opportunityKey": "issue:42",
  "case": {"issue": 42},
  "evidenceCutoff": "2026-08-23T12:00:00.000Z",
  "maturesAt": "2026-08-30T12:00:00.000Z",
  "provenance": [
    {"repository": "OWNER/REPO", "kind": "issue", "ref": "42"}
  ],
  "diagnostics": {"repository-health": 0.8}
}
```

The function must cap `evidenceCutoff` at the earlier of `evidenceAt` and `maturesAt`. A run is never intrinsically pending: the operational value is an as-of observation and may be recomputed until maturity. After maturity, the cap makes the result stable.

## Regrade a Historical Run

Recompute a run at an explicit evidence time with the same evaluator used by the original run:

```bash
gh aw graders operational-value RUN-ID \
  --evidence-at 2026-08-30T12:00:00.000Z \
  --json
```

Add `--repo [HOST/]OWNER/REPO` to select the GitHub host for the current repository checkout. The command downloads the original grader artifact, reuses its operational case and complete run subject, and refuses to execute unless the archived evaluator matches both digest records and the evaluator at the recorded commit in the trusted checkout. It prints a new observation and never modifies the original artifact.

## Build a Historical Report

Build a report across all completed runs from the contract's adoption time:

```bash
gh aw graders operational-value report WORKFLOW-NAME
```

This writes JSON, SVG, and Markdown artifacts under `reports/operational-value`. It applies the current evaluator digest to every run for comparability and backfills pre-grader runs by passing their run subject with `case: null` and `event: null`. The evaluator must reconstruct assignment from accepted evidence when the case is null.

Mature numeric results are cached by repository, workflow, evaluator digest, and Monday-based UTC week. Unavailable, immature, and failed evaluations are retried. Independent weeks are evaluated concurrently; use `--concurrency` to control evaluator executions (the default is 8). Use `--refresh` to bypass cached observations, `--until` to choose the evidence endpoint, and `--output` or `--cache-dir` to relocate generated files.

Reports preserve all run-level points. Weekly primary means retain the latest observation per repeated `opportunityKey` within each week. Declared diagnostics are plotted independently using their `latest` or `mean` aggregation. Missing evidence remains null, and changes over time or from a baseline do not establish causation.

## Definition Contract

`--definition` must contain:

- `schemaVersion: 4` and `grader: "operational-value"`;
- repository, workflow name, source path, and adoption commit/time;
- operational-value statement;
- evidence opportunity, assignment, accepted evidence, repositories, collection, maturation, zero rule, and missing rule;
- one primary metric with formula and validation examples;
- optional `diagnosticMetrics`, each with a unique ID, name, formula, `higher_is_better` direction, and `latest` or `mean` aggregation;
- baseline mode, value, cutoff, and provenance.

For `baseline-comparable`, baseline value must be in `[0,1]` and have immutable provenance. For `attainment-only`, baseline value and cutoff must be null.

## Interpretation Rules

- `value` answers “what operational value did this run attain for its assigned opportunity?”
- `deltaFromBaseline` answers “how far is this observation above or below the frozen pre-adoption reference?”
- Diagnostics provide separate normalized context and never contribute to `value`.
- Neither establishes that the workflow caused the outcome.
- Compare runs only under the same evaluator digest and evidence horizon.
- Identify a replayed observation by `(runId, evaluatorDigest, evidenceAt)`.
- Do not treat repeated observations of one run, duplicate opportunity keys, or overlapping state windows as independent samples.