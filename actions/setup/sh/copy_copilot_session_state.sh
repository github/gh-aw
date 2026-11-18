#!/usr/bin/env bash
set +o histexpand

# Copy Copilot session state to the agent logs folder for artifact collection.
# Direct-mode durable runs use their capture baseline to select the exact
# session from HOME; never copy unrelated historical sessions.
#
# Copilot CLI writes session data inside UUID-named subdirectories:
#   <session-state-dir>/<session-uuid>/events.jsonl
#   <session-state-dir>/<session-uuid>/session.db
#   <session-state-dir>/<session-uuid>/plan.md
#   <session-state-dir>/<session-uuid>/checkpoints/
#   <session-state-dir>/<session-uuid>/files/

set -euo pipefail

SESSION_STATE_DIR="${GH_AW_COPILOT_SESSION_STATE_DIR:-/tmp/gh-aw/sandbox/agent/session-state}"
SESSION_ID_FILE="${GH_AW_COPILOT_SESSION_ID_FILE:-/tmp/gh-aw/sandbox/agent/durable-sessions/copilot/capture-offset.json}"
LOGS_DIR="${GH_AW_COPILOT_SESSION_LOGS_DIR:-/tmp/gh-aw/sandbox/agent/logs/copilot-session-state}"

is_redactable_file() {
  case "$1" in
    *.txt|*.json|*.log|*.md|*.mdx|*.yml|*.jsonl|*.patch) return 0 ;;
    *) return 1 ;;
  esac
}

has_redactable_files() {
  local file
  while IFS= read -r -d '' file; do
    if is_redactable_file "$file"; then
      return 0
    fi
  done < <(find "$1" -type f -print0)
  return 1
}

SESSION_ID=""
if [ -f "$SESSION_ID_FILE" ] && [ -n "${GITHUB_RUN_ID:-}" ]; then
  SESSION_ID="$(node -e 'const fs = require("node:fs"); const state = JSON.parse(fs.readFileSync(process.argv[1], "utf8")); if (state.runId === process.env.GITHUB_RUN_ID && state.runAttempt === (process.env.GITHUB_RUN_ATTEMPT || "1") && typeof state.id === "string" && /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(state.id)) process.stdout.write(state.id);' "$SESSION_ID_FILE" 2>/dev/null || true)"
fi

SOURCE_DIR=""
if [ -n "$SESSION_ID" ]; then
  COPILOT_HOME="${COPILOT_HOME:-$HOME/.copilot}"
  for candidate in "$SESSION_STATE_DIR" "$COPILOT_HOME/session-state"; do
    if [ -d "$candidate/$SESSION_ID" ] && has_redactable_files "$candidate/$SESSION_ID"; then
      SOURCE_DIR="$candidate"
      break
    fi
  done
elif [ -d "$SESSION_STATE_DIR" ] && has_redactable_files "$SESSION_STATE_DIR"; then
  SOURCE_DIR="$SESSION_STATE_DIR"
fi

if [ -z "$SOURCE_DIR" ]; then
  echo "::warning::No Copilot session state files found in $SESSION_STATE_DIR"
  exit 0
fi

echo "Copying Copilot session state from $SOURCE_DIR to $LOGS_DIR"
mkdir -p "$LOGS_DIR"
if [ -n "$SESSION_ID" ]; then
  files_dir="$SOURCE_DIR/$SESSION_ID"
else
  files_dir="$SOURCE_DIR"
fi
while IFS= read -r -d '' file; do
  if ! is_redactable_file "$file"; then
    continue
  fi
  relative_path="${file#"$SOURCE_DIR"/}"
  destination_dir="$LOGS_DIR/$(dirname "$relative_path")"
  mkdir -p "$destination_dir"
  cp -- "$file" "$destination_dir/"
done < <(find "$files_dir" -type f -print0)
echo "Session state directory copied successfully"
