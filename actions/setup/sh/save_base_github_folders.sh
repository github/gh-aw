#!/usr/bin/env bash
set +o histexpand

#
# save_base_github_folders.sh - Snapshot agent config folders/files from the workspace
#
# Archives agent-specific folders and root instruction files from $GITHUB_WORKSPACE
# into /tmp/gh-aw/base/ so that they can be included in the activation artifact
# and later restored in the agent job after checkout_pr_branch runs.
#
# This prevents fork PRs from injecting malicious skill or instruction files
# into the agent's context: the activation job runs on the base branch, so the
# snapshot always reflects the trusted base-branch content.
#
# The lists of folders and files are passed via environment variables so that
# the engine registry in the Go compiler is the single source of truth:
#
#   GH_AW_AGENT_FOLDERS  - space-separated list of directories to snapshot
#                          (e.g. ".agents .claude .codex .gemini .github")
#   GH_AW_AGENT_FILES    - space-separated list of root files to snapshot
#                          (e.g. "AGENTS.md CLAUDE.md GEMINI.md")
#
# Exit codes:
#   0 - Success

set -euo pipefail

WORKSPACE="${GITHUB_WORKSPACE:-$(pwd)}"
DEST="/tmp/gh-aw/base"
rm -f "${DEST}/snapshot.tar"

# Parse the engine-registry-derived lists from environment variables.
# The compiler sets these so this script never needs manual maintenance.
IFS=' ' read -ra FOLDERS <<< "${GH_AW_AGENT_FOLDERS:-}"
IFS=' ' read -ra ROOT_FILES <<< "${GH_AW_AGENT_FILES:-}"
ITEMS=()

for FOLDER in "${FOLDERS[@]+"${FOLDERS[@]}"}"; do
  SRC="${WORKSPACE}/${FOLDER}"
  if [ -d "${SRC}" ]; then
    ITEMS+=("${FOLDER}")
    echo "Saving ${FOLDER} to base branch snapshot"
  else
    echo "${FOLDER} not found in workspace, skipping"
  fi
done

for FILE in "${ROOT_FILES[@]+"${ROOT_FILES[@]}"}"; do
  SRC="${WORKSPACE}/${FILE}"
  if [ -f "${SRC}" ]; then
    ITEMS+=("${FILE}")
    echo "Saving ${FILE} to base branch snapshot"
  else
    echo "${FILE} not found in workspace, skipping"
  fi
done

if [ "${#ITEMS[@]}" -gt 0 ]; then
  mkdir -p "${DEST}"
  # Artifact uploads discard file modes; keep them inside a tar archive.
  tar -cf "${DEST}/snapshot.tar" -C "${WORKSPACE}" -- "${ITEMS[@]}"
fi
