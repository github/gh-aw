#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
FIXTURE_ROOT=$(mktemp -d)
trap 'rm -rf "$FIXTURE_ROOT"' EXIT
OUTPUT="$FIXTURE_ROOT/output"

git() {
    if [ "${FAIL_INDEX:-0}" = 1 ] && [ "${1:-}" = "-C" ]; then
        echo "fixture index enumeration failure" >&2
        return 23
    fi
    command git "$@"
}

mkdir -p "$FIXTURE_ROOT/repo"
cd "$FIXTURE_ROOT/repo"
git init -q
cp "$REPO_ROOT/.gitignore" .gitignore
git add .gitignore

check() {
    bash "$SCRIPT_DIR/check-agent-local-settings.sh" >"$OUTPUT" 2>&1
}

expect_pass() {
    if ! check; then
        cat "$OUTPUT" >&2
        exit 1
    fi
    grep -Fq "PASS: No Claude Code local permission settings are tracked." "$OUTPUT"
}

expect_failure() {
    if check; then
        echo "FAIL: Tracked local settings were accepted" >&2
        exit 1
    fi
    grep -Fq "ERROR: Claude Code local permission settings must not be tracked:" "$OUTPUT"
    grep -Fq "git rm --cached" "$OUTPUT"
    if grep -Fq 'synthetic-secret-do-not-log' "$OUTPUT"; then
        echo "FAIL: Settings contents leaked into diagnostics" >&2
        exit 1
    fi
}

expect_pass

paths=(
    ".claude/settings.local.json"
    "packages/nested project/.claude/settings.local.json"
    $'packages/newline\nproject/.claude/settings.local.json'
)
for file in "${paths[@]}"; do
    mkdir -p "$(dirname "$file")"
    printf '%s\n' '{"permissions":{"allow":["Bash(PGPASSWORD=synthetic-secret-do-not-log psql)"]}}' >"$file"
    git check-ignore -q -- "$file"
done
expect_pass
echo "PASS: Root and nested local settings are ignored, including unusual paths"

for file in "${paths[@]}"; do
    git add -f -- "$file"
    expect_failure
    # An unstaged deletion must not hide the indexed secret-bearing file.
    rm -- "$file"
    expect_failure
    git rm -q --cached -- "$file"
    expect_pass
done
echo "PASS: Force-added settings fail even when missing from disk, without logging contents"

mkdir -p .claude/skills/example packages/nested/.claude
printf '{}\n' >.claude/settings.json
printf '{}\n' >packages/nested/.claude/settings.json
printf '# Shared skill\n' >.claude/skills/example/SKILL.md
printf '{}\n' >settings.local.json
printf '{}\n' >.claude/settings.local.json.example
git add -- .claude/settings.json packages/nested/.claude/settings.json \
    .claude/skills/example/SKILL.md settings.local.json .claude/settings.local.json.example
expect_pass
echo "PASS: Shared settings, skills, and unrelated similarly named files remain allowed"

git -c user.name=Fixture -c user.email=fixture@example.invalid commit -qm "Safe fixtures"
printf '{}\n' >.claude/settings.local.json
git add -f -- .claude/settings.local.json
git -c user.name=Fixture -c user.email=fixture@example.invalid commit -qm "Unsafe fixture"
expect_failure
(
    cd packages/nested
    expect_failure
)
echo "PASS: Invoking the guard from a subdirectory still checks the whole repository"

mkdir -p scripts
cp "$SCRIPT_DIR/check-agent-local-settings.sh" scripts/
for target in all build build-all build-linux build-darwin build-windows build-android build-wasm; do
    if make -f "$REPO_ROOT/Makefile" "$target" >"$OUTPUT" 2>&1; then
        echo "FAIL: $target accepted tracked local settings" >&2
        exit 1
    fi
    grep -Fq "ERROR: Claude Code local permission settings must not be tracked:" "$OUTPUT"
done
echo "PASS: Every build target rejects tracked local settings"

git rm -q --cached -- .claude/settings.local.json
expect_pass
test -f .claude/settings.local.json
echo "PASS: Already-committed settings fail and untracking preserves the local file"

export -f git
if FAIL_INDEX=1 check; then
    echo "FAIL: Git index enumeration errors must fail closed" >&2
    exit 1
fi
grep -Fq "fixture index enumeration failure" "$OUTPUT"
unset -f git
echo "PASS: Git index enumeration errors fail closed"

cd "$FIXTURE_ROOT"
if check; then
    echo "FAIL: Running outside a Git repository must fail closed" >&2
    exit 1
fi
grep -Fq "not a git repository" "$OUTPUT"
echo "PASS: Git discovery errors fail closed"
