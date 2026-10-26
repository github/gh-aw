#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
work=".copilot-version-test-$$-$RANDOM"
(umask 077; mkdir "$work")
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/scripts" "$work/actions/setup/sh" "$work/pkg/constants"
cp scripts/update-copilot-version.sh "$work/scripts/"
cp actions/setup/sh/release_integrity.sh "$work/actions/setup/sh/"
cp pkg/constants/version_constants.go "$work/baseline"
target="$work/pkg/constants/version_constants.go"
updater="$work/scripts/update-copilot-version.sh"
cp "$work/baseline" "$target"

reject() {
  cp "$target" "$work/before"
  if bash "$updater" "$@" > "$work/output" 2>&1; then
    echo "FAIL: invalid default update unexpectedly succeeded: $*" >&2
    exit 1
  fi
  cmp "$target" "$work/before"
}

reject
reject 1.0.98 extra
reject latest
reject '1.0.98;echo bad'
reject 9.9.9
grep -q 'independently authenticated pin' "$work/output"
bash "$updater" v1.0.98
grep -qx 'const DefaultCopilotVersion Version = "1.0.98"' "$target"
grep -qx 'const DefaultCopilotChecksumsSHA256 = "372f0c8d3d50076455cd19ef080dc59dc2a32401d8b2d4f77ce190c8ab13bc59"' "$target"
cp "$target" "$work/before"
bash "$updater" 1.0.98
cmp "$target" "$work/before"

bash "$updater" 1.0.103
grep -qx 'const DefaultCopilotVersion Version = "1.0.103"' "$target"
grep -qx 'const DefaultCopilotChecksumsSHA256 = "26cd3e4b32cde498e4961fb4d48e7b85d7859fc69af1a645a19201fdd577bdb2"' "$target"
cp "$target" "$work/before"
bash "$updater" v1.0.103
cmp "$target" "$work/before"

for constant in DefaultCopilotVersion DefaultCopilotChecksumsSHA256; do
  sed "/^const $constant /d" "$work/baseline" > "$target"
  reject 1.0.98
  cp "$work/baseline" "$target"
  grep "^const $constant " "$work/baseline" >> "$target"
  reject 1.0.98
done
cp "$work/baseline" "$target"
sed 's/372f0c8d3d50076455cd19ef080dc59dc2a32401d8b2d4f77ce190c8ab13bc59/invalid/' \
  actions/setup/sh/release_integrity.sh > "$work/actions/setup/sh/release_integrity.sh"
reject 1.0.98
echo "PASS: default Copilot version and checksum resolve together and fail closed"
