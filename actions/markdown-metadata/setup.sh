#!/usr/bin/env bash
set -euo pipefail

SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DESTINATION="$1"
EXECUTABLE="markdown-metadata"
if [[ "${RUNNER_OS:-}" == "Windows" ]]; then
  EXECUTABLE="${EXECUTABLE}.exe"
fi
if ! (cd "${SOURCE_DIR}" && GOWORK=off GOTOOLCHAIN=local CGO_ENABLED=0 go build -mod=readonly -trimpath -o "${DESTINATION}/${EXECUTABLE}" .); then
  echo "::error::Failed to build the native Markdown metadata helper. Go 1.23 or newer is required."
  exit 1
fi
