#!/bin/bash
set +o histexpand
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEST_SCRIPT="$SCRIPT_DIR/check-skill-file-paths.sh"

pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1"; echo "  $2"; exit 1; }

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

# Valid skill references should pass.
VALID_ROOT="$TMP_ROOT/valid"
mkdir -p "$VALID_ROOT/.github/skills/example-skill" "$VALID_ROOT/pkg/workflow/js"
cat > "$VALID_ROOT/.github/skills/example-skill/SKILL.md" <<'EOF'
Use `pkg/workflow/js/messages_core.cjs` and `./README.md`.
EOF
printf '%s\n' 'example' > "$VALID_ROOT/README.md"
: > "$VALID_ROOT/pkg/workflow/js/messages_core.cjs"
VALID_OUT="$TMP_ROOT/valid.out"
if (cd "$VALID_ROOT" && bash "$TEST_SCRIPT" --repo-root "$VALID_ROOT" >"$VALID_OUT" 2>&1); then
    pass "valid skill file paths pass"
else
    fail "valid skill file paths should pass" "$(cat "$VALID_OUT")"
fi

# Invalid repo paths should fail.
INVALID_ROOT="$TMP_ROOT/invalid"
mkdir -p "$INVALID_ROOT/.github/skills/example-skill" "$INVALID_ROOT/pkg/workflow/js"
cat > "$INVALID_ROOT/.github/skills/example-skill/SKILL.md" <<'EOF'
This doc references the stale repo path `pkg/workflow/nope.cjs` and the package name `github/gh-aw`, which should be treated differently.
EOF
INVALID_OUT="$TMP_ROOT/invalid.out"
if (cd "$INVALID_ROOT" && bash "$TEST_SCRIPT" --repo-root "$INVALID_ROOT" >"$INVALID_OUT" 2>&1); then
    fail "invalid skill path should fail" "$(cat "$INVALID_OUT")"
elif grep -q "pkg/workflow/nope.cjs" "$INVALID_OUT" && ! grep -q "github/gh-aw" "$INVALID_OUT"; then
    pass "invalid skill file paths fail with the offending path while ignoring package-name references"
else
    fail "invalid skill path output did not distinguish stale file paths from package names" "$(cat "$INVALID_OUT")"
fi

# Missing skill directory should error.
MISSING_ROOT="$TMP_ROOT/missing"
mkdir -p "$MISSING_ROOT"
MISSING_OUT="$TMP_ROOT/missing.out"
if (cd "$MISSING_ROOT" && bash "$TEST_SCRIPT" --repo-root "$MISSING_ROOT" >"$MISSING_OUT" 2>&1); then
    fail "missing skill directory should fail" "$(cat "$MISSING_OUT")"
elif grep -qi "skill directory not found" "$MISSING_OUT"; then
    pass "missing skill directory exits with an error"
else
    fail "missing skill directory output was unexpected" "$(cat "$MISSING_OUT")"
fi

echo "All skill path checks passed."
