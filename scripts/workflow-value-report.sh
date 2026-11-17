#!/usr/bin/env bash
set -euo pipefail

repository="${WORKFLOW_VALUE_REPOSITORY:-github/gh-aw}"
remote="${WORKFLOW_VALUE_REMOTE:-origin}"
output_dir="${WORKFLOW_VALUE_OUTPUT_DIR:-workflow-value-report}"
csv_dir="${WORKFLOW_VALUE_CSV_OUTPUT_DIR:-$output_dir/csv}"
repo_root="$(pwd -P)"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/gh-aw-workflow-value.XXXXXX")"

cleanup() {
  if [[ "$work_dir" == "${TMPDIR:-/tmp}"/gh-aw-workflow-value.* && -d "$work_dir" ]]; then
    rm -rf -- "$work_dir"
  fi
}
trap cleanup EXIT

mkdir -p "$work_dir/default" "$work_dir/lifecycle"
allow_missing_lifecycle=false
lifecycle_ledger="$work_dir/lifecycle/lifecycle.jsonl"

if ! git fetch --quiet --no-tags "$remote" refs/heads/memory/meta-orchestrators; then
  echo "ERROR: failed to fetch memory/meta-orchestrators from '$remote'. Authenticate the Git remote or set WORKFLOW_VALUE_REMOTE." >&2
  exit 1
fi
git archive --format=tar FETCH_HEAD metrics | tar -xf - -C "$work_dir/default"

if git fetch --quiet --no-tags "$remote" refs/heads/memory/output-lifecycle; then
  git archive --format=tar FETCH_HEAD lifecycle.jsonl collection.json | tar -xf - -C "$work_dir/lifecycle"
else
  echo "WARNING: memory/output-lifecycle is unavailable from '$remote'; lifecycle and value evidence will remain null/unavailable." >&2
  allow_missing_lifecycle=true
fi

mkdir -p "$output_dir"
GH_AW_WORKFLOW_VALUE_REPO_ROOT="$repo_root" \
GH_AW_WORKFLOW_VALUE_METRICS_DIR="$work_dir/default/metrics" \
GH_AW_WORKFLOW_VALUE_LIFECYCLE_LEDGER="$lifecycle_ledger" \
GH_AW_WORKFLOW_VALUE_OUTPUT_DIR="$output_dir" \
GH_AW_WORKFLOW_VALUE_PRUNE=false \
GH_AW_WORKFLOW_VALUE_ALLOW_MISSING_LIFECYCLE="$allow_missing_lifecycle" \
GITHUB_REPOSITORY="$repository" \
GITHUB_SHA="$(git rev-parse HEAD)" \
node actions/setup/js/workflow_value_report.cjs

provenance_repo="$repo_root"
provenance_revision="HEAD"
if [[ "${WORKFLOW_VALUE_FULL_GIT_HISTORY:-true}" == "true" ]]; then
  remote_url="$(git remote get-url "$remote")"
  if git clone --quiet --filter=blob:none --no-checkout "$remote_url" "$work_dir/provenance" &&
    git -C "$work_dir/provenance" fetch --quiet "$repo_root" HEAD:refs/heads/workflow-value-source; then
    provenance_repo="$work_dir/provenance"
    provenance_revision="refs/heads/workflow-value-source"
  else
    echo "WARNING: full git history was unavailable; CSV provenance will explicitly report shallow or missing history." >&2
  fi
fi

GH_AW_WORKFLOW_VALUE_REPO_ROOT="$repo_root" \
GH_AW_WORKFLOW_VALUE_REPORT_DIR="$output_dir" \
GH_AW_WORKFLOW_VALUE_CSV_DIR="$csv_dir" \
GH_AW_WORKFLOW_VALUE_PROVENANCE_REPO="$provenance_repo" \
GH_AW_WORKFLOW_VALUE_PROVENANCE_REVISION="$provenance_revision" \
node actions/setup/js/workflow_value_csv.cjs

GH_AW_WORKFLOW_VALUE_REPO_ROOT="$repo_root" \
GH_AW_WORKFLOW_VALUE_REPORT_DIR="$output_dir" \
GH_AW_WORKFLOW_VALUE_CSV_DIR="$csv_dir" \
node actions/setup/js/workflow_value_csv_verify.cjs

echo "Workflow value report written to $output_dir; normalized CSV dataset written to $csv_dir"
