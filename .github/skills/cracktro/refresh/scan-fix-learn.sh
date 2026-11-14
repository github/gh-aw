#!/usr/bin/env bash
# Read-only: recompute the Scan · Fix · Learn panel numbers from the public .NET runtime repo.
# Usage: scan-fix-learn.sh [START_DATE]   (default 2026-05-05; window is START..today, all-time since launch)
set -euo pipefail
R=dotnet/runtime
START=${1:-2026-05-05}
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT

gh search issues --repo "$R" --match title "[ci-scan]" --created ">=$START" --limit 1000 \
  --json number,title,state,createdAt,closedAt,author,labels \
  | jq '[.[]|select((.title|startswith("[ci-scan]")) and .author.login=="github-actions[bot]")]' > "$T/scan.json"
gh pr list -R "$R" --state all --limit 1000 --search "author:app/github-actions \"[ci-fix]\" in:title created:>=$START" \
  --json number,state,title > "$T/fix.json"
gh pr list -R "$R" --state merged --limit 500 --search "\"[ci-scan-feedback]\" in:title created:>=$START" \
  --json number,title,author > "$T/fb.json"
runs() { gh api "repos/$R/actions/workflows/$1.lock.yml/runs?per_page=1&created=>=$START" --jq .total_count; }

jq -n --arg start "$START" --arg snapshot "$(date -u +%F)" \
  --slurpfile s "$T/scan.json" --slurpfile f "$T/fix.json" --slurpfile b "$T/fb.json" \
  --argjson scanRuns "$(runs ci-failure-scan)" --argjson fixRuns "$(runs ci-failure-fix)" --argjson fbRuns "$(runs ci-failure-scan-feedback)" '
  ($s[0]) as $s | ($f[0]|map(select(.title|startswith("[ci-fix]")))) as $f | ($b[0]|map(select(.title|startswith("[ci-scan-feedback]")))) as $b |
  ($s|map(select(.state=="closed")|((.closedAt|fromdate)-(.createdAt|fromdate))/86400)|sort) as $d |
  { window: {start: $start, snapshot: $snapshot, days: ((($snapshot+"T00:00:00Z"|fromdate)-($start+"T00:00:00Z"|fromdate))/86400|floor)},
    scan: {issues: ($s|length), closed: ($s|map(select(.state=="closed"))|length),
           kbeLabelled: ($s|map(select(.labels|map(.name)|index("Known Build Error")))|length),
           medianDaysOpen: (if ($d|length)>0 then ($d[($d|length/2|floor)]*10|round/10) else null end),
           perMonth: ($s|group_by(.createdAt[:7])|map({month: .[0].createdAt[:7], n: length}))},
    fix: {prs: ($f|length), merged: ($f|map(select(.state=="MERGED"))|length), helpWanted: ($f|map(select(.title|test("Needs review")))|length)},
    feedback: {mergedPromptEdits: ($b|length), byCopilot: ($b|map(select(.author.login|test("copilot")))|length)},
    runs: {scan: $scanRuns, fix: $fixRuns, feedback: $fbRuns, total: ($scanRuns+$fixRuns+$fbRuns)} }'
