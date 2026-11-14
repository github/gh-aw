#!/usr/bin/env bash
# Read-only: recompute the measured data on the cracktro "Spec · Model · Test" slide.
# Usage: bash .github/skills/cracktro/refresh/spec-model-test.sh [SINCE=YYYY-MM-DD]
# Needs: git, gh (authenticated, read access), jq, python3. Prints one JSON object.
set -euo pipefail
REPO="${REPO:-github/gh-aw}"
SINCE="${1:-2026-10-02}"
WF="daily-work-queue-formal-verification.lock.yml"
cd "$(git rev-parse --show-toplevel)/specs/work-queue"

modules=$(ls *.tla | wc -l | tr -d ' ')
configs=$(ls *.cfg | wc -l | tr -d ' ')
mutants=$(ls Broken*.cfg | wc -l | tr -d ' ')
witnesses=$(ls *Witness*.cfg | wc -l | tr -d ' ')
prs=$(git log --since="$SINCE" --format=%s -- . | grep -oE '\(#[0-9]+\)$' | sort -u | wc -l | tr -d ' ')

record=$(ls verification-*.json | sort | tail -1)
rec=$(python3 - "$record" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
pick = {r["config"]: r for r in d["results"]}
def c(name):
    r = pick.get(name, {})
    return {"verdict": r.get("verdict"), "diagnostic": r.get("expected_diagnostic"),
            "distinct": (r.get("final_counts") or [None, None])[1]}
print(json.dumps({"file": sys.argv[1], "date": d["date"],
                  "BrokenCAS": c("BrokenCAS"), "WeakOrderingWitness": c("WeakOrderingWitness")}))
PY
)

runs=$(gh api --paginate "repos/$REPO/actions/workflows/$WF/runs?per_page=100" \
  --jq '.workflow_runs[] | [.id, .conclusion, .created_at] | @tsv')
total=$(printf '%s\n' "$runs" | grep -c . || true)
ok=$(printf '%s\n' "$runs" | awk -F'\t' '$2=="success"' | wc -l | tr -d ' ')
failed=$(printf '%s\n' "$runs" | awk -F'\t' '$2=="failure"' | wc -l | tr -d ' ')
last=$(printf '%s\n' "$runs" | awk -F'\t' '$2=="success"{print $1; exit}')

tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
gh run download "$last" -R "$REPO" -D "$tmp" -p 'work-queue-formal-FairDAGGitHub-*' -p 'work-queue-formal-QueueOrdering-*' >/dev/null
tlc=$(jq -s 'map({(.config): {status, exhausted, started_at, elapsed_seconds,
  distinct: ((.final_counts // .latest_progress).distinct_states | tonumber),
  max_depth: (.depth // .latest_progress.depth)}}) | add' "$tmp"/work-queue-formal-*/result.json)

jq -n --arg since "$SINCE" --arg snapshot "$(date -u +%F)" \
  --argjson modules "$modules" --argjson configs "$configs" --argjson mutants "$mutants" \
  --argjson witnesses "$witnesses" --argjson prs "$prs" --argjson rec "$rec" \
  --argjson total "$total" --argjson ok "$ok" --argjson failed "$failed" --arg last "$last" --argjson tlc "$tlc" \
  '{snapshot: $snapshot, window: {since: $since, until: $snapshot},
    specs: {modules: $modules, configs: $configs, mutants: $mutants, witnesses: $witnesses, merged_prs_since: $prs},
    record: $rec,
    daily: {runs: $total, success: $ok, failure: $failed, last_success_run: $last, tlc: $tlc}}'
