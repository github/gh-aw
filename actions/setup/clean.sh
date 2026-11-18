#!/usr/bin/env bash
set +o histexpand

# Clean Action Post Script
# Mirror of actions/setup/post.js for script mode (run: bash steps).
# Sends an OTLP conclusion span without reclaiming unowned shared runtime paths.
#
# Must be called from an `if: always()` step so it runs even when job steps fail,
# ensuring both the trace span and the cleanup always complete.
#
# Usage (script mode):
#   - name: Clean Scripts
#     if: always()
#     run: |
#       bash /tmp/gh-aw/actions-source/actions/setup/clean.sh
#     env:
#       INPUT_DESTINATION: ${{ runner.temp }}/gh-aw/actions

set -e

DESTINATION="${INPUT_DESTINATION:-${GH_AW_SETUP_ROOT:-${RUNNER_TEMP}/gh-aw}/actions}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUNTIME_HELPER="${SCRIPT_DIR}/js/runtime_isolation.cjs"

# Send OTLP job conclusion span (non-fatal).
# Delegates to action_conclusion_otlp.cjs (same file used by actions/setup/post.js)
# to keep dev/release and script mode behavior in sync.
if [[ -n "${GH_AW_SETUP_ROOT:-}" ]] && node "${RUNTIME_HELPER}" validate-setup && [ -f "${DESTINATION}/action_conclusion_otlp.cjs" ]; then
  echo "Sending OTLP conclusion span..."
  node "${DESTINATION}/action_conclusion_otlp.cjs" || true
  echo "OTLP conclusion span step complete"
fi

if [[ "${GH_AW_SETUP_ACTION_COMPLETE:-}" == "true" ]]; then
  echo "Deferring private runtime cleanup until action post steps have completed"
  exit 0
fi

if [[ -n "${GH_AW_RUNTIME_TMP_DIR:-}" && -f "${DESTINATION}/cleanup_runtime_tmp.sh" ]]; then
  bash "${DESTINATION}/cleanup_runtime_tmp.sh"
fi

node "${RUNTIME_HELPER}" cleanup
