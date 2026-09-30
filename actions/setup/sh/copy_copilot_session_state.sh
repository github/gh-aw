#!/usr/bin/env bash
set +o histexpand

# Copy the AWF-managed Copilot session-state directory to the agent logs folder
# for artifact collection. Fall back to the legacy host HOME path for workflows
# compiled against AWF versions that populated it directly.
#
# Copilot CLI writes session data inside UUID-named subdirectories:
#   <session-state-dir>/<session-uuid>/events.jsonl
#   <session-state-dir>/<session-uuid>/session.db
#   <session-state-dir>/<session-uuid>/plan.md
#   <session-state-dir>/<session-uuid>/checkpoints/
#   <session-state-dir>/<session-uuid>/files/

set -euo pipefail

AWF_SESSION_STATE_DIR="/tmp/gh-aw/sandbox/agent/session-state"
LEGACY_SESSION_STATE_DIR="$HOME/.copilot/session-state"
LOGS_DIR="/tmp/gh-aw/sandbox/agent/logs/copilot-session-state"

SESSION_STATE_DIR=""
if [ -d "$AWF_SESSION_STATE_DIR" ] && [ -n "$(find "$AWF_SESSION_STATE_DIR" -type f -print -quit)" ]; then
  SESSION_STATE_DIR="$AWF_SESSION_STATE_DIR"
elif [ -d "$LEGACY_SESSION_STATE_DIR" ] && [ -n "$(find "$LEGACY_SESSION_STATE_DIR" -type f -print -quit)" ]; then
  SESSION_STATE_DIR="$LEGACY_SESSION_STATE_DIR"
fi

if [ -z "$SESSION_STATE_DIR" ]; then
  echo "::warning::No Copilot session state files found in $AWF_SESSION_STATE_DIR or $LEGACY_SESSION_STATE_DIR"
  exit 0
fi

echo "Copying Copilot session state from $SESSION_STATE_DIR to $LOGS_DIR"
mkdir -p "$LOGS_DIR"
cp -r "$SESSION_STATE_DIR"/. "$LOGS_DIR/"
echo "Session state directory copied successfully"
