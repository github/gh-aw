---
private: true
emoji: "🧫"
name: Daily Harness Experiment Proposer
description: Converts compact existing workflow evidence (concluded experiment decisions/analyses, 14-day run/failure aggregates, and at most one targeted gh aw audit) into exactly one falsifiable, single-primary-dimension harness mutation wrapped in a balanced control/candidate experiment, compiles and validates the candidate, and opens an issue carrying the exact patch for human review. Never promotes or merges.
on:
  schedule: daily
  workflow_dispatch:
  skip-if-match: 'is:open in:title "[harness-experiment-proposal]"'

tracker-id: daily-harness-experiment-proposer

permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
  discussions: read

engine:
  id: claude
strict: true
max-turns: 40
max-ai-credits: 2000
timeout-minutes: 30

network:
  allowed:
    - defaults
    - go
    - github

sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor

tools:
  cli-proxy: true
  github:
    mode: local
    toolsets: [default, actions]
  bash: true
  edit:
  repo-memory:
    branch-name: "memory/harness-experiment-proposer"
    description: "Ledger of past harness-experiment proposals and their eventual EXTEND/PROMOTE/REJECT/INCONCLUSIVE decisions, used to avoid repeating rejected mutations and to prefer adapting promoted patterns."
    file-glob: ["*.json", "*.md"]
    max-file-size: 102400
    max-patch-size: 51200

safe-outputs:
  create-issue:
    title-prefix: "[harness-experiment-proposal] "
    labels: [automation, experiment-proposal, harness, needs-manual-patch]
    expires: 7d
    max: 1
  noop:
  threat-detection: false

imports:
  - shared/otlp.md
  - shared/reporting.md

features:
  gh-aw-detection: true

steps:
  - name: Build gh-aw from source
    run: |
      set -e
      cd "$GITHUB_WORKSPACE"
      make build
      echo "gh-aw version: $("$GITHUB_WORKSPACE/gh-aw" --version)"

  - name: Gather compact experiment evidence (list + analyses)
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -euo pipefail
      cd "$GITHUB_WORKSPACE"
      mkdir -p /tmp/gh-aw/harness-proposer
      ./gh-aw experiments list --json > /tmp/gh-aw/harness-proposer/experiments-list.json 2>/tmp/gh-aw/harness-proposer/experiments-list.err \
        || echo '[]' > /tmp/gh-aw/harness-proposer/experiments-list.json
      : > /tmp/gh-aw/harness-proposer/experiments-analyses.jsonl
      jq -r '.[]?.workflow_id // empty' /tmp/gh-aw/harness-proposer/experiments-list.json 2>/dev/null | sort -u | while read -r wf; do
        [ -z "$wf" ] && continue
        if out=$(./gh-aw experiments analyze "$wf" --json 2>/tmp/gh-aw/harness-proposer/analyze-"$wf".err); then
          echo "$out" | jq -c --arg wf "$wf" '{workflow_id: $wf, total_runs: (.total_runs // 0), experiments: (.experiments // []), analyses: (.analyses // [])}' \
            >> /tmp/gh-aw/harness-proposer/experiments-analyses.jsonl
        fi
      done
      echo "Active experiments tracked: $(jq 'length' /tmp/gh-aw/harness-proposer/experiments-list.json)"
      echo "Analyses collected: $(wc -l < /tmp/gh-aw/harness-proposer/experiments-analyses.jsonl)"

  - name: Gather compact run/failure evidence (14d aggregate)
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -euo pipefail
      cd "$GITHUB_WORKSPACE"
      ./gh-aw logs --start-date -14d --json > /tmp/gh-aw/harness-proposer/recent-runs.json 2>/tmp/gh-aw/harness-proposer/recent-runs.err \
        || echo '{"runs":[]}' > /tmp/gh-aw/harness-proposer/recent-runs.json
      jq '[ (.runs // []) | group_by(.workflow_name)[] | {
              workflow_name: .[0].workflow_name,
              workflow_path: .[0].workflow_path,
              run_count: length,
              success_count: ([.[] | select(.conclusion=="success")] | length),
              failure_count: ([.[] | select(.conclusion=="failure")] | length),
              total_error_count: ([.[].error_count // 0] | add // 0),
              avg_aic: (([.[].aic // 0] | add // 0) / length),
              avg_turns: (([.[].turns // 0] | add // 0) / length),
              avg_action_minutes: (([.[].action_minutes // 0] | add // 0) / length)
            } ] | sort_by(-.failure_count, -.avg_aic)' \
        /tmp/gh-aw/harness-proposer/recent-runs.json > /tmp/gh-aw/harness-proposer/per-workflow-summary.json
      echo "Summarized $(jq 'length' /tmp/gh-aw/harness-proposer/per-workflow-summary.json) workflows from 14d run history"

  - name: Compute eligible target-workflow candidates (deterministic filter)
    run: |
      set -euo pipefail
      cd "$GITHUB_WORKSPACE"
      SELF_BASENAME="daily-harness-experiment-proposer.md"
      : > /tmp/gh-aw/harness-proposer/eligible-candidates.txt
      : > /tmp/gh-aw/harness-proposer/excluded-candidates.txt
      for f in .github/workflows/*.md; do
        base="$(basename "$f")"
        reason=""
        if [ "$base" = "$SELF_BASENAME" ]; then
          reason="self"
        elif grep -qE '^source:' "$f"; then
          reason="source-managed (mirrored from another repo, must never be edited here)"
        elif echo "$base" | grep -qiE 'secret|vuln|malicious|semgrep|scan|security|detection|threat|deepsec|sighthound'; then
          reason="security/compliance-critical workflow (excluded conservatively)"
        elif echo "$base" | grep -qiE 'optimizer|proposer|updater|fixer'; then
          reason="meta/self-modifying workflow (excluded to avoid recursive mutation loops)"
        elif [ "$(grep -c '^\s*schedule:' "$f" || true)" = "0" ] && ! grep -qE 'workflow_dispatch' "$f"; then
          reason="not scheduled/dispatchable frequently enough to realistically reach min_samples"
        fi
        # Count experiment entries declared directly under this workflow's own `experiments:`
        # frontmatter block (2-space-indented keys between `experiments:` and the next
        # top-level frontmatter key), not just any indented key anywhere in the file.
        active_experiments=$(awk '
          /^experiments:[[:space:]]*$/ { in_exp=1; next }
          in_exp && /^[A-Za-z0-9_-]+:/ { in_exp=0 }
          in_exp && /^  [A-Za-z_][A-Za-z0-9_]*:/ { count++ }
          END { print count+0 }
        ' "$f")
        if [ -z "$reason" ] && [ "$active_experiments" -ge 2 ]; then
          reason="already declares $active_experiments active experiments (avoid compounding confounded experiments on one workflow)"
        fi
        if [ -n "$reason" ]; then
          echo "$f: $reason" >> /tmp/gh-aw/harness-proposer/excluded-candidates.txt
        else
          printf '%s\t%s\n' "$f" "$active_experiments" >> /tmp/gh-aw/harness-proposer/eligible-candidates.txt
        fi
      done
      echo "Eligible candidates: $(wc -l < /tmp/gh-aw/harness-proposer/eligible-candidates.txt)"
      echo "Excluded candidates: $(wc -l < /tmp/gh-aw/harness-proposer/excluded-candidates.txt)"

  - name: Load prior proposal ledger
    run: |
      set -euo pipefail
      LEDGER="/tmp/gh-aw/repo-memory/default/harness-proposals.json"
      mkdir -p /tmp/gh-aw/harness-proposer
      if [ -f "$LEDGER" ]; then
        cp "$LEDGER" /tmp/gh-aw/harness-proposer/ledger.json
        echo "✅ Loaded $(jq 'length' "$LEDGER") prior proposal ledger entries."
      else
        echo '[]' > /tmp/gh-aw/harness-proposer/ledger.json
        echo "ℹ️ No prior ledger found; starting fresh."
      fi

evals:
  - id: single_dimension_hypothesis_selected
    question: Did the agent select exactly one eligible target workflow, exactly one primary_dimension (from the six-dimension harness taxonomy) with exactly one narrow subtype, and write a structured hypothesis contract (observation, mutation, mechanism, primary_metric, expected_direction, minimum_effect, guardrails, evidence, control, candidate) citing at least one — and at least two when independently available — concrete evidence entries from the pre-gathered experiments/logs evidence files, never invented?
  - id: candidate_compiled_and_validated
    question: Did the agent add a genuine balanced control/candidate experiments block (with a supported grader:/eval:-referenced primary metric and guardrails, and a decision policy) to the target workflow, then run gh aw compile with --strict (and --validate) on that single target workflow, confirming a clean compile before proposing?
  - id: proposal_issue_emitted
    question: Did the agent emit exactly one proposal issue with all required body sections in order (Observation, Hypothesis, Candidate Mutation, Experiment, Guardrails, Expected Economics, Validation, Interpretation, Rollback) plus the exact patch/diff and an application plan — or call noop with a clear reason when no eligible, falsifiable mutation could be justified?
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Harness Experiment Proposer 🧫

You propose **exactly one** falsifiable, one-dimension harness mutation for **one** existing agentic workflow, wrapped in a genuine balanced control/candidate `experiments:` configuration, and open a **human-reviewable issue** carrying the exact patch for a human to apply. You never promote, merge, or reinterpret experiment decisions. You never touch source-managed (`source:`) workflows.

## Deterministic Preprocessing Already Done

Before your turn started, deterministic (non-agentic) steps built `gh-aw` from source and pre-aggregated all compact evidence you need into these files. **Read these files first; do not re-derive their contents by re-running many exploratory commands or replaying trajectories.**

- `/tmp/gh-aw/harness-proposer/experiments-list.json` — output of `gh aw experiments list --json`: every workflow with tracked experiment state (`workflow_id`, `branch`, `experiments`, `total_runs`, `last_run`).
- `/tmp/gh-aw/harness-proposer/experiments-analyses.jsonl` — one compact JSON object per tracked workflow, each the output of `gh aw experiments analyze <workflow_id> --json` (`total_runs`, `experiments`, `analyses[]` with `decision`, `reason_code`, `reason`, `control`, `candidate`, `samples`, `effect`, `evidence`, `decision_guardrails`). A `reason_code` of `guardrail_unsupported` on an existing experiment means its `guardrail_metrics` names never resolved to a real observation source (e.g. a bare native name instead of `grader:<id>`/`eval:<id>`) — treat that as a warning sign, not a template, and never repeat that mistake in your own guardrails.
- `/tmp/gh-aw/harness-proposer/per-workflow-summary.json` — 14-day aggregate per workflow from `gh aw logs --json` (`run_count`, `success_count`, `failure_count`, `total_error_count`, `avg_aic`, `avg_turns`, `avg_action_minutes`), sorted by failure/cost. This file contains **no grader or eval results** — it is a run-metadata aggregate only; never claim a grader/eval summary exists here.
- `/tmp/gh-aw/harness-proposer/eligible-candidates.txt` — deterministically pre-filtered list of `.github/workflows/*.md` files that are legal targets (see Eligibility below), one per line as `<path>\t<active_experiment_count>` (the count of entries already declared under that workflow's own `experiments:` block; files with 2 or more are already excluded, see below).
- `/tmp/gh-aw/harness-proposer/excluded-candidates.txt` — every excluded workflow with its exclusion reason, for your own sanity-checking (do not override these exclusions).
- `/tmp/gh-aw/harness-proposer/ledger.json` — this workflow's own repo-memory ledger of every past proposal you (or a prior run) made, each with `{date, workflow_id, primary_dimension, subtype, experiment_name, pr_or_issue, decision}` (`decision` is filled in later once `gh aw experiments analyze` reaches a verdict; may be `null`/absent for proposals still pending).

If a targeted trace of one specific run is genuinely necessary to corroborate a hypothesis (e.g. to confirm *why* a workflow's failure count is elevated), you may run **at most one** `gh aw audit <run-id>` call — never bulk-audit many runs, and prefer the pre-aggregated summaries whenever they are sufficient.

## Phase 1 — Eligibility and Exclusion (never override the deterministic filter)

Only consider workflows listed in `eligible-candidates.txt`. In addition to the deterministic filter already applied (self, `source:`-managed, security/compliance-critical, meta-optimizer/proposer/updater/fixer workflows, and workflows without a realistic schedule), also skip a candidate this run if **any** of these hold:

- **Currently broken** — its most recent run(s) in `per-workflow-summary.json`/`recent-runs.json` show it is presently failing outright (not merely noisy). Fix broken workflows directly; they are not experiment material.
- **Unresolved severe operational failure** — an open, severe operational failure (crash loop, permission failure, quota exhaustion) is evident from the evidence and has not been fixed. An A/B experiment cannot run cleanly on top of a broken harness.
- **Obvious deterministic/direct fix available** — the evidence points to a bug or misconfiguration with one clearly correct fix (e.g. a missing tool permission, an unguarded null, a wrong file path). Apply that directly instead of wrapping it in an A/B test — experiments are for genuinely uncertain trade-offs, not known bugs.
- **No measurable success criterion** — the target workflow has, and cannot minimally be given, any deterministic task-quality outcome, workflow-specific grader, built-in grader, or eval that could serve as `primary_metric` (see Phase 3's metric priority policy).
- **Sparse traffic** — its 14-day `run_count` in `per-workflow-summary.json` is so low that reaching `min_samples: 20` per variant is not realistic within a few months at its current schedule. Prefer targets with **recurring, frequent traffic** likely to accumulate 20 usable observations per arm within a reasonable window.
- **Equivalent already-active experiment** — it already declares an active `experiments:` entry whose `tags`/`primary_dimension`+`subtype` (or, absent tags, its evident mutation) match the one you are about to propose. Never start a second, redundant experiment for the same dimension/subtype pair while one is still running.
- **2 or more active experiments already** — already enforced deterministically: the filter step excludes any file with 2+ entries under its own `experiments:` block before it ever reaches `eligible-candidates.txt`, so no candidate you see here can have this problem, but never propose a second experiment for a workflow that already carries one active experiment plus your new one would make two simultaneously (check the `<active_experiment_count>` column in `eligible-candidates.txt` — 0 is preferred; 1 is acceptable only if its dimension/subtype clearly differs).
- **Rejected pattern repeats** — `experiments-analyses.jsonl` shows an experiment on this exact `(workflow_id, primary_dimension, subtype)` combination whose latest `decision` is `REJECT` and whose `reason_code`/`reason` still applies (same class of change) — **do not repeat rejected mutations**. Consult `ledger.json` for the same rule using your own proposal history, including proposals whose issue was closed without being applied.
- **Authority expansion** — the candidate mutation would broaden permissions, network access, tool scope, or write authority beyond what the workflow already has (e.g. narrow→broad tool scope, adding a new write permission). Only ever propose mutations that hold authority equal to or narrower than the current baseline.
- **Multi-dimension mutation** — the only mutation you can honestly justify touches more than one `primary_dimension` at once. Narrow it to one dimension/subtype, or skip.
- **Prerequisite bug blocks a valid experiment** — a genuine bug must be fixed before this dimension/subtype could be tested meaningfully. That fix is a direct, deterministic change, not an experiment, and must happen (in a separate change) before this proposal is viable.

**Never select a candidate merely because its `failure_count` is high.** A high failure/error count is a starting signal, not sufficient justification on its own — corroborate it with a plausible causal `mechanism` (Phase 3) traceable to a specific evidence field before treating a workflow as eligible.

If **no** eligible workflow remains after applying these rules, or no falsifiable one-dimension hypothesis can be honestly justified from the evidence, call the `noop` safe output explaining exactly which rule eliminated every candidate (or why the remaining candidates lack sufficient evidence) and stop. Do not force a low-quality proposal.

## Phase 2 — Select One Target, One Primary Dimension, and One Subtype

Pick the single strongest candidate (highest `failure_count`/`avg_aic`/`total_error_count` in `per-workflow-summary.json` **corroborated by a plausible mechanism**, or a workflow with a `PROMOTE`d pattern in `experiments-analyses.jsonl` worth adapting elsewhere; also weigh recurring/frequent traffic so `min_samples: 20` per arm is reachable in a reasonable window). Then pick **exactly one** `primary_dimension` from these six harness control surfaces — never combine two — and, within it, **exactly one** narrow `subtype`:

| `primary_dimension` | gh-aw control surface | Permitted `subtype` values (pick exactly one) |
|---|---|---|
| `context assembly` | Prompt structure, imports, DataOps, and context compression | `deterministic_prefetch`, `context_filtering`, `context_ordering`, `remove_redundant_context` |
| `tool interaction` | Tool selection, `gh-proxy`/`cli-proxy`, permissions, and result filtering | `gh_proxy_migration`, `deterministic_precondition`, `remove_unnecessary_tool`, `retry_policy` |
| `generation control` | Engine and model selection, `max-turns`, and `timeout-minutes` | `prompt_tightening`, `model_configuration`, `turn_budget` |
| `orchestration` | Deterministic steps, sub-agents, planning, execution, and refinement | `sub_agent_delegation`, `cheap_triage`, `split_planning_execution` |
| `memory management` | `cache-memory`, `repo-memory`, summaries, and stale-context removal | `compact_persistent_state`, `remove_irrelevant_memory_injection` |
| `output processing` | Safe outputs, schema validation, fallbacks, and `noop` behavior | `deterministic_post_processing`, `structured_output_simplification` |

Every proposal names **exactly one** `primary_dimension` and **exactly one** `subtype` from that dimension's row — never a subtype from another dimension's list, and never more than one of either. This is the complete permitted taxonomy; do not invent dimensions or subtypes outside it.

Prefer **adapting a pattern already validated as `PROMOTE`d** for a similar workflow (cite the promoted experiment's `workflow_id`, `primary_dimension`, and `subtype` as evidence) over inventing an untested mutation from scratch.

## Phase 3 — Write the Structured Hypothesis Contract

Before touching any file, write out this contract (you will also paste it into the issue body). Every field must be traceable to a specific file/line in the pre-gathered evidence — never invent numbers, and never invent an evidence citation.

```yaml
workflow_target: <path to .github/workflows/NAME.md>
primary_dimension: <exactly one of: context assembly | tool interaction | generation control | orchestration | memory management | output processing>
subtype: <exactly one narrow subtype from that dimension's row in Phase 2>
observation: "<one sentence: the specific pattern in the pre-gathered evidence that motivates looking at this workflow/dimension, e.g. 'avg_aic=42.3 vs fleet median 11.0 in per-workflow-summary.json, with a fixed 20k-token schema import on every run'>"
mutation: "<one sentence: the single minimal, isolated change that implements subtype for primary_dimension>"
mechanism: "<one sentence: the precise causal chain by which the candidate mutation is expected to move primary_metric, e.g. 'trimming the unused half of the imported schema reduces prompt-cache-miss tokens without touching agent reasoning'>"
primary_metric: "<grader:<id> | eval:<id> | a named deterministic task-quality outcome the workflow already emits>"
expected_direction: "<increase | decrease>, stated in primary_metric's own units"
minimum_effect: "<the smallest effect, in primary_metric's own units, worth promoting a candidate for, e.g. '>=0.10 absolute pass-rate increase' or '<=-2 count decrease in tool-failure-count'>"
guardrails:
  - name: <grader:<id> | eval:<id> — never a bare/native metric name>
    threshold: "<=<value>" # or ">=<value>"
  - name: <a second guardrail protecting correctness equivalence, required when primary_metric is a cost/efficiency signal>
    threshold: "<=<value>" # or ">=<value>"
evidence:
  - source: <experiments-analyses.jsonl | per-workflow-summary.json | experiments-list.json | ledger.json | one gh aw audit <run-id> call>
    citation: "<concrete field/value you are relying on, e.g. 'failure_count=6/14 runs, avg_aic=42.3'>"
  - source: <a second, independent source, included whenever genuinely available>
    citation: "<concrete field/value from that second source>"
control: "<one sentence: unchanged behavior, byte-identical outside the new experiments wrapper>"
candidate: "<one sentence: restates mutation as the precise control-vs-candidate delta>"
hypothesis:                    # optional — include only if a directional H0/H1 statement adds clarity beyond mechanism/expected_direction
  h0: "No meaningful difference between control and candidate on primary_metric."
  h1: "<one falsifiable, directional claim in the same units as expected_direction/minimum_effect>"
min_samples: 20
rollback_plan: "<exact steps to revert if PROMOTEd change misbehaves later, e.g. 'git revert <the commit applying this patch>; no other files depend on this change'>"
```

**Evidence rule**: cite **at least one** concrete, independent evidence entry always — never invent a number or a citation. Cite **at least two** independent evidence entries whenever a second one is genuinely available in the pre-gathered files (e.g. `per-workflow-summary.json` **and** `experiments-analyses.jsonl`, or a promoted pattern **and** the 14-day aggregate). Never fabricate a second citation just to satisfy this preference — one honest citation is required, two is preferred, none is ever invented.

**Metric priority policy** (in this order — pick the highest-priority option the target workflow can actually support; do not skip straight to the bottom):

1. **A real deterministic task-quality outcome** already produced by the workflow itself — a domain-specific pass/fail signal the workflow's own safe-outputs or downstream process emits (e.g. a labeled/merged PR outcome, a matched ground-truth value, an existing `grader:<id>` this workflow already declares). Prefer this whenever it exists.
2. **A workflow-specific deterministic grader** — add (or reuse) a custom `graders:` entry (an inline `script:` grader) that measures this workflow's own domain-specific correctness signal, then reference it as `grader:<id>`. When the signal is operational repository attainment, use the `operational value designer` skill (`/operational-value-designer`) to infer operational value from the workflow and shape the evaluator contract.
3. **A robust general grader or eval** — one of the built-in `grader:<id>` values (`tool-success-rate`, `tool-failure-count`, `retries`, `loops`, `trajectory-efficiency`, `execution-step-count`, `execution-duration`, `working-set-rebuild-factor`, `context-growth`, `artifact-production`, declared under `graders:` in the target workflow), or an `eval:<id>` BinEval question declared under `evals:`.

Do **not** mandate `eval:<id>` as the only allowed primary metric — options 1 and 2 above are preferred whenever available; fall back to option 3 only when neither a deterministic task-quality outcome nor a workflow-specific grader is practical.

`primary_metric` may be a **cost/efficiency** signal (e.g. `grader:execution-duration`) **only when** a guardrail entry protects correctness equivalence with a **supported** `grader:<id>` or `eval:<id>` observation source. This keeps a cheap-but-degraded candidate from winning purely on cost.

`guardrails`/`guardrail_metrics` entries **must** reference `grader:<id>` (with that grader declared, even as `graders: {}`, in the target workflow's own frontmatter) or `eval:<id>` (with that question declared under `evals:`) — **never** a bare/native metric name like `run_success_rate`, `ai_credits_total`, or `execution-duration` without the `grader:` prefix. Unsupported guardrail names never resolve to an observation source, so `gh aw experiments analyze` returns `EXTEND`/`guardrail_unsupported` forever and the experiment can never reach `PROMOTE`/`REJECT`. Any measurement instrumentation you add (a grader, an eval, or logging) applies **identically** to both the control and candidate branches — it is observability, never part of the treatment itself, and must sit outside the conditional block that isolates the candidate-only mutation (see the templating example in Phase 4, step 3, below).

## Phase 4 — Apply the Minimal Edit

Edit **only** `workflow_target`'s `.md` file (never its `.lock.yml` by hand — that is regenerated by compiling):

1. Add an `experiments:` entry using the **balanced, non-ramped** template (this is required — do not use `continual` ramps, which start unbalanced, and do not set `weight`, which breaks the balanced-allocation requirement):
   ```yaml
   experiments:
     <primary_dimension_subtype>_v1:
       variants: [control, candidate]
       description: "<one sentence, from the hypothesis contract>"
       hypothesis: "H0: <h0>. H1: <h1>."   # omit if the optional hypothesis contract field was not used
       metric: "<primary_metric from Phase 3, e.g. grader:tool-failure-count or eval:<id>>"
       guardrail_metrics:
         - name: "<grader:<id> or eval:<id> — never a bare/native metric name>"
           threshold: "<=<value>" # or ">=<value>"
         - name: "<second guardrail from Phase 3, required when metric is a cost/efficiency signal>"
           threshold: "<=<value>" # or ">=<value>"
       min_samples: 20
       analysis_type: <t_test | mann_whitney | proportion_test | bayesian_ab>
       decision:
         minimum_effect: <primary_metric-units value from Phase 3, absolute (not a percentage)>
         regression_tolerance: <primary_metric-units value, absolute; may equal minimum_effect>
         confidence: 0.95
       tags: ["harness_dimension:<primary_dimension>", "harness_subtype:<subtype>"]
   ```
   `analysis_type` guidance: use `proportion_test` for `eval:<id>`/YES-NO or pass/fail rate metrics; use `mann_whitney` as the safe default for skewed/small-sample continuous metrics (most grader counts/durations/AIC); use `t_test` only for a well-behaved, roughly normal continuous measure with ample samples; use `bayesian_ab` only when you deliberately want a probability-of-superiority framing instead of a p-value. Express `minimum_effect`/`regression_tolerance` as **absolute primary-metric units**, matching `minimum_effect` from the Phase 3 contract; `confidence: 0.95` is the required value. The `tags` entry records `primary_dimension`/`subtype` so future runs (and Phase 1's equivalent-active-experiment check) can detect duplicate dimension/subtype coverage on this workflow.
2. If `primary_metric` or a guardrail references a `grader:<id>` that the target workflow does not already declare, add the minimal `graders:` block needed to support it (`graders: {}` to enable all built-ins, or a single inline `script:` grader for a workflow-specific deterministic outcome) alongside the `experiments:` block. For `graders.operational-value`, use the `operational value designer` skill (`/operational-value-designer`) to infer operational value from the agentic workflow before finalizing the grader contract.
3. Wrap **only** the isolated section that implements the subtype in a template conditional (see `.github/aw/experiments.md`), placing each separator on its own line, e.g.:
   ```
   {{#if experiments.<primary_dimension_subtype>_v1 == 'candidate' }}
   ...candidate-only content implementing exactly the one described mutation...
   {{#else}}
   ...byte-identical control content, unchanged from before your edit...
   {{#endif}}
   ```
   so that the `else` (control) branch renders **byte-identical** prompt/config content to what existed before your change, and the `if` (candidate) branch differs by exactly the one described mutation. Never write the internal `__GH_AW_EXPERIMENTS__*` env-var form directly. Any measurement instrumentation (grader/eval/logging) added to support the metric applies identically to both branches and lives outside this conditional. Do not touch any other section of the file.
4. If the target workflow has no `evals:` entry and you chose an `eval:<id>` primary metric or guardrail, add exactly one minimal `evals:` question that tests the specific behavior your candidate is meant to improve.

## Phase 5 — Compile and Validate the Candidate (mandatory gate before proposing)

Run, from the repository root (the `gh-aw` binary was already built for you):

```bash
./gh-aw compile <workflow_target_basename_without_.md> --strict
./gh-aw compile <workflow_target_basename_without_.md> --strict --validate
```

Both must exit `0`. Then sanity-check the generated `.lock.yml`:
- `git diff --stat` shows **only** `workflow_target`'s `.md` and its matching `.lock.yml` changed (no other file). If anything else changed, stop and investigate before proposing.
- The compiled lock file contains the new experiment name, confirming the block was accepted and wired into the runtime templating logic.

**If compile or validation fails for any reason**, do not attempt unlimited retries: try at most one corrective fix, re-run both commands once more, and if it still fails, `git checkout` your edit to discard it and still emit the `create-issue` proposal, but with the exact error encountered recorded in `Validation` (see Phase 6), instead of claiming a clean compile.

## Phase 6 — Emit the Proposal Issue

This workflow has no write authority over workflow files, so the proposal is always delivered as an issue carrying the exact patch for a human to apply — never as a pull request.

### `create-issue`

Title: `<workflow_target basename> — <primary_dimension>/<subtype> A/B harness experiment`

Body — include the following sections **in this exact order** every time. `Summary` is optional and may be added without disturbing the relative order of the other nine (required) headings:

```markdown
### Summary
(optional) One paragraph: what workflow, what primary_dimension/subtype, why now (cite the strongest evidence signal).

### Observation
The specific pattern in the pre-gathered evidence that motivates this proposal (the hypothesis contract's `observation` field).

### Hypothesis
The `mechanism` (why the mutation should move `primary_metric`), `expected_direction`, and `minimum_effect` in `primary_metric`'s own units. Include `H0`/`H1` only if the optional hypothesis field was used.

### Candidate Mutation
`primary_dimension` and `subtype` (exactly one of each), and a precise description of the control vs. candidate difference (`control`/`candidate` fields — must be one isolated change).

### Experiment
The exact `experiments:` YAML block added (fenced code block), including `variants`, `metric`, `guardrail_metrics`, `min_samples: 20`, `analysis_type`, and `decision` (`minimum_effect`, `regression_tolerance`, `confidence: 0.95`). Include the `graders:` block too if one was added.

### Guardrails
Table of guardrail metric (`grader:<id>`/`eval:<id>`) → threshold → why it protects against regression, including — when `primary_metric` is a cost/efficiency signal — which guardrail protects correctness equivalence.

### Expected Economics
Concrete, evidence-grounded expectations: the target workflow's current run cadence/traffic (cited from `per-workflow-summary.json`), the estimated time to accumulate `min_samples: 20` usable observations per arm, and the expected cost/efficiency delta of the candidate relative to control.

### Validation
Confirmation that `gh aw compile <target> --strict` and `--strict --validate` both exited 0, and that `git diff --stat` touched only the two expected files. If Phase 5 could not produce a clean compile, state the exact error encountered here instead.

### Interpretation
State explicitly: applying this patch only *starts* the experiment; no decision is interpreted here. Once `min_samples` is reached, `gh aw experiments analyze <target>` computes the deterministic `EXTEND`/`PROMOTE`/`REJECT`/`INCONCLUSIVE` decision unchanged — this workflow never recomputes, reinterprets, or overrides that decision, and any eventual `PROMOTE` still requires a separate, human-reviewed change through the existing `daily-experiment-report` deterministic decision engine. This workflow never merges anything itself.

### Rollback
Exact steps to revert (revert the commit that applied this patch; confirm no other files/branches depend on this change).

### Manual Patch (apply by hand)
```diff
<the exact unified diff you produced, from `git diff`>
```

### Application Plan
1. Save the diff above as `proposal.patch`.
2. `git apply proposal.patch`
3. `gh aw compile <target> --strict --validate`
4. Open a PR manually if compilation succeeds.
```

## Phase 7 — Update the Ledger

Append one entry to `/tmp/gh-aw/repo-memory/default/harness-proposals.json` (create the array if the file does not exist) recording `{"date": "<YYYY-MM-DD>", "workflow_id": "<target basename without .md>", "primary_dimension": "<primary_dimension>", "subtype": "<subtype>", "experiment_name": "<name>", "pr_or_issue": "issue", "decision": null}`. This file is committed automatically by repo-memory after your turn — do not attempt to `git commit`/`git push` it yourself.

## Hard Rules (never violate)

- Exactly **one** target workflow, exactly **one** `primary_dimension` with exactly **one** `subtype` (from the six-dimension taxonomy in Phase 2), exactly **one** proposal per run.
- Never edit a workflow's `.lock.yml` by hand — only ever produce it via `gh aw compile`.
- Never edit `shared/*.md`, this proposer's own file, `source:`-managed workflows, or any file outside `.github/workflows/*.md`/`*.lock.yml`.
- Never set `weight` or use `continual` ramps — allocation must be balanced 50/50 via a plain two-variant `variants:` list.
- Never propose a mutation that expands permissions, network access, tool scope, or write authority beyond the workflow's current baseline (no authority expansion).
- Never make `primary_metric` or any `guardrail_metrics`/`guardrails` entry a bare/native metric name (e.g. `run_success_rate`, `ai_credits_total`, `execution-duration` without a prefix) — every metric reference must be `grader:<id>` or `eval:<id>` so it resolves to a real observation source. Prefer, in order: a real deterministic task-quality outcome, then a workflow-specific deterministic grader, then a general built-in grader/eval; cost/efficiency may be primary only when a supported guardrail protects correctness equivalence.
- Never compute, recompute, or reinterpret `EXTEND`/`PROMOTE`/`REJECT`/`INCONCLUSIVE` — treat any decision found in evidence as final and immutable.
- Never promote, merge, or auto-apply anything. The proposal is always an issue carrying a patch for a human to review and apply.
- Never select a candidate merely because `failure_count` is high, without a corroborating mechanism; never start a second experiment equivalent to one already active; never propose on a currently-broken workflow, an obvious direct fix, a workflow with no measurable success criterion, or a target whose real bug must be fixed first.
- If nothing eligible and falsifiable can be justified, call `noop` — do not force a proposal.
