#!/usr/bin/env bash
# Read-only data pull for the cracktro "Research · Assign · Observe" slide (rob- panel).
# Uses gh (GraphQL) and git only; prints one JSON object on stdout. Needs gh auth with repo read.
# Usage: research-assign-observe.sh [owner/repo]   (default github/gh-aw)
set -euo pipefail
REPO="${1:-github/gh-aw}"
PREFIX='[performance] Regression in'
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

# 1. Issues: GraphQL search, paginated; filter on the exact title prefix afterwards.
Q='query($q:String!,$c:String){search(query:$q,type:ISSUE,first:50,after:$c){pageInfo{hasNextPage endCursor}
 nodes{... on Issue{number title createdAt closedAt state stateReason labels(first:20){nodes{name}}
  timelineItems(itemTypes:[ASSIGNED_EVENT],first:20){nodes{... on AssignedEvent{assignee{... on Bot{login} ... on User{login}}}}}
  comments(first:100){nodes{body}}
  closedByPullRequestsReferences(includeClosedPrs:true,first:10){nodes{number title merged mergedAt author{login}}}}}}}'
cursor=""; : > "$TMP/issues.jsonl"
while :; do
  args=(-f query="$Q" -f q="repo:$REPO is:issue in:title \"$PREFIX\"")
  [ -n "$cursor" ] && args+=(-f c="$cursor")
  gh api graphql "${args[@]}" > "$TMP/page.json"
  jq -c '.data.search.nodes[]' "$TMP/page.json" >> "$TMP/issues.jsonl"
  [ "$(jq -r '.data.search.pageInfo.hasNextPage' "$TMP/page.json")" = true ] || break
  cursor="$(jq -r '.data.search.pageInfo.endCursor' "$TMP/page.json")"
done

jq -s --arg p "$PREFIX" '
  def hrs(a;b): (((b|fromdateiso8601)-(a|fromdateiso8601))/3600);
  def median: sort | if length==0 then null elif length%2==1 then .[length/2|floor] else (.[length/2-1]+.[length/2])/2 end;
  [ .[] | select(.title|startswith($p)) ] as $i
  | [ $i[] | . as $x
      | ([.closedByPullRequestsReferences.nodes[] | select(.merged and ((.author.login//"")|test("copilot";"i")))] | sort_by(.mergedAt) | first) as $pr
      | select($pr != null)
      | {issue:.number, filed:.createdAt[0:10], title:.title, pr:$pr.number, mergedAt:$pr.mergedAt, hours:(hrs($x.createdAt;$pr.mergedAt)*10|round/10)} ] as $fix
  | {
    window:{first_filed:($i|map(.createdAt)|min), last_filed:($i|map(.createdAt)|max)},
    filed:($i|length),
    open:($i|map(select(.state=="OPEN"))|length),
    closed_completed:($i|map(select(.stateReason=="COMPLETED"))|length),
    closed_not_planned:($i|map(select(.stateReason=="NOT_PLANNED"))|length),
    labelled_cookie:($i|map(select([.labels.nodes[].name]|index("cookie")))|length),
    assigned_copilot:($i|map(select([.timelineItems.nodes[].assignee.login//""]|any(test("copilot";"i"))))|length),
    issue_monster_picked:($i|map(select([.comments.nodes[].body]|any(test("Issue Monster selected this for Copilot"))))|length),
    fixes_merged:($fix|length),
    median_hours:($fix|map(.hours)|median),
    within_24h:($fix|map(select(.hours<24))|length),
    fixes:$fix
  }' "$TMP/issues.jsonl" > "$TMP/issues.json"

# 2. Benchmarks: dates added to benchmark_history.jsonl on the repo-memory branch (handles pretty-printed blobs).
git fetch -q origin memory/cli-performance 2>/dev/null || true
B=origin/memory/cli-performance
git log "$B" -p --format= -- benchmark_history.jsonl \
  | grep -E '^\+' | grep -oE '"date": ?"[0-9]{4}-[0-9]{2}-[0-9]{2}"' | grep -oE '[0-9]{4}-[0-9]{2}-[0-9]{2}' | sort -u > "$TMP/dates"
jq -n --rawfile d "$TMP/dates" --arg c "$(git log "$B" --format=%cs -- benchmark_history.jsonl | sort -u | wc -l | tr -d ' ')" \
  '($d|split("\n")|map(select(length>0))) as $x | {snapshot_dates:($x|length), first:$x[0], last:$x[-1], distinct_commit_days:($c|tonumber)}' > "$TMP/bench.json"

jq -n --slurpfile i "$TMP/issues.json" --slurpfile b "$TMP/bench.json" --arg s "$(date -u +%F)" \
  '{snapshot:$s, repo:"'"$REPO"'", issues:$i[0], benchmarks:$b[0]}'
