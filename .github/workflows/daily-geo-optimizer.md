---
private: true
emoji: "🌍"
description: Daily GEO (Generative Engine Optimization) audit of the README and documentation site using geo-optimizer-skill
on:
  schedule: daily
  workflow_dispatch:
max-daily-ai-credits: 10000
permissions:
  contents: read
  issues: read
  pull-requests: read
  discussions: read
  copilot-requests: write
tracker-id: daily-geo-optimizer
engine:
  id: copilot
  copilot-sdk: true
max-tool-denials: 3
strict: true
timeout-minutes: 30
tools:
  cli-proxy: true
  github:
    mode: gh-proxy
    toolsets: [default]
  bash:
    - "cat *"
    - "ls *"
    - "echo *"
    - "date *"
    - "jq *"
    - "find *"
    - "grep *"
if: needs.geo_audit.result == 'success'
jobs:
  geo_audit:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    env:
      REPOSITORY_URL: https://github.com/${{ github.repository }}
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7.0.1

      - name: Setup Python
        uses: actions/setup-python@v7
        with:
          python-version: "3.11"

      - name: Install geo-optimizer-skill
        run: pip install geo-optimizer-skill

      - name: Create results directory
        run: mkdir -p /tmp/gh-aw/agent/geo-optimizer

      - name: Audit documentation site homepage
        run: |
          geo audit --url https://github.github.com/gh-aw/ --format json \
            > /tmp/gh-aw/agent/geo-optimizer/docs-site-audit.json 2>&1 || true

      - name: Audit documentation sitemap
        run: |
          geo audit --sitemap https://github.github.com/gh-aw/sitemap.xml \
            --max-urls 20 --format json \
            > /tmp/gh-aw/agent/geo-optimizer/docs-sitemap-audit.json 2>&1 || true

      - name: Audit README via GitHub repository page
        run: |
          geo audit --url "$REPOSITORY_URL" --format json \
            > /tmp/gh-aw/agent/geo-optimizer/readme-audit.json 2>&1 || true

      - name: Verify documentation robots.txt
        run: |
          # runner-guard:ignore RGS-012 -- unauthenticated GET from the public documentation site; no secrets are sent.
          ROBOTS_URL="https://github.github.com/gh-aw/robots.txt"
          ROBOTS_BODY="/tmp/gh-aw/agent/geo-optimizer/docs-robots.txt"
          rm -f "$ROBOTS_BODY"
          CURL_METADATA="$(curl --silent --show-error --location --max-time 30 \
            --output "$ROBOTS_BODY" --write-out '%{http_code}\t%{content_type}' \
            "$ROBOTS_URL" || true)"
          IFS=$'\t' read -r HTTP_STATUS CONTENT_TYPE <<< "$CURL_METADATA"
          FOUND=false
          if [[ "$HTTP_STATUS" == "200" ]]; then
            FOUND=true
          else
            rm -f "$ROBOTS_BODY"
          fi
          jq -n \
            --arg url "$ROBOTS_URL" \
            --arg http_status "${HTTP_STATUS:-000}" \
            --arg content_type "$CONTENT_TYPE" \
            --argjson found "$FOUND" \
            '{url: $url, http_status: ($http_status | tonumber), content_type: $content_type, found: $found}' \
            > /tmp/gh-aw/agent/geo-optimizer/docs-robots-verification.json

      - name: Verify documentation AI discovery files
        run: |
          PROJECT_SITE_BASE_URL="https://github.github.com/gh-aw"
          CHECKS_JSONL="/tmp/gh-aw/agent/geo-optimizer/docs-ai-discovery-verification.jsonl"
          : > "$CHECKS_JSONL"

          check_url() {
            local key="$1"
            local path="$2"
            local body_path="$3"
            local url="${PROJECT_SITE_BASE_URL}/${path}"
            local curl_metadata
            local http_status
            local content_type
            local found=false

            rm -f "$body_path"
            # runner-guard:ignore RGS-012 -- unauthenticated GET from the public documentation site; no secrets are sent.
            curl_metadata="$(curl --silent --show-error --location --max-time 30 \
              --output "$body_path" --write-out '%{http_code}\t%{content_type}' \
              "$url" || true)"
            IFS=$'\t' read -r http_status content_type <<< "$curl_metadata"
            if [[ "$http_status" == "200" ]]; then
              found=true
            else
              rm -f "$body_path"
            fi
            jq -n \
              --arg key "$key" \
              --arg url "$url" \
              --arg http_status "${http_status:-000}" \
              --arg content_type "$content_type" \
              --arg body_path "$body_path" \
              --argjson found "$found" \
              '{
                key: $key,
                url: $url,
                http_status: ($http_status | tonumber),
                content_type: $content_type,
                found: $found,
                body_path: (if $found then $body_path else null end)
              }' >> "$CHECKS_JSONL"
          }

          check_url "llms_txt" "llms.txt" "/tmp/gh-aw/agent/geo-optimizer/docs-llms.txt"
          # Verify the project-scoped AI discovery signal that the docs site serves below /gh-aw.
          check_url "ai_txt" ".well-known/ai.txt" "/tmp/gh-aw/agent/geo-optimizer/docs-ai.txt"
          check_url "ai_summary_json" "ai/summary.json" "/tmp/gh-aw/agent/geo-optimizer/docs-ai-summary.json"
          check_url "ai_faq_json" "ai/faq.json" "/tmp/gh-aw/agent/geo-optimizer/docs-ai-faq.json"
          check_url "ai_service_json" "ai/service.json" "/tmp/gh-aw/agent/geo-optimizer/docs-ai-service.json"

          jq -s \
            --arg base_url "$PROJECT_SITE_BASE_URL" \
            '{
              base_url: $base_url,
              checks: ((map(. as $check | {($check.key): ($check | del(.key))}) | add) // {}),
              summary: {
                llms_txt_found: (
                  map(select(.key == "llms_txt")) |
                  if length > 0 then .[0].found else false end
                ),
                ai_discovery_found_count: (map(select(.key | startswith("ai_"))) | map(select(.found)) | length),
                ai_discovery_total: (map(select(.key | startswith("ai_"))) | length),
                ai_discovery_all_found: (
                  map(select(.key | startswith("ai_"))) as $ai_checks |
                  (($ai_checks | length) > 0 and ($ai_checks | all(.found)))
                )
              }
            }' "$CHECKS_JSONL" \
            > /tmp/gh-aw/agent/geo-optimizer/docs-ai-discovery-verification.json

      - name: Write audit metadata
        run: |
          python3 - <<'EOF'
          import datetime
          import json
          import os

          metadata = {
            "run_id": "${{ github.run_id }}",
            "timestamp": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d %H-%M-%S"),
            "docs_url": "https://github.github.com/gh-aw/",
            "readme_url": os.environ["REPOSITORY_URL"],
            "repository": os.environ["GITHUB_REPOSITORY"],
          }
          path = "/tmp/gh-aw/agent/geo-optimizer/metadata.json"
          with open(path, "w") as f:
            json.dump(metadata, f, indent=2)
          print(f"Wrote metadata to {path}")
          EOF

      - name: Upload geo-optimizer results
        uses: actions/upload-artifact@v7.0.1
        with:
          name: geo-optimizer-results
          path: /tmp/gh-aw/agent/geo-optimizer
          if-no-files-found: error
          retention-days: 3

steps:
  - name: Download geo-optimizer results
    uses: actions/download-artifact@v8.0.1
    with:
      name: geo-optimizer-results
      path: /tmp/gh-aw/agent/geo-optimizer

safe-outputs:
  create-issue:
    expires: 7d
    title-prefix: "[geo-optimizer] "
    labels: [geo, automation]
    assignees: [copilot]
    max: 1
  noop:

imports:
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[geo-optimizer] "
      expires: 3d

  - shared/otlp.md
features:
  gh-aw-detection: true
sandbox:
  agent:
    id: awf
evals:
  - id: geo_audit_performed
    question: Did the agent audit the README and documentation site using the geo-optimizer skill?
  - id: recommendations_or_pr_created
    question: Were GEO improvement recommendations produced or a PR created, or was noop used when no improvements were needed?
---

{{#runtime-import? .github/shared-instructions.md}}

# GEO Optimizer Daily Audit

You are the GEO (Generative Engine Optimization) audit agent. Your task is to analyze the audit results produced by `geo-optimizer-skill` and report on the AI visibility of the `${{ github.repository }}` README and documentation site.

## Context

- **Repository**: ${{ github.repository }}
- **Run ID**: ${{ github.run_id }}
- **Run URL**: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}

## Your Mission

Analyze the GEO audit results downloaded from the `geo-optimizer-results` artifact into `/tmp/gh-aw/agent/geo-optimizer/` and create a GitHub Discussion summarizing the findings and actionable recommendations to improve AI-engine citation coverage for this project.

---

## Phase 1: Load Audit Results

Read all JSON files from the results directory:

```bash
ls /tmp/gh-aw/agent/geo-optimizer/
```

- `docs-site-audit.json` — full GEO audit of `https://github.github.com/gh-aw/`
- `docs-sitemap-audit.json` — sitemap-wide audit of up to 20 documentation pages
- `docs-robots-verification.json` — authoritative HTTP check of the GitHub Pages project-site robots.txt
- `docs-robots.txt` — robots.txt response body when the authoritative check succeeds
- `docs-ai-discovery-verification.json` — authoritative HTTP checks for the GitHub Pages project-site llms.txt and AI discovery files
- `docs-llms.txt`, `docs-ai.txt`, `docs-ai-summary.json`, `docs-ai-faq.json`, `docs-ai-service.json` — response bodies when the authoritative checks succeed
- `readme-audit.json` — GEO audit of the GitHub repository homepage (README)
- `metadata.json` — run metadata (timestamp, URLs)

Use `cat` and `jq` to inspect the contents of each file. Focus on:
- Overall score (0–100) and score band (Critical / Foundation / Good / Excellent)
- Top issues and recommendations per category
- Citability score and methods
- Negative signals detected
- Scores broken down by area: Robots.txt, llms.txt, Schema JSON-LD, Meta Tags, Content, Brand & Entity, Signals, AI Discovery

For robots.txt findings on the documentation site, treat `docs-robots-verification.json` and
`docs-robots.txt` as authoritative. The GEO package probes the domain-root `/robots.txt`,
which does not preserve the `/gh-aw/` base path of this GitHub Pages project site. When the
verification reports `found: true`, do not report robots.txt as missing; inspect the response
body before recommending changes to crawler permissions.

For llms.txt and AI discovery findings on the documentation site, treat
`docs-ai-discovery-verification.json` as authoritative. The GEO package may probe the domain
root and miss the `/gh-aw/` base path of this GitHub Pages project site. When the authoritative
verification reports `summary.llms_txt_found: true`, do not report llms.txt as missing. When it
reports `summary.ai_discovery_all_found: true`, do not report AI discovery files as missing.
Inspect the downloaded response bodies before recommending changes to llms.txt or AI discovery
content.

## Phase 2: Analyze and Summarize

Based on the audit results, identify:

1. **Scores** — What is the current GEO score for the docs site and README?
2. **Top Strengths** — What's already optimized well?
3. **Critical Gaps** — What's missing or scoring poorly?
4. **High-Impact Fixes** — Which specific recommendations would most improve AI citation coverage?

## Phase 3: Create Discussion Report

### Title
`[geo-optimizer] GEO Audit Report — YYYY-MM-DD`

Use today's date derived from the metadata.json timestamp.

### Body

```markdown
### GEO Audit Report — ${{ github.repository }}

**Audit Date**: [date from metadata]
**Run**: [link to run]

---

### 📊 Scores

| Target | Score | Band |
|--------|-------|------|
| Docs site (`github.github.com/gh-aw/`) | X/100 | Good/Foundation/... |
| README (github.com/github/gh-aw) | X/100 | ... |

---

### ✅ Top Strengths

[3–5 items already optimized well]

---

### 🚨 Critical Gaps

[Top 3–5 issues preventing AI engine citations]

---

### 🔧 Recommended Fixes

[Prioritized, actionable list of specific improvements ordered by impact]

<details>
<summary>📋 Full Breakdown by Category</summary>

[Category-by-category scores and notes from the audit JSON]

</details>

<details>
<summary>📄 Sitemap Page Scores</summary>

[Top pages by score from the sitemap audit, if available]

</details>

---
*Automated audit powered by [geo-optimizer-skill](https://github.com/Auriti-Labs/geo-optimizer-skill) · [Run logs](${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }})*
```

---

## Phase 4: Create Improvement Issue

After successfully publishing the discussion report, identify the **single highest-impact recommendation** from the entire audit and create one GitHub issue for it.

### Selecting the top recommendation

Pick the recommendation that:
1. Has the highest estimated score impact (e.g. largest point gain)
2. Is concrete and actionable (not "improve content quality" in general)
3. Covers a gap not already tracked by an open issue

If the audit's highest-impact recommendation says documentation-site robots.txt, llms.txt, or AI
discovery files are missing, first compare it against the authoritative verification files above.
If the project-site verification found the file or files, treat that recommendation as a scanner
false negative and select the next actionable recommendation instead.

If **all scores are already Excellent (90+/100)** and there are no actionable recommendations, use `noop` and skip issue creation.

### Retry guard

Before creating the issue, query closed pull requests with `gh pr list --state closed --author Copilot --limit 1000 --json number,title,closedAt,mergedAt`. Normalize both the candidate title and each PR title by repeatedly removing leading bracketed prefixes (for example `[geo-optimizer]` or `[WIP]`), lowercasing, replacing non-alphanumeric runs with one space, and trimming.

If any normalized title matches the candidate and `mergedAt` is null, do **not** create the issue. Use `noop` instead, naming the matching PR numbers and close dates. Do not treat a closed PR as a reason to retry automatically; a maintainer must create or approve a follow-up issue after reviewing the prior attempt.

### Issue title

`[geo-optimizer] <one-line summary of the improvement>`

### Issue body

```markdown
### GEO Improvement: <short title>

**Source audit**: [GEO Audit Report — YYYY-MM-DD](<link to the discussion you just created>)
**Audit date**: <date from metadata>
**Run**: <link to GitHub Actions run>

### Finding

> <exact quote or paraphrase of the recommendation from the audit JSON>

### Why this matters

<1–2 sentences on what AI-engine citation signal this fixes and approximate score impact>

### Suggested fix

<Specific, actionable steps to implement the improvement>
```

---

## Important Guidelines

- **Be specific**: Quote actual scores and finding text from the JSON, don't make them up.
- **If a file is missing or empty**: Note it clearly rather than fabricating data.
- **Efficient**: Read each file once; avoid redundant bash calls.


### Output Format

Use `###` (h3) or lower for all report headers; never use `#` or `##` inside the report body. Wrap long lists, tables, and detailed findings in `<details><summary><b>...</b></summary>...</details>` blocks for progressive disclosure.

Structure reports as: overview → key metrics/issues → collapsible detail → next actions.