#!/usr/bin/env bash
# Read-only: recompute the measured strip of the cracktro "Spec-driven testing" slide.
# Usage: .github/skills/cracktro/refresh/spec-driven-testing.sh [SINCE=2026-04-01]
# Needs: gh (authenticated, public read), git, jq. Prints JSON to stdout.
set -euo pipefail
REPO=github/gh-aw
SINCE=${1:-2026-04-01}
git fetch -q origin main

prs() { # $1 = exact title prefix
  gh pr list -R "$REPO" --state all --limit 1000 --search "\"$1\" in:title" \
    --json title,state,additions |
    jq --arg p "$1" '[.[] | select(.title | startswith($p))]
      | {total: length, merged: map(select(.state=="MERGED")) | length,
         merged_additions: map(select(.state=="MERGED") | .additions) | (add // 0)}'
}
issues() {
  gh issue list -R "$REPO" --state all --limit 1000 --search "\"$1\" in:title" --json title |
    jq --arg p "$1" '[.[] | select(.title | startswith($p))] | length'
}
fixes=$(gh pr list -R "$REPO" --state all --limit 1000 --search "head:copilot/spec-librarian" --json state |
  jq '{total: length, merged: map(select(.state=="MERGED")) | length}')

count_specs() { # $1 = rev
  { git grep -h '^func TestSpec_' "$1" -- 'pkg/**/*spec_test.go' 'pkg/*spec_test.go' 2>/dev/null || true; } | wc -l | tr -d ' '
}
series="["
d=$SINCE
while [[ "$d" < "$(date +%F)" ]]; do
  rev=$(git rev-list -1 --before="$d" origin/main)
  series+="[\"$d\",$(count_specs "$rev")],"
  d=$(date -j -v+1m -f %F "$d" +%F 2>/dev/null || date -d "$d +1 month" +%F)
done
series+="[\"$(date +%F)\",$(count_specs origin/main)]]"
files=$(git ls-tree -r --name-only origin/main pkg | grep -c 'spec_test\.go$' || true)

jq -n --argjson ex "$(prs '[spec-extractor]')" --argjson en "$(prs '[spec-enforcer]')" \
  --argjson rv "$(prs '[spec-review]')" --argjson li "$(issues '[spec-librarian]')" \
  --argjson fx "$fixes" --argjson ts "$series" --argjson files "$files" \
  --arg snap "$(date +%F)" \
  '{snapshot: $snap, spec_extractor: $ex, spec_enforcer: $en, spec_review: $rv,
    spec_librarian_issues: $li, librarian_fix_prs: $fx,
    testspec_by_month: $ts, spec_test_files: $files}'
