#!/usr/bin/env bash
set +o histexpand

# Append the agent's step summary to the real $GITHUB_STEP_SUMMARY.
# The compiler requires successful built-in and custom redaction first.
exec node "${RUNNER_TEMP}/gh-aw/actions/append_agent_step_summary.cjs"
