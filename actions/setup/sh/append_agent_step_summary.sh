#!/usr/bin/env bash
set +o histexpand

# Append the agent's step summary to the real $GITHUB_STEP_SUMMARY.
# The file was written by the agent and already redacted for secrets.
# This is a no-op when the file is empty (agent wrote nothing).
gh_aw_dir="${GH_AW_TMP_DIR:-/tmp/gh-aw}"
if [ -s "$gh_aw_dir/agent-step-summary.md" ]; then
  cat "$gh_aw_dir/agent-step-summary.md" >> "$GITHUB_STEP_SUMMARY"
fi
