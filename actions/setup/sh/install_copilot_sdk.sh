#!/usr/bin/env bash
set -euo pipefail

variant="${1:-}"
expected_digest="${2:-}"
version="${3:-}"
case "$variant" in
  node|typescript) ;;
  *) echo "::error::Unknown Copilot SDK npm project" >&2; exit 1 ;;
esac

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project="${script_dir}/copilot-sdk/${variant}"
target="${RUNNER_TEMP:?RUNNER_TEMP must be set}"
if [[ -n "$version" ]]; then
  if [[ ! "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    echo "::error::Invalid Copilot SDK version" >&2
    exit 1
  fi
  project="${GITHUB_WORKSPACE:?GITHUB_WORKSPACE must be set}/.github/aw/copilot-sdk/$version/$variant"
fi

if command -v sha256sum >/dev/null 2>&1; then
  hash_command=(sha256sum -b)
elif command -v shasum >/dev/null 2>&1; then
  hash_command=(shasum -a 256 -b)
else
  echo "::error::Neither sha256sum nor shasum is available to verify the Copilot SDK npm project" >&2
  exit 1
fi

verify_project_digest() {
  local directory="$1" digest_output
  if ! digest_output=$(
    { cat "$directory/package.json" && printf '\0' && cat "$directory/package-lock.json"; } |
      "${hash_command[@]}"
  ); then
    echo "::error::Could not hash the Copilot SDK npm project" >&2
    exit 1
  fi
  if [[ "${digest_output%% *}" != "$expected_digest" ]]; then
    echo "::error::Copilot SDK npm project does not match the compiled workflow" >&2
    exit 1
  fi
}

verify_project_digest "$project"
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

if [[ -n "$version" ]]; then
  verify_project_digest .
  if ! jq -e --arg version "$version" '.dependencies["@microsoft/copilot-sdk"] == $version' package.json >/dev/null ||
     ! jq -e --arg version "$version" '
       .lockfileVersion == 3 and
       .packages[""].dependencies["@microsoft/copilot-sdk"] == $version and
       .packages["node_modules/@microsoft/copilot-sdk"].version == $version and
       all(.packages | to_entries[] | select(.key != "");
         (.key | startswith("node_modules/")) and
         (.value.link != true) and
         (.value.resolved | startswith("https://registry.npmjs.org/")) and
         (.value.integrity | test("^sha512-[A-Za-z0-9+/]{86}==$")))
     ' package-lock.json >/dev/null; then
    echo "::error::Expected a registry-only npm v3 lockfile with SHA-512 integrity and @microsoft/copilot-sdk@$version" >&2
    exit 1
  fi
fi
npm ci --ignore-scripts --no-audit --no-fund
