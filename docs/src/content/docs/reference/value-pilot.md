---
title: Value report pilot
description: What a ten-workflow field trial established about measurement, normalized sessions, and the limits of value reporting.
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

The identity-incomplete CLI-performance run aborted the first combined collection.
Collecting each fixed run independently exposed that failure without hiding
the other nine results. A successful lifecycle-only `value` command does not
repair the missing session identity or make its evidence valid.

The initial trial used a shallow checkout. Full historical merge windows and
the schema slides were not replayed in that trial. No workflow was dispatched.
The full-history follow-up below removes that checkout limitation.

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

## Typed evidence reevaluation

A second pass replayed **the same ten fixed runs**, with ten executable
`evidence-number-v1` definitions and explicit window recipes. It did not replace
the first trial, dispatch workflows, repair the incomplete session, or fetch
missing Git history.
The [typed input bundle](https://github.com/github/gh-aw/blob/main/.github/aw/value/pilots/2026-11-18.evidence.json)
and [generated reevaluation results](https://github.com/github/gh-aw/blob/main/.github/aw/value/pilots/2026-11-18.reevaluation.json)
retain original identities, unavailable reasons, exact declaration digests,
policies, and results.

| Result | First pass | Typed replay |
| --- | --- | --- |
| Configured executable definitions | 0 | 10 |
| Workflows with numeric independent observations evaluated by `value` | 0 | 1 |
| Workflows without sufficient independent goal evidence | 10 | 9 |
| Independently observed mechanical witness | File Diet, verified separately | File Diet, computed through the production evaluator |
| Attributed-value claims | 0 | 0 |

These are ten executable input/aggregation contracts, **not ten independently
validated goal-measurement implementations**. Nine return explicit unavailable
observations: the new format cannot create missing eligibility, correctness
references, analyzer results, accepted target versions, or benchmark samples.
The native complete-zero coverage gap and malformed CLI-performance collection
remain visible; no missing outcome was converted into zero.

For File Diet, the bundle fixes the six changed source paths as one cohort,
including verified absences at each endpoint. Raw newline counts are
`[1202,0,0,0,0,0]` at exact parent
`d6f2493fa9d7545895f5ca3ed2fb24ab43a46b28` and
`[0,93,417,381,199,151]` at merge
`ecf4e21e9438774092bcd313ddbb1c6fc18e8737`.
The Go excess-line evaluator computes **403 → 0**, an improvement of 403
over-budget lines in that cohort, not absolute whole-repository totals.
The complete source diff establishes the corresponding repository delta;
neither number measures quality or saved time.

### Why not seven days for everything

Seven days was the slides' reporting convention, not a validated causal or
statistical threshold. The original slide recipe now pins that policy
explicitly. The ten new recipes instead record retrospective candidate
policies:

| Workflow | Before / after days | Outcome lag | Durability | Evidence rationale |
| --- | --- | --- | --- | --- |
| Documentation Healer | 14 / 30 | 7 | 30 | Corrections and review can take longer than a weekly cycle. |
| Dead Code Remover | 1 / 7 | 0 | 14 | Immediate controlled analysis; separate regression/survival horizon. |
| Go Test Parallelizer | 7 / 14 | 0 | 14 | Five controlled timing replicates minimum; calendar time is not replication. |
| Go Logger | 7 / 14 | 0 | 30 | Independent scenarios first; longer reversal observation. |
| File Diet | 1 / 14 | 0 | 14 | Exact source endpoints; longer retention than the original trial. |
| Code Metrics | 7 / 7 | 0 | 7 | Run-scoped report-quality checks, not longitudinal value. |
| Issue Triage | 7 / 14 | 1 | 14 | Independent classification and later label retention. |
| Stale PR Cleanup | 14 / 30 | 1 | 30 | Complete eligibility and delayed reopen/reversal observations. |
| CLI Performance | 7 / 14 | 0 | 14 | Five comparable benchmark replicates minimum, independently of days. |
| Dependabot Burner | 1 / 14 | 0 | 30 | Exact target versions and validation, followed by rollback observation. |

These choices were made after the first trial and are **not empirically
validated or backdated contracts**. Future collection must test their adequacy.
Changing days did not make any of the nine unavailable goals measurable.

Imported replay records these policies but does not execute historical Git
sampling, overlap checks, or survival. File Diet's candidate 14-day horizon
matures on 1 December at 06:09:04 UTC, after the fixed cutoff.
Even eventual maturity will not establish complete samples or attribution:
the evidence is caller-supplied, the issue-to-PR link is not a native direct-PR
attribution path, and complete survival was not evaluated.

### Replay the new inputs

For each workflow in the fixed cohort:

```bash wrap
gh aw value WORKFLOW --repo github/gh-aw \
  --value-function .github/aw/value/WORKFLOW.json \
  --recipe .github/aw/value/WORKFLOW.pilot.json \
  --evidence .github/aw/value/pilots/2026-11-18.evidence.json \
  --logs-dir OUTPUT/native --json
```

The original unconfigured first pass used revision `9f70921`; current
auto-discovery finds the new definitions. Retain the original native ledger to
reproduce native adoption clocks and maturity anchoring. Without it, numeric
measurements replay, but native lifecycle coverage is unavailable.

## Full-history follow-up

On 18 November, the checkout was explicitly unshallowed for `main`, retaining
19,128 first-parent commits. The CLI itself still never fetches history.
The original schema slides recipe was then replayed against pinned ref
`30aaa011200b522ca1ffce0c8aed019048aac096`, with freshly collected native
evidence from completed run `36389625388`.

The [recorded full-history result](https://github.com/github/gh-aw/blob/main/.github/aw/value/pilots/2026-11-18.full-history.json)
contains 36 observations, coverage 32/45 → 75/75, a 39-field stable cohort
improving from 30 to 39 covered, 44 producer-linked PRs, 19 merges, and six
selected rows with 84 samples.

PR #63949 qualifies for **five attributed fields**: exact-parent coverage
63/68 → 68/68, matching native explicit-human adoption, no neighboring workflow
merge, and five changed files untouched through seven days and 257 subsequent
main commits. Attribution coverage remains partial; the other two selected
witnesses lack collected native adoption proof.

This is a full historical Git replay, not imported-evidence attribution.
It does not resolve the nine independent goal-evidence gaps in the ten-workflow
cohort. Original trial observations remain unchanged, and no workflow was
dispatched.

## Session-first compatibility probe

A fourth pass inspected the **same ten completed runs**, using the existing
full-session validator and compact projector, without executing graders.
The [session transport audit](https://github.com/github/gh-aw/blob/main/.github/aw/value/pilots/2026-11-18.sessions.json)
pins source hashes, per-run event counts, recorded-grade comparisons, and
verification limits. It covers four observed engines: Copilot (four runs),
Claude (two), Codex (two), and Pi (one). CLI Performance has no canonical
workflow metadata identifying its engine.

| Check | Full `aw_session.jsonl` | Compact profile |
| --- | --- | --- |
| Full-session schema validation | 10 passed | Not a full-session contract |
| Matching repository, workflow, run, and attempt | 9 runs | 9 runs |
| Events | 8,396 | 354 |
| Tool-start records | 227 | 0 |
| Individual built-in grader results | 100: 96 numeric, four unavailable | 0 |
| Recorded candidate operational-value grades | 0 | 0 |

All 100 recorded grade triples (`id`, `status`, nullable `value`) match their
original grader artifacts. The nine successful original Go collection reports
preserve the same 90 triples. This checks existing result projections; it does
**not** rerun a Go grader or demonstrate target session-query execution.
Built-in execution and efficiency grades remain distinct from goal outcomes.
The absence of a recorded candidate grade does not prove that no useful fact
could be recovered from arbitrary tool output; that would require independently
validating the candidate's full measurement contract.

### Two boundaries matter

CLI Performance is **schema-valid but identity-incomplete**, not malformed JSON:
its 53 events include ten grader records, four unavailable, but no
`workflow.info` or canonical tool-start records. Schema conformance alone does
not establish usable execution identity or evidence completeness. Its original
native lifecycle collection failure remains unchanged; no identity was
inferred from a filename to bypass that guard.

The compact profile deliberately excludes tool evidence and nested grader
arrays. These sessions store individual grades in
`grader.result.data.results[]`; compaction preserves a run summary, not those
results. Nine projections also report size or oversized-line truncation.
CLI Performance reports `truncated: false` but still loses every individual
grade through routine projection. **False truncation does not mean complete
evidence.** See the [compact-session contract](../specs/unified-agent-session-specification/#38-compact-unified-session-normative).

An ingestion path that needs session-based regrading must resolve the full
artifact with verified repository/run/attempt/invocation binding, rather than
treat compact notifications as complete sessions. A recorded-result consumer
can instead use the existing grader artifacts. OTLP export can additionally
strip identity from event bodies, so its external correlation must also be
verified. This probe did not access the external data lake or establish which
ingestion path it uses.

### What this supports

The full unified session is a viable shared **input and result carrier** across
the four observed engines; the experiment does not justify another session
format or a JavaScript grading runtime in the Go server. Future workflow-specific
Go/query graders still need their independently verifiable measurement facts
recorded before grading. The ten historical runs do not already contain the
requested goal grades, and this pass did not implement ten new goal evaluators.

Later adoption and durability belong to the existing lifecycle observations,
joined by a report. File Diet's later 403-line Git outcome is not a historical
session grade and must not be inserted into that immutable session.
At the time of this probe, `gh aw value` used Git or supplied typed evidence;
automatic consumption of session grades was not implemented by the probe.
No workflow was dispatched, no commits were supplied to graders, and no
published sessions or grader results were changed.

A subsequent [read-only reporting bridge](../value/#recorded-grader-results)
exposes primary operational-value grades already present in the existing logs
summary. It does not execute graders or invent the missing goal grades in this
historical cohort.

To check a downloaded full session with the existing producer validator:

```bash wrap
node actions/setup/js/scripts/validate_session.cjs unified \
  OUTPUT/native/run-RUN_ID/usage/aw_session.jsonl
```

For the compact comparison, call
`readCompactUnifiedSession(path)` from
`actions/setup/js/compact_unified_session.cjs` on those same immutable bytes.
Compare individual results inside `grader.result.data.results[]`, not just the
presence of a `grader.result` event or its aggregate pass count.
