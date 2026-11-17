#!/usr/bin/env bash
# Read-only: recompute the measured strip of the cracktro "Decide · Draft · Record" slide from docs/adr on origin/main.
# Usage: decide-draft-record.sh [YYYY-MM-DD snapshot, default today UTC]  → JSON on stdout
set -euo pipefail
SNAP="${1:-$(date -u +%F)}"
REF="${REF:-origin/main}"
git fetch -q origin main 2>/dev/null || true
# pin every metric to the last first-parent commit on or before the snapshot day (UTC)
COMMIT=$(git rev-list -1 --first-parent --before="${SNAP}T23:59:59Z" "$REF")
[ -n "$COMMIT" ] || { echo "no commit on $REF before $SNAP" >&2; exit 1; }
files=$(git ls-tree --name-only "$COMMIT" docs/adr/ | grep -E '^docs/adr/[0-9]+-.*\.md$' || true)
total=$(printf '%s\n' "$files" | grep -c . || true)

# first add (or rename-in) date per ADR file (UTC): minimum date per file, restricted to the snapshot file set
adds=$(git log "$COMMIT" --diff-filter=AR --format='@%cs' --name-only -- docs/adr/ |
  awk -v keep="$(printf '%s\n' "$files" | tr '\n' ' ')" 'BEGIN{n=split(keep,k," ");for(i=1;i<=n;i++)K[k[i]]=1}
    /^@/{d=substr($0,2);next} NF&&K[$0]&&(!($0 in A)||d<A[$0]){A[$0]=d} END{for(f in A)print A[f]}')

monthly=$(printf '%s\n' "$adds" | cut -c1-7 | sort | uniq -c | awk '{printf "%s[\"%s\",%d]", (NR>1?",":""), $2, $1}')
first=$(printf '%s\n' "$adds" | sort | head -1)
since=$(date -u -j -v-30d -f %F "$SNAP" +%F 2>/dev/null || date -u -d "$SNAP -30 days" +%F)
last30=$(printf '%s\n' "$adds" | awk -v s="$since" '$0>s' | wc -l | tr -d ' ')

# one read per file; grep reads a here-string, so no-match and early exit cannot fail the pipeline
footer=0 statuses=""
for f in $files; do
  body=$(git show "$COMMIT:$f")
  if grep -q 'ADR created by \[adr-writer agent\]' <<<"$body"; then footer=$((footer + 1)); fi
  s=$(grep -m1 -iE '^\*\*Status\*\*:|^Status:' <<<"$body" || echo none)
  statuses+="$s"$'\n'
done
status=$(printf '%s' "$statuses" | sed -E 's/.*[Ss]tatus\**: *//; s/[^A-Za-z].*//' |
  awk '{c[tolower($1)]++} END{for(k in c) printf "%s\"%s\":%d", (n++?",":""), k, c[k]}')

printf '{"snapshot":"%s","ref":"%s","commit":"%s","total":%s,"first":"%s","last30":%s,"agentFooter":%s,"status":{%s},"monthly":[%s]}\n' \
  "$SNAP" "$REF" "$COMMIT" "$total" "$first" "$last30" "$footer" "$status" "$monthly"
