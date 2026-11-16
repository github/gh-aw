#!/usr/bin/env bash
# Read-only: recompute the measured strip of the cracktro "Decide · Draft · Record" slide from docs/adr on origin/main.
# Usage: decide-draft-record.sh [YYYY-MM-DD snapshot, default today UTC]  → JSON on stdout
set -euo pipefail
SNAP="${1:-$(date -u +%F)}"
REF="${REF:-origin/main}"
git fetch -q origin main 2>/dev/null || true
files=$(git ls-tree --name-only "$REF" docs/adr/ | grep -E '^docs/adr/[0-9]+-.*\.md$')
total=$(printf '%s\n' "$files" | wc -l | tr -d ' ')

# first add (or rename-in) date per ADR file (UTC), restricted to the current file set
adds=$(git log "$REF" --until="${SNAP}T23:59:59Z" --diff-filter=AR --format='@%cs' --name-only -- docs/adr/ |
  awk -v keep="$(printf '%s\n' "$files" | tr '\n' ' ')" 'BEGIN{n=split(keep,k," ");for(i=1;i<=n;i++)K[k[i]]=1} /^@/{d=substr($0,2);next} NF&&K[$0]&&!S[$0]++{print d}')

monthly=$(printf '%s\n' "$adds" | cut -c1-7 | sort | uniq -c | awk '{printf "%s[\"%s\",%d]", (NR>1?",":""), $2, $1}')
first=$(printf '%s\n' "$adds" | sort | head -1)
since=$(date -u -j -v-30d -f %F "$SNAP" +%F 2>/dev/null || date -u -d "$SNAP -30 days" +%F)
last30=$(printf '%s\n' "$adds" | awk -v s="$since" '$0>s' | wc -l | tr -d ' ')

status() { for f in $files; do git show "$REF:$f" | grep -m1 -iE '^\*\*Status\*\*:|^Status:' || echo none; done |
  sed -E 's/.*[Ss]tatus\**: *//; s/[^A-Za-z].*//' | awk '{c[tolower($1)]++} END{for(k in c) printf "%s\"%s\":%d", (n++?",":""), k, c[k]}'; }
footer=$(for f in $files; do git show "$REF:$f" | grep -q 'ADR created by \[adr-writer agent\]' && echo 1; done | wc -l | tr -d ' ')

printf '{"snapshot":"%s","ref":"%s","total":%s,"first":"%s","last30":%s,"agentFooter":%s,"status":{%s},"monthly":[%s]}\n' \
  "$SNAP" "$REF" "$total" "$first" "$last30" "$footer" "$(status)" "$monthly"
