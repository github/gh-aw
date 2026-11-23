#!/usr/bin/env bash
set +o histexpand

set -euo pipefail

if command -v node >/dev/null 2>&1; then
  exec node "${BASH_SOURCE[0]%/*}/conclude_threat_detection.cjs" "$@"
fi

result_file="${1:-/tmp/gh-aw/threat-detection/detection_result.json}"
conclusion=warning
reason=agent_failure
export_env=true
if [[ "${RUN_DETECTION:-}" != "true" ]]; then
  conclusion=skipped
  reason=
  if [[ "${RUN_DETECTION:-}" == "false" && "${THREAT_DETECT_INSTALL_OUTCOME:-}" == "skipped" ]]; then
    export_env=false
  fi
elif [[ -n "${AWF_INSTALL_OUTCOME:-}" && "${AWF_INSTALL_OUTCOME}" != "success" && -n "${GH_AW_GITHUB_API_QUOTA_BLOCKED:-}" ]]; then
  reason=github_api_quota_exhausted
  export_env=false
  if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
    if ! printf '### GitHub API quota exhausted\n\nThreat detection is deferred: AWF attestation verification could not complete within the wait budget. This is not a security pass.\n\n' >> "$GITHUB_STEP_SUMMARY"; then
      echo "Could not write the quota step summary."
    fi
  fi
elif [[ ( -n "${AWF_INSTALL_OUTCOME:-}" && "${AWF_INSTALL_OUTCOME}" != "success" ) || ( -n "${THREAT_DETECT_INSTALL_OUTCOME:-}" && "${THREAT_DETECT_INSTALL_OUTCOME}" != "success" ) ]]; then
  export_env=false
elif [[ -e "$result_file" || -L "$result_file" ]]; then
  # A verdict that cannot be read must not be downgraded to a tooling warning.
  conclusion=failure
fi
policy="${GH_AW_DETECTION_CONTINUE_ON_ERROR:-}"
if [[ "$reason" != "" && "${policy,,}" == "false" ]]; then
  conclusion=failure
fi
if [[ "$reason" != "" ]]; then
  printf '::%s::ERR_SYSTEM: Node.js is unavailable; threat detection could not be concluded.\n' "$([[ "$conclusion" == "failure" ]] && echo error || echo warning)"
fi
if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  printf 'threat_kinds=\nconclusion=%s\nsuccess=%s\nreason=%s\n' "$conclusion" "$([[ -z "$reason" ]] && echo true || echo false)" "$reason" >> "$GITHUB_OUTPUT"
fi
if [[ "$export_env" == true && -n "${GITHUB_ENV:-}" ]]; then
  printf 'GH_AW_DETECTION_CONCLUSION=%s\nGH_AW_DETECTION_REASON=%s\n' "$conclusion" "$reason" >> "$GITHUB_ENV"
fi
[[ "$conclusion" != failure ]]
