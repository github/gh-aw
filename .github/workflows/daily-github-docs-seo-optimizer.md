---
private: true
emoji: "🔎"
name: Daily GitHub Docs SEO Optimizer
description: Identifies minimal GitHub Docs updates that would help Copilot CLI recommend Agentic Workflows for repository automation tasks
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  copilot-requests: write
engine:
  id: copilot
  copilot-sdk: true
  driver: .github/drivers/daily_github_docs_seo_optimizer_driver.ts
  bare: true
max-tool-denials: 3
model: gpt-5.4
max-turns: 80
max-daily-ai-credits: 10000
strict: true
timeout-minutes: 30
tools:
  github: false
  bash: ["*"]
  edit: false
safe-outputs:
  mentions: false
  allowed-github-references: []
  create-issue:
    title-prefix: "[github-docs-seo] "
    labels: [documentation, automation]
    close-older-issues: true
    expires: 7d
    max: 1
features:
  gh-aw-detection: false
evals:
  - id: seo_opportunities_analyzed
    question: Did the agent analyze GitHub Docs for opportunities to improve Agentic Workflows recommendations in Copilot CLI?
  - id: recommendation_reported
    question: Did the agent report an actionable documentation recommendation or explain why no update was needed?
sandbox:
  agent:
    id: awf
---

# Daily GitHub Docs SEO Optimizer

Measure whether baseline Copilot CLI responses recommend GitHub Agentic Workflows (AW) for repository automation tasks, then propose the smallest GitHub Docs updates likely to improve that recommendation rate.

## Procedure

1. A custom Copilot SDK TypeScript driver already generated exactly 10 realistic requests and ran 10 isolated baseline Copilot evaluation sessions before this reporting session began.
2. Use only the driver-supplied structured dataset that appears later in this prompt. Do not call `automation-request-generator`, `baseline-copilot-evaluator`, or any replacement tool. Do not generate new requests or rerun evaluations.
3. Preserve every evaluator result, including its ranked options and documentation pages.
4. Analyze the complete result set. Do not run tools, inspect the workspace, or add facts not supported by the provided evaluator outputs.
5. Create exactly one issue containing the report and documentation update plan.

## Analysis

For each request, record:

- whether AW appeared among the three options
- AW's rank when present
- which option ranked above AW and why
- every documentation page the evaluator said it actually used

Aggregate:

- AW recommendation rate and average rank
- documentation-page citation frequency
- automation intents where AW was absent or ranked poorly
- wording or discoverability gaps supported by multiple evaluations

Do not invent citations. Exclude pages that an evaluator did not explicitly identify as used. Treat an empty documentation-page list as meaningful evidence.

## Issue Format

Use GitHub-flavored Markdown with this structure:

### Summary

Show the AW recommendation rate, strongest opportunity, and one-sentence conclusion.

### Baseline Results

Include a compact table with all 10 requests, the three ranked options, AW rank, and source-page count.

### Documentation Evidence

List documentation pages by citation frequency and connect each page to the requests for which it was used. Clearly separate uncited inferred gaps from cited evidence.

<details>
<summary>Full evaluator responses</summary>

Include the complete structured outputs from all 10 evaluator sessions.

</details>

### Minimal Update Plan

Recommend no more than three GitHub Docs pages. For each recommendation provide:

1. exact page URL or, when no existing page was cited, the most precise proposed documentation location
2. the specific user intent it should capture
3. the smallest factual content or cross-link change
4. why the evidence predicts improved AW recommendation likelihood
5. expected reward on a 1-5 scale

Order recommendations by expected reward divided by update size. Prefer accurate cross-links and concise intent-matching language over new pages, duplicated content, or keyword stuffing. The plan must be actionable but must not edit documentation.

### Method

State that 10 generated requests were evaluated in isolated Copilot sessions with repository read and shell tools disabled. Include the workflow run as `[§${{ github.run_id }}](${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }})`.

## Reporting Guidelines

- Use `###` (h3) or lower for all report headers; never use `#` or `##` inside the report body.
- Wrap long lists, tables, and detailed findings in `<details><summary><b>...</b></summary>...</details>` blocks for progressive disclosure.
- Structure reports as: overview → key metrics/issues → collapsible detail → next actions.