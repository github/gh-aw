#!/usr/bin/env bash

set -euo pipefail

fail() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

allow_correction=false
if [[ ${1:-} == --correction ]]; then
    allow_correction=true
    shift
fi

(( $# <= 1 )) || fail "usage: verify-operational-value-contract-change.sh [--correction] [base-ref]"

base_ref=${1:-origin/${GITHUB_BASE_REF:-main}}
repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || fail "not inside a Git repository"
git -C "$repo_root" cat-file -e "$base_ref^{commit}" 2>/dev/null \
    || fail "base ref is not a commit: $base_ref"

changed_evaluators=$(git -C "$repo_root" diff --name-only --diff-filter=AMD "$base_ref" -- \
    '.github/graders/*-operational-value.sh')

while IFS= read -r evaluator; do
    [[ -n $evaluator ]] || continue
    workflow_name=${evaluator##*/}
    workflow_name=${workflow_name%-operational-value.sh}
    workflow_path=.github/workflows/$workflow_name.md

    if git -C "$repo_root" diff --quiet "$base_ref" -- "$workflow_path"; then
        if [[ $allow_correction == true ]]; then
            printf 'prospective evaluator-only correction: %s\n' "$evaluator" >&2
            continue
        fi
        fail "$evaluator changed without $workflow_path; change the contract pair together or use --correction for a prospective evaluator-only defect fix"
    fi
done <<< "$changed_evaluators"

printf 'verified operational-value contract changes against %s\n' "$base_ref"