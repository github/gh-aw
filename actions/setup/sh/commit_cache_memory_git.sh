#!/bin/bash
set +o histexpand

# commit_cache_memory_git.sh
# Post-agent git commit for integrity-aware cache-memory.
#
# This script is run AFTER the agent executes and BEFORE the cache is saved.
# It commits all agent-written changes to the current integrity branch so that
# the git history accurately reflects which run wrote which data.
#
# Required environment variables:
#   GH_AW_CACHE_DIR:   Path to the cache-memory directory (e.g. /tmp/gh-aw/cache-memory)
#   GITHUB_RUN_ID:     GitHub Actions run ID (used as commit message)

set -euo pipefail

CACHE_DIR="${GH_AW_CACHE_DIR:-/tmp/gh-aw/cache-memory}"
RUN_ID="${GITHUB_RUN_ID:-unknown}"

if [ ! -d "$CACHE_DIR/.git" ]; then
  echo "No git repository found at $CACHE_DIR — skipping git commit"
  exit 0
fi

cd "$CACHE_DIR"

scrub_git_config_entries() {
  local key_prefix="$1"
  while IFS= read -r key_name; do
    [ -n "$key_name" ] || continue
    git config --unset-all "$key_name" >/dev/null 2>&1 || true
  done < <(
    git config --local --name-only --list 2>/dev/null \
      | grep -E -i "^${key_prefix}\\." \
      | sort -u
  )
}

has_symlinked_git_metadata() {
  [ -L .git ] || [ -n "$(find .git -type l -print -quit 2>/dev/null)" ]
}

if has_symlinked_git_metadata; then
  echo "Refusing to mutate symlinked cache-memory git metadata" >&2
  exit 1
fi

# Agent-written cache state may contain hooks or configuration that executes
# during staging or committing. Clear those command surfaces before either step.
if [ -d .git/hooks ]; then
  find .git/hooks -mindepth 1 -maxdepth 1 \( -type f -o -type l \) ! -name '*.sample' -delete
fi
mkdir -p .git/info
rm -f .git/info/exclude .git/info/attributes .git/info/grafts .git/info/sparse-checkout
rm -f .git/config.worktree

git config --unset-all extensions.worktreeConfig >/dev/null 2>&1 || true
git config --unset-all core.attributesFile >/dev/null 2>&1 || true
git config --unset-all core.fsmonitor >/dev/null 2>&1 || true
git config --unset-all core.sshCommand >/dev/null 2>&1 || true
git config --unset-all core.hooksPath >/dev/null 2>&1 || true
git config --unset-all core.worktree >/dev/null 2>&1 || true
git config --unset-all core.gitProxy >/dev/null 2>&1 || true
scrub_git_config_entries include
scrub_git_config_entries includeif
scrub_git_config_entries credential
scrub_git_config_entries alias
scrub_git_config_entries filter
scrub_git_config_entries merge

git config user.email "gh-aw@github.com"
git config user.name "gh-aw"
git config core.hooksPath /dev/null
git config core.fsmonitor false

# --- Log cache directory contents before commit ---
echo "=== Cache directory: non-git files being committed ==="
_commit_files=$(find . -not -path './.git/*' -type f 2>/dev/null | sort || true)
if [ -n "$_commit_files" ]; then
  echo "$_commit_files"
else
  echo "(no non-git files)"
fi

# Stage all changes (new files, modifications, deletions)
git add -A

# Commit on the current integrity branch; allow empty commits in case
# the agent made no changes (idempotent).
COMMIT_TEMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/gh-aw-commit.XXXXXX")
COMMIT_ERROR_FILE="$COMMIT_TEMP_DIR/stderr"
trap 'rm -f -- "$COMMIT_ERROR_FILE"; rmdir -- "$COMMIT_TEMP_DIR"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
if git -c commit.gpgSign=false commit --no-verify --allow-empty -m "run-${RUN_ID}" -q 2>"$COMMIT_ERROR_FILE"; then
  echo "Cache memory git commit complete (run: $RUN_ID)"
else
  # Distinguish "nothing to commit" (benign) from real errors
  if grep -qiE "nothing to commit|nothing added" "$COMMIT_ERROR_FILE" 2>/dev/null; then
    echo "Cache memory git: nothing to commit (run: $RUN_ID)"
  else
    echo "Warning: git commit encountered an issue:" >&2
    cat "$COMMIT_ERROR_FILE" >&2
  fi
fi

# Keep the repo small: pack loose objects and prune unreachable ones.
git gc --auto -q 2>/dev/null || true

echo "Cache memory git post-agent complete (run: $RUN_ID)"
