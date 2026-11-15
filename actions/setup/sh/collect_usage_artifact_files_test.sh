#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_DIR="$(mktemp -d)"
trap 'rm -rf "$TEST_DIR"' EXIT
ROOT="$TEST_DIR/gh-aw"
SCRIPT="$TEST_DIR/collect_usage_artifact_files.sh"
sed "s|/tmp/gh-aw|$ROOT|g" "$SCRIPT_DIR/collect_usage_artifact_files.sh" > "$SCRIPT"

mkdir -p "$TEST_DIR/bin-with-node" "$TEST_DIR/bin-without-node" "$ROOT/usage/activity"
mkdir -p "$ROOT/agent" "$ROOT/runtime" "$ROOT/threat-detection"
printf '{"attempts":1,"exit_code":0,"attempt_results":[{"attempt":1,"exit_code":0,"failure_classes":[]}]}\n' > "$ROOT/agent/harness_retry.json"
printf '{"attempts":2,"exit_code":1,"attempt_results":[{"attempt":1,"exit_code":1,"failure_classes":["rate_limit_error"]},{"attempt":2,"exit_code":1,"failure_classes":["rate_limit_error"]}]}\n' > "$ROOT/threat-detection/detection_harness_retry.json"
printf '{"model":"early"}\n' > "$ROOT/aw_info.json"
printf '{"model":"final","model_routing":{"status":"selected"}}\n' > "$ROOT/runtime/aw_info.json"
printf '{"model":"forged"}\n' > "$ROOT/agent/aw_info.json"
for utility in mkdir cp find sort dirname mktemp mv rm; do
  ln -s "$(command -v "$utility")" "$TEST_DIR/bin-with-node/$utility"
  ln -s "$(command -v "$utility")" "$TEST_DIR/bin-without-node/$utility"
done
ln -s "$(command -v node)" "$TEST_DIR/bin-with-node/node"
touch "$ROOT/usage/activity/collector-test-marker"

output_without_node="$(RUNNER_TEMP="$TEST_DIR" PATH="$TEST_DIR/bin-without-node" /bin/bash "$SCRIPT" 2>&1)"
grep -q 'Node was not found on PATH' <<<"$output_without_node"
grep -Fq "$ROOT/usage/activity/collector-test-marker" <<<"$output_without_node"

output_with_node="$(RUNNER_TEMP="$TEST_DIR" PATH="$TEST_DIR/bin-with-node" /bin/bash "$SCRIPT" 2>&1)"
! grep -q 'Node was not found on PATH' <<<"$output_with_node"
grep -Fq "$ROOT/usage/activity/collector-test-marker" <<<"$output_with_node"

test ! -f "$ROOT/usage/activity/summary.json"
cmp "$ROOT/agent/harness_retry.json" "$ROOT/usage/agent/harness_retry.json"
cmp "$ROOT/threat-detection/detection_harness_retry.json" "$ROOT/usage/detection/harness_retry.json"
cmp "$ROOT/runtime/aw_info.json" "$ROOT/usage/aw_info.json"

rm "$ROOT/agent/harness_retry.json" "$ROOT/threat-detection/detection_harness_retry.json"
rm "$ROOT/usage/agent/harness_retry.json" "$ROOT/usage/detection/harness_retry.json"
PATH="$TEST_DIR/bin-with-node" /bin/bash "$SCRIPT" >/dev/null
test ! -f "$ROOT/usage/agent/harness_retry.json"
test ! -f "$ROOT/usage/detection/harness_retry.json"

rm "$ROOT/runtime/aw_info.json"
PATH="$TEST_DIR/bin-with-node" /bin/bash "$SCRIPT" >/dev/null
cmp "$ROOT/aw_info.json" "$ROOT/usage/aw_info.json"

rm "$ROOT/aw_info.json" "$ROOT/usage/aw_info.json"
PATH="$TEST_DIR/bin-with-node" /bin/bash "$SCRIPT" >/dev/null
test ! -f "$ROOT/usage/aw_info.json"

ISOLATED_ROOT="$TEST_DIR/isolated/gh-aw"
ISOLATED_SCRIPT="$TEST_DIR/collect_usage_artifact_files.sh"
mkdir -p "$ISOLATED_ROOT/usage" "$ISOLATED_ROOT/agent"
printf '{"model":"safe"}\n' > "$ISOLATED_ROOT/aw_info.json"
printf 'external sentinel\n' > "$TEST_DIR/sentinel"
ln -s "$TEST_DIR/sentinel" "$ISOLATED_ROOT/usage/aw_info.json"
ln -s "$TEST_DIR/sentinel" "$ISOLATED_ROOT/agent_usage.json"
sed "s|/tmp/gh-aw|$ISOLATED_ROOT|g" "$SCRIPT_DIR/collect_usage_artifact_files.sh" > "$ISOLATED_SCRIPT"
if /bin/bash "$ISOLATED_SCRIPT" >/dev/null 2>&1; then
  test "$(cat "$TEST_DIR/sentinel")" = "external sentinel"
  test -L "$ISOLATED_ROOT/usage/aw_info.json"
  test -L "$ISOLATED_ROOT/agent_usage.json"
else
  echo "Usage collector failed when rejecting symlinks"
  exit 1
fi

rm "$ISOLATED_ROOT/usage/aw_info.json" "$ISOLATED_ROOT/aw_info.json"
printf '{"model":"forged"}\n' > "$ISOLATED_ROOT/agent/aw_info.json"
/bin/bash "$ISOLATED_SCRIPT" >/dev/null
test ! -e "$ISOLATED_ROOT/usage/aw_info.json"

echo "Usage collector tests passed"
