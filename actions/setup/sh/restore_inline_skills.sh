#!/usr/bin/env bash
set +o histexpand

#
# restore_inline_skills.sh - Copy inline skill files from the activation
#                            artifact into the workspace so the engine CLI
#                            can discover them.
#

set -euo pipefail

SRC="${GH_AW_RUNTIME_DIR:?}/${GH_AW_SKILL_DIR}"
DST="${GITHUB_WORKSPACE}/${GH_AW_SKILL_DIR}"

echo "[restore-inline-skills] source: $SRC"

if [ -d "$SRC" ]; then
  echo "[restore-inline-skills] restoring skills from $SRC to $DST"
  mkdir -p "$DST"
  cp -R "$SRC/"* "$DST/" 2>/dev/null || echo "[restore-inline-skills] no files to copy"
  echo "[restore-inline-skills] destination ($DST) after copy:"
  ls -la "$DST" 2>/dev/null || echo "[restore-inline-skills] destination directory is empty or missing"
else
  echo "[restore-inline-skills] source directory not found — no inline skills to restore"
fi
