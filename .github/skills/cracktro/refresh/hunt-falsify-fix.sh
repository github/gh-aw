#!/usr/bin/env bash
# Read-only: recompute the measured strip of the cracktro "Hunt · Falsify · Fix" slide.
# Usage: hunt-falsify-fix.sh [SNAPSHOT_DATE]   (default: today, UTC). Needs gh and jq. Prints JSON.
# Do not fetch issue or PR text. The output holds numbers, dates, enums, booleans and nulls only.
set -euo pipefail
REPO="${REPO:-github/gh-aw}"
END="${1:-$(date -u +%F)}"
WF="daily-vulnhunter-scan.lock.yml"

# 1. Findings issues: filter by the workflow label and fetch only structural fields.
issues=$(gh issue list -R "$REPO" --state all --limit 500 \
  --label vulnhunter --search "created:<=$END" \
  --json number,createdAt,closedAt,state,stateReason,closedByPullRequestsReferences |
  jq --arg end "$END" '[.[] | select(.createdAt[0:10] <= $end)] | sort_by(.createdAt)')

# 2. Candidate fix PRs: linked closing PRs, else structured cross-reference events.
rows='[]'
for n in $(jq -r '.[].number' <<<"$issues"); do
  issue=$(jq --argjson n "$n" '.[] | select(.number == $n)' <<<"$issues")
  linked=$(jq -r '[.closedByPullRequestsReferences[]?.number] | join(" ")' <<<"$issue")
  superseded=$(jq '.stateReason == "NOT_PLANNED"' <<<"$issue")
  kind="none" pr='null'
  for p in $linked; do
    j=$(gh pr view "$p" -R "$REPO" --json number,mergedAt,author 2>/dev/null || echo null)
    # Only merges on or before the snapshot date (UTC) count.
    if [ "$(jq -r --arg end "$END" '(.mergedAt // "") | select(. != "" and .[0:10] <= $end)' <<<"$j")" != "" ]; then
      pr=$(jq '{number, mergedAt, copilot: (.author.login == "app/copilot-swe-agent" or .author.login == "copilot-swe-agent")}' <<<"$j")
      kind="fixed"
      break
    fi
  done
  if [ "$kind" = "none" ]; then
    created=$(jq -r '.createdAt' <<<"$issue")
    refs=$(gh api graphql -F owner="${REPO%%/*}" -F name="${REPO#*/}" -F number="$n" -f query='
      query($owner: String!, $name: String!, $number: Int!) {
        repository(owner: $owner, name: $name) {
          issue(number: $number) {
            timelineItems(first: 100, itemTypes: [CROSS_REFERENCED_EVENT]) {
              nodes {
                ... on CrossReferencedEvent {
                  willCloseTarget
                  source {
                    __typename
                    ... on PullRequest {
                      number
                      mergedAt
                      author { login }
                    }
                  }
                }
              }
            }
          }
        }
      }' |
      jq --arg end "$END" --arg created "$created" '[.data.repository.issue.timelineItems.nodes[]?
        | select(.willCloseTarget == false)
        | .source
        | select(.__typename == "PullRequest" and .mergedAt != null and .mergedAt >= $created and .mergedAt[0:10] <= $end)
        | {number, mergedAt, copilot: (.author.login == "app/copilot-swe-agent" or .author.login == "copilot-swe-agent")}]')
    pr=$(jq 'sort_by(.mergedAt) | .[0] // null' <<<"$refs")
    if [ "$pr" != "null" ]; then kind="related"; fi
  fi
  [ "$kind" = "none" ] && [ "$superseded" = "true" ] && kind="superseded"
  rows=$(jq --argjson i "$issue" --arg kind "$kind" --argjson pr "$pr" \
    '. + [{number: $i.number, created: $i.createdAt, state: $i.state, kind: $kind,
           cwe: null, pr: $pr}]' <<<"$rows")
done

# 3. Runs of the compiled workflow, up to the snapshot date.
runs=$(gh run list -R "$REPO" --workflow "$WF" --limit 1000 --json conclusion,createdAt |
  jq --arg end "$END" '[.[] | select(.createdAt[0:10] <= $end)] | sort_by(.createdAt)')

jq -n --arg end "$END" --argjson rows "$rows" --argjson runs "$runs" '
  def hours(a; b): (((b | fromdateiso8601) - (a | fromdateiso8601)) / 3600);
  def median: sort | if length == 0 then null elif length % 2 == 1 then .[length / 2 | floor] else (.[length / 2 - 1] + .[length / 2]) / 2 end;
  ($rows | map(select(.kind == "fixed"))) as $fixed
  | ($rows | map(select(.kind == "fixed" or .kind == "related"))) as $merged
  | ($rows[0].created[0:10]) as $start
  | def day(d): (((d[0:10] + "T00:00:00Z") | fromdateiso8601) - (($start + "T00:00:00Z") | fromdateiso8601)) / 86400 | floor;
  {
    snapshot: $end,
    window: [$start, $end],
    days: day($end + "T00:00:00Z"),
    reports: ($rows | length),
    fixed: ($fixed | length),
    related: ($rows | map(select(.kind == "related")) | length),
    superseded: ($rows | map(select(.kind == "superseded")) | length),
    unresolved: ($rows | map(select(.kind == "none")) | length),
    open: ($rows | map(select(.state == "OPEN")) | length),
    copilotFixes: ($rows | map(select(.pr != null and .pr.copilot)) | length),
    medianHoursToMerge: ($merged | map(hours(.created; .pr.mergedAt)) | median | . * 10 | round / 10),
    cwe: null,
    REPORTS: ($rows | map([day(.created), (.kind | .[0:1])])),
    runs: {
      total: ($runs | length),
      success: ($runs | map(select(.conclusion == "success")) | length),
      first: ($runs[0].createdAt[0:10]),
      lastFailure: ($runs | map(select(.conclusion != "success")) | last | .createdAt[0:10]),
      streak: ($runs | (map(.conclusion == "success") | reverse | index(false)) // length),
      byMonth: ($runs | group_by(.createdAt[0:7]) | map([.[0].createdAt[0:7], length, (map(select(.conclusion == "success")) | length)]))
    },
    detail: $rows
  }'
