#!/bin/bash
set +o histexpand
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT=$(mktemp -d)
trap 'rm -rf "$ROOT"' EXIT
cd "$ROOT"
mkdir -p actions/setup/sh actions/setup/js/nested actions/setup/js/node_modules/dependency actions/other
touch actions/setup/js/helper.cjs actions/setup/js/node_modules/dependency/vendor.py actions/other/allowed.py

bash "$SCRIPT_DIR/check-setup-no-python.sh" > output 2>&1
grep -Fq "No Python files" output

touch "actions/setup/sh/installer with spaces.py" actions/setup/js/nested/helper.py
ln -s helper.cjs actions/setup/js/linked.py
if bash "$SCRIPT_DIR/check-setup-no-python.sh" > output 2>&1; then
    echo "Python files did not fail lint" >&2
    exit 1
fi
grep -Fq "actions/setup/sh/installer with spaces.py:1: error:" output
grep -Fq "actions/setup/js/nested/helper.py:1: error:" output
grep -Fq "actions/setup/js/linked.py:1: error:" output
grep -Fq "Found 3 Python file(s)" output
echo "PASS: setup Python files fail lint, including nested paths, spaces, and symlinks"

mkdir scripts
cp "$SCRIPT_DIR/check-setup-no-python.sh" scripts/
for target in build build-all build-linux build-darwin build-windows build-android build-wasm; do
    if make -f "$SCRIPT_DIR/../Makefile" "$target" > output 2>&1; then
        echo "$target accepted Python files" >&2
        exit 1
    fi
    grep -Fq "Python files are not allowed" output
    if grep -Fq "go build" output; then
        echo "$target compiled Go before rejecting Python files" >&2
        exit 1
    fi
done
echo "PASS: all binary build targets fail before compilation when setup Python files exist"
