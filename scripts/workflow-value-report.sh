#!/usr/bin/env bash
set -euo pipefail

repository="${WORKFLOW_VALUE_REPOSITORY:-github/gh-aw}"
remote="${WORKFLOW_VALUE_REMOTE:-origin}"
output_dir="${WORKFLOW_VALUE_OUTPUT_DIR:-workflow-value-report}"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/gh-aw-workflow-value.XXXXXX")"

cleanup() {
  if [[ "$work_dir" == "${TMPDIR:-/tmp}"/gh-aw-workflow-value.* && -d "$work_dir" ]]; then
    rm -rf -- "$work_dir"
  fi
}
trap cleanup EXIT

mkdir -p "$work_dir/default" "$work_dir/lifecycle"

if ! git fetch --quiet --no-tags "$remote" refs/heads/memory/meta-orchestrators; then
  echo "ERROR: failed to fetch memory/meta-orchestrators from '$remote'. Authenticate the Git remote or set WORKFLOW_VALUE_REMOTE." >&2
  exit 1
fi
git archive --format=tar FETCH_HEAD metrics | tar -xf - -C "$work_dir/default"

if ! git fetch --quiet --no-tags "$remote" refs/heads/memory/output-lifecycle; then
  echo "ERROR: failed to fetch memory/output-lifecycle from '$remote'. Run the updated Metrics Collector before generating a local retained-lifecycle report." >&2
  exit 1
fi
git archive --format=tar FETCH_HEAD lifecycle.jsonl collection.json | tar -xf - -C "$work_dir/lifecycle"

mkdir -p "$output_dir"
GH_AW_WORKFLOW_VALUE_REPO_ROOT="$(pwd)" \
GH_AW_WORKFLOW_VALUE_METRICS_DIR="$work_dir/default/metrics" \
GH_AW_WORKFLOW_VALUE_LIFECYCLE_LEDGER="$work_dir/lifecycle/lifecycle.jsonl" \
GH_AW_WORKFLOW_VALUE_OUTPUT_DIR="$output_dir" \
GH_AW_WORKFLOW_VALUE_PRUNE=false \
GITHUB_REPOSITORY="$repository" \
GITHUB_SHA="$(git rev-parse HEAD)" \
node actions/setup/js/workflow_value_report.cjs

echo "Workflow value report written to $output_dir"
