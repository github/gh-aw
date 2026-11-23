#!/usr/bin/env bash
# Summarize honest-outcome safe outputs (noop, report_missing_tool,
# report_missing_data, report_incomplete) across downloaded gh-aw runs.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: safe-output-stats.sh [--dir DIR] [--download] [--format table|json] [-- LOGS_ARGS...]

  --dir DIR       Logs directory containing run-<id>/ folders (default: .github/aw/logs)
  --download      Run `gh aw logs --artifacts agent -o DIR LOGS_ARGS...` first
  --format FMT    table (default) or json
  LOGS_ARGS       Extra `gh aw logs` arguments (workflow name, -c, --start-date, --repo, ...)
EOF
}

dir=".github/aw/logs"
download=0
format="table"
logs_args=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir) dir="$2"; shift 2 ;;
    --download) download=1; shift ;;
    --format) format="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    --) shift; logs_args=("$@"); break ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done
[[ "$format" == "table" || "$format" == "json" ]] || { echo "invalid --format: $format" >&2; exit 2; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

if [[ $download -eq 1 ]]; then
  gh aw logs --artifacts agent -o "$dir" ${logs_args[@]+"${logs_args[@]}"} >&2
fi
[[ -d "$dir" ]] || { echo "logs directory not found: $dir (use --download)" >&2; exit 1; }

# One JSON record per run: workflow, conclusion and the safe-output item types.
records() {
  local run out summary
  for run in "$dir"/run-*/; do
    [[ -d "$run" ]] || continue
    out=$(find "$run" -name agent_output.json -type f 2>/dev/null | head -n 1)
    summary="${run}run_summary.json"
    jq -n -c \
      --arg id "$(basename "$run" | sed 's/^run-//')" \
      --slurpfile s <( [[ -f "$summary" ]] && cat "$summary" || echo '{}' ) \
      --slurpfile o <( [[ -n "$out" ]] && cat "$out" || echo '{"missing":true}' ) '
      ($s[0].run // {}) as $r
      | {
          run_id: $id,
          workflow: ($r.workflowName // $r.workflow_name // "unknown"),
          conclusion: ($r.conclusion // "unknown"),
          has_output: (($o[0].missing // false) | not),
          types: [($o[0].items // [])[] | .type // empty]
        }' 2>/dev/null || echo "warning: skipped unreadable $run" >&2
  done
}

jq -s --arg format "$format" '
  def norm: if . == "missing_tool" then "report_missing_tool"
            elif . == "missing_data" then "report_missing_data" else . end;
  def honest: ["noop","report_missing_tool","report_missing_data","report_incomplete"];
  def classify:
    (.types | map(norm)) as $t
    | if (.has_output | not) then "no_output"
      elif ($t | index("report_incomplete")) then "incomplete"
      elif ($t | map(select(. as $x | honest | index($x) | not)) | length) > 0 then "acted"
      elif ($t | index("report_missing_tool")) or ($t | index("report_missing_data")) then "blocked"
      elif ($t | index("noop")) then "noop"
      else "silent" end;
  def counts: (map(.types | map(norm)) | add // []) as $all
    | reduce honest[] as $k ({}; .[$k] = ($all | map(select(. == $k)) | length));
  def outcomes: (map(classify) | group_by(.) | map({key: .[0], value: length}) | from_entries);
  def stats: {runs: length, items: counts, outcomes: outcomes,
              noop_rate: (if length > 0 then ((map(select(classify == "noop")) | length) / length * 100 | round) else 0 end)};
  . as $runs
  | {total: ($runs | stats),
     by_workflow: ($runs | group_by(.workflow) | map({workflow: .[0].workflow} + stats) | sort_by(-.runs))}
  | if $format == "json" then .
    else
      (["WORKFLOW","RUNS","NOOP","MISSING_TOOL","MISSING_DATA","INCOMPLETE","ACTED","BLOCKED","SILENT","NO_OUTPUT","NOOP_%"] | @tsv),
      ((.by_workflow[]), (.total + {workflow: "TOTAL"})
        | [.workflow, .runs, .items.noop, .items.report_missing_tool, .items.report_missing_data,
           .items.report_incomplete, (.outcomes.acted // 0), (.outcomes.blocked // 0),
           (.outcomes.silent // 0), (.outcomes.no_output // 0), .noop_rate]
        | @tsv)
    end
' -r < <(records) | if [[ "$format" == "table" ]]; then column -t -s $'\t'; else cat; fi
