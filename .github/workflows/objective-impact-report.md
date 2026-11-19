---
private: true
engine:
  id: copilot
  copilot-sdk: true
emoji: 📊
description: Report workflow delivery, adoption, measured value evidence, and AI Credit efficiency separately.
on:
  workflow_dispatch:
permissions:
  contents: read
  pull-requests: read
  actions: read
  issues: read
cache:
  - key: objective-impact-report-cache-${{ github.run_id }}
    name: Save objective impact report dataset cache
    path: /tmp/gh-aw/agent/objective-impact-report
    restore-keys: |
      objective-impact-report-cache-
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
    read-only: true
    toolsets: [default]
  bash:
    - "cat /tmp/gh-aw/agent/objective-impact-report/*.json"
    - "cat /tmp/gh-aw/agent/objective-impact-report/*.jsonl"
    - "jq *"
    - "ls /tmp/gh-aw/agent/objective-impact-report"
    - "head -n * /tmp/gh-aw/agent/objective-impact-report/*.json /tmp/gh-aw/agent/objective-impact-report/*.jsonl"
pre-agent-steps:
  - name: Install gh-aw CLI
    uses: ./actions/setup-cli
    with:
      github-token: ${{ secrets.GITHUB_TOKEN }}
  - name: Prepare deterministic impact datasets
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      EXPR_GITHUB_REPOSITORY: ${{ github.repository }}
    run: bash scripts/prepare-objective-impact-report-dataset.sh
  - name: Prepare safe-output issue lifecycle observations
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      EXPR_GITHUB_REPOSITORY: ${{ github.repository }}
    run: node actions/setup/js/prepare_objective_impact_lifecycle.cjs
safe-outputs:
  close-issue:
    required-title-prefix: "Impact Efficiency Report - "
    target: "*"
    max: 1
  create-issue:
    title-prefix: "Impact Efficiency Report - "
    max: 1
imports:
  - shared/reporting.md
---

### Impact Efficiency Report

Report delivery, adoption, and measured attributed value as separate layers.
Use deterministic evidence; do not use an LLM judge or invent a combined score.

#### Inputs

Read `/tmp/gh-aw/agent/objective-impact-report/`:

- `dataset-manifest.json` and `run-context.json` for scope and provenance.
- `workflow-logs.json` for selected runs and recorded measurements.
- `aic-by-workflow.json` for workflow AIC across the analysis window.
- `safe-output-issue-evaluations.jsonl` and `safe-output-issue-summary.json`
  for gh-aw delivery/adoption observations and coverage.
- `merged-prs-linked.json`, `closed-unmerged-prs-linked.json`, and
  `objective-mapping.json` only for repository/objective context.

Use the current `gh aw logs --adoption` classifications directly. Never infer
human adoption from successful creation, closure, a bot merge, or label weights.
Distinguish `explicit_human`, `automation`, and `fallback_delivery`; only
`explicit_human` is human adoption. Infrastructure
`not_applicable` records are not repository value.

An empty evaluations file is valid only when the summary establishes evaluated
empty evidence. Preserve partial observations. Report selected/evaluated/
unavailable/partial run counts, each run error, and item-level availability errors.
Do not refetch unless an input is missing, malformed, or its manifest explicitly
requires a fallback. Stop and report unavailable evidence if no safe fallback
exists; missing data is not zero.

#### Measurement

For each producing workflow, report delivered issue effects, adopted effects
split by acceptance kind, pending/rejected/lifecycle/unknown counts, and AIC.
Use source run IDs and exact resource URLs for attribution; do not count repeated
snapshots as additional deliveries. State that the issue dataset is not a census
of all safe-output types.

Use `aic-by-workflow.json` first, then recorded per-run AIC when that aggregate is
unavailable. Keep the same analysis window. Missing cost is unknown, not zero;
do not allocate a workflow's full cost repeatedly to individual effects.
Report AIC per adopted output only with adequate cost and adoption coverage.
It is an efficiency ratio, not a value score.

Report recorded `graders.operational-value` results separately, preserving each
metric's ID, native unit, workflow revision, and availability. Do not compare or
sum incompatible units. Attributed value additionally requires a declared
measurement, attribution evidence, and survival through the complete post-adoption
window. If those proofs are absent, state **Attributed value: unavailable**.
Current survival observations, acceptance counts, objective-label weights, and
run-scoped grader results alone do not prove longitudinal attributed value.
Never rewrite historical grader results.

#### Publication

Search for today's open `Impact Efficiency Report - YYYY-MM-DD` issue.
If present, close it with a replacement explanation before creating one report
with that title. Use `###` section headings and progressive disclosure.

Include a short executive summary, a per-workflow delivery/adoption/AIC table,
a separately qualified measured-value section, and data-quality gaps. Link
representative effects and source runs. Rank efficiency only among comparable
output classes and evidence coverage; do not rank workflows by invented value.
