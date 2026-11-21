#!/usr/bin/env bash
set +o histexpand
set -euo pipefail

PID_FILE="${RUNNER_TEMP}/gh-aw/actions/mcp-scripts-server.pid"

if [[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" ]]; then
  # shellcheck source=assert_docker_lease.sh
  source "$(dirname "${BASH_SOURCE[0]}")/assert_docker_lease.sh"
fi

if [[ ! -e "$PID_FILE" ]]; then
  exit 0
fi
if [[ -L "${RUNNER_TEMP}/gh-aw/actions" || -L "$PID_FILE" || ! -f "$PID_FILE" ]]; then
  echo "::error::Refusing to read an unsafe MCP Scripts server PID file."
  exit 1
fi

IFS=$'\t' read -r owner pid < "$PID_FILE"
if [[ ! "$pid" =~ ^[0-9]+$ ]] || (( pid <= 1 )); then
  echo "::error::MCP Scripts server PID file is invalid."
  exit 1
fi

process_running() {
  local state
  kill -0 "$pid" 2>/dev/null || return 1
  state="$(ps -p "$pid" -o stat= 2>/dev/null || true)"
  [[ -n "$state" && "$state" != Z* ]]
}

if ! process_running; then
  rm -f "$PID_FILE"
  exit 0
fi

if [[ "${RUNNER_ENVIRONMENT:-}" == "self-hosted" && "$owner" != "${GH_AW_DOCKER_OWNER:-}" ]]; then
  echo "::error::MCP Scripts server PID file is not owned by this invocation."
  exit 1
fi
if ! ps -p "$pid" -o args= 2>/dev/null | grep -Fq "mcp-server.cjs"; then
  echo "::error::Refusing to stop PID $pid because it is not the tracked MCP Scripts server."
  exit 1
fi
kill "$pid" 2>/dev/null || true
for _ in {1..10}; do
  if ! process_running; then
    rm -f "$PID_FILE"
    exit 0
  fi
  sleep 1
done
echo "::error::MCP Scripts server PID $pid did not exit; retaining the shared-daemon lease."
exit 1
