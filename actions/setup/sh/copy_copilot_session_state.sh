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

AWF_SESSION_STATE_DIR="${GH_AW_COPILOT_SESSION_STATE_DIR:-/tmp/gh-aw/sandbox/agent/session-state}"
LEGACY_SESSION_STATE_DIR="${GH_AW_COPILOT_LEGACY_SESSION_STATE_DIR:-$HOME/.copilot/session-state}"
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

SESSION_STATE_DIR=""
if [ -d "$AWF_SESSION_STATE_DIR" ] && has_redactable_files "$AWF_SESSION_STATE_DIR"; then
  SESSION_STATE_DIR="$AWF_SESSION_STATE_DIR"
elif [ -d "$LEGACY_SESSION_STATE_DIR" ] && has_redactable_files "$LEGACY_SESSION_STATE_DIR"; then
  SESSION_STATE_DIR="$LEGACY_SESSION_STATE_DIR"
fi

if [ -z "$SESSION_STATE_DIR" ]; then
  echo "::warning::No Copilot session state files found in $AWF_SESSION_STATE_DIR or $LEGACY_SESSION_STATE_DIR"
  exit 0
fi

echo "Copying Copilot session state from $SESSION_STATE_DIR to $LOGS_DIR"
mkdir -p "$LOGS_DIR"
while IFS= read -r -d '' file; do
  if ! is_redactable_file "$file"; then
    continue
  fi
  relative_path="${file#"$SESSION_STATE_DIR"/}"
  destination_dir="$LOGS_DIR/$(dirname "$relative_path")"
  mkdir -p "$destination_dir"
  cp -- "$file" "$destination_dir/"
done < <(find "$SESSION_STATE_DIR" -type f -print0)
echo "Session state directory copied successfully"
