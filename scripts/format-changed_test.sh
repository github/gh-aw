#!/bin/bash
set +o histexpand
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TMP_ROOT=$(mktemp -d)
trap 'rm -rf "$TMP_ROOT"' EXIT
mkdir -p "$TMP_ROOT/scripts" "$TMP_ROOT/pkg/space name" "$TMP_ROOT/bin"
cp "$SCRIPT_DIR/format-changed.sh" "$SCRIPT_DIR/resolve-base-commit.sh" "$TMP_ROOT/scripts/"
cd "$TMP_ROOT"
git init -q
git config user.name Fixture
git config user.email fixture@example.invalid
printf 'package fixture\n' > "pkg/space name/staged.go"
printf 'package fixture\n' > "pkg/space name/unstaged.go"
printf 'package fixture\n' > "pkg/space name/unchanged.go"
printf 'package fixture\n' > "pkg/space name/deleted.go"
git add .
git commit -qm fixture
export BASE_REF=HEAD

cat > "$TMP_ROOT/bin/gofmt" <<'SH'
#!/bin/bash
set -euo pipefail
printf 'gofmt:<%s>\n' "$@" >> "$CALL_LOG"
if [ "${FAIL_FORMAT:-0}" = 1 ]; then
    echo "fixture formatting failure" >&2
    exit 1
fi
SH
chmod +x "$TMP_ROOT/bin/gofmt"
export PATH="$TMP_ROOT/bin:$PATH"
export CALL_LOG="$TMP_ROOT/calls.txt"
OUTPUT="$TMP_ROOT/output.txt"

run_format() {
    : > "$CALL_LOG"
    bash scripts/format-changed.sh "$@" > "$OUTPUT" 2>&1
}

run_format
test ! -s "$CALL_LOG"
grep -Fq "No changed" "$OUTPUT"
echo "PASS: unchanged repository requires no formatter dependencies"

printf '\n' >> "pkg/space name/staged.go"
git add "pkg/space name/staged.go"
printf '\n' >> "pkg/space name/staged.go"
printf '\n' >> "pkg/space name/unstaged.go"
printf 'package fixture\n' > "pkg/space name/untracked.go"
rm "pkg/space name/deleted.go"
run_format
printf '%s\n' 'gofmt:<-w>' 'gofmt:<pkg/space name/staged.go>' \
    'gofmt:<pkg/space name/unstaged.go>' 'gofmt:<pkg/space name/untracked.go>' > "$TMP_ROOT/expected.txt"
cmp "$TMP_ROOT/expected.txt" "$CALL_LOG"
echo "PASS: staged, unstaged, and untracked Go files are deduplicated; deleted and unchanged files are skipped"

git add "pkg/space name/staged.go"
git commit -qm "branch change"
export BASE_REF=HEAD~1
git diff --quiet HEAD -- "pkg/space name/staged.go"
run_format
grep -Fxq "gofmt:<pkg/space name/staged.go>" "$CALL_LOG"
echo "PASS: committed branch changes are formatted even with a clean file in the working tree"

printf '{"value":1}\n' > "data with spaces.json"
if run_format "data with spaces.json"; then
    echo "JSON formatting succeeded without pinned Prettier" >&2
    exit 1
fi
grep -Fq "Pinned Prettier is not installed" "$OUTPUT"
echo "PASS: JavaScript/JSON changes fail explicitly when pinned Prettier is missing"

mkdir -p actions/setup/js/node_modules/.bin
cat > actions/setup/js/node_modules/.bin/prettier <<'SH'
#!/bin/bash
set -euo pipefail
printf 'prettier:<%s>\n' "$@" >> "$CALL_LOG"
SH
chmod +x actions/setup/js/node_modules/.bin/prettier
run_format "data with spaces.json" "pkg/space name/unstaged.go"
grep -Fxq "prettier:<data with spaces.json>" "$CALL_LOG"
grep -Fxq "prettier:<--ignore-path>" "$CALL_LOG"
grep -Fxq "gofmt:<pkg/space name/unstaged.go>" "$CALL_LOG"
echo "PASS: explicit changed-file lists share Go and pinned Prettier formatting"

export FAIL_FORMAT=1
if run_format "pkg/space name/unstaged.go"; then
    echo "Formatter failure was ignored" >&2
    exit 1
fi
grep -Fq "fixture formatting failure" "$OUTPUT"
echo "PASS: formatter failures propagate"
