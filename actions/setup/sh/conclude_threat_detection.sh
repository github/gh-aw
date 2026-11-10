#!/usr/bin/env bash
set +o histexpand

set -euo pipefail

RESULT_FILE="${1:-/tmp/gh-aw/threat-detection/detection_result.json}"
RESULT_DIR="$(dirname "${RESULT_FILE}")"
DETECTION_LOG_FILE="${DETECTION_LOG_FILE:-${RESULT_DIR}/detection.log}"
echo "threat_kinds=" >> "${GITHUB_OUTPUT}"

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

# An explicit no-output skip must not invoke a preinstalled detector or inspect
# payloads that bypassed redaction because detection was not needed.
THREAT_DETECT_INSTALL_OUTCOME="${THREAT_DETECT_INSTALL_OUTCOME:-}"
if [ "${RUN_DETECTION:-}" = "false" ] && [ "${THREAT_DETECT_INSTALL_OUTCOME}" = "skipped" ]; then
  echo "conclusion=skipped" >> "${GITHUB_OUTPUT}"
  echo "success=true" >> "${GITHUB_OUTPUT}"
  echo "reason=" >> "${GITHUB_OUTPUT}"
  exit 0
fi

# The detector runs under AWF; execution is gated on a verified AWF install.
# This is checked first: a failed AWF install skips the later threat-detect
# install, so its outcome would otherwise mask the real cause.
# When that install stopped on GitHub API quota exhaustion (attestation
# verification could not complete before the server reset fit the wait budget),
# report it explicitly. A deferred detection is never a security pass; apply the
# configured warn-mode policy and leave completed agent artifacts untouched.
AWF_INSTALL_OUTCOME="${AWF_INSTALL_OUTCOME:-}"
if [ "${RUN_DETECTION:-}" = "true" ] && [ -n "${AWF_INSTALL_OUTCOME}" ] && [ "${AWF_INSTALL_OUTCOME}" != "success" ]; then
  if [ -n "${GH_AW_GITHUB_API_QUOTA_BLOCKED:-}" ]; then
    quota_details="$(printf '%s' "${GH_AW_GITHUB_API_QUOTA_BLOCKED}" | tr -cd 'A-Za-z0-9_=;:.+ -' | cut -c1-512)"
    echo "success=false" >> "${GITHUB_OUTPUT}"
    echo "reason=github_api_quota_exhausted" >> "${GITHUB_OUTPUT}"
    if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
      {
        echo "### GitHub API quota exhausted"
        echo ""
        echo "Threat detection is deferred: AWF attestation verification could not complete within the wait budget (${quota_details})."
        echo "This is not a security pass. Safe-output handling follows the configured detection policy; completed agent artifacts are retained."
        echo ""
      } >> "${GITHUB_STEP_SUMMARY}" || true
    fi
    continue_on_error="${GH_AW_DETECTION_CONTINUE_ON_ERROR:-true}"
    if [ "${continue_on_error,,}" != "false" ]; then
      echo "conclusion=warning" >> "${GITHUB_OUTPUT}"
      echo "::warning::GitHub API quota exhausted: threat detection deferred because AWF attestation verification did not complete (${quota_details}); continuing because GH_AW_DETECTION_CONTINUE_ON_ERROR != false."
      exit 0
    fi
    echo "conclusion=failure" >> "${GITHUB_OUTPUT}"
    echo "::error::GitHub API quota exhausted: threat detection deferred because AWF attestation verification did not complete (${quota_details}). Safe outputs are not published."
    exit 1
  fi
  report_detector_unavailable "AWF installation did not complete verification (install outcome: ${AWF_INSTALL_OUTCOME})"
fi

# Fail closed when the pinned-digest installation did not succeed. The binary on
# PATH is then either absent, a stale/preinstalled copy that was never verified
# against the compiler-supplied digests, or the install script's fail-closed
# placeholder. None of them may be executed, and the placeholder cannot report a
# conclusion itself, so the outputs are emitted here instead. The guard is scoped
# to runs where detection was expected. A missing detector is a legitimate skip
# only when the guard explicitly disabled detection and installation was skipped.
if [ "${RUN_DETECTION:-}" = "true" ] && [ -n "${THREAT_DETECT_INSTALL_OUTCOME}" ] && [ "${THREAT_DETECT_INSTALL_OUTCOME}" != "success" ]; then
  report_detector_unavailable "threat-detect installation did not complete verification (install outcome: ${THREAT_DETECT_INSTALL_OUTCOME})"
fi

# last_output_value prints the last value written for a key to $GITHUB_OUTPUT.
# GitHub Actions resolves duplicate keys to the last written value, so this
# matches what downstream jobs observe.
last_output_value() {
  local key="$1"
  [ -n "${GITHUB_OUTPUT:-}" ] && [ -f "${GITHUB_OUTPUT}" ] || return 0
  grep -E "^${key}=" "${GITHUB_OUTPUT}" | tail -n 1 | cut -d= -f2- || true
}

# threat-detect conclude handles every branch of the conclusion contract
# (missing/malformed result file, warn-mode vs strict-mode
# hard-fail rules, status-reason mapping, diagnostics, and step summary).
# The only failure it cannot report on itself is its own absence from PATH.
if ! command -v threat-detect >/dev/null 2>&1; then
  report_detector_unavailable "threat-detect binary not found on PATH"
fi

conclude_exit=0
threat-detect conclude \
  --result-file "${RESULT_FILE}" \
  --detection-log "${DETECTION_LOG_FILE}" || conclude_exit=$?

# threat-detect conclude exited non-zero without recording a conclusion (for
# example it crashed before writing outputs). An empty conclusion must never
# reach safe_outputs, so report the detector as unavailable: warn mode concludes
# warning/agent_failure, strict mode fails closed.
if [ "${conclude_exit}" -ne 0 ] && [ -z "$(last_output_value conclusion)" ]; then
  report_detector_unavailable "threat-detect conclude exited with status ${conclude_exit} without reporting a conclusion"
fi

# Publish only schema-defined boolean findings, never the detector's prose.
if [ "$(last_output_value reason)" = "threat_detected" ]; then
  if threat_kinds="$(jq -er '
    . as $verdict |
    ["prompt_injection", "secret_leak", "malicious_patch"] as $kinds |
    if type == "object" and all($kinds[]; $verdict[.] | type == "boolean")
    then [$kinds[] | select($verdict[.] == true)] | join(",")
    else error("Invalid threat verdict") end
  ' "${RESULT_FILE}" 2>/dev/null)"; then
    echo "threat_kinds=${threat_kinds}" >> "${GITHUB_OUTPUT}"
  else
    echo "::warning::Detected threat types could not be read from the structured verdict; threat details are unavailable."
  fi
fi

# In warn mode a detection engine failure (agent_failure/parse_error) applies the
# warn-mode safe-outputs policy (WTD1a annotate, WTD2 convert, WTD3 abort) instead
# of failing closed. threat-detect conclude (<= v0.5.2) still concludes failure
# when the detection execution step itself failed; downgrade that case to warning
# so the external path matches the inline parse_threat_detection_results.cjs path.
# Real threat verdicts and strict mode are left untouched.
continue_on_error="${GH_AW_DETECTION_CONTINUE_ON_ERROR:-true}"
if [ "${conclude_exit}" -ne 0 ] && [ "${continue_on_error,,}" != "false" ]; then
  concluded="$(last_output_value conclusion)"
  concluded_reason="$(last_output_value reason)"
  if [ "${concluded}" = "failure" ] && { [ "${concluded_reason}" = "agent_failure" ] || [ "${concluded_reason}" = "parse_error" ]; }; then
    echo "::warning::Threat detection engine failed (reason: ${concluded_reason}); applying the warn-mode safe-outputs policy because GH_AW_DETECTION_CONTINUE_ON_ERROR != false"
    echo "conclusion=warning" >> "${GITHUB_OUTPUT}"
    echo "success=false" >> "${GITHUB_OUTPUT}"
    if [ -n "${GITHUB_ENV:-}" ]; then
      echo "GH_AW_DETECTION_CONCLUSION=warning" >> "${GITHUB_ENV}"
    fi
    exit 0
  fi
fi

exit "${conclude_exit}"
