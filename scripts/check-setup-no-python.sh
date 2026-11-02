#!/bin/bash
set +o histexpand
set -euo pipefail

files=$(mktemp)
trap 'rm -f "$files"' EXIT

# Do not inspect third-party dependencies installed for local development.
find actions/setup -type d -name node_modules -prune -o -name '*.py' ! -type d -print0 > "$files"
violations=0
while IFS= read -r -d '' file; do
    printf '%s:1: error: Python files are not allowed under actions/setup; replace with a .cjs helper.\n' "$file" >&2
    violations=$((violations + 1))
done < "$files"

if [ "$violations" -gt 0 ]; then
    printf 'Found %s Python file(s) under actions/setup.\n' "$violations" >&2
    exit 1
fi
echo "No Python files under actions/setup."
