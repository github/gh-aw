#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT=$(mktemp)
FIXTURE_ROOT=$(mktemp -d)
trap 'rm -f "$OUTPUT"; rm -rf "$FIXTURE_ROOT"' EXIT

log_critical() { echo "CRITICAL: $1"; }
log_high() { echo "HIGH: $1"; }
log_pass() { echo "PASS: $1"; }
eval "$(sed -n '/^check_transport_integrity() {/,/^}/p' "$SCRIPT_DIR/check-safe-outputs-conformance.sh")"

assert_result() {
    local expected="$1"
    local name="$2"
    check_transport_integrity >"$OUTPUT"
    if [[ "$expected" == "pass" ]]; then
        if ! grep -q '^PASS: PTI-001:' "$OUTPUT" || grep -qE '^(HIGH|CRITICAL):' "$OUTPUT"; then
            echo "FAIL: PTI-005 must accept $name"
            cat "$OUTPUT"
            exit 1
        fi
    elif ! grep -q '^HIGH: PTI-005:' "$OUTPUT" || grep -q '^PASS:' "$OUTPUT"; then
        echo "FAIL: PTI-005 must reject $name"
        cat "$OUTPUT"
        exit 1
    fi
}

(cd "$REPO_ROOT" && assert_result pass "the production transport integrity implementation")

# Exercise malformed retry guards without changing production runtime code.
cd "$FIXTURE_ROOT"
mkdir -p actions/setup/js
cp "$REPO_ROOT"/actions/setup/js/{transport_integrity,resolve_transport_paths,create_pull_request,push_to_pull_request_branch}.cjs actions/setup/js/

check_retry_guard() {
    local expected="$1"
    local name="$2"
    local condition="$3"
    printf 'if (%s) {\n    throw error;\n}\n' "$condition" >actions/setup/js/error_recovery.cjs
    assert_result "$expected" "$name"
}

check_retry_guard pass "adjacent integrity and classifier guards" \
    'error?.code === "ERR_TRANSPORT_INTEGRITY" || !fullConfig.shouldRetry(error)'
check_retry_guard pass "intervening short-circuit deadline guard" \
    'error?.code === "ERR_TRANSPORT_INTEGRITY" || (fullConfig.deadline !== undefined && now() >= fullConfig.deadline) || !fullConfig.shouldRetry(error)'
check_retry_guard fail "classifier before integrity rejection" \
    '!fullConfig.shouldRetry(error) || error?.code === "ERR_TRANSPORT_INTEGRITY"'
check_retry_guard fail "classifier before integrity rejection with a deadline" \
    '!fullConfig.shouldRetry(error) || (fullConfig.deadline !== undefined && now() >= fullConfig.deadline) || error?.code === "ERR_TRANSPORT_INTEGRITY"'
check_retry_guard fail "non-short-circuiting integrity guard" \
    'error?.code === "ERR_TRANSPORT_INTEGRITY" && (fullConfig.deadline !== undefined && now() >= fullConfig.deadline) || !fullConfig.shouldRetry(error)'
check_retry_guard fail "classifier that can suppress integrity rejection" \
    '(error?.code === "ERR_TRANSPORT_INTEGRITY" || (fullConfig.deadline !== undefined && now() >= fullConfig.deadline)) && !fullConfig.shouldRetry(error)'
check_retry_guard fail "missing integrity guard" \
    '(fullConfig.deadline !== undefined && now() >= fullConfig.deadline) || !fullConfig.shouldRetry(error)'
check_retry_guard fail "missing custom classifier guard" \
    'error?.code === "ERR_TRANSPORT_INTEGRITY" || (fullConfig.deadline !== undefined && now() >= fullConfig.deadline)'
rm actions/setup/js/error_recovery.cjs
assert_result fail "a missing retry implementation"

echo "PASS: PTI-005 accepts integrity-first retry guards and rejects unsafe or missing guards"
