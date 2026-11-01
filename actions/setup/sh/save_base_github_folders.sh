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
# Root instruction symlinks and targets outside a saved directory are
# materialized from the trusted workspace; directory-internal links are retained.
# Targets outside the workspace are rejected.
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

validate_item() {
  # Registry entries are workspace-root names, never paths or tar options.
  if [[ ! "$1" =~ ^[a-zA-Z0-9_.-]+$ || "$1" == "." || "$1" == ".." ]]; then
    echo "Invalid agent configuration item: $1" >&2
    exit 1
  fi
}

for ITEM in "${FOLDERS[@]+"${FOLDERS[@]}"}" "${ROOT_FILES[@]+"${ROOT_FILES[@]}"}"; do
  validate_item "${ITEM}"
done

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
  # Materialize targets outside a restored tree so links cannot read PR content.
  STAGING_DIR="${DEST}/.save.$$"
  (umask 077; mkdir "${STAGING_DIR}")
  trap 'rm -rf "${STAGING_DIR}"' EXIT
  node - "${WORKSPACE}" "${STAGING_DIR}" "${ITEMS[@]}" <<'NODE'
const fs = require("node:fs");
const path = require("node:path");
const [workspaceArg, staging, ...items] = process.argv.slice(2);
const workspace = fs.realpathSync(workspaceArg);
const within = (root, candidate) => candidate === root || candidate.startsWith(root + path.sep);

function confined(source) {
  const resolved = fs.realpathSync(source);
  if (!within(workspace, resolved)) {
    throw new Error("Agent configuration target is outside the workspace: " + source);
  }
  return resolved;
}

function copyTree(source, destination, treeRoot, ancestors = []) {
  const resolved = confined(source);
  if (fs.lstatSync(source).isSymbolicLink()) {
    const target = fs.readlinkSync(source);
    const lexicalTarget = path.resolve(path.dirname(source), target);
    // Retain only relative links within the tree restored together.
    if (!path.isAbsolute(target) && within(treeRoot, lexicalTarget) && within(treeRoot, resolved)) {
      fs.symlinkSync(target, destination);
      return;
    }
    source = resolved;
    treeRoot = fs.statSync(source).isDirectory() ? source : path.dirname(source);
  }
  if (ancestors.includes(resolved)) {
    throw new Error("Cyclic agent configuration symlink: " + source);
  }
  const stat = fs.statSync(source);
  if (stat.isDirectory()) {
    fs.mkdirSync(destination);
    for (const child of fs.readdirSync(source).sort()) {
      copyTree(path.join(source, child), path.join(destination, child), treeRoot, [...ancestors, resolved]);
    }
  } else if (stat.isFile()) {
    fs.copyFileSync(source, destination);
  } else {
    throw new Error("Unsupported agent configuration entry: " + source);
  }
  fs.chmodSync(destination, stat.mode & 0o7777);
  fs.utimesSync(destination, stat.atime, stat.mtime);
}

for (const item of new Set(items)) {
  const source = confined(path.join(workspace, item));
  // Root instruction files and top-level directory aliases are dereferenced.
  copyTree(source, path.join(staging, item), fs.statSync(source).isDirectory() ? source : workspace);
}
NODE
  tar -cf "${DEST}/snapshot.tar" -C "${STAGING_DIR}" -- "${ITEMS[@]}"
fi
