#!/bin/bash
set +o histexpand
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TMP_ROOT=$(mktemp -d)
trap 'rm -rf "$TMP_ROOT"' EXIT

REAL_MAKE=$(command -v make)
cache_env_target='sandbox-env-test:; @printf "%s\n" "$$GOCACHE" "$$GOMODCACHE" "$$GOLANGCI_LINT_CACHE" "$$npm_config_cache"'
mkdir -p "$TMP_ROOT/cache root"
cache_env=$(
    printf '%s\n' "$cache_env_target" | env -u GOCACHE -u GOMODCACHE -u GOLANGCI_LINT_CACHE -u npm_config_cache \
        TMPDIR="$TMP_ROOT/cache root" \
        "$REAL_MAKE" --no-print-directory -f "$SCRIPT_DIR/../Makefile" -f - sandbox-env-test
)
printf '%s\n' "$TMP_ROOT/cache root/gh-aw-go-build" "$TMP_ROOT/cache root/gh-aw-go-mod" "$TMP_ROOT/cache root/gh-aw-golangci-lint" \
    "$TMP_ROOT/cache root/gh-aw-npm-cache" > "$TMP_ROOT/expected-caches.txt"
printf '%s\n' "$cache_env" > "$TMP_ROOT/actual-caches.txt"
cmp "$TMP_ROOT/expected-caches.txt" "$TMP_ROOT/actual-caches.txt"
echo "PASS: make exports sandbox-writable tool caches, including paths with spaces"

cache_env=$(
    printf '%s\n' "$cache_env_target" | env \
        GOCACHE="$TMP_ROOT/custom-go-build" GOMODCACHE="$TMP_ROOT/custom-go-mod" \
        GOLANGCI_LINT_CACHE="$TMP_ROOT/custom-golangci-lint" \
        npm_config_cache="$TMP_ROOT/custom-npm-cache" \
        "$REAL_MAKE" --no-print-directory -f "$SCRIPT_DIR/../Makefile" -f - sandbox-env-test
)
printf '%s\n' "$TMP_ROOT/custom-go-build" "$TMP_ROOT/custom-go-mod" "$TMP_ROOT/custom-golangci-lint" \
    "$TMP_ROOT/custom-npm-cache" > "$TMP_ROOT/expected-caches.txt"
printf '%s\n' "$cache_env" > "$TMP_ROOT/actual-caches.txt"
cmp "$TMP_ROOT/expected-caches.txt" "$TMP_ROOT/actual-caches.txt"
echo "PASS: make preserves explicit tool cache overrides"

cache_env=$(
    printf '%s\n' "$cache_env_target" | env -u GOCACHE -u GOMODCACHE -u GOLANGCI_LINT_CACHE -u npm_config_cache \
        "$REAL_MAKE" --no-print-directory -f "$SCRIPT_DIR/../Makefile" -f - TMPDIR= sandbox-env-test
)
test -z "$cache_env"
echo "PASS: make leaves default tool caches alone when TMPDIR is empty"

mkdir -p "$TMP_ROOT/scripts" "$TMP_ROOT/bin" "$TMP_ROOT/pkg/space name" "$TMP_ROOT/pkg/another"
cp "$SCRIPT_DIR/agent-report-progress.sh" "$SCRIPT_DIR/format-changed.sh" "$TMP_ROOT/scripts/"
printf '%s\n' 'echo fixture-base' > "$TMP_ROOT/scripts/resolve-base-commit.sh"
touch "$TMP_ROOT/pkg/space name/one.go" "$TMP_ROOT/pkg/space name/two.go" "$TMP_ROOT/pkg/another/example.go"

cat > "$TMP_ROOT/bin/git" <<'SH'
#!/bin/bash
set -euo pipefail
case "$*" in
    "diff --name-only "*) cat "$CHANGED_LIST_FILE" ;;
    "ls-files --others --exclude-standard"|"diff --check fixture-base") ;;
    *) echo "Unexpected git arguments: $*" >&2; exit 1 ;;
esac
SH
cat > "$TMP_ROOT/bin/make" <<'SH'
#!/bin/bash
set -euo pipefail
printf 'make:<%s>\n' "$@" >> "$CALL_LOG"
if [ "${FAIL_SCHEMA:-0}" = 1 ] && [[ "$*" = *check-stale-schema-binary* ]]; then
    echo "fixture schema failure" >&2
    exit 1
fi
if [ -n "${FAIL_TARGET:-}" ] && [[ "$*" = *"$FAIL_TARGET"* ]]; then
    echo "fixture failure: $FAIL_TARGET" >&2
    exit 1
fi
SH
cat > "$TMP_ROOT/bin/go" <<'SH'
#!/bin/bash
set -euo pipefail
if [ "$*" = "env GOPATH" ]; then
    printf '%s\n' "$FIXTURE_ROOT"
elif [[ "$*" = "test -run ^$ "* ]]; then
    printf 'compile:<%s>\n' "$@" >> "$CALL_LOG"
    if [ "${FAIL_COMPILE:-0}" = 1 ]; then
        echo "fixture compile failure" >&2
        exit 1
    fi
elif [ "$#" -eq 4 ] && [ "$1 $2 $3" = "test -list ^Test" ]; then
    printf 'list:<%s>\n' "$4" >> "$CALL_LOG"
    if [ -n "${GO_LIST_STDOUT_FILE:-}" ]; then
        cat "$GO_LIST_STDOUT_FILE"
    fi
    if [ -n "${GO_LIST_STDERR:-}" ]; then
        printf '%s\n' "$GO_LIST_STDERR" >&2
    fi
    exit "${GO_LIST_STATUS:-0}"
elif [[ "$*" = "test -v -parallel=4 -timeout=10m -short -run "* ]]; then
    printf 'gotest:<%s>\n' "$@" >> "$CALL_LOG"
    if [ "${FAIL_GO_TEST:-0}" = 1 ]; then
        echo "fixture sampled test failure" >&2
        exit 1
    fi
else
    echo "Unexpected go arguments: $*" >&2
    exit 1
fi
SH
cat > "$TMP_ROOT/bin/gofmt" <<'SH'
#!/bin/bash
set -euo pipefail
printf 'gofmt:<%s>\n' "$@" >> "$CALL_LOG"
SH
cat > "$TMP_ROOT/bin/golangci-lint" <<'SH'
#!/bin/bash
set -euo pipefail
printf 'lint:<%s>\n' "$@" >> "$CALL_LOG"
if [ -n "${LINT_DIAGNOSTICS_FILE:-}" ]; then
    cat "$LINT_DIAGNOSTICS_FILE"
    exit "${LINT_STATUS:-1}"
fi
SH
chmod +x "$TMP_ROOT/bin/git" "$TMP_ROOT/bin/make" "$TMP_ROOT/bin/go" "$TMP_ROOT/bin/gofmt" "$TMP_ROOT/bin/golangci-lint"

export PATH="$TMP_ROOT/bin:$PATH"
export FIXTURE_ROOT="$TMP_ROOT"
export CHANGED_LIST_FILE="$TMP_ROOT/changes.txt"
export CALL_LOG="$TMP_ROOT/calls.txt"
OUTPUT="$TMP_ROOT/output.txt"
cd "$TMP_ROOT"

run_gate() {
    : > "$CALL_LOG"
    "$BASH" scripts/agent-report-progress.sh "$@" > "$OUTPUT" 2>&1
}

: > "$CHANGED_LIST_FILE"
run_gate
grep -Fq "No changes relative to" "$OUTPUT"
test ! -s "$CALL_LOG"
echo "PASS: empty change list skips validation"

printf '%s\n' "notes with spaces.txt" > "$CHANGED_LIST_FILE"
run_gate
grep -Fq "Validating 1 changed file(s)" "$OUTPUT"
grep -Fq "Intermediate validation passed" "$OUTPUT"
grep -Fxq "make:<check-agent-local-settings>" "$CALL_LOG"
if grep -Eq '^(lint|gofmt|compile):|make:<(build|check-stale-schema-binary|check-workflow-drift|golint-custom|test-unit)>' "$CALL_LOG"; then
    echo "Unexpected expensive validation for a non-Go change" >&2
    exit 1
fi
echo "PASS: non-Go changes tolerate empty Go arrays under nounset"

grep -Fxq "make:<check-instruction-prefixes>" "$CALL_LOG"
export FAIL_TARGET=check-instruction-prefixes
if run_gate; then
    echo "Expected instruction prefix violations to block validation" >&2
    exit 1
fi
grep -Fq "fixture failure: check-instruction-prefixes" "$OUTPUT"
unset FAIL_TARGET
echo "PASS: instruction prefix violations block saves before expensive validation"

printf '%s\n' "pkg/space name/one.go" > "$CHANGED_LIST_FILE"
run_gate
grep -Fxq "compile:<test>" "$CALL_LOG"
grep -Fxq "compile:<-run>" "$CALL_LOG"
grep -Fxq 'compile:<^$>' "$CALL_LOG"
grep -Fxq "compile:<./pkg/space name>" "$CALL_LOG"
grep -Fq "Deferring full workflow recompilation" "$OUTPUT"
if grep -Eq '^lint:|make:<(build|golint-custom|check-workflow-drift|test-unit)>' "$CALL_LOG"; then
    echo "Intermediate Go gate ran a final-only check" >&2
    exit 1
fi
echo "PASS: intermediate Go gate compiles/vets without package linters or workflow drift"

export FAIL_COMPILE=1
if run_gate; then
    echo "Intermediate gate ignored a compile failure" >&2
    exit 1
fi
unset FAIL_COMPILE
grep -Fq "fixture compile failure" "$OUTPUT"
echo "PASS: intermediate compile failures propagate"

for target in check-stale-lock-files check-agent-local-settings; do
    export FAIL_TARGET="$target"
    if run_gate; then
        echo "Intermediate gate ignored a failure in $target" >&2
        exit 1
    fi
    grep -Fq "fixture failure: $target" "$OUTPUT"
done
unset FAIL_TARGET
echo "PASS: intermediate stale-lock and local-permission failures propagate"

printf '%s\n' "go.mod" > "$CHANGED_LIST_FILE"
run_gate
grep -Fxq "compile:<./cmd/gh-aw>" "$CALL_LOG"
echo "PASS: module changes compile/vet the CLI even without changed Go files"

printf '%s\n' "pkg/space name/two.go" "pkg/another/example.go" "pkg/space name/one.go" "pkg/space name/one.go" > "$CHANGED_LIST_FILE"
run_gate --with-tests
grep -Fq "Validating 3 changed file(s)" "$OUTPUT"
grep -Fxq "gofmt:<pkg/space name/one.go>" "$CALL_LOG"
grep -Fxq "make:<test-unit>" "$CALL_LOG"
grep -Fxq "make:<build>" "$CALL_LOG"
grep -Fxq "make:<golint-custom>" "$CALL_LOG"
grep -Fxq "make:<check-stale-schema-binary>" "$CALL_LOG"
grep -Fxq "make:<check-workflow-drift>" "$CALL_LOG"
grep '^lint:' "$CALL_LOG" > "$TMP_ROOT/actual-lint.txt"
printf '%s\n' "lint:<run>" "lint:<--timeout=40m>" "lint:<./pkg/another>" "lint:<./pkg/space name>" > "$TMP_ROOT/expected-lint.txt"
cmp "$TMP_ROOT/expected-lint.txt" "$TMP_ROOT/actual-lint.txt"
grep -Fq "impacted tests pass" "$OUTPUT"
echo "PASS: changed files and Go packages are sorted, deduplicated, and preserve spaces"

for target in build golint-custom test-unit check-workflow-drift; do
    export FAIL_TARGET="$target"
    if run_gate --with-tests; then
        echo "Final gate ignored a failure in $target" >&2
        exit 1
    fi
    grep -Fq "fixture failure: $target" "$OUTPUT"
done
unset FAIL_TARGET
echo "PASS: final build, lint, test, and workflow drift failures propagate"

mkdir -p "$TMP_ROOT/actions/setup/js/node_modules/.bin" "$TMP_ROOT/pkg/parser/schemas"
touch "$TMP_ROOT/pkg/parser/schemas/example.json"
cat > "$TMP_ROOT/actions/setup/js/node_modules/.bin/prettier" <<'SH'
#!/bin/bash
set -euo pipefail
printf 'prettier:<%s>\n' "$@" >> "$CALL_LOG"
SH
chmod +x "$TMP_ROOT/actions/setup/js/node_modules/.bin/prettier"
printf '%s\n' "pkg/parser/schemas/example.json" > "$CHANGED_LIST_FILE"
run_gate
grep -Fxq "make:<build>" "$CALL_LOG"
grep -Fxq "make:<check-stale-schema-binary>" "$CALL_LOG"
export FAIL_SCHEMA=1
if run_gate; then
    echo "Intermediate gate ignored a stale schema binary" >&2
    exit 1
fi
unset FAIL_SCHEMA
grep -Fq "fixture schema failure" "$OUTPUT"
echo "PASS: schema changes still rebuild and check the binary in the intermediate gate"

printf '%s\n' "pkg/another/deleted.go" > "$CHANGED_LIST_FILE"
export LINT_DIAGNOSTICS_FILE="$TMP_ROOT/lint-diagnostics.txt"
cat > "$LINT_DIAGNOSTICS_FILE" <<'EOF'
pkg/another/example.go:3:1: unused declaration (unused)
var unused int
^
1 issues:
* unused: 1
EOF
run_gate --with-tests
unset LINT_DIAGNOSTICS_FILE
grep -Fxq "lint:<./pkg/another>" "$CALL_LOG"
grep -Fq "Go linter diagnostics were limited to unchanged files" "$OUTPUT"
grep -Fq "Pre-PR validation passed" "$OUTPUT"
if grep -Eq '^gofmt:' "$CALL_LOG" || grep -Eq 'unused declaration|unbound variable' "$OUTPUT"; then
    echo "Deleted Go files were formatted or unchanged-file diagnostics were not filtered" >&2
    exit 1
fi
echo "PASS: deleted Go files tolerate empty Go file arrays when filtering unchanged-file diagnostics"

printf '%s\n' "pkg/space name/one.go" > "$CHANGED_LIST_FILE"
export LINT_DIAGNOSTICS_FILE="$TMP_ROOT/lint-timeout.txt" LINT_STATUS=4
cat > "$LINT_DIAGNOSTICS_FILE" <<'EOF'
pkg/another/example.go:3:1: unused declaration (unused)
var unused int
^
0 issues.
level=error msg="Timeout exceeded: try increasing it by passing --timeout option"
EOF
if run_gate --with-tests; then
    echo "Final gate accepted a Go lint timeout because the other diagnostics were in unchanged files" >&2
    exit 1
fi
unset LINT_DIAGNOSTICS_FILE LINT_STATUS
grep -Fxq "lint:<--timeout=40m>" "$CALL_LOG"
grep -Fq "Timeout exceeded: try increasing it by passing --timeout option" "$OUTPUT"
if grep -Eq 'limited to unchanged files|Pre-PR validation passed' "$OUTPUT"; then
    echo "Go lint timeout was filtered as an unchanged-file diagnostic" >&2
    exit 1
fi
echo "PASS: Go lint timeouts fail the final gate even when the remaining output reports 0 issues"

printf '%s\n' "notes.txt" > "$CHANGED_LIST_FILE"
export FAIL_SCHEMA=1
if run_gate --with-tests; then
    echo "Gate ignored a failing background validation job" >&2
    exit 1
fi
unset FAIL_SCHEMA
grep -Fq "fixture schema failure" "$OUTPUT"
echo "PASS: background validation failures propagate"

printf '%s\n' "Makefile" "scripts/format-changed.sh" > "$CHANGED_LIST_FILE"
run_gate --with-tests
grep -Fxq "make:<test-agent-dev-loop>" "$CALL_LOG"
echo "PASS: final gate runs regression tests for dev-loop tooling changes"

if run_gate --invalid; then
    echo "Gate accepted an invalid argument" >&2
    exit 1
fi
grep -Fq "Usage:" "$OUTPUT"
echo "PASS: invalid arguments are rejected"

# The remaining cases run the real Makefile recipes with the real make binary;
# go and recursive make invocations resolve to the fixture stubs.
FIXTURE_MAKE="$TMP_ROOT/bin/make"
run_real_make() {
    : > "$CALL_LOG"
    env -u MAKEFLAGS -u MFLAGS -u MAKELEVEL \
        "$REAL_MAKE" --no-print-directory -f "$SCRIPT_DIR/../Makefile" "$@" > "$OUTPUT" 2>&1
}

for run_full in 0 1; do
    export FAIL_TARGET=test-impacted-go
    if run_real_make test-unit MAKE="$FIXTURE_MAKE" TEST_UNIT_RUN_FULL="$run_full"; then
        echo "make test-unit ignored a failing test-impacted-go (TEST_UNIT_RUN_FULL=$run_full)" >&2
        exit 1
    fi
    unset FAIL_TARGET
    grep -Fq "fixture failure: test-impacted-go" "$OUTPUT"
    grep -Fxq "make:<test-impacted-go>" "$CALL_LOG"
    if grep -Fxq "make:<test-unit-all>" "$CALL_LOG"; then
        echo "make test-unit ran the full suite after impacted tests failed (TEST_UNIT_RUN_FULL=$run_full)" >&2
        exit 1
    fi

    run_real_make test-unit MAKE="$FIXTURE_MAKE" TEST_UNIT_RUN_FULL="$run_full"
    grep -Fxq "make:<test-impacted-go>" "$CALL_LOG"
    if [ "$run_full" = 1 ]; then
        grep -Fxq "make:<test-unit-all>" "$CALL_LOG"
    elif grep -Fxq "make:<test-unit-all>" "$CALL_LOG"; then
        echo "make test-unit ran the full suite without TEST_UNIT_RUN_FULL=1" >&2
        exit 1
    fi
done
echo "PASS: make test-unit propagates impacted Go test failures with full tests off and on"

printf '%s\n' "pkg/another/example.go" > "$CHANGED_LIST_FILE"
run_sampled_impacted_go() {
    run_real_make test-impacted-go BASE_REF=origin/main CI_COVERAGE_ENABLED=0 GO_IMPACTED_TEST_FALLBACK_MODE=sample "$@"
}
export GO_LIST_STDOUT_FILE="$TMP_ROOT/go-list.txt"

: > "$GO_LIST_STDOUT_FILE"
printf 'FAIL\t./pkg/another [setup failed]\n' > "$TMP_ROOT/go-list-fail.txt"
for list_stdout in "$GO_LIST_STDOUT_FILE" "$TMP_ROOT/go-list-fail.txt"; do
    if GO_LIST_STDOUT_FILE="$list_stdout" GO_LIST_STATUS=1 GO_LIST_STDERR="fixture list failure: no space left on device" \
        run_sampled_impacted_go; then
        echo "make test-impacted-go treated failed test discovery as success" >&2
        exit 1
    fi
    grep -Fxq "list:<./pkg/another>" "$CALL_LOG"
    grep -Fq "fixture list failure: no space left on device" "$OUTPUT"
    grep -Fq "Failed to list top-level tests in ./pkg/another (go test -list exit 1)" "$OUTPUT"
    if grep -Fq "No top-level tests discovered" "$OUTPUT" || grep -q '^gotest:' "$CALL_LOG"; then
        echo "make test-impacted-go treated failed test discovery as a package without tests" >&2
        exit 1
    fi
done
echo "PASS: failed impacted Go test discovery is not reported as no tests"

printf 'TestAlpha\nTestBeta\nok  \tgithub.com/github/gh-aw/pkg/another\t0.012s\n' > "$GO_LIST_STDOUT_FILE"
run_sampled_impacted_go GO_IMPACTED_TEST_SAMPLE_PER_PACKAGE=5
grep '^gotest:<-run>$' -A1 "$CALL_LOG" | tail -n 1 > "$TMP_ROOT/actual-pattern.txt"
printf '%s\n' 'gotest:<^(TestAlpha|TestBeta)$>' > "$TMP_ROOT/expected-pattern.txt"
cmp "$TMP_ROOT/expected-pattern.txt" "$TMP_ROOT/actual-pattern.txt"
grep -Fxq "gotest:<./pkg/another>" "$CALL_LOG"
test "$(grep -c '^gotest:<-run>$' "$CALL_LOG")" -eq 1

run_sampled_impacted_go GO_IMPACTED_TEST_SAMPLE_PER_PACKAGE=1
grep -Fxq 'gotest:<^(TestAlpha)$>' "$CALL_LOG"

if FAIL_GO_TEST=1 run_sampled_impacted_go GO_IMPACTED_TEST_SAMPLE_PER_PACKAGE=5; then
    echo "make test-impacted-go ignored a failing sampled test" >&2
    exit 1
fi
grep -Fq "fixture sampled test failure" "$OUTPUT"
echo "PASS: sampled impacted Go tests run only listed top-level tests up to the per-package cap"

printf 'ok  \tgithub.com/github/gh-aw/pkg/another\t0.010s\n' > "$GO_LIST_STDOUT_FILE"
run_sampled_impacted_go
grep -Fq "No top-level tests discovered in ./pkg/another; skipping sampled run for this package." "$OUTPUT"
if grep -q '^gotest:' "$CALL_LOG"; then
    echo "make test-impacted-go ran tests for a package without top-level tests" >&2
    exit 1
fi
unset GO_LIST_STDOUT_FILE
echo "PASS: successful impacted Go test discovery without tests still succeeds"
