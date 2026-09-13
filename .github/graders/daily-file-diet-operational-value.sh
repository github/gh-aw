#!/usr/bin/env bash

set -euo pipefail

export LC_ALL=C

METRIC_ID=large-file-triage-output
THRESHOLD_LINES=800
AGENT_OUTPUT_PATH=${GH_AW_AGENT_OUTPUT:-/tmp/gh-aw/agent_output.json}

emit_metric() {
    jq -cn --arg id "$METRIC_ID" --argjson value "$1" '[{id: $id, value: $value}]'
}

if [[ $# -ne 0 ]]; then
    printf 'error: this evaluator accepts no arguments\n' >&2
    exit 1
fi

request=$(cat)
if ! printf '%s\n' "$request" | jq -e '
    .schemaVersion == 1
    and (.run | type == "object")
    and (.event == null or (.event | type == "object"))
    and (.config | type == "object")
' >/dev/null 2>&1; then
    emit_metric null
    exit 0
fi

if [[ ! -d pkg ]]; then
    emit_metric null
    exit 0
fi

largest_lines=-1
largest_file=
while IFS= read -r -d '' source_file; do
    line_count=$(wc -l <"$source_file" | tr -d ' ')
    if (( line_count > largest_lines )); then
        largest_lines=$line_count
        largest_file=$source_file
    fi
done < <(find pkg -type f -name '*.go' ! -name '*_test.go' -print0)

if (( largest_lines < 0 )); then
    emit_metric null
    exit 0
fi

if [[ ! -f $AGENT_OUTPUT_PATH ]]; then
    emit_metric null
    exit 0
fi
if ! jq -e '.items | type == "array"' "$AGENT_OUTPUT_PATH" >/dev/null 2>&1; then
    emit_metric null
    exit 0
fi

if (( largest_lines < THRESHOLD_LINES )); then
    value=$(jq -e '
        (.items | type == "array")
        and any(.items[]; .type == "noop")
        and (any(.items[]; .type == "create_issue") | not)
    ' "$AGENT_OUTPUT_PATH" >/dev/null 2>&1 && printf 1 || printf 0)
else
    value=$(jq -e --arg source_file "$largest_file" '
        (.items | type == "array")
        and any(.items[];
            .type == "create_issue"
            and (((.title // "") + "\n" + (.body // "")) | contains($source_file)))
    ' "$AGENT_OUTPUT_PATH" >/dev/null 2>&1 && printf 1 || printf 0)
fi

emit_metric "$value"
