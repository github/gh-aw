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

DESTINATION="${INPUT_DESTINATION:-${RUNNER_TEMP}/gh-aw/actions}"

# Send OTLP job conclusion span (non-fatal).
# Delegates to action_conclusion_otlp.cjs (same file used by actions/setup/post.js)
# to keep dev/release and script mode behavior in sync.
if command -v node &>/dev/null && [ -f "${DESTINATION}/action_conclusion_otlp.cjs" ]; then
  echo "Sending OTLP conclusion span..."
  node "${DESTINATION}/action_conclusion_otlp.cjs" || true
  echo "OTLP conclusion span step complete"
fi

# Shared legacy state has no per-invocation ownership record (mirrors post.js).
tmpDir="/tmp/gh-aw"
if [ -e "${tmpDir}" ] || [ -L "${tmpDir}" ]; then
  echo "Warning: leaving shared ${tmpDir} untouched; per-invocation ownership cannot be established." >&2
fi
