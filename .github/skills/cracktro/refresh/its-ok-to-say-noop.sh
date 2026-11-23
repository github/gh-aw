#!/usr/bin/env bash
# Read-only data pull for the cracktro "It's ok to say no(op)" slide (nop- measured rows).
# Lists completed agentic runs (.lock.yml, success|failure) in github/gh-aw for a UTC date window,
# saves `gh aw audit <id> --json --artifacts agent` per run, then aggregates only those audit JSONs
# (noops, missing_tools, missing_data, report_incomplete). Prints one JSON object on stdout.
# Usage: its-ok-to-say-noop.sh START END [JOBS]   (UTC dates, inclusive; default 8 parallel audits)
# Env: GH_AW (audit command, default "gh aw"), REPO (default github/gh-aw),
#      OUT (private audit cache dir you own; default a fresh mktemp dir, so pass OUT to reuse audits on rerun),
#      SAMPLE (runs to audit, evenly spaced over the window by run ID; default 1000, 0 = all),
#      ALLOW_PARTIAL (1 = print results even when some audits failed; default fails).
set -euo pipefail
START="${1:?START date YYYY-MM-DD}"; END="${2:?END date YYYY-MM-DD}"; JOBS="${3:-8}"
REPO="${REPO:-github/gh-aw}"
GH_AW="${GH_AW:-gh aw}"
SAMPLE="${SAMPLE:-1000}"
umask 077
if [ -z "${OUT:-}" ]; then
  OUT="$(mktemp -d "${TMPDIR:-/tmp}/nop-audit.XXXXXX")"
  echo "audit cache: $OUT" >&2
else
  mkdir -p "$OUT"
fi
if [ -L "$OUT" ] || [ ! -O "$OUT" ]; then echo "OUT must be a directory you own, not a symlink: $OUT" >&2; exit 1; fi
chmod 700 "$OUT"
mkdir -p "$OUT/json"
ALL="$OUT/runs-all.tsv"; IDS="$OUT/runs.tsv"

: > "$ALL"
# Per-workflow queries: the repo-wide runs list caps filtered results at 1,000 (under one day here).
gh api "repos/$REPO/actions/workflows" --paginate --jq '.workflows[] | select(.path|endswith(".lock.yml")) | .id' |
while read -r wid; do
  gh api -X GET "repos/$REPO/actions/workflows/$wid/runs" -f created="$START..$END" -f status=completed -f per_page=100 --paginate \
    --jq '.workflow_runs[] | select(.conclusion=="success" or .conclusion=="failure") | .id'
done >> "$ALL"

# Run IDs grow with time: an evenly spaced pick over the sorted IDs spreads the sample over the window and all workflows.
sort -un "$ALL" | awk -v n="$SAMPLE" '{ id[NR] = $1 } END { if (n <= 0 || n >= NR) n = NR; for (i = 0; i < n; i++) print id[int(i * NR / n) + 1] }' > "$IDS"

export GH_AW REPO OUT
# One run per audit call (several IDs switch audit to diff mode). Saved JSON is reused on rerun.
# A failed audit keeps its stderr in $OUT/json/<id>.err and exits non-zero.
# shellcheck disable=SC2016
xargs -P "$JOBS" -I{} sh -c \
  'j="$OUT/json/$1.json"; [ -s "$j" ] && exit 0
   if $GH_AW audit "$1" --repo "$REPO" --json --artifacts agent --no-baseline -o "$OUT" > "$j.tmp" 2> "$OUT/json/$1.err" && [ -s "$j.tmp" ]; then
     mv "$j.tmp" "$j"; rm -f "$OUT/json/$1.err"
   else rm -f "$j.tmp"; exit 1; fi' _ {} < "$IDS" || true

MISSING="$OUT/missing.txt"
while read -r id; do [ -s "$OUT/json/$id.json" ] || echo "$id"; done < "$IDS" > "$MISSING"
if [ -s "$MISSING" ]; then
  echo "$(wc -l < "$MISSING" | tr -d ' ') sampled audits failed (IDs in $MISSING, stderr in $OUT/json/<id>.err); rerun with the same OUT to retry." >&2
  [ "${ALLOW_PARTIAL:-0}" = 1 ] || exit 1
fi

while read -r id; do
  if [ -s "$OUT/json/$id.json" ]; then cat "$OUT/json/$id.json"; fi
done < "$IDS" | jq -s --arg s "$START" --arg e "$END" --arg snap "$(date -u +%F)" --argjson listed "$(sort -u "$ALL" | wc -l)" --argjson sampled "$(wc -l < "$IDS")" --argjson missing "$(wc -l < "$MISSING")" '
  def kinds: {noop:"noops", missing_tool:"missing_tools", missing_data:"missing_data", report_incomplete:"incomplete"};
  map(. as $r | {w:.overview.workflow_name, agent:((.metrics.turns // 0) > 0),
                 n:(kinds | map_values(($r[.] // []) | length))}) as $a
  | ($a | map(select(.agent))) as $g
  | {
    window: {start:$s, end:$e}, snapshot:$snap,
    runs: $listed, sampled: $sampled, audited: ($a|length), missing: $missing, agent_runs: ($g|length),
    runs_with: (kinds | with_entries(.key as $k | .value = ($g | map(select(.n[$k] > 0)) | length))),
    items: (kinds | with_entries(.key as $k | .value = ([$g[].n[$k]] | add // 0))),
    quiet: ($g | map(select(.n.noop > 0 and .n.missing_tool == 0 and .n.missing_data == 0 and .n.report_incomplete == 0)) | length),
    top_noop: ($g | map(select(.n.noop > 0)) | group_by(.w) | map({w:.[0].w, n:length}) | sort_by(-.n) | .[:5])
  }'
