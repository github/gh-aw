#!/bin/bash
set +o histexpand
set -euo pipefail

files=("$@")
if [ "$#" -eq 0 ]; then
    base_commit=$(bash "$(dirname "${BASH_SOURCE[0]}")/resolve-base-commit.sh" \
        --base-ref "${BASE_REF:-origin/main}" --no-fetch)
    while IFS= read -r file; do
        files+=("$file")
    done < <(
        {
            git diff --name-only --diff-filter=ACDMR "$base_commit"
            git ls-files --others --exclude-standard
        } | sed '/^$/d' | LC_ALL=C sort -u
    )
fi

go_files=()
prettier_files=()
for file in "${files[@]}"; do
    [ -f "$file" ] || continue
    case "$file" in
        *.go) go_files+=("$file") ;;
        *.cjs|*.js|*.mjs|*.ts|*.json) prettier_files+=("$file") ;;
    esac
done

if [ "${#go_files[@]}" -gt 0 ]; then
    echo "Formatting ${#go_files[@]} changed Go file(s)..."
    gofmt -w "${go_files[@]}"
fi
if [ "${#prettier_files[@]}" -gt 0 ]; then
    prettier_bin="./actions/setup/js/node_modules/.bin/prettier"
    if [ ! -x "$prettier_bin" ]; then
        echo "Pinned Prettier is not installed. Run 'npm ci --prefix actions/setup/js' before validation." >&2
        exit 1
    fi
    echo "Formatting ${#prettier_files[@]} changed JavaScript/TypeScript/JSON file(s)..."
    "$prettier_bin" --write "${prettier_files[@]}" --ignore-path .prettierignore --log-level=error
fi
if [ "${#go_files[@]}" -eq 0 ] && [ "${#prettier_files[@]}" -eq 0 ]; then
    echo "No changed Go/JavaScript/TypeScript/JSON files to format."
fi
