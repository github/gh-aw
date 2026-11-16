---
private: true
engine:
  id: copilot
  copilot-sdk: true
emoji: 📊
description: Executive impact efficiency report from workflow adoptions tied to tracked objectives.
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
  - name: Prepare safe-output issue evaluations
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      EXPR_GITHUB_REPOSITORY: ${{ github.repository }}
    run: node actions/setup/js/prepare_objective_impact_adoptions.cjs
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

#### Required Inputs (already precomputed)

Use these deterministic files first:

- /tmp/gh-aw/agent/objective-impact-report/dataset-manifest.json
- /tmp/gh-aw/agent/objective-impact-report/run-context.json
- /tmp/gh-aw/agent/objective-impact-report/objective-mapping.json
- /tmp/gh-aw/agent/objective-impact-report/workflow-logs.json
- /tmp/gh-aw/agent/objective-impact-report/aic-by-workflow.json
- /tmp/gh-aw/agent/objective-impact-report/merged-prs-linked.json
- /tmp/gh-aw/agent/objective-impact-report/closed-unmerged-prs-linked.json
- /tmp/gh-aw/agent/objective-impact-report/safe-output-issue-adoptions.jsonl
- /tmp/gh-aw/agent/objective-impact-report/safe-output-issue-summary.json

`safe-output-issue-adoptions.jsonl` contains the current `gh aw logs --adoptions --json` classifications, evidence, workflow attribution, and any available objective attribution. Use these fields directly; never reclassify acceptance from successful issue creation or closure. If objective attribution is unavailable, use non-empty optional `*-with-objective.json` files or compute objective mapping from linked/root issue labels.

Read `safe-output-issue-summary.json` for coverage. When `partial` is true, retain available observations, label safe-output adoption totals and derived impact metrics as partial, and report `evaluated_run_count` / `selected_run_count`, `unavailable_run_count`, and each `run_errors` entry's run ID and `eval_error` in Data Quality. Missing evidence is unknown coverage, not zero adoptions or rejected work. If all selected runs are unavailable, report safe-output adoptions and their impact as unavailable; distinguish this from fully evaluated runs with verified empty adoptions.

Do **not** re-fetch these datasets with GitHub tools unless a required file is missing, empty, or fails JSON parsing. An empty `safe-output-issue-adoptions.jsonl` is valid when the summary reports zero issue adoptions; use its coverage fields to distinguish verified empty results from unavailable evidence rather than re-fetching.

#### Goal

Produce a comprehensive executive report on what work was performed, what AIC tokens were spent on, which adoptions delivered the highest impact, and which workflows contributed that impact. The report must clearly answer: *What did we build, fix, and ship, what was the most impactful work in the repository, which workflows drove that impact, and was it worth the cost?*

Focus only on **pull request adoptions** and **safe output adoptions** (issues created or closed via the safe-output mechanism). Other adoption types are excluded because their acceptance criteria are not yet well-defined and most remain pending.

Use this model:

```text
Adoption = a PR or safe-output issue produced by a GitHub Agentic Workflow run
Objective Value = numeric value from the repository objective-mapping configuration applied to traced root labels
Adoption Indicator = 1 for accepted/delivered adoptions, 0 otherwise
Adoption Value = Adoption Indicator × Objective Value
Impact Efficiency = Σ Adoption Value / AI Credits
```

Treat AI Credits as total model-credit cost aggregated per workflow across the full analysis window, not just the subset of runs that produced the analyzed adoptions.
Start with `/tmp/gh-aw/agent/objective-impact-report/aic-by-workflow.json` as the primary AIC source, and `/tmp/gh-aw/agent/objective-impact-report/workflow-logs.json` and `/tmp/gh-aw/agent/objective-impact-report/dataset-manifest.json` as additional context for run details and source provenance.
When available, use deterministic precomputed run data that already includes each run's `aic` field.
Prefer existing gh-aw outputs that already surface `aic`, such as pre-downloaded `gh aw logs --json` data or audit/log artifacts derived from the same run summaries.
Only fall back to MCP or other live retrieval if deterministic precomputed AIC inputs are unavailable or the manifest says the fallback is still required.
Use the same time window for AIC as for adoptions.

Perform direct workflow attribution for every analyzed adoption.
Adoptions deliver value.
Objectives provide context and importance.
Workflows explain where delivered value came from.
AI Credits provide cost.
Do not use an LLM judge.

#### AIC Source of Truth

Resolve AI Credits in this order:

1. **Primary: `/tmp/gh-aw/agent/objective-impact-report/aic-by-workflow.json`** — aggregated per-workflow AIC from daily token-audit memory snapshots covering the analysis window. Each entry has `workflow_name`, `total_aic`, and `run_count`. Use this as the denominator for overall and per-workflow Impact Efficiency. Check `dataset-manifest.json` for `aic_by_workflow_source` and `aic_by_workflow_snapshot_count` to understand coverage. The `aic-by-workflow.json` data is pre-aggregated across all available daily snapshots within the window and is the most reliable AIC source.
2. Deterministic precomputed `/tmp/gh-aw/agent/objective-impact-report/workflow-logs.json` data with per-run `aic` (use only when `aic-by-workflow.json` is unavailable or has `source: "none"`)
3. MCP or other live retrieval only as a documented fallback

When computing total AI Credits for the report:
- Sum `total_aic` across all entries in `aic-by-workflow.json` for the repository-wide total AIC
- For per-workflow AIC, look up the workflow by name in `aic-by-workflow.json`
- If a workflow has no entry in `aic-by-workflow.json`, treat its AIC as unknown (not zero) and add a note in the Data Quality section of the report listing which workflows had no AIC data available.

If a run's `aic` field is missing or null, treat it as `0` and count it as missing-cost data in the report.

#### Scope

Analyze only the following adoption types from the last 180 days:

- **Pull request adoptions**: PRs created by GitHub Agentic Workflow runs **that have a linked closing issue** (`Closes #N`). Accepted = merged. Rejected = closed without merge. Skip open (pending) PRs. **Exclude entirely** any PR without a traceable linked issue — do not fall back to PR labels.
- **Safe output adoptions**: issues created or closed by workflow runs via the safe-output mechanism. Accepted only when `gh aw logs --adoptions` reports `adoption_status: "accepted"`. Exclude unresolved, error, and lifecycle-only classifications from delivered-value totals and report their counts separately.

Exclude all other adoption types (direct issue adoptions, comments, discussions, etc.). These are omitted because their acceptance criteria are incomplete and most are left pending, which would distort the metric.

#### Objective value mapping

Objective values should be resolved from deterministic inputs whenever available.

For pull request adoptions, use:

- `/tmp/gh-aw/agent/objective-impact-report/merged-prs-linked.json`
- `/tmp/gh-aw/agent/objective-impact-report/closed-unmerged-prs-linked.json`

If optional `/tmp/gh-aw/agent/objective-impact-report/merged-prs-with-objective.json` and `/tmp/gh-aw/agent/objective-impact-report/closed-unmerged-prs-with-objective.json` exist and are non-empty, use their precomputed `objective_value`, `objective_labels`, `root_issue_numbers`, `root_issue_labels`, and `attribution_source` fields directly.

For safe-output issue adoptions, use adoption identity and status from:

- `/tmp/gh-aw/agent/objective-impact-report/safe-output-issue-adoptions.jsonl`

The mapping uses the `adoption/` root-level resolver (mirrors `intent.Resolver.ResolvePullRequest` in Go):
- For PRs with exactly one closing issue (`attribution_source: "closing_issue"`): objective computed from the closing issue's labels.
- For PRs with no closing issue (`attribution_source: "artifact_labels"` or `"none"`): **exclude from analysis** — do not fall back to PR labels.
- For safe-output issues (`attribution_source: "issue_labels"`): objective computed from the issue's own labels fetched from the issue record.

If `objective_value` is `0` and the entry has root/issue labels present, mark the adoption as `unmapped` (no matching label in the mapping). If there are no root labels at all, mark it as excluded.

Do not invent fallback scoring rules such as milestone bonuses, project bonuses, or priority-to-points heuristics.

#### Adoption association rules

For each in-scope adoption, use the precomputed root-tracing results:

1. For pull-request adoptions, start from `linked_issue_numbers` in `merged-prs-linked.json` / `closed-unmerged-prs-linked.json`.
2. If an optional `*-with-objective.json` dataset exists and is non-empty, use its `attribution_source`, `root_issue_numbers`, `root_issue_labels`, `objective_value`, and `objective_labels` fields directly.
3. Otherwise, if `linked_issue_numbers` is empty, **exclude the PR from analysis entirely**. Do not fall back to PR labels. Count it in the "PRs excluded (no linked issue)" total.
4. Otherwise, resolve root issue labels from the linked issues (deterministic cache first; live issue lookup only when labels are missing), then compute objective mapping from those root labels.
5. For safe-output issue adoptions, use `/tmp/gh-aw/agent/objective-impact-report/safe-output-issue-adoptions.jsonl` as the primary source for adoption state, workflow attribution, issue identity, and available objective attribution; fetch issue labels only when objective attribution is missing.
6. Record traced issue numbers (`root_issue_numbers` when present, otherwise `linked_issue_numbers`) in the report as the audit trail.
7. If `objective_value` is `0` and labels are present, mark the adoption as `unmapped`, exclude it from `Σ Adoption Value`, and report it separately.

#### Computation

For each in-scope adoption:

```text
Adoption Indicator:
  PR adoption:           1 if merged, 0 if closed without merge (open PRs excluded)
  Safe output adoption:  1 if CLI adoption_status is accepted, 0 otherwise
Adoption Value = Adoption Indicator × Objective Value
```

Then compute:

```text
Accepted Adoption Count = count(adoptions where Adoption Indicator = 1)
Total Adoption Value    = sum(Adoption Value)
AI Credits             = sum(run.aic across analyzed runs)
Impact Efficiency      = Total Adoption Value / AI Credits  (value units per AI Credit; undefined when AI Credits = 0)
```

Also compute per-workflow attribution using the workflow run that directly produced each analyzed adoption:

```text
Workflow Contributed Value = sum(Adoption Value for adoptions produced by that workflow)
Workflow Accepted Adoptions = count(accepted, mapped adoptions produced by that workflow)
Workflow AI Credits        = sum(run.aic for analyzed runs of that workflow)
Workflow Value Share       = Workflow Contributed Value / Total Adoption Value
Workflow Impact Efficiency = Workflow Contributed Value / Workflow AI Credits
```

Use the workflow name from the producing run as the attribution key. If multiple runs from the same workflow produced analyzed adoptions, aggregate them together. If the workflow name cannot be resolved for an analyzed adoption, report it as unattributed and exclude it from workflow ranking totals while still counting the adoption in overall delivered-value totals.

If AI Credits is missing or zero, report that Impact Efficiency is not computable and explain whether credits data was unavailable or no credits were consumed in the analysis window.
If only some runs are missing `aic`, still compute the metric from the available values and explicitly report how many runs had missing cost data.

#### Report

Before creating the new report, search for an existing open issue titled:

```text
Impact Efficiency Report - YYYY-MM-DD
```

If one already exists for today:

1. Close that issue first with a brief comment explaining that it is being replaced by a freshly generated report for the same day.
2. Then create the new report issue.

Create one issue titled:

```text
Impact Efficiency Report - YYYY-MM-DD
```

Format the report for progressive disclosure:

- Begin with the compact `### Executive Summary` and keep the key takeaway, critical findings, and recommendations visible.
- Use `###` headings for report sections and `####` headings for subsections.
- Wrap verbose tables, long lists, or extended analysis in `<details><summary><b>View full findings</b></summary>` blocks.

The report must include:

### Executive Summary

Write 2–4 sentences that directly answer: *What did the agent work on, what was the highest-impact agentic work, which workflows contributed most to that impact, how efficiently were AIC tokens spent, and what high-impact work was delivered outside agentic workflows (if any)?* Highlight the most impactful objective categories, the workflows contributing the most value, and any significant gaps (e.g., large AIC spend with no mapped objective value).

### Summary

| Metric | Value |
|---|---:|

When a metric includes sub-counts, format the Value as `merged: X, closed: Y, open excluded: Z`.

Include:
- PRs analyzed with linked issue (merged / closed / excluded open)
- PRs excluded (no linked closing issue)
- Safe output adoptions analyzed
- Adoptions mapped to objectives
- Unmapped adoptions
- Accepted adoption count
- Total adoption value
- AI Credits
- Impact Efficiency

### Agentic Work by Objective

Group all **accepted, mapped** adoptions by objective category (the highest-value objective label from the mapping). For each category, list:

- Objective category name and its mapping value
- Number of accepted adoptions in this category
- Total adoption value contributed
- AIC consumed by adoptions in this category
- Impact Efficiency for this category (total adoption value / AIC consumed)
- Representative examples (up to 3 linked adoptions)

Sort categories by total adoption value descending. Also call out separately which category consumed the **most AIC** (highest denominator cost), so readers can see where budget was spent regardless of value delivered.

This section should make the most impactful work in the repository obvious at a glance.

### Which Workflows Drove That Impact

Group all analyzed adoptions by the workflow that directly produced them. For each workflow, list:

- Workflow name
- Number of accepted, mapped adoptions attributed to this workflow
- Total adoption value contributed
- Share of total delivered adoption value
- AIC consumed by this workflow's analyzed runs
- Workflow Impact Efficiency (contributed value / AIC consumed)
- Top objective categories this workflow contributed to
- Representative examples (up to 3 linked adoptions)

Sort workflows by total adoption value descending. Also call out separately:

- which workflow contributed the **most total value**
- which workflow contributed the **largest share** of delivered value
- which workflow consumed the **most AIC**

If any analyzed adoptions cannot be attributed to a workflow, report an unattributed bucket with counts and total adoption value, but do not rank it alongside named workflows.

### Top adoptions by adoption value

| Adoption | Workflow | Type | Root / Associated Objective | Objective Value | Adoption Value |
|---|---|---|---|---:|---:|

List the top 15 adoptions with highest Adoption Value. Include a link to the PR or issue.

### Unmapped adoptions

| Adoption | Type | Reason objective was not mapped |
|---|---|---|

Only include adoptions that were in scope (linked-issue PRs and safe-output issues) but had no matching label in the objective mapping. Do not include PRs that were excluded for lacking a linked issue — those are already counted in "PRs excluded".

### Interpretation

Compare:

- accepted adoption count alone
- Impact Efficiency

Explain which one better reflects meaningful delivered value relative to cost.

Call out the most significant findings:

- which objective category delivered the most value per AIC
- which workflow contributed the most delivered value
- which workflows consumed AIC with little or no mapped value

### Data quality

Mention missing or weak links in:

- PR root tracing and linked-closing-issue coverage (count of PRs excluded for lacking a linked issue)
- safe-output issue label mapping coverage in `.github/objective-mapping.json`
- workflow attribution coverage and any unattributed analyzed adoptions
- AI Credits availability

State whether AI Credits came from deterministic precomputed data or from a live fallback path.

If AI Credits are unavailable, still produce the delivered-value analysis and clearly state that the cost-normalized Impact Efficiency metric could not be computed.

### Human Work

This section is independent of AIC and the agentic efficiency analysis above. It captures pull requests merged in the analysis window that could not be attributed to any GitHub Agentic Workflow run in the deterministic logs.

Identify merged PRs from `/tmp/gh-aw/agent/objective-impact-report/merged-prs-linked.json` that have **no** matching run in `/tmp/gh-aw/agent/objective-impact-report/workflow-logs.json` (i.e., PRs whose author or head branch cannot be linked to any workflow run that produced an adoption). Treat these as human-authored contributions for reporting, but explicitly note that missing log coverage or attribution gaps can inflate this count.

For each human-authored merged PR that has a linked closing issue (non-empty `linked_issue_numbers`), use precomputed objective fields from `merged-prs-with-objective.json` when available; otherwise resolve issue labels from linked issues and apply `objective-mapping.json`. Group results by objective category (highest-value mapped label) and report:

- Objective category name and its mapping value
- Number of human-authored merged PRs in this category
- Total objective value contributed
- Representative examples (up to 3 linked PRs)

Also report:

- Total number of human-authored merged PRs identified in the analysis window
- Number with a linked closing issue vs. without
- Number mapped to an objective vs. unmapped

Sort categories by total objective value descending. Do **not** compute AIC or Impact Efficiency for this section — human work has no associated AI Credits cost.

#### Safe output

Use only `close-issue` and `create-issue`.