#!/usr/bin/env bash
# Read-only: recompute the measured strip of the cracktro "Hunt · Falsify · Fix" slide.
# Usage: hunt-falsify-fix.sh [SNAPSHOT_DATE]   (default: today, UTC). Needs gh and jq. Prints JSON.
# Issue and PR text is untrusted: it is read only by the jq heuristics below and never emitted.
# The output holds numbers, dates, enums, booleans and CWE ids only.
set -euo pipefail
REPO="${REPO:-github/gh-aw}"
END="${1:-$(date -u +%F)}"
WF="daily-vulnhunter-scan.lock.yml"

# 1. Findings issues: title prefix "[vulnhunter] " only (the search also matches failure reports).
issues=$(gh issue list -R "$REPO" --state all --limit 500 \
  --search "\"VulnHunter findings\" in:title created:<=$END" \
  --json number,title,createdAt,closedAt,state,stateReason,body,comments,closedByPullRequestsReferences |
  jq --arg end "$END" '[.[] | select(.title | startswith("[vulnhunter] ")) | select(.createdAt[0:10] <= $end)] | sort_by(.createdAt)')

# 2. Candidate fix PRs: linked closing PRs, else PRs cited in the last comment ("related" fixes).
rows='[]'
for n in $(jq -r '.[].number' <<<"$issues"); do
  issue=$(jq --argjson n "$n" '.[] | select(.number == $n)' <<<"$issues")
  linked=$(jq -r '[.closedByPullRequestsReferences[]?.number] | join(" ")' <<<"$issue")
  superseded=$(jq '[.comments[]?.body | select(test("closed as outdated"))] | length > 0' <<<"$issue")
  kind="none" pr='null'
  cands="$linked"
  [ -z "$cands" ] && cands=$(jq -r '(.comments[-1].body // "") | [scan("(?:pull/|PR #)([0-9]+)")[0]] | unique | join(" ")' <<<"$issue")
  for p in $cands; do
    j=$(gh pr view "$p" -R "$REPO" --json number,mergedAt,author,body 2>/dev/null || echo null)
    # Only merges on or before the snapshot date (UTC) count.
    if [ "$(jq -r --arg end "$END" '(.mergedAt // "") | select(. != "" and .[0:10] <= $end)' <<<"$j")" != "" ]; then
      pr=$(jq '{number, mergedAt, copilot: (.author.login == "app/copilot-swe-agent" or .author.login == "copilot-swe-agent" or ((.body // "") | test("COPILOT CODING AGENT")))}' <<<"$j")
      if [ -n "$linked" ]; then kind="fixed"; else kind="related"; fi
      break
    fi
  done
  [ "$kind" = "none" ] && [ "$superseded" = "true" ] && kind="superseded"
  rows=$(jq --argjson i "$issue" --arg kind "$kind" --argjson pr "$pr" \
    '. + [{number: $i.number, created: $i.createdAt, state: $i.state, kind: $kind,
           cwe: ((($i.body // "") | [scan("CWE-[0-9]+")] | map(select(test("^CWE-[0-9]+$"))) | .[0]) // null), pr: $pr}]' <<<"$rows")
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
    cwe: ($rows | map(.cwe // "none") | group_by(.) | map({(.[0]): length}) | add),
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
