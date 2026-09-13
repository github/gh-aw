#!/usr/bin/env bash

set -euo pipefail

fail() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

[[ $# -eq 1 ]] || fail "usage: verify-operational-value-evaluator.sh <operational-value-evaluator.sh>"

evaluator=$1
[[ -f $evaluator ]] || fail "operational-value evaluator not found: $evaluator"
[[ -x $evaluator ]] || fail "operational-value evaluator is not executable: $evaluator"
command -v jq >/dev/null 2>&1 || fail "jq is required"
bash -n "$evaluator"

max_output_bytes=$((1024 * 1024))

request=$(jq -cn '{
    schemaVersion: 1,
    run: {
        id: "1",
        attempt: 1,
        repository: "owner/repo",
        workflow: "Verification workflow",
        ref: "refs/heads/main",
        sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        eventName: "workflow_dispatch"
    },
    event: {},
    config: {verification: true}
}')

if ! output=$(printf '%s\n' "$request" | "$evaluator"); then
    fail "operational-value evaluator exited unsuccessfully"
fi
output_bytes=$(printf '%s' "$output" | LC_ALL=C wc -c | tr -d ' ')
(( output_bytes <= max_output_bytes )) || fail "operational-value evaluator output exceeds 1 MiB"

printf '%s\n' "$output" | jq -se '
    length == 1
    and (.[0] | type == "array" and length > 0)
    and (.[0] | all(.[];
        type == "object"
        and keys == ["id", "value"]
        and (.id | type == "string" and test("[^[:space:]]"))
        and (.value == null or (.value | type == "number" and isfinite and . >= 0 and . <= 1))))
    and (.[0] | [.[].id] | unique | length) == (.[0] | length)
' >/dev/null || fail "operational-value evaluator must print exactly one non-empty array of unique {id,value} metrics"

printf 'verified %s\n' "$evaluator"