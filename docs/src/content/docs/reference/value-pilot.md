---
title: Value report pilot
description: What a ten-workflow field trial established about measurement, evidence scopes, and the limits of value reporting.
sidebar:
  order: 299
---

A first applicability pilot inspected **ten workflows other than
schema-feature-coverage**. It ran native collection against one fixed completed
run per workflow and applied `gh aw value` to the resulting evidence.
Nine collections succeeded; one failed explicitly. All ten value commands
returned reports, but **none had a configured, validated value measurement**.
The metric candidates below are not ten working production value functions.

The strongest independent outcome witness was File Diet: a human-accepted issue
led to a merged source-file split that resolved **403 lines over the size
budget**. That is an observed mechanical outcome, not attributed value.

The [machine-readable cohort, candidate contracts, and observations](https://github.com/github/gh-aw/blob/main/.github/aw/value/pilots/2026-11-18.json)
pin the run IDs, source SHAs, concept revision, and witness blobs. All examples
are from `github/gh-aw`; this trial does not establish real cross-repository
effectiveness or population-wide workflow value.

## What was tried

The model selected ten active workflows for varied goals and output types,
without requiring successful runs or delivered changes. Each selected run
completed before the common report cutoff, 17 November 2026 at 12:00 UTC.
Native adoption was observed on 18 November, not reconstructed as of the cutoff.
One run per workflow is an observability probe, not an effectiveness estimate.

| Workflow and recorded run | Candidate outcome | Observed evidence | Assessment |
| --- | --- | --- | --- |
| [Documentation Healer](https://github.com/github/gh-aw/actions/runs/43981238321) | Confirmed documentation gaps resolved | No-op; zero delivered effects | Needs independent gap identities and verified corrections. |
| [Dead Code Remover](https://github.com/github/gh-aw/actions/runs/43886415206) | Eligible unreachable functions remaining | No-op; zero delivered effects | Promising with pinned, successful analyzer snapshots and safety checks. |
| [Go Test Parallelizer](https://github.com/github/gh-aw/actions/runs/44065036647) | Fixed-suite test time, in milliseconds | Incomplete; zero delivered effects | Needs comparable timing and passing race validation, not counts of `t.Parallel`. |
| [Go Logger](https://github.com/github/gh-aw/actions/runs/44009171201) | Independently verified diagnostic scenarios | No-op; zero delivered effects | Reject log-call counts as value; a scenario reference is missing. |
| [File Diet](https://github.com/github/gh-aw/actions/runs/43870412927) | Source lines over the fixed size budget | One human-accepted issue; independently verified downstream refactor | Strongest mechanical outcome candidate; not yet attributable by the command. |
| [Code Metrics](https://github.com/github/gh-aw/actions/runs/43928873951) | Independently verified metric observations | One discussion; acceptance pending | Potential report-quality check, not proof the report improved repository health. |
| [Issue Triage](https://github.com/github/gh-aw/actions/runs/43889622699) | Eligible issues correctly classified | One label and one comment on one issue; both delivery fallbacks | Needs independent correctness and complete eligibility, not action counts. |
| [Stale PR Cleanup](https://github.com/github/gh-aw/actions/runs/43574891405) | Stale PRs safely retired under the full policy | No-op; zero delivered effects | Needs complete pre-action eligibility and independent closure justification. |
| [CLI Performance](https://github.com/github/gh-aw/actions/runs/43890801339) | Fixed compilation benchmark, in ns/op | Collection failed: canonical session identity missing | Promising metric, but needs valid benchmark-series evidence. |
| [Dependabot Burner](https://github.com/github/gh-aw/actions/runs/44044481567) | Validated dependency target versions actually adopted | Incomplete; zero delivered effects | Needs the complete update batch and verified accepted target state. |

Across the nine readable runs, native reporting found four applied effects:
one human-accepted issue, one pending discussion, and two delivery-fallback
effects on a single triaged issue. Those are not four units of value.
Four other runs were no-ops and two reported incomplete work. Successful
execution and recorded built-in tool-efficiency grades did not establish
workflow-specific outcomes.

## File Diet witness

[Issue #78380](https://github.com/github/gh-aw/issues/78380) identified
`pkg/cli/logs_orchestrator_download.go` at 1,202 lines.
The workflow revision used a healthy threshold of 800 lines: files at or above
that threshold required analysis.

[PR #78635](https://github.com/github/gh-aw/pull/78635), linked from the issue,
deleted that file and added five focused source files. Exact Git blobs contain
93, 417, 381, 199, and 151 newline-delimited lines. The test-file addition
does not enter the non-test source metric.

For tracked, non-test Go files under `pkg/`, the candidate is:

```text
excess lines = sum(max(0, newline_count(file) - 799))
improvement = excess lines before - excess lines after
```

The removed file contributed 403 excess lines; none of the five new files
contributes excess. The change therefore resolves 403 lines over budget.
It does **not** remove 403 lines of code, prove better behavior, or quantify
maintainer time saved.

Native evidence records explicit-human completion of the issue. The current
command nevertheless cannot attribute this outcome: its attribution path
requires a directly produced PR, not an issue-to-PR chain. The seven-day window
after the 17 November adoption is also incomplete. No attributed-value number
was assigned, and no prospective grader contract was backdated.

## Limits exposed by the trial

The snapshot model is useful for source-state outcomes, but not sufficient
for every workflow. Benchmark time series, reviewed classifications, complete
eligibility snapshots, and downstream issue resolution have different evidence
scopes. None of these ten workflows currently supplies the committed numeric
projection required by `json-number-v1`. A missing input is not a reason to
replace a goal with an activity count or an arbitrary normalized score.

The ledger also loses complete-zero coverage. Native `logs` reports six
complete runs with zero delivered effects, whereas `value` reports unavailable
lifecycle coverage for those workflows because no item records exist.
An absent item ledger must not be interpreted as zero; preserving verified
zero requires run-level coverage evidence.

The malformed CLI-performance run aborted the first combined collection.
Collecting each fixed run independently exposed that failure without hiding
the other nine results. A successful lifecycle-only `value` command does not
repair the missing session identity or make its evidence valid.

The local checkout was shallow. Full historical merge windows and the schema
slides were not replayed in this trial. No workflow was dispatched.

## Reproduce the probe

For each fixed `run_id` and `workflow` in the cohort, collect and report
independently. Preserve each collection's exit status; do not score failed
collections as zero.

```bash wrap
printf '%s\n' RUN_ID | gh aw logs --stdin --repo github/gh-aw \
  --adoption --count 1 --artifacts usage,delivery --output OUTPUT/native --json
gh aw value WORKFLOW --repo github/gh-aw --logs-dir OUTPUT/native \
  --start 2026-11-01 --end 2026-11-17 --json
```

Provider observations and artifact availability can change. Run IDs and Git
blobs fix the cohort and code, not future provider state.

The next strongest candidates are File Diet's size-policy projection, Dead
Code Remover's independent analyzer projection, and CLI Performance's typed
benchmark series. Label correctness, diagnostic usefulness, safe PR retirement,
and report-driven decisions need independent references before they become
credible outcome measurements.
