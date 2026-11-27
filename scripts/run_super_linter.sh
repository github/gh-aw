#!/bin/bash
set -euo pipefail

cd "${GITHUB_WORKSPACE:?}"
mkdir -p super-linter-output

# The container creates the log as root; the runner must be able to read it.
trap 'if [ -f "${LOG_FILE:-}" ]; then chmod a+r -- "$LOG_FILE"; fi' EXIT

# Retain setup failures separately; they are not evidence of lint coverage.
(
  export GOTOOLCHAIN
  GOTOOLCHAIN="$(go env GOVERSION)"
  export GOBIN=/tmp/gh-aw-super-linter-bin
  CGO_ENABLED=0 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
  export PATH="$GOBIN:$PATH"
  exec /action/lib/linter.sh
) 2>&1 | tee super-linter-output/super-linter-startup.log
