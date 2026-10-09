#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT=$(mktemp)
FIXTURE_ROOT=$(mktemp -d)
trap 'rm -f "$OUTPUT"; rm -rf "$FIXTURE_ROOT"' EXIT

# Exercise SEC-005 independently so unsafe production handlers must still be reported.
(
    cd "$FIXTURE_ROOT"
    mkdir -p actions/setup/js
    for name in adapter_checks adapter_test adapter.test adapter_checks_helper unsafe_handler; do
        printf 'const adapter = { "target-repo": "owner/repo" };\n' >"actions/setup/js/$name.cjs"
    done
    printf '// @safe-outputs-exempt SEC-005: fixed scope validated upstream\nconst targetRepo = "owner/repo";\n' >actions/setup/js/exempt_handler.cjs
    printf 'const target_repo = "owner/repo";\nvalidateTargetRepo(target_repo);\n' >actions/setup/js/allowed_handler.cjs

    log_high() { echo "HIGH: $1"; }
    log_pass() { echo "PASS: $1"; }
    eval "$(sed -n '/^check_cross_repo() {/,/^}/p' "$SCRIPT_DIR/check-safe-outputs-conformance.sh")"
    check_cross_repo >"$OUTPUT"

    if [[ $(grep -c '^HIGH:' "$OUTPUT") -ne 2 ]] ||
        ! grep -q 'adapter_checks_helper.cjs supports target-repo' "$OUTPUT" ||
        ! grep -q 'unsafe_handler.cjs supports target-repo' "$OUTPUT"; then
        echo "FAIL: SEC-005 must skip only tests, allowlisted handlers, and documented exemptions"
        cat "$OUTPUT"
        exit 1
    fi
    if grep -q '^PASS:' "$OUTPUT"; then
        echo "FAIL: SEC-005 must not pass with unsafe production handlers"
        exit 1
    fi

    rm actions/setup/js/adapter_checks_helper.cjs actions/setup/js/unsafe_handler.cjs
    check_cross_repo >"$OUTPUT"
    if ! grep -q '^PASS: SEC-005: All cross-repo handlers validate allowlists' "$OUTPUT" ||
        grep -q '^HIGH:' "$OUTPUT"; then
        echo "FAIL: SEC-005 must pass for fixtures and authorized production handlers"
        cat "$OUTPUT"
        exit 1
    fi
)

(cd "$REPO_ROOT" && bash "$SCRIPT_DIR/check-safe-outputs-conformance.sh" >"$OUTPUT" 2>&1) || true

if ! grep -q 'SEC-005: All cross-repo handlers validate allowlists' "$OUTPUT" ||
    grep -q '\[HIGH\].*SEC-005:' "$OUTPUT"; then
    echo "FAIL: Expected no SEC-005 cross-repository authorization gaps"
    grep 'SEC-005:' "$OUTPUT" || true
    exit 1
fi

mapfile -t findings < <(grep "IMP-004: Safe output config property is missing" "$OUTPUT" || true)
mapfile -t target_findings < <(grep "IMP-005: Safe output target authorization conformance gap" "$OUTPUT" || true)

if [[ ${#findings[@]} -ne 0 ]]; then
    echo "FAIL: Expected no safe-output config schema gaps"
    printf '  %s\n' "${findings[@]}"
    exit 1
fi

if [[ ${#target_findings[@]} -ne 0 ]]; then
    echo "FAIL: Expected no safe-output target authorization conformance gaps"
    printf '  %s\n' "${target_findings[@]}"
    exit 1
fi

if ! grep -q "IMP-004: All safe output config properties are declared in the schema" "$OUTPUT"; then
    echo "FAIL: Expected IMP-004 complete schema coverage result"
    exit 1
fi

if ! grep -q "IMP-005: Safe output target authorization is specified, tested, and enforced" "$OUTPUT"; then
    echo "FAIL: Expected IMP-005 target authorization coverage result"
    exit 1
fi

echo "PASS: SEC-005 enforces cross-repository authorization, IMP-004 resolves referenced safe-output schemas, and IMP-005 enforces target authorization coverage"
