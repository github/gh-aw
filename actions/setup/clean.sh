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
#       bash "${GH_AW_ACTIONS_SOURCE_DIR}/actions/setup/clean.sh"
#     env:
#       INPUT_DESTINATION: ${{ runner.temp }}/gh-aw/actions

set -e

DESTINATION="${INPUT_DESTINATION:-${RUNNER_TEMP}/gh-aw/actions}"
LEASE_VALIDATED=true
GH_AW_NODE="${GH_AW_NODE:-${GH_AW_NODE_BIN:-$(command -v node 2>/dev/null || true)}}"

if [[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" ]]; then
  if [[ -z "$GH_AW_NODE" || ! -x "$GH_AW_NODE" ]] ||
    ! "$GH_AW_NODE" "${DESTINATION}/docker_lease.cjs" assert; then
    echo "Warning: shared runtime finalization skipped because this invocation does not own a validated lease." >&2
    LEASE_VALIDATED=false
  fi
fi

# Send OTLP job conclusion span (non-fatal).
# Delegates to action_conclusion_otlp.cjs (same file used by actions/setup/post.js)
# to keep dev/release and script mode behavior in sync.
if [[ "$LEASE_VALIDATED" == "true" && -n "$GH_AW_NODE" && -f "${DESTINATION}/action_conclusion_otlp.cjs" ]]; then
  echo "Sending OTLP conclusion span..."
  "$GH_AW_NODE" "${DESTINATION}/action_conclusion_otlp.cjs" || true
  echo "OTLP conclusion span step complete"
fi

# Shared legacy state has no per-invocation ownership record (mirrors post.js).
tmpDir="/tmp/gh-aw"
if [[ "$LEASE_VALIDATED" == "true" ]] && { [ -e "${tmpDir}" ] || [ -L "${tmpDir}" ]; }; then
  echo "Warning: leaving shared ${tmpDir} untouched; per-invocation ownership cannot be established." >&2
fi

if [[ "$LEASE_VALIDATED" == "true" && "${RUNNER_ENVIRONMENT:-}" == "self-hosted" ]]; then
  bash "${DESTINATION}/stop_mcp_scripts_server.sh"
fi

if [[ "$LEASE_VALIDATED" == "true" && -n "${GH_AW_RUNTIME_TMP_DIR:-}" && -f "${DESTINATION}/cleanup_runtime_tmp.sh" ]]; then
  bash "${DESTINATION}/cleanup_runtime_tmp.sh"
fi

if [[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" ]]; then
  if [[ -z "$GH_AW_NODE" || ! -x "$GH_AW_NODE" ]]; then
    echo "::error::Node.js is required to release the shared-daemon lease." >&2
    exit 1
  fi
  "$GH_AW_NODE" "${DESTINATION}/docker_lease.cjs" release
fi
