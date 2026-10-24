#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

# Resolve the version/checksum pair from the reviewed release pin table, offline.
# Add new reviewed pins with update-release-pins.sh before selecting a new default.
fail() {
  echo "error: $*" >&2
  exit 1
}

[[ "$#" -eq 1 ]] || fail "Usage: $0 VERSION"
version="${1#v}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "VERSION must be a semantic release tag"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=actions/setup/sh/release_integrity.sh
source "$script_dir/../actions/setup/sh/release_integrity.sh"
digest="$(release_artifact_sha256 github/copilot-cli "$version" SHA256SUMS.txt)" ||
  fail "add an independently authenticated pin with scripts/update-release-pins.sh before changing the default"

target="$script_dir/../pkg/constants/version_constants.go"
[[ -f "$target" && ! -L "$target" ]] || fail "version_constants.go must be a regular, non-symlink file"
work="$target.update-$$-$RANDOM"
(umask 077; mkdir "$work") || fail "cannot create private update directory"
trap 'rm -rf "$work"' EXIT
cp -p "$target" "$work/original"
cp -p "$target" "$work/updated"
awk -v version="$version" -v digest="$digest" '
  /^const DefaultCopilotVersion Version = / {
    versions++
    print "const DefaultCopilotVersion Version = \"" version "\""
    next
  }
  /^const DefaultCopilotChecksumsSHA256 = / {
    digests++
    print "const DefaultCopilotChecksumsSHA256 = \"" digest "\""
    next
  }
  { print }
  END {
    if (versions != 1 || digests != 1) {
      print "error: expected exactly one Copilot default version and checksum constant" > "/dev/stderr"
      exit 1
    }
  }
' "$work/original" > "$work/updated" || fail "constant resolution failed; no changes made"
cmp -s "$target" "$work/original" || fail "version_constants.go changed during update; retry after review"
if cmp -s "$work/original" "$work/updated"; then
  echo "Default Copilot version and checksum are already in sync."
else
  mv "$work/updated" "$target"
  echo "Updated default Copilot CLI to $version with reviewed SHA256SUMS.txt pin $digest; review the diff before committing."
fi
