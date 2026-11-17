#!/usr/bin/env bash
# Read-only data pull for the cracktro "Issue Monster" slide (qga- strip).
# Uses gh (GraphQL + REST) and jq only; prints one JSON object on stdout. Needs gh auth with repo read.
# Usage: issue-monster.sh [END=YYYY-MM-DD] [owner/repo]   (defaults: today, github/gh-aw); ~5 min (one jobs call per run)
set -euo pipefail
END="${1:-$(date -u +%F)}"
REPO="${2:-github/gh-aw}"
START=2026-04-01 # picks begin 19 Apr 2026; older Issue Monster comments used other wording
MARK='Issue Monster selected this for Copilot'
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
d() { date -u -j -f %F "$1" "+%s" 2>/dev/null || date -u -d "$1" +%s; }
fmt() { date -u -r "$1" +%F 2>/dev/null || date -u -d "@$1" +%F; }

# 1. Picked issues: search slices of half a month (gh search stops at 1,000 results).
Q='query($q:String!,$c:String){search(query:$q,type:ISSUE,first:100,after:$c){pageInfo{hasNextPage endCursor}
 nodes{... on Issue{number title createdAt state stateReason
  closedByPullRequestsReferences(includeClosedPrs:true,first:10){nodes{merged mergedAt author{login}}}}}}}'
: > "$TMP/picks.jsonl"
s=$(d "$START"); e=$(d "$END")
while [ "$s" -le "$e" ]; do
  a=$(fmt "$s"); n=$((s + 15 * 86400)); [ "$n" -gt "$e" ] && n=$((e + 86400))
  b=$(fmt $((n - 86400)))
  cursor=""
  while :; do
    args=(-f query="$Q" -f q="repo:$REPO is:issue \"$MARK\" in:comments created:$a..$b")
    [ -n "$cursor" ] && args+=(-f c="$cursor")
    gh api graphql "${args[@]}" > "$TMP/page.json"
    jq -c '.data.search.nodes[]' "$TMP/page.json" >> "$TMP/picks.jsonl"
    [ "$(jq -r '.data.search.pageInfo.hasNextPage' "$TMP/page.json")" = true ] || break
    cursor="$(jq -r '.data.search.pageInfo.endCursor' "$TMP/page.json")"
  done
  s=$n
done

# 2. Runs on the 7 full UTC days before END and whether the agent job ran (skipped = stopped in pre-activation).
since=$(fmt $((e - 7 * 86400))); until=$(fmt $((e - 86400)))
gh api --paginate "repos/$REPO/actions/workflows/issue-monster.lock.yml/runs?per_page=100&created=$since..$until" \
  --jq '.workflow_runs[] | "\(.id) \(.created_at)"' > "$TMP/runs.txt"
: > "$TMP/jobs.txt"
while read -r id _; do
  gh api "repos/$REPO/actions/runs/$id/jobs" --jq '[.jobs[] | select(.name=="agent") | .conclusion] | first // "none"' >> "$TMP/jobs.txt"
done < "$TMP/runs.txt"

# Titles are untrusted: only a prefix matching ^[a-z0-9-]+ inside brackets is ever printed.
jq -s --arg end "$END" --arg since "$since" --arg until "$until" --rawfile runs "$TMP/runs.txt" --rawfile jobs "$TMP/jobs.txt" '
  def hrs(a;b): (((b|fromdateiso8601)-(a|fromdateiso8601))/3600);
  def median: sort | if length==0 then null elif length%2==1 then .[length/2|floor] else (.[length/2-1]+.[length/2])/2 end;
  (unique_by(.number)) as $p
  | [ $p[] | . as $x
      | ([.closedByPullRequestsReferences.nodes[] | select(.merged and .mergedAt[0:10] <= $end and ((.author.login//"")|test("copilot";"i")))] | sort_by(.mergedAt) | first) as $pr
      | {month:.createdAt[0:7], hours:(if $pr then hrs($x.createdAt;$pr.mergedAt) else null end)} ] as $r
  | ($r|map(select(.hours!=null))) as $m
  | ($jobs|split("\n")|map(select(length>0))) as $j
  | {
    snapshot:$end,
    window:[($p|map(.createdAt)|min[0:10]), $end],
    picked:($p|length),
    open:($p|map(select(.state=="OPEN"))|length),
    completed:($p|map(select(.stateReason=="COMPLETED"))|length),
    notPlanned:($p|map(select(.stateReason=="NOT_PLANNED"))|length),
    copilotMerged:($m|length),
    pct:(($m|length)*100/($p|length)|round),
    medianHoursToMerge:($m|map(.hours)|median|.*10|round/10),
    within24h:($m|map(select(.hours<24))|length),
    MONTHS:($r|group_by(.month)|map([.[0].month, length, (map(select(.hours!=null))|length)])),
    prefixes:($p|map(.title|capture("^\\[(?<x>[a-z0-9-]+)\\]").x // empty)|group_by(.)|map([.[0],length])|sort_by(-.[1])|.[0:5]),
    runs:{window:[$since,$until], total:($j|length),
          agentSkipped:($j|map(select(.=="skipped"))|length), agentRan:($j|map(select(.!="skipped" and .!="none"))|length)}
  }' "$TMP/picks.jsonl"
