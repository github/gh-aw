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

request=$(jq -cn '{
    schemaVersion: 1,
    run: {
        id: "1",
        attempt: 1,
        repository: "owner/repo",
        workflow: "Verification",
        ref: "refs/heads/main",
        sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        eventName: "workflow_dispatch"
    },
    event: {},
    config: {verification: true}
}')

metrics=$(printf '%s\n' "$request" | "$evaluator")
printf '%s\n' "$metrics" | jq -e '
    type == "array"
    and length > 0
    and all(.[ ];
        type == "object"
        and (keys | sort) == ["id", "value"]
        and (.id | type == "string" and length > 0)
        and (.value == null or (.value | type == "number" and isfinite and . >= 0 and . <= 1)))
    and ([.[].id] | unique | length) == length
' >/dev/null || fail "evaluator must return a non-empty array of unique {id,value} metrics with values in [0,1] or null"

printf 'verified %s\n' "$evaluator"