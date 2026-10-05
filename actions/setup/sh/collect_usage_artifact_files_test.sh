#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SCRIPT_DIR/collect_usage_artifact_files.sh"
TEST_DIR="$(mktemp -d)"

cleanup() {
  rm -f /tmp/gh-aw/usage/activity/collector-test-marker
  if [ -f "$TEST_DIR/previous-summary.json" ]; then
    cp "$TEST_DIR/previous-summary.json" /tmp/gh-aw/usage/activity/summary.json
  else
    rm -f /tmp/gh-aw/usage/activity/summary.json
  fi
  rm -rf "$TEST_DIR"
}
trap cleanup EXIT

mkdir -p "$TEST_DIR/bin-with-node" "$TEST_DIR/bin-without-node" /tmp/gh-aw/usage/activity
if [ -f /tmp/gh-aw/usage/activity/summary.json ]; then
  cp /tmp/gh-aw/usage/activity/summary.json "$TEST_DIR/previous-summary.json"
  rm /tmp/gh-aw/usage/activity/summary.json
fi
for utility in mkdir cp find sort; do
  ln -s "$(command -v "$utility")" "$TEST_DIR/bin-with-node/$utility"
  ln -s "$(command -v "$utility")" "$TEST_DIR/bin-without-node/$utility"
done
ln -s "$(command -v node)" "$TEST_DIR/bin-with-node/node"
touch /tmp/gh-aw/usage/activity/collector-test-marker

output_without_node="$(RUNNER_TEMP="$TEST_DIR" PATH="$TEST_DIR/bin-without-node" /bin/bash "$SCRIPT" 2>&1)"
grep -q 'Node was not found on PATH' <<<"$output_without_node"
grep -q '/tmp/gh-aw/usage/activity/collector-test-marker' <<<"$output_without_node"

output_with_node="$(RUNNER_TEMP="$TEST_DIR" PATH="$TEST_DIR/bin-with-node" /bin/bash "$SCRIPT" 2>&1)"
! grep -q 'Node was not found on PATH' <<<"$output_with_node"
grep -q '/tmp/gh-aw/usage/activity/collector-test-marker' <<<"$output_with_node"

test ! -f /tmp/gh-aw/usage/activity/summary.json

echo "Usage collector tests passed"
