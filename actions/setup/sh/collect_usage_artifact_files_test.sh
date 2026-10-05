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

mkdir -p "$TEST_DIR/bin" /tmp/gh-aw/usage/activity
if [ -f /tmp/gh-aw/usage/activity/summary.json ]; then
  cp /tmp/gh-aw/usage/activity/summary.json "$TEST_DIR/previous-summary.json"
  rm /tmp/gh-aw/usage/activity/summary.json
fi
for utility in mkdir cp find sort; do
  ln -s "$(command -v "$utility")" "$TEST_DIR/bin/$utility"
done
touch /tmp/gh-aw/usage/activity/collector-test-marker

output="$(RUNNER_TEMP="$TEST_DIR" PATH="$TEST_DIR/bin" /bin/bash "$SCRIPT" 2>&1)"
grep -q '::warning::node not found on PATH' <<<"$output"
grep -q '/tmp/gh-aw/usage/activity/collector-test-marker' <<<"$output"
test ! -f /tmp/gh-aw/usage/activity/summary.json

cat > "$TEST_DIR/bin/node" <<'EOF'
#!/bin/sh
printf '%s\n' "${1##*/}" >> "$NODE_MARKER"
if [ "${1##*/}" = generate_usage_activity_summary.cjs ]; then
  printf '{}\n' > /tmp/gh-aw/usage/activity/summary.json
fi
EOF
chmod +x "$TEST_DIR/bin/node"

output="$(RUNNER_TEMP="$TEST_DIR" NODE_MARKER="$TEST_DIR/node-calls" PATH="$TEST_DIR/bin" /bin/bash "$SCRIPT" 2>&1)"
! grep -q '::warning::node not found' <<<"$output"
grep -q '/tmp/gh-aw/usage/activity/summary.json' <<<"$output"
grep -qx 'generate_usage_activity_summary.cjs' "$TEST_DIR/node-calls"
grep -qx 'unified_session.cjs' "$TEST_DIR/node-calls"
test -f /tmp/gh-aw/usage/activity/summary.json

echo "Usage collector tests passed"
