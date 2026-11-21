#!/usr/bin/env bash
set -euo pipefail

variant="${1:-}"
expected_digest="${2:-}"
case "$variant" in
  node|typescript) ;;
  *) echo "::error::Unknown Copilot SDK npm project" >&2; exit 1 ;;
esac

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project="${script_dir}/copilot-sdk/${variant}"
target="${RUNNER_TEMP:?RUNNER_TEMP must be set}"

if command -v sha256sum >/dev/null 2>&1; then
  hash_command=(sha256sum -b)
elif command -v shasum >/dev/null 2>&1; then
  hash_command=(shasum -a 256 -b)
else
  echo "::error::Neither sha256sum nor shasum is available to verify the Copilot SDK npm project" >&2
  exit 1
fi

# Match the compiler's manifest + NUL + lockfile byte stream.
if ! digest_output=$(
  { cat "$project/package.json" && printf '\0' && cat "$project/package-lock.json"; } |
    "${hash_command[@]}"
); then
  echo "::error::Could not hash the Copilot SDK npm project" >&2
  exit 1
fi
if [[ "${digest_output%% *}" != "$expected_digest" ]]; then
  echo "::error::Copilot SDK npm project does not match the compiled workflow" >&2
  exit 1
fi

for component in gh-aw engine-cli copilot-sdk; do
  target="$target/$component"
  if [[ -L "$target" ]]; then
    echo "::error::Unexpected symlink in the Copilot SDK install directory" >&2
    exit 1
  fi
  mkdir -p "$target"
done
cd "$target"
if [[ -e npm-shrinkwrap.json || -L npm-shrinkwrap.json || -L package.json || -L package-lock.json ]]; then
  echo "::error::Unexpected package-file override in the Copilot SDK install directory" >&2
  exit 1
fi
cp "$project/package.json" "$project/package-lock.json" .
npm ci --prefix "$target" --ignore-scripts --no-audit --no-fund
