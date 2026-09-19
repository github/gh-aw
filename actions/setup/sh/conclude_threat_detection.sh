#!/usr/bin/env bash
set +o histexpand

set -euo pipefail

RESULT_FILE="${1:-/tmp/gh-aw/threat-detection/detection_result.json}"
RESULT_DIR="$(dirname "${RESULT_FILE}")"
DETECTION_LOG_FILE="${DETECTION_LOG_FILE:-${RESULT_DIR}/detection.log}"

# report_detector_unavailable emits the agent_failure conclusion outputs for a
# detector that could never run, without invoking any binary. Warn mode exits 0
# so the surrounding job stays green with an explicit warning; strict mode exits
# non-zero after recording the same outputs.
report_detector_unavailable() {
  local message="$1"
  local continue_on_error="${GH_AW_DETECTION_CONTINUE_ON_ERROR:-true}"

  echo "success=false" >> "${GITHUB_OUTPUT}"
  echo "reason=agent_failure" >> "${GITHUB_OUTPUT}"
  if [ "${continue_on_error,,}" != "false" ]; then
    echo "::warning::${message}; continuing because GH_AW_DETECTION_CONTINUE_ON_ERROR != false"
    echo "conclusion=warning" >> "${GITHUB_OUTPUT}"
    exit 0
  fi
  echo "conclusion=failure" >> "${GITHUB_OUTPUT}"
  echo "ERR_SYSTEM: ${message}"
  exit 1
}

# Fail closed when the pinned-digest installation did not succeed. The binary on
# PATH is then either absent, a stale/preinstalled copy that was never verified
# against the compiler-supplied digests, or the install script's fail-closed
# placeholder. None of them may be executed, and the placeholder cannot report a
# conclusion itself, so the outputs are emitted here instead. The guard is scoped
# to runs where detection was expected: when the guard step skipped detection the
# install step is skipped too, and `threat-detect conclude` reports conclusion=skipped.
THREAT_DETECT_INSTALL_OUTCOME="${THREAT_DETECT_INSTALL_OUTCOME:-}"
if [ "${RUN_DETECTION:-}" = "true" ] && [ -n "${THREAT_DETECT_INSTALL_OUTCOME}" ] && [ "${THREAT_DETECT_INSTALL_OUTCOME}" != "success" ]; then
  report_detector_unavailable "threat-detect installation did not complete verification (install outcome: ${THREAT_DETECT_INSTALL_OUTCOME})"
fi

# threat-detect conclude handles every branch of the conclusion contract
# (skipped run, missing/malformed result file, warn-mode vs strict-mode
# hard-fail rules, status-reason mapping, diagnostics, and step summary).
# The only failure it cannot report on itself is its own absence from PATH.
if ! command -v threat-detect >/dev/null 2>&1; then
  report_detector_unavailable "threat-detect binary not found on PATH"
fi

exec threat-detect conclude \
  --result-file "${RESULT_FILE}" \
  --detection-log "${DETECTION_LOG_FILE}"
