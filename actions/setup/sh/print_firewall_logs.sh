#!/usr/bin/env bash
set +o histexpand

# print_firewall_logs.sh - Reclaim firewall sandbox dir ownership and print AWF firewall log summary.
#
# Usage: bash print_firewall_logs.sh [--rootless]
#
# Options:
#   --rootless   Use non-interactive sudo (sudo -n) with a non-sudo chmod fallback.
#                Default: use plain sudo (AWF ran with full sudo access).
#
# Environment:
#   AWF_LOGS_DIR         Path to the AWF logs directory (must be set).
#                        The Go compiler (engine_firewall_support.go) sets this to
#                        "${RUNNER_TEMP}/gh-aw/sandbox/firewall/logs"; the parent
#                        directory (firewall sandbox root) is used for chown/chmod.
#   GITHUB_STEP_SUMMARY  Path to the GitHub Actions step summary file.
#                        If unset or empty, log output is printed to stdout only.

ROOTLESS=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --rootless) ROOTLESS=true; shift ;;
    *) echo "Unknown argument: $1" >&2; exit 1 ;;
  esac
done

# FIREWALL_DIR is the firewall sandbox root, derived as the parent of AWF_LOGS_DIR.
# The Go compiler sets AWF_LOGS_DIR="${RUNNER_TEMP}/gh-aw/sandbox/firewall/logs",
# so FIREWALL_DIR resolves to "${RUNNER_TEMP}/gh-aw/sandbox/firewall".
FIREWALL_DIR="$(dirname "${AWF_LOGS_DIR}")"
GATEWAY_STARTUP_MARKER="${MCP_GATEWAY_STARTUP_MARKER:-${GH_AW_RUNTIME_DIR:?}/mcp-gateway-started}"

if [[ "${ROOTLESS}" == "true" ]]; then
  # A rootless run must never elevate to reclaim foreign-owned state.
  chmod -R u+rX "${FIREWALL_DIR}" 2>/dev/null || true
else
  # Privileged AWF can create root-owned logs; this is not rootless reclamation.
  sudo chown -R "$(id -u):$(id -g)" "${FIREWALL_DIR}" 2>/dev/null || true
  sudo chmod -R a+rX "${FIREWALL_DIR}" 2>/dev/null || true
fi

# Only run awf logs summary if awf command exists (it may not be installed if workflow failed before install step)
if command -v awf &> /dev/null; then
  awf logs summary | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}"
else
  echo 'AWF binary not installed, skipping firewall log summary'
fi

# Warn if Squid access.log is missing (current layout: squid-logs/; legacy layout: directly under logs/).
# A missing access.log means egress traffic for this run cannot be audited.
ACCESS_LOG_FOUND=false
for candidate in \
  "${AWF_LOGS_DIR}/squid-logs/access.log" \
  "${AWF_LOGS_DIR}/access.log"; do
  if [[ -f "${candidate}" ]]; then
    ACCESS_LOG_FOUND=true
    break
  fi
done
if [[ "${ACCESS_LOG_FOUND}" == "false" ]]; then
  if [[ -f "${GATEWAY_STARTUP_MARKER}" ]]; then
    echo "WARNING: Squid access.log not found under ${AWF_LOGS_DIR}; the MCP gateway started but produced no auditable egress traffic." >&2
  else
    echo "WARNING: Squid access.log not found under ${AWF_LOGS_DIR}; the MCP gateway did not complete startup. Inspect the Start MCP Gateway step diagnostics." >&2
  fi
fi
