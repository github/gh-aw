#!/usr/bin/env bash
# Read-only: recompute the measured data for the cracktro "Merge fast, unslop daily" slide.
# Usage: merge-fast-unslop-daily.sh [END_DATE]   (END_DATE = last full UTC day, default yesterday)
# Needs: gh (authenticated, read access to the public repo), jq. Prints JSON on stdout.
set -euo pipefail
REPO="${REPO:-github/gh-aw}"
shift_day() { date -u -j -v"$2"d -f %F "$1" +%F 2>/dev/null || date -u -d "$1 $2 days" +%F; }
END="${1:-$(shift_day "$(date -u +%F)" -1)}"
START="$(shift_day "$END" -29)"
PREFIXES='["jsweep","dead-code","code-simplifier","tidy","file-diet","duplicate-code"]'
FIELDS=number,title,createdAt,mergedAt,additions,deletions,author
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

# 30-day window, fetched in 8-day slices so no slice hits gh's 1000-result search cap.
s="$START"
while [[ ! "$s" > "$END" ]]; do
  e="$(shift_day "$s" +7)"; [[ "$e" > "$END" ]] && e="$END"
  gh pr list -R "$REPO" --state merged --search "merged:$s..$e" --limit 1000 --json "$FIELDS" > "$tmp/w_$s.json"
  if [[ "$(jq length "$tmp/w_$s.json")" -ge 1000 ]]; then echo "slice $s..$e hit 1000; shorten slices" >&2; exit 1; fi
  s="$(shift_day "$e" +1)"
done
jq -s 'add | unique_by(.number)' "$tmp"/w_*.json > "$tmp/window.json"
total="$(gh api -X GET search/issues -f q="repo:$REPO is:pr is:merged merged:$START..$END" --jq .total_count)"

# All-time improver PRs, by exact title prefix.
for p in $(jq -r '.[]' <<<"$PREFIXES"); do
  gh pr list -R "$REPO" --state merged --search "\"[$p]\" in:title" --limit 1000 --json "$FIELDS" \
    | jq --arg p "[$p]" '[.[] | select(.title | startswith($p))]' > "$tmp/p_$p.json"
done
jq -s 'add | unique_by(.number)' "$tmp"/p_*.json > "$tmp/unslop.json"

jq -n --arg start "$START" --arg end "$END" --argjson total "$total" --argjson prefixes "$PREFIXES" \
  --slurpfile w "$tmp/window.json" --slurpfile u "$tmp/unslop.json" '
  def med: sort | if length == 0 then null elif length % 2 == 1 then .[length/2|floor] else (.[length/2-1] + .[length/2]) / 2 end;
  def unslop: .title | test("^\\[(" + ($prefixes | join("|")) + ")\\]");
  ($w[0]) as $w | ($u[0]) as $u |
  ([range(0; 30)] | map(($start + "T00:00:00Z" | fromdateiso8601) + . * 86400 | strftime("%Y-%m-%d"))) as $d |
  {
    snapshot: (now | strftime("%Y-%m-%d")),
    window: { start: $start, end: $end, merged: ($w | length), search_total: $total,
      per_day: (($w | length) / 30 | round),
      median_lines: ($w | map(.additions + .deletions) | med),
      median_minutes_to_merge: ($w | map(((.mergedAt | fromdateiso8601) - (.createdAt | fromdateiso8601)) / 60) | med | round),
      DAY_ALL: ($d | map(. as $x | $w | map(select(.mergedAt[0:10] == $x)) | length)),
      DAY_UN: ($d | map(. as $x | $w | map(select(.mergedAt[0:10] == $x and unslop)) | length)),
      unslop_days: ($d | map(. as $x | $w | any(.mergedAt[0:10] == $x and unslop)) | map(select(.)) | length) },
    all_time: { merged: ($u | length), additions: ($u | map(.additions) | add), deletions: ($u | map(.deletions) | add),
      days: ($u | map(.mergedAt[0:10]) | unique | length),
      first: ($u | map(.mergedAt) | min | .[0:10]), last: ($u | map(.mergedAt) | max | .[0:10]),
      by_prefix: ($prefixes | map(. as $p | { key: $p, value: ($u | map(select(.title | startswith("[" + $p + "]"))) | length) }) | from_entries) }
  }'
