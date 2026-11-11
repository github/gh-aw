#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

# Check the actual compiler, not just the module directive or setup-go request.
version="$(go env GOVERSION)"
if [[ ! "$version" =~ ^go1\.([0-9]+)\.([0-9]+)$ ]]; then
  echo "ERROR: Release builds require a stable patched Go toolchain; found ${version}" >&2
  exit 1
fi
minor="${BASH_REMATCH[1]}"
patch="${BASH_REMATCH[2]}"
if (( minor < 27 || (minor == 27 && patch < 4) )); then
  echo "ERROR: ${version} is below the required Go 1.27.4 toolchain baseline; use Go 1.27.4 or a newer supported stable release" >&2
  exit 1
fi
printf '%s\n' "$version"
