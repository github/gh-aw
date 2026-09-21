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

for FOLDER in "${FOLDERS[@]+"${FOLDERS[@]}"}"; do
  SNAPSHOT="${SRC}/${FOLDER}"
  DEST="${WORKSPACE}/${FOLDER}"
  if [ -d "${SNAPSHOT}" ]; then
    rm -rf "${DEST}"
    cp -r "${SNAPSHOT}" "${DEST}"
    echo "Restored ${FOLDER} from base branch snapshot"
  elif [ -d "${DEST}" ]; then
    # PR branch injected this directory but base doesn't have it — remove it
    rm -rf "${DEST}"
    echo "Removed PR-injected ${FOLDER} (not present in base branch)"
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
    # PR branch injected this file but base doesn't have it — remove it
    rm -f "${DEST}"
    echo "Removed PR-injected ${FILE} (not present in base branch)"
  else
    echo "No base branch snapshot for ${FILE}, skipping"
  fi
done

# Remove .mcp.json — may contain untrusted MCP server config from the PR branch
if [ -f "${WORKSPACE}/.mcp.json" ]; then
  rm -f "${WORKSPACE}/.mcp.json"
  echo "Removed .mcp.json from workspace"
fi
