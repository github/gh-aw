#!/usr/bin/env bash
#
# restore_git_credentials.sh - Restore git credentials from backup
#
# This script restores git credentials that were backed up by clean_git_credentials.sh
# before agent execution. This allows safe-outputs operations that need git to work
# properly after the agent has completed its execution.
#
# Exit codes:
#   0 - Success (credentials restored or no backup found)
#   1 - Error (failed to restore credentials)

set -euo pipefail

# Get the workspace directory (defaults to current GITHUB_WORKSPACE)
WORKSPACE="${GITHUB_WORKSPACE:-.}"
GIT_CONFIG_PATH="${WORKSPACE}/.git/config"
BACKUP_DIR="/tmp/gh-aw/git-credentials-backup"
BACKUP_PATH="${BACKUP_DIR}/config.backup"

echo "Restoring git credentials to ${GIT_CONFIG_PATH}"

# Check if backup exists
if [ ! -f "${BACKUP_PATH}" ]; then
  echo "No backup found at ${BACKUP_PATH}, nothing to restore"
  exit 0
fi

# Restore the backup
cp "${BACKUP_PATH}" "${GIT_CONFIG_PATH}"
echo "Restored git config from ${BACKUP_PATH}"

# Verify the file is still valid git config
if ! git config --file "${GIT_CONFIG_PATH}" --list >/dev/null 2>&1; then
  echo "ERROR: Restored git config file is corrupted"
  exit 1
fi

echo "✓ Git credentials restored successfully"

exit 0
