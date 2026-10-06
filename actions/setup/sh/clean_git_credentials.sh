#!/usr/bin/env bash
set +o histexpand

#
# clean_git_credentials.sh - Remove git credentials from all git checkouts
#
# This script removes any git credentials that may have been left on disk
# accidentally by an injected step. It recursively finds all .git/config
# files in $GITHUB_WORKSPACE and /tmp/ and cleans credentials from each.
#
# This is a security measure to ensure that git credentials configured by
# custom steps or other workflow steps are removed before the agentic engine
# executes, preventing the agent from accessing or exfiltrating credentials.
#
# Exit codes:
#   0 - Success (credentials cleaned or no .git/config found)
#   1 - Error (failed to clean credentials)

set -euo pipefail

# clean_git_config removes credentials from a single .git/config file
clean_git_config() {
  local GIT_CONFIG_PATH="$1"

  echo "Cleaning git credentials from ${GIT_CONFIG_PATH@Q}"

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

  # Remove credential URL-specific sections using sed
  # Pattern: match lines from "[credential ..." to the next section header,
  # deleting the credential header line and all lines until the next "[" section.
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

  # Checkout stores persisted credentials in a separate config reached via includeIf.
  while IFS= read -r -d '' entry; do
    local key="${entry%%$'\n'*}" include_path="${entry#*$'\n'}"
    if [[ "$include_path" =~ (^|/)git-credentials-[0-9a-f-]+\.config$ ]] &&
      git config --file "${GIT_CONFIG_PATH}" --fixed-value --get-all "$key" "$include_path" >/dev/null; then
      git config --file "${GIT_CONFIG_PATH}" --fixed-value --unset-all "$key" "$include_path"
      if [[ -n "${RUNNER_TEMP:-}" && "$include_path" = /* ]]; then
        local temp_dir resolved_path
        temp_dir=$(realpath -m -- "$RUNNER_TEMP")
        resolved_path=$(realpath -m -- "$include_path")
        if [[ "$resolved_path" == "$temp_dir"/* ]]; then
          rm -f -- "$include_path"
        fi
      fi
      echo "Removed checkout credential include from git config"
    fi
  done < <(git config --file "${GIT_CONFIG_PATH}" --null --get-regexp '^includeif\..*\.path$' 2>/dev/null)

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

  echo "✓ Git credentials cleaned from ${GIT_CONFIG_PATH@Q}"

  # Verify the file is still valid git config
  if ! git config --file "${GIT_CONFIG_PATH}" --list >/dev/null 2>&1; then
    echo "ERROR: Git config file is corrupted after cleaning: ${GIT_CONFIG_PATH@Q}"
    exit 1
  fi
}

# Get the workspace directory (defaults to current GITHUB_WORKSPACE)
WORKSPACE="${GITHUB_WORKSPACE:-.}"

# Collect all git config files to clean from workspace and /tmp/.
# Includes both repository configs and submodule configs under .git/modules/**/config.
# Use maxdepth 15 as a conservative cap to cover deeply nested submodules
# (.git/modules/<a>/modules/<b>/...) without unbounded traversal.
CLEANED=0
while IFS= read -r git_config; do
  clean_git_config "${git_config}"
  CLEANED=$((CLEANED + 1))
done < <(find "${WORKSPACE}" /tmp -maxdepth 15 -type f -name "config" \( -path "*/.git/config" -o -path "*/.git/modules/*/config" \) 2>/dev/null | sort -u)

if [ "${CLEANED}" -eq 0 ]; then
  echo "No .git/config files found, nothing to clean"
fi

exit 0
