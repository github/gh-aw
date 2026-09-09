#!/usr/bin/env bash

set -euo pipefail

export LC_ALL=C

REPOSITORY=github/gh-aw
WORKFLOW_NAME="Daily File Diet"
THRESHOLD_LINES=1000
TARGET_LINES=500
MATURATION_SECONDS=172800

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/daily-file-diet-operational-value.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

local_repo_root=''
if command -v git >/dev/null 2>&1; then
    local_repo_root=$(git rev-parse --show-toplevel 2>/dev/null || true)
fi

definition() {
    cat <<'JSON'
{
  "schemaVersion": 4, "grader": "operational-value",
  "repository": "github/gh-aw", "workflowName": "Daily File Diet",
  "sourcePath": ".github/workflows/daily-file-diet.md",
  "adoption": {"commit": "1186030a620f4113f655c156bedf70cf2c164f79", "adoptedAt": "2025-11-15T13:36:21Z"},
  "operationalValue": "Decompose the run's largest oversized non-test Go file toward 500 lines.",
  "evidence": {
    "opportunity": "Largest non-test pkg/**/*.go blob at the run commit; below 1000 is healthy.",
    "assignment": "Greatest wc -l, reverse lexical tie-break. Key: go-file:<path> or repository-health:non-test-go-under-1000; duplicates repeat.",
    "accepted": "Git evidence of assigned-path reduction toward 500 lines or tree-proven absence; issues and traces are excluded.",
    "repositories": ["github/gh-aw"],
    "collection": "With contents:read, count newlines in the run commit archive for assignment and in the cutoff commit blob for evidence.",
    "maturation": "Two days; five pre-adoption issue/PR pairs (#1636-#3564) took 0.04-0.84 days.",
    "zeroRule": "No reduction from the initial oversized file scores 0.",
    "missingRule": "Invalid, unavailable, or truncated Git evidence scores null; tree-proven path absence is attainment."
  },
  "primaryMetric": {"id": "assigned-go-file-decomposition", "formula": "initialLines < 1000 => 1; else clamp((initialLines-currentLines)/(initialLines-500),0,1). Proven absence sets currentLines=0.", "direction": "higher_is_better"},
    "diagnosticMetrics": [
        {"id": "largest-file-health", "name": "Largest-file health at assignment", "formula": "min(1, 999 / initialLines) when the assignment archive contains eligible files.", "direction": "higher_is_better", "aggregation": "latest"},
        {"id": "compliant-line-mass-share", "name": "Compliant line-mass share at assignment", "formula": "compliantLines / totalLines when the assignment archive contains eligible files and positive line mass.", "direction": "higher_is_better", "aggregation": "latest"}
    ],
  "baseline": {"mode": "attainment-only", "value": null, "evidenceCutoff": null, "provenance": []},
  "validationExamples": {
    "targetAttained": {"valid":true,"initialLines":1907,"currentLines":500,"thresholdLines":1000,"targetLines":500},
    "targetMissed": {"valid":true,"initialLines":1907,"currentLines":1907,"thresholdLines":1000,"targetLines":500},
    "missing": {"valid":false},
    "malformed": {"valid":"yes","initialLines":"1907"}
  }
}
JSON
}

metric() {
    local evidence
    evidence=$(cat)
    if ! printf '%s\n' "$evidence" | jq -e . >/dev/null 2>&1; then
        printf 'null\n'
        return
    fi

    printf '%s\n' "$evidence" | jq '
      if .valid != true or ([.initialLines,.currentLines,.thresholdLines,.targetLines]|all(.[];type=="number")|not)
        or .initialLines<0 or .currentLines<0 or .targetLines<0 or .thresholdLines<=.targetLines then null
      elif .initialLines<.thresholdLines then 1
      elif .initialLines<=.targetLines then null
      else ((.initialLines-.currentLines)/(.initialLines-.targetLines)) as $v
        | if $v<0 then 0 elif $v>1 then 1 else $v end
      end'
}

normalize_timestamp() {
    jq -nr --arg timestamp "$1" '
        ($timestamp | sub("\\.[0-9]+Z$"; "Z")) as $normalized
        | if ($normalized | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$"))
            and (try (($normalized | fromdateiso8601 | todateiso8601) == $normalized) catch false)
        then $normalized
        else error("invalid timestamp")
        end
    ' 2>/dev/null
}

timestamp_epoch() {
    jq -nr --arg timestamp "$1" '$timestamp | fromdateiso8601'
}

add_seconds() {
    jq -nr --arg timestamp "$1" --argjson seconds "$2" \
        '$timestamp | fromdateiso8601 + $seconds | todateiso8601'
}

github_api() {
    gh api "$@" 2>"$tmp_dir/gh-api-error"
}

blob_line_count() {
    local repository=$1 blob_sha=$2 output=$3

    if ! github_api -H "Accept: application/vnd.github.raw+json" \
        "repos/$repository/git/blobs/$blob_sha" >"$output"; then
        return 1
    fi
    wc -l <"$output" | tr -d ' '
}

load_tree() {
    local repository=$1 commit_sha=$2 output=$3

    if ! github_api "repos/$repository/git/trees/$commit_sha?recursive=1" >"$output"; then
        return 1
    fi
    jq -e '.truncated == false and (.tree | type == "array")' "$output" >/dev/null
}

assign_case() {
    local repository=$1 commit_sha=$2
    local archive_file="$tmp_dir/assignment-archive.tar.gz"
    local extract_dir="$tmp_dir/assignment-archive"
    local root_dir path lines
    local largest_path='' largest_lines=-1
    local eligible_file_count=0 total_lines=0 compliant_lines=0

    mkdir -p "$extract_dir"
    if [[ -n $local_repo_root ]] \
        && git -C "$local_repo_root" cat-file -e "$commit_sha^{commit}" 2>/dev/null; then
        git -C "$local_repo_root" archive "$commit_sha" -- pkg | tar -xf - -C "$extract_dir" || return 1
        root_dir=$extract_dir
    else
        github_api -H "Accept: application/vnd.github+json" \
            "repos/$repository/tarball/$commit_sha" >"$archive_file" || return 1
        tar -xzf "$archive_file" -C "$extract_dir" || return 1
        root_dir=$(find "$extract_dir" -mindepth 1 -maxdepth 1 -type d -print -quit)
    fi
    [[ -n $root_dir && -d $root_dir/pkg ]] || return 1

    while IFS= read -r -d '' path; do
        lines=$(wc -l <"$root_dir/$path" | tr -d ' ') || return 1
        eligible_file_count=$((eligible_file_count + 1))
        total_lines=$((total_lines + lines))
        if (( lines < THRESHOLD_LINES )); then
            compliant_lines=$((compliant_lines + lines))
        fi
        if (( lines > largest_lines )) \
            || { (( lines == largest_lines )) && [[ $path > $largest_path ]]; }; then
            largest_path=$path
            largest_lines=$lines
        fi
    done < <(cd "$root_dir" && find pkg -type f -name '*.go' ! -name '*_test.go' -print0)

    [[ -n $largest_path ]] || return 1

    jq -cn \
        --arg path "$largest_path" \
        --argjson initialLines "$largest_lines" \
        --argjson thresholdLines "$THRESHOLD_LINES" \
        --argjson targetLines "$TARGET_LINES" \
                --argjson eligibleFileCount "$eligible_file_count" \
                --argjson totalLines "$total_lines" \
                --argjson compliantLines "$compliant_lines" \
        --arg subjectSha "$commit_sha" \
        '{path: $path, initialLines: $initialLines, thresholdLines: $thresholdLines,
                    targetLines: $targetLines, subjectSha: $subjectSha,
                    eligibleFileCount: $eligibleFileCount, totalLines: $totalLines,
                    compliantLines: $compliantLines}'
}

case_diagnostics() {
        local case_json=$1 current_lines=$2

        printf '%s\n' "$case_json" | jq -c \
                --argjson currentLines "$current_lines" \
                --argjson healthyLimit "$((THRESHOLD_LINES - 1))" '
                {
                    "largest-file-health":
                        (if (.initialLines | type) == "number" and .initialLines > 0
                         then ([1, ($healthyLimit / .initialLines)] | min) else null end),
                    "compliant-line-mass-share":
                        (if (.eligibleFileCount | type) == "number" and .eligibleFileCount > 0
                                and (.totalLines | type) == "number" and .totalLines > 0
                                and (.compliantLines | type) == "number"
                         then (.compliantLines / .totalLines) else null end),
                    currentLines: $currentLines
                }'
}

latest_commit_at_cutoff() {
    local repository=$1 cutoff=$2
    local repository_json default_branch commits_json local_commit

    if [[ -n $local_repo_root ]] \
        && git -C "$local_repo_root" show-ref --verify --quiet refs/remotes/origin/main; then
        local_commit=$(git -C "$local_repo_root" rev-list -1 --before="$cutoff" refs/remotes/origin/main) || return 1
        if [[ -n $local_commit ]]; then
            printf '%s\n' "$local_commit"
            return
        fi
    fi

    repository_json=$(github_api "repos/$repository") || return 1
    default_branch=$(printf '%s\n' "$repository_json" | jq -er '.default_branch | select(type == "string" and length > 0)') \
        || return 1
    commits_json=$(github_api -X GET "repos/$repository/commits" \
        -f sha="$default_branch" -f until="$cutoff" -f per_page=1) || return 1
    printf '%s\n' "$commits_json" | jq -er '.[0].sha | select(type == "string" and test("^[0-9a-f]{40}$"))'
}

emit_null() {
    local opportunity_key=$1 case_json=$2 evidence_cutoff=$3 matures_at=$4 reason=$5

    jq -cn \
        --arg opportunityKey "$opportunity_key" \
        --argjson case "$case_json" \
        --arg evidenceCutoff "$evidence_cutoff" \
        --arg maturesAt "$matures_at" \
        --arg reason "$reason" \
        '{value: null, opportunityKey: $opportunityKey, case: $case,
          evidenceCutoff: $evidenceCutoff, maturesAt: $maturesAt,
          provenance: [], diagnostics: {missingReason: $reason}}'
}

grade_run() {
    local request run_id repository workflow run_sha created_at evidence_at
    local matures_at evidence_cutoff evidence_epoch matures_epoch
    local case_json path initial_lines opportunity_key evidence value diagnostics
    local cutoff_commit blob_sha current_lines
    local tree_file="$tmp_dir/cutoff-tree.json"
    local blob_file="$tmp_dir/cutoff-blob"

    request=$(cat)
    if ! printf '%s\n' "$request" | jq -e '
        .schemaVersion == 1
        and (.run.id | type == "string" and length > 0)
        and (.run.repository | type == "string")
        and (.run.workflow | type == "string")
        and (.run.sha | type == "string" and test("^[0-9a-f]{40}$"))
        and (.run.createdAt | type == "string")
        and (.evidenceAt | type == "string")
        and (.case == null or (.case | type == "object"))
    ' >/dev/null 2>&1; then
        printf '%s\n' '{"value":null,"opportunityKey":"invalid-request","case":{"invalidRequest":true},"evidenceCutoff":"1970-01-01T00:00:00Z","maturesAt":"1970-01-01T00:00:00Z","provenance":[],"diagnostics":{"missingReason":"invalid request"}}'
        return
    fi

    run_id=$(printf '%s\n' "$request" | jq -r '.run.id')
    repository=$(printf '%s\n' "$request" | jq -r '.run.repository')
    workflow=$(printf '%s\n' "$request" | jq -r '.run.workflow')
    run_sha=$(printf '%s\n' "$request" | jq -r '.run.sha')
    created_at=$(printf '%s\n' "$request" | jq -r '.run.createdAt')
    evidence_at=$(printf '%s\n' "$request" | jq -r '.evidenceAt')

    if ! created_at=$(normalize_timestamp "$created_at") \
        || ! evidence_at=$(normalize_timestamp "$evidence_at"); then
        printf '%s\n' '{"value":null,"opportunityKey":"invalid-timestamp","case":{"invalidTimestamp":true},"evidenceCutoff":"1970-01-01T00:00:00Z","maturesAt":"1970-01-01T00:00:00Z","provenance":[],"diagnostics":{"missingReason":"invalid timestamp"}}'
        return
    fi

    matures_at=$(add_seconds "$created_at" "$MATURATION_SECONDS")
    evidence_epoch=$(timestamp_epoch "$evidence_at")
    matures_epoch=$(timestamp_epoch "$matures_at")
    if (( evidence_epoch < matures_epoch )); then
        evidence_cutoff=$evidence_at
    else
        evidence_cutoff=$matures_at
    fi

    if [[ $repository != "$REPOSITORY" || $workflow != "$WORKFLOW_NAME" ]]; then
        emit_null "run:$run_id" '{"assignmentMissing":true}' "$evidence_cutoff" "$matures_at" \
            "run repository or workflow does not match the frozen contract"
        return
    fi

    case_json=$(printf '%s\n' "$request" | jq -c '.case')
    if [[ $case_json == null ]]; then
        if ! case_json=$(assign_case "$repository" "$run_sha"); then
            emit_null "run:$run_id" '{"assignmentMissing":true}' "$evidence_cutoff" "$matures_at" \
                "assignment-unavailable"
            return
        fi
    elif ! printf '%s\n' "$case_json" | jq -e \
        --argjson threshold "$THRESHOLD_LINES" \
        --argjson target "$TARGET_LINES" '
            (.path | type == "string" and test("^pkg/.*\\.go$") and (endswith("_test.go") | not))
            and (.initialLines | type == "number" and . >= 0 and floor == .)
            and .thresholdLines == $threshold
            and .targetLines == $target
            and (.subjectSha | type == "string" and test("^[0-9a-f]{40}$"))
            and ((has("eligibleFileCount") | not) or (.eligibleFileCount | type == "number" and . >= 0 and floor == .))
            and ((has("totalLines") | not) or (.totalLines | type == "number" and . >= 0 and floor == .))
            and ((has("compliantLines") | not) or (.compliantLines | type == "number" and . >= 0 and floor == .))
        ' >/dev/null; then
        emit_null "run:$run_id" '{"assignmentMissing":true}' "$evidence_cutoff" "$matures_at" \
            "invalid-case"
        return
    fi

    path=$(printf '%s\n' "$case_json" | jq -r '.path')
    initial_lines=$(printf '%s\n' "$case_json" | jq -r '.initialLines')
    if (( initial_lines < THRESHOLD_LINES )); then
        opportunity_key="repository-health:non-test-go-under-1000"
        evidence=$(jq -cn \
            --argjson initialLines "$initial_lines" \
            --argjson thresholdLines "$THRESHOLD_LINES" \
            --argjson targetLines "$TARGET_LINES" \
            '{valid: true, initialLines: $initialLines, currentLines: $initialLines, thresholdLines: $thresholdLines, targetLines: $targetLines}')
        value=$(printf '%s\n' "$evidence" | metric)
        diagnostics=$(case_diagnostics "$case_json" "$initial_lines")
        jq -cn \
            --argjson value "$value" \
            --arg opportunityKey "$opportunity_key" \
            --argjson case "$case_json" \
            --arg evidenceCutoff "$evidence_cutoff" \
            --arg maturesAt "$matures_at" \
            --arg repository "$repository" \
            --arg sha "$run_sha" \
                        --argjson diagnostics "$diagnostics" \
            '{value: $value, opportunityKey: $opportunityKey, case: $case,
              evidenceCutoff: $evidenceCutoff, maturesAt: $maturesAt,
              provenance: [{repository: $repository, kind: "git-tree", ref: $sha}],
                            diagnostics: ($diagnostics + {repositoryHealthyAtAssignment: true})}'
        return
    fi

    opportunity_key="go-file:$path"
    if ! cutoff_commit=$(latest_commit_at_cutoff "$repository" "$evidence_cutoff"); then
        emit_null "$opportunity_key" "$case_json" "$evidence_cutoff" "$matures_at" \
            "cutoff-commit-unavailable"
        return
    fi
    if [[ -n $local_repo_root ]] \
        && git -C "$local_repo_root" cat-file -e "$cutoff_commit^{commit}" 2>/dev/null; then
        if git -C "$local_repo_root" cat-file -e "$cutoff_commit:$path" 2>/dev/null; then
            current_lines=$(git -C "$local_repo_root" show "$cutoff_commit:$path" | wc -l | tr -d ' ') || return 1
        else
            current_lines=0
        fi
    else
        if ! load_tree "$repository" "$cutoff_commit" "$tree_file"; then
            emit_null "$opportunity_key" "$case_json" "$evidence_cutoff" "$matures_at" \
                "cutoff-tree-unavailable"
            return
        fi

        blob_sha=$(jq -r --arg path "$path" \
            '.tree[] | select(.type == "blob" and .path == $path) | .sha' "$tree_file")
        if [[ -z $blob_sha ]]; then
            current_lines=0
        elif ! current_lines=$(blob_line_count "$repository" "$blob_sha" "$blob_file"); then
            emit_null "$opportunity_key" "$case_json" "$evidence_cutoff" "$matures_at" \
                "blob-unavailable"
            return
        fi
    fi

    evidence=$(jq -cn \
        --argjson initialLines "$initial_lines" \
        --argjson currentLines "$current_lines" \
        --argjson thresholdLines "$THRESHOLD_LINES" \
        --argjson targetLines "$TARGET_LINES" \
        '{valid: true, initialLines: $initialLines, currentLines: $currentLines, thresholdLines: $thresholdLines, targetLines: $targetLines}')
    value=$(printf '%s\n' "$evidence" | metric)
    diagnostics=$(case_diagnostics "$case_json" "$current_lines")
    jq -cn \
        --argjson value "$value" \
        --arg opportunityKey "$opportunity_key" \
        --argjson case "$case_json" \
        --arg evidenceCutoff "$evidence_cutoff" \
        --arg maturesAt "$matures_at" \
        --arg repository "$repository" \
        --arg cutoffCommit "$cutoff_commit" \
        --arg path "$path" \
        --argjson currentLines "$current_lines" \
                --argjson diagnostics "$diagnostics" \
        '{value: $value, opportunityKey: $opportunityKey, case: $case,
          evidenceCutoff: $evidenceCutoff, maturesAt: $maturesAt,
          provenance: [{repository: $repository, kind: "git-commit", ref: $cutoffCommit},
                       {repository: $repository, kind: "go-source", ref: ($path + "@" + $cutoffCommit)}],
                    diagnostics: $diagnostics}'
}

case ${1:-} in
    --definition)
        definition
        ;;
    --metric)
        metric
        ;;
    --grade-run)
        grade_run
        ;;
    *)
        printf 'usage: %s --definition|--metric|--grade-run\n' "$0" >&2
        exit 1
        ;;
esac
