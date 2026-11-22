#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" != 2 ]]; then
  echo "::error::TypeScript LSP installer requires manifest and lockfile SHA-256 digests."
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=actions/setup/sh/release_integrity.sh
source "$script_dir/release_integrity.sh"
install_dir="${RUNNER_TEMP:?}/gh-aw/engine-cli/typescript-lsp"
mkdir -p "$install_dir"
for filename in package.json package-lock.json; do
  cp "$script_dir/typescript-lsp/$filename" "$install_dir/$filename"
  verify_release_digest "$install_dir/$filename" "$1"
  shift
done

# Run outside the workflow checkout so npm doesn't discover its workspaces.
# Map canonical npmjs lock URLs to the runner's configured registry.
(
  cd "$install_dir"
  npm ci --replace-registry-host=npmjs --no-audit --no-fund
)

bin_dir="$install_dir/node_modules/.bin"
if [[ "${RUNNER_OS:-}" == Windows ]]; then
  if command -v wslpath >/dev/null 2>&1; then
    bin_dir="$(wslpath -w "$bin_dir")"
  else
    bin_dir="$(cygpath -w "$bin_dir")"
  fi
fi
printf '%s\n' "$bin_dir" >> "${GITHUB_PATH:?}"
