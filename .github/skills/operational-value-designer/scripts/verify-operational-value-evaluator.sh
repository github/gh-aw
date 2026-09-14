#!/usr/bin/env bash

set -euo pipefail

fail() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

(( $# == 1 || $# == 2 )) || fail "usage: verify-operational-value-evaluator.sh <operational-value-evaluator.sh> [fixtures.json]"

evaluator=$1
fixtures=${2:-}
[[ -f $evaluator ]] || fail "operational-value evaluator not found: $evaluator"
[[ -x $evaluator ]] || fail "operational-value evaluator is not executable: $evaluator"
command -v jq >/dev/null 2>&1 || fail "jq is required"
bash -n "$evaluator"

max_output_bytes=$((1024 * 1024))

validate_output() {
    local output_file=$1
    local context=$2
    local output_bytes
    local text_bytes

    output_bytes=$(LC_ALL=C wc -c < "$output_file" | tr -d ' ')
    (( output_bytes <= max_output_bytes )) || fail "$context output exceeds 1 MiB"
    text_bytes=$(LC_ALL=C tr -d '\000' < "$output_file" | LC_ALL=C wc -c | tr -d ' ')
    (( text_bytes == output_bytes )) || fail "$context output must not contain NUL bytes"

    jq -se '
        length == 1
        and (.[0] | type == "array" and length > 0)
        and (.[0] | all(.[];
            type == "object"
            and keys == ["id", "value"]
            and (.id | type == "string" and test("[^[:space:]]"))
            and (.value == null or (.value | type == "number" and isfinite))))
        and (.[0] | [.[].id] | unique | length) == (.[0] | length)
    ' "$output_file" >/dev/null || fail "$context must print exactly one non-empty array of unique {id,value} metrics"
}

output_file=$(mktemp "${TMPDIR:-/tmp}/operational-value-output.XXXXXX")
trap 'rm -f "$output_file"' EXIT HUP INT TERM

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
    outputs: [],
    config: {verification: true}
}')

if ! printf '%s\n' "$request" | "$evaluator" > "$output_file"; then
    fail "operational-value evaluator exited unsuccessfully"
fi
validate_output "$output_file" "operational-value evaluator"

if [[ -n $fixtures ]]; then
    [[ -f $fixtures ]] || fail "operational-value fixtures not found: $fixtures"
    fixture_count=$(jq -er '
        . as $fixtures
        | if (($fixtures | type) == "array"
            and ($fixtures | length) >= 4
            and all($fixtures[];
                type == "object"
                and keys == ["expected", "name", "request"]
                and (.name | type == "string" and length > 0)
                and (.request | type == "object")
                and (.expected | type == "array"))
            and ([$fixtures[].name] | unique | length) == ($fixtures | length)
            and (["attained", "missed", "unavailable", "malformed"] - [$fixtures[].name] | length == 0))
          then ($fixtures | length)
          else error("invalid fixtures")
          end
    ' "$fixtures") || fail "operational-value fixtures are invalid"

    fixture_index=0
    attained_value=null
    missed_value=null
    unavailable_value=false
    malformed_value=false
    metric_ids=
    while (( fixture_index < fixture_count )); do
        fixture_name=$(jq -er ".[$fixture_index].name" "$fixtures")
        fixture_request=$(jq -c ".[$fixture_index].request" "$fixtures")
        fixture_expected=$(jq -cS ".[$fixture_index].expected" "$fixtures")

        printf '%s\n' "$fixture_expected" > "$output_file"
        validate_output "$output_file" "fixture $fixture_name expected"
        if ! printf '%s\n' "$fixture_request" | "$evaluator" > "$output_file"; then
            fail "operational-value evaluator exited unsuccessfully for fixture $fixture_name"
        fi
        validate_output "$output_file" "fixture $fixture_name"
        fixture_actual=$(jq -cS . "$output_file")
        [[ $fixture_actual == "$fixture_expected" ]] || fail "fixture $fixture_name output does not match expected metrics"
        fixture_metric_ids=$(jq -c '[.[].id]' "$output_file")
        if [[ -z $metric_ids ]]; then
            metric_ids=$fixture_metric_ids
        else
            [[ $fixture_metric_ids == "$metric_ids" ]] || fail "fixture $fixture_name changes metric IDs or order"
        fi

        if ! printf '%s\n' "$fixture_request" | "$evaluator" > "$output_file"; then
            fail "operational-value evaluator exited unsuccessfully when repeating fixture $fixture_name"
        fi
        repeated_actual=$(jq -cS . "$output_file")
        [[ $repeated_actual == "$fixture_actual" ]] || fail "fixture $fixture_name is not deterministic"

        if [[ $fixture_name == attained ]]; then
            attained_value=$(jq -c '.[0].value' "$output_file")
        elif [[ $fixture_name == missed ]]; then
            missed_value=$(jq -c '.[0].value' "$output_file")
        elif [[ $fixture_name == unavailable ]]; then
            unavailable_value=$(jq -c '.[0].value' "$output_file")
        elif [[ $fixture_name == malformed ]]; then
            malformed_value=$(jq -c '.[0].value' "$output_file")
        fi
        fixture_index=$((fixture_index + 1))
    done

    jq -en --argjson attained "$attained_value" --argjson missed "$missed_value" '
        $attained != null and $missed != null and $attained != $missed
    ' >/dev/null || fail "attained and missed fixtures must have distinct primary values"
    [[ $unavailable_value == null ]] || fail "unavailable fixture primary metric must be null"
    [[ $malformed_value == null ]] || fail "malformed fixture primary metric must be null"
fi

printf 'verified %s\n' "$evaluator"