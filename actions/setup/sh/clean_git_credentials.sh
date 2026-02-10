#!/usr/bin/env bash
#
# clean_git_credentials.sh - Temporarily remove git credentials from .git/config
#
# This script temporarily removes any git credentials that may have been left on disk
# accidentally by an injected step. It backs up the configuration so it can be restored
# after agent execution. This prevents the agent from accessing or exfiltrating credentials
# while still allowing safe-outputs operations that need git to work after agent execution.
#
# Exit codes:
#   0 - Success (credentials cleaned or no .git/config found)
#   1 - Error (failed to clean credentials)

set -euo pipefail

# Get the workspace directory (defaults to current GITHUB_WORKSPACE)
WORKSPACE="${GITHUB_WORKSPACE:-.}"
GIT_CONFIG_PATH="${WORKSPACE}/.git/config"
BACKUP_DIR="/tmp/gh-aw/git-credentials-backup"
BACKUP_PATH="${BACKUP_DIR}/config.backup"

echo "Backing up and cleaning git credentials from ${GIT_CONFIG_PATH}"

# Check if .git/config exists
if [ ! -f "${GIT_CONFIG_PATH}" ]; then
  echo "No .git/config found at ${GIT_CONFIG_PATH}, nothing to clean"
  exit 0
fi

# Create backup directory and save current config
mkdir -p "${BACKUP_DIR}"
cp "${GIT_CONFIG_PATH}" "${BACKUP_PATH}"
echo "Backed up git config to ${BACKUP_PATH}"

# Remove credential helper configuration
# This removes lines like:
#   [credential]
#       helper = ...
# And any credential URL-specific configs like:
#   [credential "https://github.com"]
#       helper = ...
if git config --file "${GIT_CONFIG_PATH}" --remove-section credential 2>/dev/null; then
  echo "Removed [credential] section from git config"
fi

# Remove credential URL-specific sections using grep
# This handles multi-line credential sections with URLs
sed -i '/^\[credential /,/^\[/{ /^\[credential /d; /^\[/!d; }' "${GIT_CONFIG_PATH}" 2>/dev/null || true

# Remove http extraheader (used by GitHub Actions for authentication)
# This is used by actions/checkout to authenticate
if git config --file "${GIT_CONFIG_PATH}" --unset-all http.extraheader 2>/dev/null; then
  echo "Removed http.extraheader from git config"
fi

# Remove any http.<url>.extraheader configurations
git config --file "${GIT_CONFIG_PATH}" --get-regexp '^http\..*\.extraheader$' 2>/dev/null | while read -r key _; do
  git config --file "${GIT_CONFIG_PATH}" --unset-all "$key" || true
  echo "Removed $key from git config"
done || true

# Remove any credentials from remote URLs (https://username:password@github.com format)
# Replace authenticated URLs with unauthenticated ones
if git config --file "${GIT_CONFIG_PATH}" --get-regexp '^remote\..*\.url$' 2>/dev/null | grep -q '@'; then
  echo "Found authenticated remote URLs, cleaning..."
  git config --file "${GIT_CONFIG_PATH}" --get-regexp '^remote\..*\.url$' 2>/dev/null | while read -r key url; do
    # Remove credentials from URL: https://user:pass@host -> https://host
    clean_url=$(echo "$url" | sed -E 's|(https?://)([^@]+@)?(.*)|\1\3|')
    if [ "$url" != "$clean_url" ]; then
      git config --file "${GIT_CONFIG_PATH}" "$key" "$clean_url"
      echo "Cleaned credentials from $key"
    fi
  done || true
fi

echo "✓ Git credentials cleaned successfully (backed up for restoration)"

# Verify the file is still valid git config
if ! git config --file "${GIT_CONFIG_PATH}" --list >/dev/null 2>&1; then
  echo "ERROR: Git config file is corrupted after cleaning"
  exit 1
fi

exit 0
