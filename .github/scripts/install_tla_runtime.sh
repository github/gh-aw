#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

# Usage: bash install_tla_runtime.sh INSTALL_DIR [--jar-only]
# Repository-owned pins are reviewed against publisher provenance, never download responses.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=actions/setup/sh/release_integrity.sh
source "$SCRIPT_DIR/../../actions/setup/sh/release_integrity.sh"

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ] || { [ "$#" -eq 2 ] && [ "$2" != "--jar-only" ]; }; then
  echo "ERROR: Usage: install_tla_runtime.sh INSTALL_DIR [--jar-only]" >&2
  exit 1
fi
tools="${1%/}"
jar_only=false
if [ "${2:-}" = "--jar-only" ]; then jar_only=true; fi
if [[ "$tools" != /* ]] || [ -z "$tools" ]; then
  echo "ERROR: INSTALL_DIR must be an absolute non-root path" >&2
  exit 1
fi
if [ "$jar_only" = false ] && { [ "$(uname -s)" != Linux ] || [ "$(uname -m)" != x86_64 ]; }; then
  echo "ERROR: The pinned TLA+ JRE supports Linux x64 only; use --jar-only with separately installed Java" >&2
  exit 1
fi
if [ -e "$tools" ] || [ -L "$tools" ]; then
  echo "ERROR: INSTALL_DIR already exists; use a fresh directory for verified TLA+ installation" >&2
  exit 1
fi

jar_sha=936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88
jre_sha=2413149700df0f7d440500a84a8f764c535f21e5a5e87d38328b64eec2c5b500
mkdir -p "$(dirname "$tools")"
staging=$(mktemp -d "${tools}.install.XXXXXX")
trap 'rm -rf "$staging"' EXIT
mkdir "$staging/runtime"

download_verified() {
  local url="$1" file="$2" digest="$3"
  if ! curl --fail --location --silent --show-error \
    --proto '=https' --proto-redir '=https' \
    --retry 2 --connect-timeout 10 --max-time 120 \
    --output "$file" "$url"; then
    echo "ERROR: TLA+ runtime download failed: $url; refusing installation" >&2
    return 1
  fi
  verify_release_digest "$file" "$digest"
}

download_verified \
  'https://github.com/tlaplus/tlaplus/releases/download/v1.7.4/tla2tools.jar' \
  "$staging/runtime/tla2tools.jar" "$jar_sha"
if [ "$jar_only" = false ]; then
  download_verified \
    'https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jre_x64_linux_hotspot_21.0.12.1_1.tar.gz' \
    "$staging/jre.tar.gz" "$jre_sha"
  mkdir "$staging/runtime/jre"
  if ! tar -xzf "$staging/jre.tar.gz" --strip-components=1 --no-same-owner -C "$staging/runtime/jre"; then
    echo "ERROR: Verified JRE archive could not be extracted; refusing installation" >&2
    exit 1
  fi
  if ! "$staging/runtime/jre/bin/java" -version; then
    echo "ERROR: Verified Java runtime smoke check failed; refusing installation" >&2
    exit 1
  fi
  # TLC 2.19 prints help successfully but intentionally exits with status 1.
  tlc_status=0
  "$staging/runtime/jre/bin/java" -cp "$staging/runtime/tla2tools.jar" tlc2.TLC -help > "$staging/tlc-help" 2>&1 || tlc_status=$?
  if [ "$tlc_status" -ne 1 ] ||
    ! grep -Fq 'provides model checking and simulation of TLA+ specifications' "$staging/tlc-help"; then
    echo "ERROR: Verified TLA+ runtime smoke check failed; refusing installation" >&2
    exit 1
  fi
fi
# Publish only after every download, checksum, extraction and smoke check succeeds.
mv "$staging/runtime" "$tools"
echo "Installed verified TLA+ Tools v1.7.4 in $tools"
