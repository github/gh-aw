#!/usr/bin/env bash
set +o histexpand

#
# compute_artifact_prefix.sh - Compute a stable artifact name prefix for workflow_call runs
#
# When the same reusable workflow is called by multiple jobs within a single parent
# workflow run, all invocations would upload artifacts with identical names
# (e.g. "activation", "agent") causing 409 Conflict errors.
#
# Bind the prefix to the activation job's check run so consumers can identify
# the invocation via the Jobs API even when concurrent calls share inputs.

set -euo pipefail

if [[ -v GH_AW_ACTIVATION_CHECK_RUN_ID ]]; then
  if [[ ! "$GH_AW_ACTIVATION_CHECK_RUN_ID" =~ ^[1-9][0-9]*$ ]]; then
    echo "::error::Cannot compute artifact prefix without an activation check run ID"
    exit 1
  fi
  echo "prefix=call-${GH_AW_ACTIVATION_CHECK_RUN_ID}-" >> "$GITHUB_OUTPUT"
  exit 0
fi

# Older compiled locks pass inputs instead of a check run ID.
# INPUTS_JSON is set to ${{ toJSON(inputs) }} in the step env.
# When a workflow_call has no inputs, toJSON(inputs) returns "{}", so "{}" is
# the correct default for the no-inputs case (not an error).
INPUTS="${INPUTS_JSON:-{}}"
# GITHUB_RUN_ATTEMPT is provided automatically by GitHub Actions. Default to 1
# for local/test environments where it may not be set.
ATTEMPT="${GITHUB_RUN_ATTEMPT:-1}"

echo "Computing artifact prefix from workflow inputs and run attempt..."
echo "  GITHUB_RUN_ATTEMPT: ${ATTEMPT}"
echo "  Inputs JSON length: ${#INPUTS} chars"

# Combine inputs JSON with run attempt using a separator unlikely to appear in
# valid JSON ("::attempt=" contains characters that would be escaped in JSON
# string values), so different inputs cannot produce the same pre-hash string
# as each other or as a different run attempt.
HASH_INPUT="${INPUTS}::attempt=${ATTEMPT}"
PREFIX=$(printf '%s' "$HASH_INPUT" | sha256sum | cut -c1-8)

echo "  SHA256 digest (first 8 chars): ${PREFIX}"
echo "  Artifact prefix: ${PREFIX}-"
echo "prefix=${PREFIX}-" >> "$GITHUB_OUTPUT"
