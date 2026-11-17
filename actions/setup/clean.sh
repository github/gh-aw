#!/usr/bin/env bash
set +o histexpand

# Clean Action Post Script
# Mirror of actions/setup/post.js for script mode (run: bash steps).
# Sends an OTLP conclusion span and reclaims only the recorded private runtime.
#
# Must be called from an `if: always()` step so it runs even when job steps fail,
# ensuring both the trace span and the cleanup always complete.
#
# Usage (script mode):
#   - name: Clean Scripts
#     if: always()
#     run: |
#       bash "$GH_AW_ACTION_SOURCE/actions/setup/clean.sh"
#     env:
#       INPUT_DESTINATION: ${{ runner.temp }}/gh-aw/actions

set -e

DESTINATION="${INPUT_DESTINATION:-${RUNNER_TEMP}/gh-aw/actions}"

# Send OTLP job conclusion span (non-fatal).
# Delegates to action_conclusion_otlp.cjs (same file used by actions/setup/post.js)
# to keep dev/release and script mode behavior in sync.
if command -v node &>/dev/null && [ -f "${DESTINATION}/action_conclusion_otlp.cjs" ]; then
  echo "Sending OTLP conclusion span..."
  node "${DESTINATION}/action_conclusion_otlp.cjs" || true
  echo "OTLP conclusion span step complete"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Also works from the setup destination after checkout replaces the workspace.
if [[ -f "${SCRIPT_DIR}/runtime_lifecycle.sh" ]]; then
  # shellcheck source=actions/setup/sh/runtime_lifecycle.sh
  source "${SCRIPT_DIR}/runtime_lifecycle.sh"
else
  # shellcheck source=actions/setup/sh/runtime_lifecycle.sh
  source "${SCRIPT_DIR}/sh/runtime_lifecycle.sh"
fi
cleanup_status=0
cleanup_gh_aw_runtime || cleanup_status=$?

if [[ "$cleanup_status" -ne 0 ]]; then
  echo "::warning::Private gh-aw runtime cleanup failed; leaving it untouched"
fi
