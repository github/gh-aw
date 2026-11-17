#!/usr/bin/env bash
set +o histexpand

#
# restore_inline_sub_agents.sh - Copy inline sub-agent files from the activation
#                                 artifact into the workspace so the engine CLI
#                                 can discover them.
#
# During the activation job, `interpolate_prompt.cjs` extracts `## agent: \`name\``
# sections from the workflow markdown (after runtime-import macros are resolved)
# and writes them to $GH_AW_RUNTIME_DIR/<GH_AW_SUB_AGENT_DIR>/. Those files are then
# uploaded as part of the activation artifact.
#
# This script copies those files from the downloaded artifact path into the
# workspace so the engine CLI (Copilot, Claude, etc.) can pick them up.
#
# Environment variables:
#   GH_AW_SUB_AGENT_DIR  - path relative to GH_AW_RUNTIME_DIR/ and GITHUB_WORKSPACE/
#                          where sub-agent files live (e.g. ".agents/agents")
#   GH_AW_SUB_AGENT_EXT  - filename extension for sub-agent files (e.g. ".agent.md")
#
# Exit codes:
#   0 - Success (even when there are no sub-agent files to copy)

set -euo pipefail

SRC="${GH_AW_RUNTIME_DIR:?}/${GH_AW_SUB_AGENT_DIR}"
DST="${GITHUB_WORKSPACE}/${GH_AW_SUB_AGENT_DIR}"

echo "[restore-sub-agents] source: $SRC"

if [ -d "$SRC" ]; then
  count=$(ls "$SRC"/*"${GH_AW_SUB_AGENT_EXT}" 2>/dev/null | wc -l || echo 0)
  echo "[restore-sub-agents] found $count *${GH_AW_SUB_AGENT_EXT} file(s) in source"
  ls -la "$SRC"/*"${GH_AW_SUB_AGENT_EXT}" 2>/dev/null || echo "[restore-sub-agents] no *${GH_AW_SUB_AGENT_EXT} files in source"
  mkdir -p "$DST"
  cp "$SRC/"*"${GH_AW_SUB_AGENT_EXT}" "$DST/" 2>/dev/null || echo "[restore-sub-agents] cp failed or no files to copy"
  echo "[restore-sub-agents] destination ($DST) after copy:"
  ls -la "$DST" 2>/dev/null || echo "[restore-sub-agents] destination directory is empty or missing"
else
  echo "[restore-sub-agents] source directory not found — no inline sub-agents to restore"
fi
