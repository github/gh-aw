#!/usr/bin/env bash
set +o histexpand

#
# compute_artifact_prefix.sh - Compute a stable artifact name prefix for workflow_call runs
#
# When the same reusable workflow is called by multiple jobs within a single parent
# workflow run, all invocations would upload artifacts with identical names
# (e.g. "activation", "agent") causing 409 Conflict errors.
#
# This script derives a unique prefix from the workflow inputs so that each
# distinct invocation produces separate artifact names (e.g.
# "a1b2c3d4-activation", "e5f6a7b8-activation").
#
# Environment variables:
#   INPUTS_JSON          (required) JSON-serialised workflow inputs, typically
#                        set via ${{ toJSON(inputs) }} in the workflow step env.
#                        Passed through an env-var to prevent template injection.
#
# GitHub Actions output:
#   prefix               8-hex-char SHA256 digest followed by "-"
#                        (e.g. "a1b2c3d4-"), or empty string on error.
#
# Uniqueness guarantee:
#   - Two calls with different inputs → different prefixes (collision-resistant SHA256).
#   - The same call gets the same prefix on every run attempt so downstream jobs can
#     retrieve artifacts uploaded by successful jobs on an earlier attempt.
#   - Two calls with identical inputs in the same workflow run → same prefix (conflict).
#     Callers MUST provide different inputs to avoid this edge case.
#
# Security:
#   Inputs are consumed via an environment variable, never interpolated directly
#   into the shell, preventing script-injection attacks.

set -euo pipefail

# INPUTS_JSON is set to ${{ toJSON(inputs) }} in the step env.
# When a workflow_call has no inputs, toJSON(inputs) returns "{}", so "{}" is
# the correct default for the no-inputs case (not an error).
INPUTS="${INPUTS_JSON:-{}}"
echo "Computing artifact prefix from workflow inputs..."
echo "  Inputs JSON length: ${#INPUTS} chars"

PREFIX=$(printf '%s' "$INPUTS" | sha256sum | cut -c1-8)

echo "  SHA256 digest (first 8 chars): ${PREFIX}"
echo "  Artifact prefix: ${PREFIX}-"
echo "prefix=${PREFIX}-" >> "$GITHUB_OUTPUT"
