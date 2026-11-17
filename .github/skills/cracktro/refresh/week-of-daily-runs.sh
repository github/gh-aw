#!/usr/bin/env bash
# Read-only data pull for the cracktro "Week of daily runs" slide (fzw- heat map).
# Reads the local checkout only (git, grep, awk and jq); prints one JSON object on stdout.
# Usage: week-of-daily-runs.sh [ref]   (default: HEAD; run on an up-to-date main checkout)
set -euo pipefail
REF="${1:-HEAD}"
ROOT="$(git rev-parse --show-toplevel)"
DIR=.github/workflows
OUT="${TMPDIR:-/tmp}/fzw.$$"
trap 'rm -f "$OUT"' EXIT

# Every daily source: `schedule:` or `- cron:` starting with `daily` (plain, on weekdays, around HH:MM, between A and B).
git -C "$ROOT" ls-tree --name-only "$REF" "$DIR/" | grep -E '\.md$' | while read -r md; do
  src="$(git -C "$ROOT" show "$REF:$md" | awk 'NR==1&&/^---$/{f=1;next} f&&/^---$/{exit} f' | grep -E '^\s+(schedule|- cron):\s*"?daily\b' | head -1 || true)"
  [ -n "$src" ] || continue
  lock="${md%.md}.lock.yml"
  git -C "$ROOT" cat-file -e "$REF:$lock" 2>/dev/null || continue
  git -C "$ROOT" show "$REF:$lock" | grep -oE 'cron: "[^"]+"' | head -1 | sed -E 's/cron: "([^"]+)"/\1/' |
    while read -r m h dom mon dow; do printf '%s %s %s %s\n' "$(basename "$md" .md)" "$m" "$h" "$dow"; done
done > "$OUT"

# Grid rows Mon..Sun (cron dow 1..6,0), columns UTC hour 00..23; a row counts one run per workflow scheduled that day.
jq -R -s --arg ref "$(git -C "$ROOT" log -1 --format=%cs "$REF")" '
  def days($d): if $d == "*" then [0,1,2,3,4,5,6] elif $d == "1-5" then [1,2,3,4,5] else error("unexpected dow \($d)") end;
  [split("\n")[] | select(length>0) | split(" ") | {w:.[0], m:(.[1]|tonumber), h:(.[2]|tonumber), d:days(.[3])}] as $r
  | [1,2,3,4,5,6,0] as $order
  | [ $order[] as $dw | [range(0;24) as $h | ($r|map(select(.h==$h and (.d|index($dw))))|length)] ] as $grid
  | {
    snapshot: $ref,
    workflows: ($r|length),
    weekdays_only: ($r|map(select(.d|length==5))|length),
    grid: $grid,
    runs_per_week: ($grid|flatten|add),
    weekday_runs: ($grid[0]|add),
    weekend_runs: ($grid[5]|add),
    at_00_01: ($grid|map(.[0]+.[1])|add),
    busiest: ($grid|flatten|max)
  }' "$OUT"
