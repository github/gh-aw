#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$SCRIPT_DIR/collect_usage_artifact_files.sh"
TEST_DIR="$(mktemp -d)"
for file in /tmp/gh-aw/aw_info.json /tmp/gh-aw/agent/aw_info.json /tmp/gh-aw/usage/aw_info.json; do
  if [ -f "$file" ]; then
    cp "$file" "$TEST_DIR/$(printf '%s' "$file" | tr '/' '_')"
    touch "$TEST_DIR/$(printf '%s' "$file" | tr '/' '_').present"
  fi
done
if [ -d /tmp/gh-aw/agent ]; then touch "$TEST_DIR/agent-dir.present"; fi

cleanup() {
  for file in /tmp/gh-aw/aw_info.json /tmp/gh-aw/agent/aw_info.json /tmp/gh-aw/usage/aw_info.json; do
    backup="$TEST_DIR/$(printf '%s' "$file" | tr '/' '_')"
    if [ -f "$backup.present" ]; then
      mkdir -p "$(dirname "$file")"
      cp "$backup" "$file"
    else
      rm -f "$file"
    fi
  done
  if [ ! -f "$TEST_DIR/agent-dir.present" ]; then rmdir /tmp/gh-aw/agent 2>/dev/null || true; fi
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
mkdir -p /tmp/gh-aw/agent
printf '{"model":"early"}\n' > /tmp/gh-aw/aw_info.json
printf '{"model":"final","model_routing":{"status":"selected"}}\n' > /tmp/gh-aw/agent/aw_info.json
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
grep -q '"model":"final"' /tmp/gh-aw/usage/aw_info.json

echo "Usage collector tests passed"
