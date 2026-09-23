#!/usr/bin/env bash
set +o histexpand
set -uo pipefail

# Stage the converted Copilot MCP configuration for the Copilot SDK driver.
#
# The SDK driver reads its MCP configuration from GH_AW_MCP_CONFIG inside the AWF
# sandbox, which only mounts ${RUNNER_TEMP}/gh-aw. The converted config written by
# the MCP setup step lives in $HOME/.copilot and may contain credentials, so it is
# copied into a private (0700) directory as a private (0600) file.
#
# Every step is fail-closed: the caller must not launch the agent with a stale or
# world-readable configuration.

if [ -z "${RUNNER_TEMP:-}" ]; then
  echo "RUNNER_TEMP is required to stage Copilot SDK MCP config" >&2
  exit 1
fi

GH_AW_MCP_CONFIG_DIR="${RUNNER_TEMP}/gh-aw/mcp-config"
GH_AW_MCP_CONFIG_FILE="${GH_AW_MCP_CONFIG_DIR}/copilot-sdk.json"

if ! (umask 077 && mkdir -p "${GH_AW_MCP_CONFIG_DIR}"); then
  echo "Failed to create Copilot SDK MCP config directory" >&2
  exit 1
fi
if ! chmod 700 "${GH_AW_MCP_CONFIG_DIR}"; then
  echo "Failed to secure Copilot SDK MCP config directory" >&2
  exit 1
fi
if ! (umask 077 && cp "$HOME/.copilot/mcp-config.json" "${GH_AW_MCP_CONFIG_FILE}"); then
  echo "Failed to stage Copilot SDK MCP config" >&2
  exit 1
fi
if ! chmod 600 "${GH_AW_MCP_CONFIG_FILE}"; then
  echo "Failed to secure Copilot SDK MCP config" >&2
  exit 1
fi
