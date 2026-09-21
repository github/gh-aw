#!/usr/bin/env bash
set +o histexpand

#
# restore_base_github_folders.sh - Restore agent config folders/files from the base
#                                   branch snapshot after PR checkout
#
# After checkout_pr_branch runs the workspace contains PR-branch content,
# which may include attacker-controlled skill/instruction files for fork PRs.
# This script overwrites agent-specific folders and root instruction files with
# the trusted snapshot saved by save_base_github_folders.sh during the activation
# job.  It also removes .mcp.json from the workspace root, which may contain
# untrusted MCP server configuration from the PR branch.
#
# For each item:
#   - If present in the base snapshot: restore it (overwrite PR-branch content)
#   - If absent from the base snapshot but present in the workspace: remove it
#     (prevent the PR branch from injecting files that the base branch doesn't have)
#
# PR-branch content is removed by deleting the git-tracked files under each item
# rather than the whole directory.  Everything the PR branch can control is
# tracked in git, so untracked files are necessarily produced by earlier steps of
# the agent job itself (for example the APM package restore, which unpacks skills
# into .claude/skills/ or .github/skills/).  Those files are trusted and must
# survive this step.  When the workspace is not a git repository the script falls
# back to removing the whole directory.
#
# The lists of folders and files MUST match those used in save_base_github_folders.sh
# and are passed via the same environment variables:
#
#   GH_AW_AGENT_FOLDERS  - space-separated list of directories to restore
#                          (e.g. ".agents .claude .codex .gemini .github")
#   GH_AW_AGENT_FILES    - space-separated list of root files to restore
#                          (e.g. "AGENTS.md CLAUDE.md GEMINI.md")
#
# Exit codes:
#   0 - Success

set -euo pipefail

WORKSPACE="${GITHUB_WORKSPACE:-$(pwd)}"
SRC="/tmp/gh-aw/base"

# Parse the engine-registry-derived lists from environment variables.
# These must match the values used in save_base_github_folders.sh.
IFS=' ' read -ra FOLDERS <<< "${GH_AW_AGENT_FOLDERS:-}"
IFS=' ' read -ra ROOT_FILES <<< "${GH_AW_AGENT_FILES:-}"

# Detect whether the workspace is a git working tree.  If it is, PR-branch
# content can be identified precisely as the git-tracked files, which lets this
# script keep untracked files installed by earlier trusted steps of the agent job.
USE_GIT_TRACKING=0
if git -C "${WORKSPACE}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  USE_GIT_TRACKING=1
else
  echo "Workspace is not a git working tree, removing PR content by path"
fi

# remove_pr_content <relative-path>
# Removes PR-branch content at the given workspace path, preserving untracked
# files (installed by earlier steps such as the APM package restore).
remove_pr_content() {
  local rel_path="$1"
  local dest="${WORKSPACE}/${rel_path}"

  if [ "${USE_GIT_TRACKING}" -eq 0 ]; then
    rm -rf "${dest}"
    return
  fi

  # Collect tracked paths in a file: command substitution cannot carry the NUL
  # separators, and a failing git call must fall back to removing everything.
  local tracked_list tracked
  tracked_list="$(mktemp)"
  if ! git -C "${WORKSPACE}" ls-files -z -- "${rel_path}" >"${tracked_list}" 2>/dev/null; then
    rm -f "${tracked_list}"
    echo "Could not list tracked files for ${rel_path}, removing it entirely"
    rm -rf "${dest}"
    return
  fi

  while IFS= read -r -d '' tracked; do
    # -r handles gitlink entries (submodule directories)
    rm -rf "${WORKSPACE:?}/${tracked:?}"
  done <"${tracked_list}"
  rm -f "${tracked_list}"

  # Drop directories that only held PR-branch files
  if [ -d "${dest}" ]; then
    find "${dest}" -type d -empty -delete
  fi
}

for FOLDER in "${FOLDERS[@]+"${FOLDERS[@]}"}"; do
  SNAPSHOT="${SRC}/${FOLDER}"
  DEST="${WORKSPACE}/${FOLDER}"
  if [ -d "${SNAPSHOT}" ]; then
    remove_pr_content "${FOLDER}"
    mkdir -p "${DEST}"
    cp -R "${SNAPSHOT}/." "${DEST}/"
    echo "Restored ${FOLDER} from base branch snapshot"
  elif [ -d "${DEST}" ]; then
    # PR branch injected this directory but base doesn't have it — remove it
    remove_pr_content "${FOLDER}"
    if [ -d "${DEST}" ]; then
      echo "Removed PR-injected files from ${FOLDER} (not present in base branch), kept files installed by earlier steps"
    else
      echo "Removed PR-injected ${FOLDER} (not present in base branch)"
    fi
  else
    echo "No base branch snapshot for ${FOLDER}, skipping"
  fi
done

for FILE in "${ROOT_FILES[@]+"${ROOT_FILES[@]}"}"; do
  SNAPSHOT="${SRC}/${FILE}"
  DEST="${WORKSPACE}/${FILE}"
  if [ -f "${SNAPSHOT}" ]; then
    cp "${SNAPSHOT}" "${DEST}"
    echo "Restored ${FILE} from base branch snapshot"
  elif [ -f "${DEST}" ]; then
    # PR branch injected this file but base doesn't have it — remove it when it
    # comes from the PR branch (tracked); keep step-generated untracked files
    remove_pr_content "${FILE}"
    if [ -f "${DEST}" ]; then
      echo "Kept ${FILE} (not tracked in the PR branch)"
    else
      echo "Removed PR-injected ${FILE} (not present in base branch)"
    fi
  else
    echo "No base branch snapshot for ${FILE}, skipping"
  fi
done

# Remove .mcp.json — may contain untrusted MCP server config from the PR branch
if [ -f "${WORKSPACE}/.mcp.json" ]; then
  rm -f "${WORKSPACE}/.mcp.json"
  echo "Removed .mcp.json from workspace"
fi
