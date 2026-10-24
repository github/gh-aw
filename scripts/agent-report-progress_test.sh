#!/bin/bash
set +o histexpand
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TMP_ROOT=$(mktemp -d)
trap 'rm -rf "$TMP_ROOT"' EXIT

mkdir -p "$TMP_ROOT/scripts" "$TMP_ROOT/bin" "$TMP_ROOT/pkg/space name" "$TMP_ROOT/pkg/another"
cp "$SCRIPT_DIR/agent-report-progress.sh" "$TMP_ROOT/scripts/"
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
SH
cat > "$TMP_ROOT/bin/go" <<'SH'
#!/bin/bash
set -euo pipefail
if [ "$*" != "env GOPATH" ]; then
    echo "Unexpected go arguments: $*" >&2
    exit 1
fi
printf '%s\n' "$FIXTURE_ROOT"
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
    exit 1
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
grep -Fq "Pre-PR validation passed" "$OUTPUT"
grep -Fxq "make:<check-stale-schema-binary>" "$CALL_LOG"
if grep -Eq '^(lint|gofmt):' "$CALL_LOG"; then
    echo "Unexpected Go validation for a non-Go change" >&2
    exit 1
fi
echo "PASS: non-Go changes tolerate empty Go arrays under nounset"

printf '%s\n' "pkg/space name/two.go" "pkg/another/example.go" "pkg/space name/one.go" "pkg/space name/one.go" > "$CHANGED_LIST_FILE"
run_gate --with-tests
grep -Fq "Validating 3 changed file(s)" "$OUTPUT"
grep -Fxq "gofmt:<pkg/space name/one.go>" "$CALL_LOG"
grep -Fxq "make:<test-unit>" "$CALL_LOG"
grep '^lint:' "$CALL_LOG" > "$TMP_ROOT/actual-lint.txt"
printf '%s\n' "lint:<run>" "lint:<./pkg/another>" "lint:<./pkg/space name>" > "$TMP_ROOT/expected-lint.txt"
cmp "$TMP_ROOT/expected-lint.txt" "$TMP_ROOT/actual-lint.txt"
grep -Fq "impacted tests pass" "$OUTPUT"
echo "PASS: changed files and Go packages are sorted, deduplicated, and preserve spaces"

printf '%s\n' "pkg/another/deleted.go" > "$CHANGED_LIST_FILE"
export LINT_DIAGNOSTICS_FILE="$TMP_ROOT/lint-diagnostics.txt"
cat > "$LINT_DIAGNOSTICS_FILE" <<'EOF'
pkg/another/example.go:3:1: unused declaration (unused)
var unused int
^
1 issues:
* unused: 1
EOF
run_gate
unset LINT_DIAGNOSTICS_FILE
grep -Fxq "lint:<./pkg/another>" "$CALL_LOG"
grep -Fq "Go linter diagnostics were limited to unchanged files" "$OUTPUT"
grep -Fq "Pre-PR validation passed" "$OUTPUT"
if grep -Eq '^gofmt:' "$CALL_LOG" || grep -Eq 'unused declaration|unbound variable' "$OUTPUT"; then
    echo "Deleted Go files were formatted or unchanged-file diagnostics were not filtered" >&2
    exit 1
fi
echo "PASS: deleted Go files tolerate empty Go file arrays when filtering unchanged-file diagnostics"

printf '%s\n' "notes.txt" > "$CHANGED_LIST_FILE"
export FAIL_SCHEMA=1
if run_gate; then
    echo "Gate ignored a failing background validation job" >&2
    exit 1
fi
grep -Fq "fixture schema failure" "$OUTPUT"
echo "PASS: background validation failures propagate"

if run_gate --invalid; then
    echo "Gate accepted an invalid argument" >&2
    exit 1
fi
grep -Fq "Usage:" "$OUTPUT"
echo "PASS: invalid arguments are rejected"
