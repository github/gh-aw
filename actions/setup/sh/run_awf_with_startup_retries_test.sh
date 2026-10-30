#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="${SCRIPT_DIR}/run_awf_with_startup_retries.sh"

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

WORKDIR="${PWD}/.startup-retry-test-$$"
mkdir -p "$WORKDIR"
cleanup() {
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

export RUNNER_TEMP="$WORKDIR/runner-temp"
export GH_AW_HARNESS_TELEMETRY_DIR="$WORKDIR/telemetry"
mkdir -p "$RUNNER_TEMP"

run_with_env() {
  GH_AW_AWF_ENGINE_NAME="codex" \
    GH_AW_AWF_HARNESS_MARKER="[codex-harness]" \
    GH_AW_AWF_LOG_FILE="$WORKDIR/agent.log" \
    GH_AW_AWF_ATTEMPT_LOG_NAME="codex" \
    GH_AW_HARNESS_STARTUP_RETRIES="1" \
    GH_AW_HARNESS_INITIAL_DELAY_MS="0" \
    bash "$SCRIPT" -- "$@"
}

ATTEMPT_FILE="$WORKDIR/attempts"
OUTPUT="$WORKDIR/retry-output.log"
if ! run_with_env bash -c '
  attempts_file="$1"
  attempts="$(cat "$attempts_file" 2>/dev/null || echo 0)"
  attempts=$((attempts + 1))
  printf "%s" "$attempts" > "$attempts_file"
  if [ "$attempts" -eq 1 ]; then
    echo "Fatal error: Refusing to use symlink as bind mountpoint: /usr/bin/go"
    echo "Process exiting with code: 1"
    exit 1
  fi
  echo "[codex-harness] started"
' bash "$ATTEMPT_FILE" > "$OUTPUT" 2>&1; then
  cat "$OUTPUT" >&2
  fail "expected startup retry to recover"
fi

[ "$(cat "$ATTEMPT_FILE")" = "2" ] || fail "expected exactly two attempts"
grep -Fq "[codex-awf-retry] AWF startup failed before codex harness; retrying fresh" "$OUTPUT" || fail "expected retry diagnostic"
grep -Fq "Fatal error: Refusing to use symlink as bind mountpoint" "$WORKDIR/agent.log" || fail "expected first attempt in agent log"
grep -Fq "[codex-harness] started" "$WORKDIR/agent.log" || fail "expected successful attempt in agent log"
node - "$GH_AW_HARNESS_TELEMETRY_DIR/harness_retry.json" <<'NODE'
const assert = require("node:assert/strict");
const telemetry = require(process.argv[2]);
assert.equal(telemetry.startup_attempt_results.length, 2);
for (const [index, attempt] of telemetry.startup_attempt_results.entries()) {
  assert.equal(attempt.attempt_id, `startup:${index + 1}`);
  assert.equal(attempt.strategy, "restart");
  assert.equal(attempt.outcome, index === 0 ? "failure" : "success");
  assert.equal(attempt.exit_code, index === 0 ? 1 : 0);
  assert.equal(attempt.backoff_ms, 0);
  assert.ok(attempt.finished_at >= attempt.started_at);
}
assert.deepEqual(telemetry.startup_attempt_results[0].failure_classes, ["startup_error"]);
NODE

ATTEMPT_FILE="$WORKDIR/attempts-with-marker"
OUTPUT="$WORKDIR/no-retry-output.log"
set +e
run_with_env bash -c '
  attempts_file="$1"
  attempts="$(cat "$attempts_file" 2>/dev/null || echo 0)"
  attempts=$((attempts + 1))
  printf "%s" "$attempts" > "$attempts_file"
  echo "[codex-harness] started"
  echo "Fatal error: post-harness failure"
  exit 7
' bash "$ATTEMPT_FILE" > "$OUTPUT" 2>&1
STATUS=$?
set -e

[ "$STATUS" -eq 7 ] || fail "expected original post-harness failure status, got $STATUS"
[ "$(cat "$ATTEMPT_FILE")" = "1" ] || fail "expected no retry after harness marker"

run_routing_with_env() {
  GH_AW_MODEL_ROUTING="1" \
    GH_AW_AWF_ENGINE_NAME="copilot" \
    GH_AW_AWF_HARNESS_MARKER="[copilot-harness]" \
    GH_AW_AWF_LOG_FILE="$WORKDIR/routing-agent.log" \
    GH_AW_AWF_ATTEMPT_LOG_NAME="copilot-routing" \
    GH_AW_HARNESS_STARTUP_RETRIES="2" \
    GH_AW_HARNESS_INITIAL_DELAY_MS="0" \
    bash "$SCRIPT" -- "$@"
}

OUTPUT="$WORKDIR/model-routing-failure.log"
ATTEMPT_FILE="$WORKDIR/model-routing-attempts"
set +e
run_routing_with_env bash -c '
  attempts="$(cat "$1" 2>/dev/null || echo 0)"
  printf "%s" "$((attempts + 1))" > "$1"
  echo "Fatal error: task has no routing candidate"
  exit 78
' bash "$ATTEMPT_FILE" > "$OUTPUT" 2>&1
STATUS=$?
set -e

[ "$STATUS" -eq 78 ] || fail "expected routing failure status 78, got $STATUS"
[ "$(cat "$ATTEMPT_FILE")" = "1" ] || fail "expected routing failure not to be retried"
grep -Fq "[ERROR] Fatal error: AWF model routing failed (exit code 78)" "$OUTPUT" || fail "expected routing-specific failure diagnostic"
grep -Fq "AWF model routing failed (exit code 78)" "$WORKDIR/routing-agent.log" || fail "expected routing diagnostic in agent log"

run_with_evidence_env() {
  GH_AW_AWF_ENGINE_NAME="codex" \
    GH_AW_AWF_HARNESS_MARKER="[codex-harness]" \
    GH_AW_AWF_LOG_FILE="$WORKDIR/agent.log" \
    GH_AW_AWF_ATTEMPT_LOG_NAME="codex" \
    GH_AW_HARNESS_STARTUP_RETRIES="0" \
    GH_AW_HARNESS_INITIAL_DELAY_MS="0" \
    GH_AW_AWF_EXECUTION_COMPONENT="agent" \
    GH_AW_AWF_EXECUTION_EVIDENCE_FILE="$EVIDENCE_FILE" \
    GITHUB_RUN_ID="12345" \
    GITHUB_RUN_ATTEMPT="1" \
    bash "$SCRIPT" -- "$@"
}

EVIDENCE_FILE="$WORKDIR/evidence/agent_execution.json"
mkdir -p "$(dirname "$EVIDENCE_FILE")"
printf '{"version":1,"component":"agent","run_id":12345,"run_attempt":1,"state":"started"}\n' > "$EVIDENCE_FILE"
SAFE_OUTPUTS_FILE="$WORKDIR/safe-outputs.jsonl"
printf '{"type":"noop","message":"already done"}\n' > "$SAFE_OUTPUTS_FILE"
OUTPUT="$WORKDIR/preflight-noop-output.log"
INVOKED_FILE="$WORKDIR/preflight-noop-invoked"
GH_AW_SAFE_OUTPUTS="$SAFE_OUTPUTS_FILE" run_with_evidence_env bash -c 'printf invoked > "$1"' bash "$INVOKED_FILE" > "$OUTPUT" 2>&1
[ ! -f "$INVOKED_FILE" ] || fail "pre-flight noop must not invoke AWF"
grep -Fq '"state":"not_started"' "$EVIDENCE_FILE" || fail "expected host pre-flight noop evidence"
grep -Fq "skipped execution for a pre-existing noop" "$OUTPUT" || fail "expected host pre-flight noop diagnostic"

printf '{"version":1,"component":"agent","run_id":12345,"run_attempt":1,"state":"started"}\n' > "$EVIDENCE_FILE"
OUTPUT="$WORKDIR/evidence-output.log"
set +e
run_with_evidence_env bash -c '
  echo "Fatal error: cloud-hypervisor --version exited with code undefined"
  echo "Process exiting with code: 1"
  exit 1
' > "$OUTPUT" 2>&1
STATUS=$?
set -e

[ "$STATUS" -eq 1 ] || fail "expected original startup failure status, got $STATUS"
grep -Fq '"state":"not_started"' "$EVIDENCE_FILE" || fail "expected execution evidence downgraded to not_started"
grep -Fq '"run_id":12345' "$EVIDENCE_FILE" || fail "expected run id in downgraded evidence"
grep -Fq "recorded agent execution evidence as not_started" "$OUTPUT" || fail "expected evidence diagnostic"
[ ! -f "${EVIDENCE_FILE}.tmp" ] || fail "expected temporary evidence file to be moved"

printf '{"version":1,"component":"agent","run_id":12345,"run_attempt":1,"state":"started"}\n' > "$EVIDENCE_FILE"
OUTPUT="$WORKDIR/evidence-marker-output.log"
: > "$SAFE_OUTPUTS_FILE"
set +e
GH_AW_SAFE_OUTPUTS="$SAFE_OUTPUTS_FILE" run_with_evidence_env bash -c '
  echo "[codex-harness] started"
  printf "{\"type\":\"noop\",\"message\":\"written after execution started\"}\n" >> "$GH_AW_SAFE_OUTPUTS"
  echo "Fatal error: post-harness failure"
  exit 3
' > "$OUTPUT" 2>&1
STATUS=$?
set -e

[ "$STATUS" -eq 3 ] || fail "expected original post-harness failure status, got $STATUS"
grep -Fq '"state":"started"' "$EVIDENCE_FILE" || fail "expected execution evidence to remain started after harness ran"

echo "run_awf_with_startup_retries.sh tests passed"
