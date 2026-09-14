#!/usr/bin/env bash

# Ultimate goal: decompose oversized Go files into focused files while
# preserving behavior and improving tests.
# Selected grading-time effect: verify that one repository scan requests the
# action required by the largest non-test pkg/**/*.go file at the run SHA.
# Stronger effects are unavailable here: this workflow does not refactor code,
# and the grader runs before the safe-output job applies a requested issue.
# Metrics:
# - file-diet-decision-conformance (primary): 1 for the correct target-bound
#   issue/noop, 0 for an observed missing or contradictory decision, and null
#   when the run or repository evidence is unavailable or malformed.
# - largest-file-under-threshold: 1 when the largest file is below 800 lines,
#   0 when it is at least 800 lines, and null when the scan is unavailable.

set -euo pipefail

export LC_ALL=C

REPOSITORY="github/gh-aw"
WORKFLOW_NAME="Daily File Diet"
THRESHOLD_LINES=800

emit_metrics() {
    local primary=$1 healthy=$2

    jq -cn \
        --argjson primary "$primary" \
        --argjson healthy "$healthy" \
        '[
            {id: "file-diet-decision-conformance", value: $primary},
            {id: "largest-file-under-threshold", value: $healthy}
        ]'
}

request=$(cat)
if ! printf '%s\n' "$request" | jq -e \
    --arg repository "$REPOSITORY" \
    --arg workflow "$WORKFLOW_NAME" '
        .schemaVersion == 1
        and .run.repository == $repository
        and .run.workflow == $workflow
        and (.run.sha | type == "string" and test("^[0-9a-f]{40}$"))
        and (.event | type == "object")
        and (.outputs | type == "array")
        and (.config | type == "object")
    ' >/dev/null 2>&1; then
    emit_metrics null null
    exit 0
fi

run_sha=$(printf '%s\n' "$request" | jq -r '.run.sha')
subject=$(printf '%s\n' "$request" | jq -c '
    if .config.verification == true then .config.subject else null end
')

if [[ $subject != null ]]; then
    if ! printf '%s\n' "$subject" | jq -e '
        (.path | type == "string" and test("^pkg/.+\\.go$") and (endswith("_test.go") | not))
        and (.lines | type == "number" and . >= 0 and floor == .)
    ' >/dev/null 2>&1; then
        emit_metrics null null
        exit 0
    fi
    largest_path=$(printf '%s\n' "$subject" | jq -r '.path')
    largest_lines=$(printf '%s\n' "$subject" | jq -r '.lines')
else
    tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/daily-file-diet-operational-value.XXXXXX")
    trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM
    extract_dir="$tmp_dir/repository"
    mkdir -p "$extract_dir"

    repo_root=''
    if command -v git >/dev/null 2>&1; then
        repo_root=$(git rev-parse --show-toplevel 2>/dev/null || true)
    fi

    snapshot_root=''
    if [[ -n $repo_root ]] && git -C "$repo_root" cat-file -e "$run_sha^{commit}" 2>/dev/null; then
        if git -C "$repo_root" archive "$run_sha" -- pkg | tar -xf - -C "$extract_dir"; then
            snapshot_root=$extract_dir
        fi
    elif command -v gh >/dev/null 2>&1; then
        archive_file="$tmp_dir/repository.tar.gz"
        if gh api -H "Accept: application/vnd.github+json" \
            "repos/$REPOSITORY/tarball/$run_sha" >"$archive_file" 2>"$tmp_dir/gh-api-error" \
            && tar -xzf "$archive_file" -C "$extract_dir"; then
            snapshot_root=$(find "$extract_dir" -mindepth 1 -maxdepth 1 -type d -print -quit)
        fi
    fi

    if [[ -z $snapshot_root || ! -d $snapshot_root/pkg ]]; then
        emit_metrics null null
        exit 0
    fi

    largest_path=''
    largest_lines=-1
    while IFS= read -r -d '' source_file; do
        relative_path=${source_file#"$snapshot_root/"}
        line_count=$(wc -l <"$source_file" | tr -d ' ')
        if (( line_count > largest_lines )) \
            || { (( line_count == largest_lines )) && [[ $relative_path > $largest_path ]]; }; then
            largest_path=$relative_path
            largest_lines=$line_count
        fi
    done < <(find "$snapshot_root/pkg" -type f -name '*.go' ! -name '*_test.go' -print0)

    if [[ -z $largest_path || $largest_lines -lt 0 ]]; then
        emit_metrics null null
        exit 0
    fi
fi

if (( largest_lines < THRESHOLD_LINES )); then
    healthy=1
else
    healthy=0
fi

line_label="$largest_lines lines"
if (( healthy == 1 )); then
    output_valid=$(printf '%s\n' "$request" | jq -r \
        --arg path "$largest_path" \
        --arg lineLabel "$line_label" '
        if (.outputs | length) != 1 then false
        else .outputs[0] as $output
            | ($output.type == "noop")
            and ($output.message | type == "string" and length > 0)
            and ($output.message | contains($path))
            and ($output.message | contains($lineLabel))
            and ($output.message | ascii_downcase | test("healthy|no refactoring"))
        end
    ')
else
    output_valid=$(printf '%s\n' "$request" | jq -r \
        --arg path "$largest_path" \
        --arg lineLabel "$line_label" '
        if (.outputs | length) != 1 then false
        else .outputs[0] as $output
            | ($output.type == "create_issue")
            and ($output.title | type == "string" and length > 0)
            and ($output.body | type == "string" and length > 0)
            and ($output.body | contains($path))
            and ($output.body | contains($lineLabel))
            and ($output.body | test("(?i)refactoring strategy"))
            and ($output.body | test("(?i)test coverage"))
            and ($output.body | test("(?i)acceptance criteria"))
        end
    ')
fi

if [[ $output_valid == true ]]; then
    emit_metrics 1 "$healthy"
else
    emit_metrics 0 "$healthy"
fi
