#!/usr/bin/env bash
# Sanitize PATH by removing empty elements, leading/trailing colons
# Usage: source sanitize_path.sh <raw_path>
#   or:  . sanitize_path.sh <raw_path>
#
# This script sanitizes the PATH to prevent security risks from malformed PATH entries.
# Empty PATH elements cause the current directory to be searched for executables,
# which could allow malicious code execution (privilege escalation vector).
#
# Arguments:
#   $1 - Raw PATH value to sanitize (may contain shell expansions)
#
# The sanitization:
#   - Removes leading colons (e.g., ":/usr/bin" -> "/usr/bin")
#   - Removes trailing colons (e.g., "/usr/bin:" -> "/usr/bin")
#   - Collapses empty elements (e.g., "/a::/b" -> "/a:/b")
#
# After sourcing, PATH will be exported with the sanitized value.

set -euo pipefail

_GH_AW_PATH="${1:-}"

# Remove leading colons
while [ "${_GH_AW_PATH#:}" != "$_GH_AW_PATH" ]; do
  _GH_AW_PATH="${_GH_AW_PATH#:}"
done

# Remove trailing colons
while [ "${_GH_AW_PATH%:}" != "$_GH_AW_PATH" ]; do
  _GH_AW_PATH="${_GH_AW_PATH%:}"
done

# Collapse multiple consecutive colons into single colons
while case "$_GH_AW_PATH" in *::*) true;; *) false;; esac; do
  _GH_AW_PATH="$(printf '%s' "$_GH_AW_PATH" | sed 's/::*/:/g')"
done

export PATH="$_GH_AW_PATH"
