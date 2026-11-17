#!/usr/bin/env bash
# Read-only data pull for the cracktro "Fuzzy schedules" slide (fzs- histogram).
# Reads the local checkout only (git, grep, sed and jq); prints one JSON object on stdout.
# Usage: fuzzy-schedules.sh [ref]   (default: HEAD; run on an up-to-date main checkout)
set -euo pipefail
REF="${1:-HEAD}"
ROOT="$(git rev-parse --show-toplevel)"
DIR=.github/workflows

# Fuzzy all-day daily sources: `schedule:` or `- cron:` set to `daily` or `daily on weekdays` (no around/between window).
: > "${TMPDIR:-/tmp}/fzs.$$"
trap 'rm -f "${TMPDIR:-/tmp}/fzs.$$"' EXIT
git -C "$ROOT" ls-tree --name-only "$REF" "$DIR/" | grep -E '\.md$' | while read -r md; do
  src="$(git -C "$ROOT" show "$REF:$md" | awk 'NR==1&&/^---$/{f=1;next} f&&/^---$/{exit} f' | grep -E '^\s+(schedule|- cron):\s*"?daily( on weekdays)?"?\s*(#.*)?$' || true)"
  [ -n "$src" ] || continue
  lock="${md%.md}.lock.yml"
  git -C "$ROOT" cat-file -e "$REF:$lock" 2>/dev/null || continue
  git -C "$ROOT" show "$REF:$lock" | grep -oE 'cron: "[^"]+"' | head -1 | sed -E 's/cron: "([^"]+)"/\1/' |
    while read -r m h _; do printf '%s %s %s\n' "$(basename "$md" .md)" "$h" "$m"; done
done >> "${TMPDIR:-/tmp}/fzs.$$"

jq -R -s --arg ref "$(git -C "$ROOT" log -1 --format=%cs "$REF")" '
  [split("\n")[] | select(length>0) | split(" ") | {w:.[0], h:(.[1]|tonumber), m:(.[2]|tonumber)}] as $r
  | {
    snapshot: $ref,
    workflows: ($r|length),
    hours: [range(0;24) as $h | ($r|map(select(.h==$h))|length)],
    minutes: [range(0;60) as $m | ($r|map(select(.m==$m))|length)],
    preferred_02_05: ($r|map(select(.h>=2 and .h<=5))|length),
    at_00_01: ($r|map(select(.h<=1))|length),
    on_quarter_minutes: ($r|map(select(.m%15<=3 or .m%15>=12))|length),
    distinct_times: ($r|map("\(.h):\(.m)")|unique|length)
  }' "${TMPDIR:-/tmp}/fzs.$$"
