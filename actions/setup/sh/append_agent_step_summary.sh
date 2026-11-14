#!/usr/bin/env bash
set +o histexpand

# Append the agent's step summary to the real $GITHUB_STEP_SUMMARY.
# The compiler requires successful built-in and custom redaction first.
SUMMARY_SCRIPT="${RUNNER_TEMP}/gh-aw/actions/append_agent_step_summary.cjs"
if [ -L "${RUNNER_TEMP}/gh-aw" ] || [ -L "${RUNNER_TEMP}/gh-aw/actions" ] || [ -L "$SUMMARY_SCRIPT" ] || [ ! -f "$SUMMARY_SCRIPT" ]; then
  echo "::error::Refusing to execute an invalid or symlinked agent summary script" >&2
  exit 1
fi
exec node "$SUMMARY_SCRIPT"
