---
private: true
emoji: "🏆"
name: Daily Community Attribution Updater
description: Maintains a live community contributions section in README.md and an all-time Community Contributors wiki page by attributing all community-labeled issues using the five-tier attribution strategy
on:
  schedule:
    - cron: daily
  workflow_dispatch:

max-daily-ai-credits: 10000
permissions:
  contents: read
  pull-requests: read
  issues: read

model: claude-haiku-4.5
engine:
  id: copilot
timeout-minutes: 20

network:
  allowed:
    - defaults

sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
tools:
  cli-proxy: true
  github:
    mode: local
    toolsets: [issues]
  repo-memory:
    wiki: true
    description: "All-time Community Contributors list"
    max-patch-size: 102400  # 100KB; default (10KB) is too small for the full contributors list
  bash:
    - "gh pr list *"
    - "gh issue list *"
    - "gh api *"
    - "jq *"
    - "grep *"
    - "sort *"
    - "mkdir *"
    - "echo *"
    - "cp *"
    - "cat *"
    - "head *"
    - "wc *"
    - "sed *"
    - "date *"
  edit:

safe-outputs:
  steer: true
  create-pull-request:
    expires: 1d
    title-prefix: "[community] "
    labels: [community, automation]
    reviewers: []
    draft: true
    protected-files:
      exclude:
        - README.md    # this workflow updates the Community Contributions section in README.md
  create-issue:
    title-prefix: "[community-attribution] "
    labels: [community, automation]
    close-older-issues: true
    group-by-day: true
    expires: 7d

experiments:
  prompt_style: [concise, verbose]

imports:
  - shared/community-attribution.md
  - shared/otlp.md
  - shared/issue-dedup.md

steps:
  - name: Re-fetch full community issue history with pagination
    env:
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      mkdir -p /tmp/gh-aw/agent/community-data

      echo "Fetching full paginated history of issues with 'community' label..."
      REPO="${GITHUB_REPOSITORY:-github/gh-aw}"
      OWNER="${REPO%%/*}"
      NAME="${REPO#*/}"

      # shellcheck disable=SC2016  # GraphQL variables $owner/$name/$endCursor are GraphQL syntax, not shell variables
      if ! gh api graphql --paginate \
        -f owner="$OWNER" \
        -f name="$NAME" \
        -f query='
          query($owner: String!, $name: String!, $endCursor: String) {
            repository(owner: $owner, name: $name) {
              issues(
                first: 100
                after: $endCursor
                labels: ["community"]
                states: [OPEN, CLOSED]
                orderBy: {field: CREATED_AT, direction: DESC}
              ) {
                nodes {
                  number
                  title
                  createdAt
                  closedAt
                  url
                  stateReason
                  author {
                    login
                  }
                  labels(first: 100) {
                    nodes {
                      name
                    }
                  }
                }
                pageInfo {
                  hasNextPage
                  endCursor
                }
              }
            }
          }
        ' \
        | jq -s '
          [.[].data.repository.issues.nodes[] |
            {
              number,
              title,
              author: ({login: (.author.login // "ghost")}),
              labels: (.labels.nodes // []),
              closedAt,
              createdAt,
              url,
              stateReason
            }
          ]
        ' > /tmp/gh-aw/agent/community-data/community_issues.json; then
        echo "[]" > /tmp/gh-aw/agent/community-data/community_issues.json
      fi

      COMMUNITY_COUNT=$(jq length /tmp/gh-aw/agent/community-data/community_issues.json)
      echo "✓ Fetched $COMMUNITY_COUNT community-labeled issues (all-time, paginated)"

  - name: Fetch PR data for attribution index
    env:
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      mkdir -p /tmp/gh-aw/agent/community-data

      # Fetch merged PRs from the last 30 days (daily runs attribute recent closures, with extra buffer for lag).
      SINCE=$(date -d '30 days ago' '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null \
              || date -v-30d '+%Y-%m-%dT%H:%M:%SZ')

      echo "Fetching PRs merged since $SINCE..."
      gh pr list \
        --state merged \
        --limit 500 \
        --json number,title,author,mergedAt,url,body,closingIssuesReferences \
        --jq "[.[] | select(.mergedAt >= \"$SINCE\")]" \
        > /tmp/gh-aw/agent/community-data/pull_requests.json \
        || echo "[]" > /tmp/gh-aw/agent/community-data/pull_requests.json

      PR_COUNT=$(jq length /tmp/gh-aw/agent/community-data/pull_requests.json)
      echo "✓ Fetched $PR_COUNT merged PRs"

      # Build closing references index: {issue_number: [pr_numbers]}
      # Use a nested reduce so the outer body always returns the accumulator,
      # even when closingIssuesReferences is empty (avoids jq setting acc to null).
      jq '
        reduce .[] as $pr (
          {};
          reduce ($pr.closingIssuesReferences // [])[] as $issue (
            .;
            ($issue.number | tostring) as $key |
            .[$key] = (.[$key] // []) + [$pr.number]
          )
        )
      ' /tmp/gh-aw/agent/community-data/pull_requests.json \
        > /tmp/gh-aw/agent/community-data/closing_refs_by_issue.json 2>/dev/null \
        || echo "{}" > /tmp/gh-aw/agent/community-data/closing_refs_by_issue.json

      LINK_COUNT=$(jq 'keys | length' /tmp/gh-aw/agent/community-data/closing_refs_by_issue.json)
      echo "✓ Built closing refs index: $LINK_COUNT issues with native GitHub close links"

      # Copy the current README so the agent can read it without extra tool calls
      cp README.md /tmp/gh-aw/agent/community-data/README_current.md 2>/dev/null \
        || echo "⚠ README.md not found; agent will read it from the working directory"

      # Find community issues closed within the PR lookback window (attribution candidates)
      jq --arg since "$SINCE" \
        '[.[] | select(.closedAt != null and .closedAt >= $since)]' \
        /tmp/gh-aw/agent/community-data/community_issues.json \
        > /tmp/gh-aw/agent/community-data/community_issues_closed_in_window.json 2>/dev/null \
        || echo "[]" > /tmp/gh-aw/agent/community-data/community_issues_closed_in_window.json

      CLOSED_COUNT=$(jq length /tmp/gh-aw/agent/community-data/community_issues_closed_in_window.json)
      echo "✓ Found $CLOSED_COUNT community issues closed in the lookback window"

      echo ""
      echo "Data available in /tmp/gh-aw/agent/community-data/:"
      echo "  community_issues.json                  — all community-labeled issues (includes stateReason)"
      echo "  pull_requests.json                     — merged PRs (last 30 days)"
      echo "  closing_refs_by_issue.json             — native GitHub close links"
      echo "  community_issues_closed_in_window.json — closed during lookback"

  - name: Compute deterministic attributions (Tier 0, 1, 2)
    env:
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      # Tier 0: COMPLETED issues (direct contributions, no PR needed)
      jq '[.[] | select(.stateReason == "COMPLETED") |
          . + {tier: 0, attribution_type: "direct issue", closing_prs: []}]' \
        /tmp/gh-aw/agent/community-data/community_issues.json \
        > /tmp/gh-aw/agent/community-data/tier0_attributed.json

      T0=$(jq length /tmp/gh-aw/agent/community-data/tier0_attributed.json)
      echo "Tier 0 (direct issue — COMPLETED): $T0"

      # Tier 1: native GitHub close references (exclude Tier 0 issues)
      jq --slurpfile issues /tmp/gh-aw/agent/community-data/community_issues.json \
         --slurpfile t0 /tmp/gh-aw/agent/community-data/tier0_attributed.json '
        ($t0[0] | map(.number) | map(tostring)) as $t0_keys |
        ($issues[0] | map(.number | tostring)) as $issue_keys |
        to_entries |
        map(select(
          .key as $k |
          ($issue_keys | index($k) != null) and
          ($t0_keys | index($k) == null)
        )) |
        map(.key as $k | .value as $prs |
          ($issues[0] | map(select(.number | tostring == $k))[0]) +
          {tier: 1, attribution_type: "resolved by PR", closing_prs: $prs}
        )
      ' /tmp/gh-aw/agent/community-data/closing_refs_by_issue.json \
        > /tmp/gh-aw/agent/community-data/tier1_attributed.json 2>/dev/null \
        || echo "[]" > /tmp/gh-aw/agent/community-data/tier1_attributed.json

      T1=$(jq length /tmp/gh-aw/agent/community-data/tier1_attributed.json)
      echo "Tier 1 (native close refs): $T1"

      # Tier 2: PR body keyword matching (exclude Tier 0 and Tier 1 issues)
      KW_ISSUES=$(jq -r '.[].body // ""' /tmp/gh-aw/agent/community-data/pull_requests.json \
        | grep -oP '(?i)(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s*(?:github/gh-aw#|#)\K[0-9]+' 2>/dev/null \
        | sort -u | jq -R 'tonumber' | jq -s 'sort | unique' 2>/dev/null \
        || echo "[]")

      jq --argjson kw "$KW_ISSUES" \
         --slurpfile t0 /tmp/gh-aw/agent/community-data/tier0_attributed.json \
         --slurpfile t1 /tmp/gh-aw/agent/community-data/tier1_attributed.json '
        ($t0[0] | map(.number)) as $t0_nums |
        ($t1[0] | map(.number)) as $t1_nums |
        [.[] |
          select(
            .number as $n |
            ($kw | index($n) != null) and
            ($t0_nums | index($n) == null) and
            ($t1_nums | index($n) == null)
          ) |
          . + {tier: 2, attribution_type: "resolved by PR", closing_prs: []}
        ]
      ' /tmp/gh-aw/agent/community-data/community_issues.json \
        > /tmp/gh-aw/agent/community-data/tier2_attributed.json 2>/dev/null \
        || echo "[]" > /tmp/gh-aw/agent/community-data/tier2_attributed.json

      T2=$(jq length /tmp/gh-aw/agent/community-data/tier2_attributed.json)
      echo "Tier 2 (PR body keywords): $T2"

      # Combine Tier 0 + 1 + 2 into pre_attributed.json
      jq -n \
        --slurpfile t0 /tmp/gh-aw/agent/community-data/tier0_attributed.json \
        --slurpfile t1 /tmp/gh-aw/agent/community-data/tier1_attributed.json \
        --slurpfile t2 /tmp/gh-aw/agent/community-data/tier2_attributed.json \
        '$t0[0] + $t1[0] + $t2[0]' \
        > /tmp/gh-aw/agent/community-data/pre_attributed.json

      TOTAL=$(jq length /tmp/gh-aw/agent/community-data/pre_attributed.json)
      echo ""
      echo "Pre-attributed: $TOTAL issues (Tier 0: $T0, Tier 1: $T1, Tier 2: $T2)"

      # Compute Tier 3+ candidates (closed in window, not yet pre-attributed)
      jq --slurpfile pre /tmp/gh-aw/agent/community-data/pre_attributed.json '
        ($pre[0] | map(.number)) as $attributed |
        [.[] | select(.number as $n | $attributed | index($n) == null)]
      ' /tmp/gh-aw/agent/community-data/community_issues_closed_in_window.json \
        > /tmp/gh-aw/agent/community-data/tier3_candidates.json 2>/dev/null \
        || echo "[]" > /tmp/gh-aw/agent/community-data/tier3_candidates.json

      T3=$(jq length /tmp/gh-aw/agent/community-data/tier3_candidates.json)
      echo "Tier 3+ candidates (agent lookup needed): $T3"

      # Cap Tier 3 to 5 per run — prevents runaway API call loops when there are
      # many unlinked issues. Remaining candidates will be processed in future runs.
      jq '.[0:5]' /tmp/gh-aw/agent/community-data/tier3_candidates.json \
        > /tmp/gh-aw/agent/community-data/tier3_candidates_capped.json
      T3_CAPPED=$(jq length /tmp/gh-aw/agent/community-data/tier3_candidates_capped.json)
      if [ "$T3_CAPPED" -lt "$T3" ]; then
        echo "⚠ Capped Tier 3 lookups: processing $T3_CAPPED of $T3 candidates this run"
      fi
      echo ""
      echo "Data available in /tmp/gh-aw/agent/community-data/:"
      echo "  pre_attributed.json             — Tier 0+1+2 confirmed attributions"
      echo "  tier3_candidates_capped.json    — up to 5 issues needing Tier 3 agent lookup (this run)"
      echo "  tier3_candidates.json           — full list of Tier 3+ candidates (for reference)"

  - name: Format attribution data for agent
    run: |
      DATA_DIR=/tmp/gh-aw/agent/community-data

      # Group Tier 0-2 attributions by author for agent-ready consumption.
      # Produces a structured JSON the agent can read with `cat` — no jq needed.
      jq '
        group_by(.author.login) |
        sort_by(.[0].author.login | ascii_downcase) |
        map({
          author: .[0].author.login,
          count: length,
          issues: (sort_by(-.number))
        })
      ' "$DATA_DIR/pre_attributed.json" \
        > "$DATA_DIR/attribution_by_author.json" 2>/dev/null \
        || echo "[]" > "$DATA_DIR/attribution_by_author.json"

      AUTHOR_COUNT=$(jq length "$DATA_DIR/attribution_by_author.json")
      ISSUE_COUNT=$(jq length "$DATA_DIR/pre_attributed.json")
      echo "✓ Grouped: $AUTHOR_COUNT authors, $ISSUE_COUNT issues"

      # Generate the pre-formatted README community section (Tier 0-2 only).
      # The agent reads this file directly, appends Tier 3 results, and inserts
      # the result into README.md — no bash data-processing required.
      {
        REPO_URL="https://github.com/${GITHUB_REPOSITORY:-github/gh-aw}"
        SEARCH_BASE="${REPO_URL}/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3A"
        echo "## 🌍 Community Contributions"
        echo ""
        echo "<sup>Community members whose issues were resolved — updated automatically.</sup>"
        echo ""
        jq -r --arg base "$SEARCH_BASE" '
          .[] |
          "[@\(.author) (\(.count))](\($base + .author))"
        ' "$DATA_DIR/attribution_by_author.json"
        echo ""
      } > "$DATA_DIR/readme_community_section_tier012.md"

      echo "✓ Generated readme_community_section_tier012.md"
      echo ""
      echo "Data available in $DATA_DIR/:"
      echo "  attribution_by_author.json          — Tier 0-2 issues grouped by author (agent-ready)"
      echo "  readme_community_section_tier012.md — pre-formatted README section (Tier 0-2 only)"
features:
  gh-aw-detection: true
evals:
  - id: community_contributions_scanned
    question: Did the agent scan community-labeled issues and attribute contributions using the five-tier strategy?
  - id: readme_or_wiki_updated
    question: Was the README community section or the Community Contributors wiki page updated, or was noop used when no changes were needed?
---

# Daily Community Attribution Updater

Maintain an up-to-date **🌍 Community Contributions** section in `README.md`
and an all-time **Community Contributors** wiki page by attributing all
resolved community-labeled issues using the five-tier attribution strategy.

## Mission

The `community` label is the **primary attribution signal**: every issue
tagged with it was explicitly identified by a maintainer as community-authored.
This workflow attributes those issues (including direct-issue contributions
with `stateReason == "COMPLETED"`), updates `README.md`, maintains the wiki,
and opens a PR for review.

## Pre-fetched Data

All data is in `/tmp/gh-aw/agent/community-data/`. Use `cat` to read files — no
shell pipelines or data-processing scripts are needed:

```bash
# Pre-formatted README section (Tier 0-2 only — agent adds Tier 3 results):
cat /tmp/gh-aw/agent/community-data/readme_community_section_tier012.md

# Tier 0-2 issues grouped by author (structured JSON, agent-ready):
cat /tmp/gh-aw/agent/community-data/attribution_by_author.json

# Issues still needing Tier 3 agent lookup (capped at 5 per run):
cat /tmp/gh-aw/agent/community-data/tier3_candidates_capped.json

# Current README (pre-fetched):
head -80 /tmp/gh-aw/agent/community-data/README_current.md

# Existing wiki page (if any):
cat /tmp/gh-aw/repo-memory-default/Community-Contributors.md
```

## Workflow

{{#if experiments.prompt_style == 'concise'}}
### 1. Attribute Issues

Read `attribution_by_author.json` (Tier 0–2, pre-grouped and pre-sorted — do not
re-derive). For each entry in `tier3_candidates_capped.json` (≤5), apply Tier 3
(one `issue_read` call per issue). Anything unresolved → Tier 4. Issues beyond
the first 5 in `tier3_candidates.json` are deferred to the next run — do not
process them.
{{#else}}
### 1. Attribute All Resolved Community Issues

**Tier 0, 1, and 2 attributions are already pre-computed and pre-grouped** in
`attribution_by_author.json` — do not re-derive them. Read this file directly
(using `cat`); it contains all confirmed attributions grouped by author with
issues sorted by number descending, ready for use.

For each issue in `tier3_candidates_capped.json` (at most 5 per run), apply **Tier 3** from the
imported Community Attribution Strategy (GitHub MCP `issue_read` to
look for indirect linkage via follow-up or split issues).

Any candidate still unresolved after Tier 3 becomes a **Tier 4**
"needs review" item. Issues in `tier3_candidates.json` beyond the first 5
are deferred to the next run — do not attempt to process them.
{{#endif}}

{{#if experiments.prompt_style == 'concise'}}
### 2. Update Wiki Page

Read the existing wiki at `/tmp/gh-aw/repo-memory-default/Community-Contributors.md`
(empty/missing on first run). Merge all confirmed attributions without duplicating
entries. Group by author (alphabetical), issues descending. Keep under 9 KB (remove
oldest entries from most-prolific author if needed). Format:
`- [#N](url) Title — YYYY-MM-DD — attribution_type`. Write back with edit tool.
{{#else}}
### 2. Update the Community Contributors Wiki Page

Read the existing wiki page at
`/tmp/gh-aw/repo-memory-default/Community-Contributors.md` (empty/missing on
first run).  Merge all confirmed attributions — both newly found ones and all
previously recorded ones — without duplicating entries.

> **Wiki page size limit**: Keep `Community-Contributors.md` under **9 KB**
> (hard limit is 10 KB). Check the byte size with `wc -c` before calling
> `push_repo_memory`. If the page exceeds 9 KB, remove entries to reduce it:
> sort all authors by total contribution count (descending), then remove the
> oldest entry (lowest issue number) from the author with the most entries,
> and repeat until the page is under 9 KB.

The wiki page uses issue numbers as link text for quick scanning, while `README.md`
uses issue titles. Both use full GitHub issue URLs.

The wiki page format:

```markdown
# Community Contributors

### @author

- [#N](https://github.com/OWNER/REPO/issues/N) Issue title — YYYY-MM-DD — direct issue
- [#N](https://github.com/OWNER/REPO/issues/N) Issue title — YYYY-MM-DD — resolved by #PR

### @author2

- [#N](https://github.com/OWNER/REPO/issues/N) Issue title — YYYY-MM-DD — direct issue
```

- Group entries by author (alphabetical order)
- Within each author section, sort by issue number descending (newest first)
- **`direct issue`** — Tier 0: closed as `COMPLETED`, no PR linkage
- **`resolved by #PR`** — Tiers 1–3: attributed to a specific merged PR
- Do not add entries for unresolved or ambiguous candidates (Tier 4)

Write the updated content back to
`/tmp/gh-aw/repo-memory-default/Community-Contributors.md` using the edit tool.
{{#endif}}

{{#if experiments.prompt_style == 'concise'}}
### 3. Build Community Section

Start from `readme_community_section_tier012.md` (pre-formatted Tier 0-2 content).
Insert Tier 3 entries (sorted, alphabetical author order). Ignore Tier 4 items
in `README.md` output. Leave a blank line after the last entry.
{{#else}}
### 3. Build the Community Contributions Section

The pre-step has already produced a formatted starting point in
`readme_community_section_tier012.md` — read it with `cat`. This file contains
the complete Tier 0–2 `## 🌍 Community Contributions` section in the correct
format. If there are Tier 3 attributions, insert them into the paragraph
(maintaining alphabetical author order). Do not re-format the Tier 0–2 entries;
only add new lines.

The expected format (for reference):

```markdown
## 🌍 Community Contributions

<sup>Community members whose issues were resolved — updated automatically.</sup>

[@author (N)](https://github.com/OWNER/REPO/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aauthor)
[@author2 (M)](https://github.com/OWNER/REPO/issues?q=is%3Aissue+is%3Aclosed+label%3Acommunity+author%3Aauthor2)

```

**Important**: always leave a blank line after the last entry (as shown
above) so that the next markdown header renders correctly.

- One link per author, each on its own source line (renders as a single paragraph)
- Sorted alphabetically by username
- Link text: `@alias (count)` where count is the total number of attributed issues
- Link target: closed-issues search URL with `community` label for that author
- When inserting Tier 3 entries, maintain alphabetical author order
- Omit issues that cannot be attributed (Tier 4); do not render an attribution
  candidates section in `README.md`
{{#endif}}

{{#if experiments.prompt_style == 'concise'}}
### 4. Update README.md

Replace `## 🌍 Community Contributions` in `README.md` with the new content
(or append after `## Contributing` if absent). Use edit tool.
{{#else}}
### 4. Update README.md

Replace the existing `## 🌍 Community Contributions` section in `README.md`
with the newly generated content, or append it after the `## Contributing`
section if it does not yet exist.

Use the edit tool to make the change in-place.
{{#endif}}

{{#if experiments.prompt_style == 'concise'}}
### 5. Open Pull Request

If `README.md` or wiki changed: call `create_pull_request` with title
`[community] Update community contributions in README`. If no changes:
call `noop`.
{{#else}}
### 5. Open a Pull Request

If `README.md` **or** the wiki page changed, call the `create_pull_request`
safe-output tool to open a PR with the changes.

**PR title**: `[community] Update community contributions in README`

**PR body template**:
```markdown
### Community Contributions Update

Automated update to the 🌍 Community Contributions section in `README.md`
and the Community Contributors wiki page.

#### Changes
- N community issues newly attributed
- N attribution candidates flagged for review (if any)
- Wiki page updated: Y/N

#### Attribution Summary
[brief summary of what changed and how each was attributed]
```

**Important**: If no action is needed after completing your analysis, you
**MUST** call the `noop` safe-output tool with a brief explanation.

```json
{"noop": {"message": "No action needed: [brief explanation]"}}
```
{{#endif}}

{{#if experiments.prompt_style == 'concise'}}
## Token Budget

- Read each data file once only; use `cat` on pre-formatted files — no bash pipelines
- Process only `tier3_candidates_capped.json` (≤5 issues)
- One `issue_read` per Tier 3 candidate
- Stop after safe-output call
- PR body under 400 words
- Do not access external URLs; use only GitHub MCP `issue_read` for GitHub data
{{#else}}
## Token Budget Guidelines

This workflow uses the Copilot engine — max-turns is not available. Follow these rules to avoid runaway token consumption:

- **Use `cat` on pre-formatted files** — `readme_community_section_tier012.md` and `attribution_by_author.json` are ready to use directly; do not run bash pipelines or data-processing scripts on them
- **Read each data file at most once** — do not re-read `attribution_by_author.json`, `readme_community_section_tier012.md`, or `README_current.md`
- **Tier 3 cap enforced in pre-step** — `tier3_candidates_capped.json` contains at most 5 issues; process only those, then stop
- **At most 1 `issue_read` call per Tier 3 candidate** — call `issue_read` with `method: "get_comments"` once per issue to look for indirect linkage; do not chain further lookups from the results
- **Stop immediately after the safe-output call** — once `create_pull_request` or `noop` is called, halt without any further tool calls or reasoning
- **Keep the PR body under 400 words** — use `<details>` for any extended attribution summary
- **Do not access any external URLs** — use only GitHub MCP `issue_read` for GitHub data; do not call `gh api` or any external HTTP endpoints directly
{{#endif}}

{{#if experiments.prompt_style == 'concise'}}
### 6. Report Failures

On error: call `create_issue` safe-output tool with a brief title and body.
Do not use GitHub MCP `create_issue` directly.
{{#else}}
### 6. Report Failures

If you encounter a genuine error that prevents completion (e.g., data fetch failure, unexpected error), report it using the `create_issue` safe-output tool — **never use the GitHub MCP `create_issue` tool directly**. The safe-output tool has built-in deduplication (`group-by-day` and `close-older-issues`) that prevents duplicate failure issues from accumulating.

```json
{"create_issue": {"title": "Brief description of the failure", "body": "### What failed\n\nDescribe the specific step or data source that failed.\n\n### Error details\n\n(Include the error message or unexpected output here)\n\n### Steps to investigate\n\n1. Check the workflow run logs for the full error message\n2. Verify that community_issues.json and pull_requests.json were fetched successfully\n3. Re-run the workflow manually via workflow_dispatch to see if the failure is transient"}}
```
{{#endif}}

### Output Format

- Use `###` (h3) or lower for all report headers; never use `#` or `##` inside the report body.
- Wrap long lists, tables, and detailed findings in `<details><summary><b>...</b></summary>...</details>` blocks to reduce scrolling.
- Structure reports as: overview → key metrics/issues → collapsible detail → next actions.