#!/bin/bash

# sync-compat.sh - Keep .github/aw/compat.json and install_copilot_cli.sh in sync with DefaultCopilotVersion
#
# Reads DefaultCopilotVersion from pkg/constants/version_constants.go and updates:
#   - the max-agent field of the latest interval (the entry with "open": true) in
#     .github/aw/compat.json
#   - the DEFAULT_COPILOT_VERSION constant baked into actions/setup/sh/install_copilot_cli.sh
# Refuses to update either file unless the default checksum matches the reviewed pin.
#
# Usage:
#   sync-compat.sh [--check]
#
# Options:
#   --check   Exit with code 1 if either file is out of sync instead of updating it.
#
# Exit codes:
#   0 - both files are already in sync, or were updated successfully
#   1 - a file is out of sync (only when --check is passed), or an error occurred

set -euo pipefail

# Script must be run from the repository root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

CONSTANTS_FILE="$REPO_ROOT/pkg/constants/version_constants.go"
COMPAT_FILE="$REPO_ROOT/.github/aw/compat.json"
INSTALL_SCRIPT="$REPO_ROOT/actions/setup/sh/install_copilot_cli.sh"

CHECK_ONLY=0
for arg in "$@"; do
  if [ "$arg" = "--check" ]; then
    CHECK_ONLY=1
  fi
done

# Extract DefaultCopilotVersion from Go constants file
COPILOT_VERSION=$(grep -E '^\s*const DefaultCopilotVersion' "$CONSTANTS_FILE" | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$COPILOT_VERSION" ]; then
  echo "Error: could not extract DefaultCopilotVersion from $CONSTANTS_FILE" >&2
  exit 1
fi

# Never propagate a version unless its compiled checksum matches the reviewed pin.
COPILOT_CHECKSUM=$(sed -nE 's/^const DefaultCopilotChecksumsSHA256 = "([0-9a-f]{64})"$/\1/p' "$CONSTANTS_FILE")
# shellcheck source=actions/setup/sh/release_integrity.sh
source "$REPO_ROOT/actions/setup/sh/release_integrity.sh"
REVIEWED_CHECKSUM="$(release_artifact_sha256 github/copilot-cli "$COPILOT_VERSION" SHA256SUMS.txt)" || {
  echo "Error: no reviewed checksum for Copilot version $COPILOT_VERSION" >&2
  exit 1
}
if [ "$COPILOT_CHECKSUM" != "$REVIEWED_CHECKSUM" ]; then
  echo "Error: Copilot version and checksum do not match the reviewed pin; run scripts/update-copilot-version.sh $COPILOT_VERSION" >&2
  exit 1
fi

# Read the current max-agent of the latest interval (entry with "open": true)
CURRENT_MAX_AGENT=$(jq -r '."agent-compat-v1".copilot[] | select(.open == true) | ."max-agent"' "$COMPAT_FILE")
if [ -z "$CURRENT_MAX_AGENT" ] || [ "$CURRENT_MAX_AGENT" = "null" ]; then
  echo "Error: could not find latest interval (\"open\": true) in $COMPAT_FILE" >&2
  exit 1
fi

# Read the current DEFAULT_COPILOT_VERSION baked into the install script
SCRIPT_DEFAULT_VERSION=$(grep -E '^DEFAULT_COPILOT_VERSION=' "$INSTALL_SCRIPT" | sed -E 's/DEFAULT_COPILOT_VERSION="([^"]+)"/\1/')
if [ -z "$SCRIPT_DEFAULT_VERSION" ]; then
  echo "Error: could not extract DEFAULT_COPILOT_VERSION from $INSTALL_SCRIPT" >&2
  exit 1
fi

COMPAT_IN_SYNC=1
SCRIPT_IN_SYNC=1

if [ "$CURRENT_MAX_AGENT" != "$COPILOT_VERSION" ]; then
  COMPAT_IN_SYNC=0
fi
if [ "$SCRIPT_DEFAULT_VERSION" != "$COPILOT_VERSION" ]; then
  SCRIPT_IN_SYNC=0
fi

if [ "$COMPAT_IN_SYNC" = "1" ] && [ "$SCRIPT_IN_SYNC" = "1" ]; then
  echo "compat.json and install_copilot_cli.sh are already in sync (version=$COPILOT_VERSION)"
  exit 0
fi

if [ "$CHECK_ONLY" = "1" ]; then
  if [ "$COMPAT_IN_SYNC" = "0" ]; then
    echo "Error: compat.json is out of sync: max-agent=$CURRENT_MAX_AGENT, DefaultCopilotVersion=$COPILOT_VERSION" >&2
  fi
  if [ "$SCRIPT_IN_SYNC" = "0" ]; then
    echo "Error: install_copilot_cli.sh is out of sync: DEFAULT_COPILOT_VERSION=$SCRIPT_DEFAULT_VERSION, DefaultCopilotVersion=$COPILOT_VERSION" >&2
  fi
  echo "Run 'make sync-compat' to update." >&2
  exit 1
fi

if [ "$COMPAT_IN_SYNC" = "0" ]; then
  # Update max-agent in the latest interval
  UPDATED=$(jq --arg version "$COPILOT_VERSION" \
    '."agent-compat-v1".copilot = [."agent-compat-v1".copilot[] | if .open == true then ."max-agent" = $version else . end]' \
    "$COMPAT_FILE")

  echo "$UPDATED" > "$COMPAT_FILE"
  echo "Updated compat.json: max-agent $CURRENT_MAX_AGENT -> $COPILOT_VERSION"
fi

if [ "$SCRIPT_IN_SYNC" = "0" ]; then
  # Update DEFAULT_COPILOT_VERSION in the install script
  sed -i "s/^DEFAULT_COPILOT_VERSION=\"[^\"]*\"/DEFAULT_COPILOT_VERSION=\"$COPILOT_VERSION\"/" "$INSTALL_SCRIPT"
  echo "Updated install_copilot_cli.sh: DEFAULT_COPILOT_VERSION $SCRIPT_DEFAULT_VERSION -> $COPILOT_VERSION"
fi
