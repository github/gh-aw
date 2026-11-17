#!/bin/bash
set +o histexpand

# setup_cache_memory_git.sh
# Pre-agent git setup for integrity-aware cache-memory.
#
# This script is run AFTER the cache is restored and BEFORE the agent executes.
# It ensures the cache directory contains a git repository with integrity branches
# and checks out the correct branch for the current run's integrity level.
# After git setup it applies pre-agent security sanitization: strips execute bits from
# all working-tree files, and removes files with disallowed extensions when
# GH_AW_ALLOWED_EXTENSIONS is set.
#
# Required environment variables:
#   GH_AW_CACHE_DIR:             Path to the cache-memory directory (e.g. /tmp/gh-aw/cache-memory)
#   GH_AW_MIN_INTEGRITY:         Integrity level for this run (merged|approved|unapproved|none)
#
# Optional environment variables:
#   GH_AW_ALLOWED_EXTENSIONS:    Colon-separated list of allowed file extensions for pre-agent
#                                sanitization (e.g. .json:.md:.txt). When set, any restored file
#                                whose extension is not in this list is removed before the agent runs.

set -euo pipefail

TMP_DIR="$(mktemp -d /tmp/gh-aw-cache-memory.XXXXXX)"
trap 'rm -rf -- "$TMP_DIR"' EXIT

CACHE_DIR="${GH_AW_CACHE_DIR:-/tmp/gh-aw/cache-memory}"
INTEGRITY="${GH_AW_MIN_INTEGRITY:-none}"

# All integrity levels in descending order (highest first)
LEVELS=("merged" "approved" "unapproved" "none")

ensure_writable_dir() {
  local dir="$1"
  local purpose="$2"
  local probe_file=""
  local mkdir_err
  local chmod_err
  local write_err
  mkdir_err="$(mktemp /tmp/gh-aw-cache-mkdir-err.XXXXXX)"
  chmod_err="$(mktemp /tmp/gh-aw-cache-chmod-err.XXXXXX)"
  write_err="$(mktemp /tmp/gh-aw-cache-write-err.XXXXXX)"

  if ! mkdir -p "$dir" 2>"$mkdir_err"; then
    echo "ERROR: cache-memory setup error: failed to create ${purpose} (${dir})" >&2
    cat "$mkdir_err" >&2 || true
    rm -f "$mkdir_err" "$chmod_err" "$write_err" 2>/dev/null || true
    exit 1
  fi

  if ! chmod u+rwx "$dir" 2>"$chmod_err"; then
    echo "ERROR: cache-memory setup error: ${purpose} is not writable (${dir})" >&2
    cat "$chmod_err" >&2 || true
    rm -f "$mkdir_err" "$chmod_err" "$write_err" 2>/dev/null || true
    exit 1
  fi

  if ! probe_file="$(mktemp "${dir}/gh-aw-write-check.XXXXXX" 2>"$write_err")"; then
    echo "ERROR: cache-memory setup error: ${purpose} is not writable (${dir})" >&2
    cat "$write_err" >&2 || true
    rm -f "$mkdir_err" "$chmod_err" "$write_err" 2>/dev/null || true
    exit 1
  fi
  rm -f "$probe_file" "$mkdir_err" "$chmod_err" "$write_err" 2>/dev/null || true
}

initialize_cache_memory_git_repo() {
  # No git repo yet — either a fresh cache or a legacy flat-file cache.
  # Initialize a git repository with an empty baseline commit on the highest-trust
  # branch, then create all other integrity branches from that empty state.
  # IMPORTANT: Legacy flat files (written at unknown/none integrity in a previous
  # version of gh-aw) are committed to the 'none' branch only to prevent trust
  # escalation — do NOT commit them to 'merged' or any higher-trust branch.
  git init -b merged -q
  git config user.email "gh-aw@github.com"
  git config user.name "gh-aw"
  # Disable hooks immediately after init so that no cached hook file can fire
  # during checkout or merge operations later in this script.
  git config core.hooksPath /dev/null
  # Create an empty initial commit as the trusted baseline for all branches
  git commit --allow-empty -m "initial" -q

  # Create all integrity branches from the empty baseline
  for level in "${LEVELS[@]}"; do
    if [ "$level" != "merged" ]; then
      git branch "$level" 2>/dev/null || true
    fi
  done

  # Migrate any pre-existing flat files to the 'none' branch only (lowest trust).
  # Switching to 'none' before staging ensures legacy data cannot be read by
  # higher-integrity runs via the merge-down step.
  git checkout -q none
  git add -A
  git commit --allow-empty -m "migrate-legacy-files" -q

  echo "Cache memory git repository initialized with branches: ${LEVELS[*]}"
}

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
  local repo_root="$1"
  local path
  for path in "$repo_root/.git" "$repo_root/.git/config" "$repo_root/.git/info" "$repo_root/.git/hooks"; do
    if [ -L "$path" ]; then
      return 0
    fi
  done
  return 1
}

mkdir -p "$CACHE_DIR"
cd "$CACHE_DIR"

# --- Flatten legacy nested artifact layout before git setup ---
# Older cache-memory artifact uploads could restore into a nested directory whose
# name matched the cache directory basename (for example ./cache-memory/* inside
# /tmp/gh-aw/cache-memory). If that layout is restored, move the nested contents
# back to the cache root before cache-hit detection so the agent sees the files
# at the expected paths again.
CACHE_BASENAME="$(basename "$CACHE_DIR")"
LEGACY_NESTED_DIR="./${CACHE_BASENAME}"
if [ -d "$LEGACY_NESTED_DIR" ] && [ ! -d .git ]; then
  _root_non_git_entries=$(find . -mindepth 1 -maxdepth 1 ! -name '.git' ! -name "$CACHE_BASENAME" | wc -l | tr -d ' ')
  if [ "${_root_non_git_entries}" = "0" ]; then
    echo "Flattening legacy nested cache directory: ${LEGACY_NESTED_DIR}"
    shopt -s dotglob nullglob
    _legacy_nested_entries=("${LEGACY_NESTED_DIR}"/*)
    if [ "${#_legacy_nested_entries[@]}" -gt 0 ] && [ -e "${_legacy_nested_entries[0]}" ]; then
      mv "${_legacy_nested_entries[@]}" .
    fi
    shopt -u dotglob nullglob
    rmdir "${LEGACY_NESTED_DIR}" 2>/dev/null || true
  fi
fi

# --- Detect cache hit before any git operations ---
# A pre-existing .git directory indicates the cache was restored from a previous run.
IS_CACHE_HIT=false
if [ -d .git ]; then
  IS_CACHE_HIT=true
  echo "Cache hit detected: git repository found (restored from a previous run)"
else
  echo "Cache cold start: no git repository found, will initialize"
fi

# --- Security: reject symlinked git metadata before any .git operations ---
if [ -d .git ] && has_symlinked_git_metadata "$CACHE_DIR"; then
  echo "WARNING: Detected symlinked cache-memory git metadata; reinitializing git metadata"
  rm -rf .git
  IS_CACHE_HIT=false
  initialize_cache_memory_git_repo
fi

# --- Log cache directory contents after restore (before git setup) ---
echo "=== Cache directory: non-git files present after restore ==="
_pre_files=$(find . -not -path './.git/*' -type f 2>/dev/null | sort || true)
if [ -n "$_pre_files" ]; then
  echo "$_pre_files"
else
  echo "(no non-git files)"
fi

# --- Security: clear git hooks before any git operations ---
# Git hook files under .git/hooks/ are preserved in the cache but are NOT tracked
# by git (git add -A ignores .git/). A compromised agent run could write executable
# hooks (e.g. post-checkout, post-merge) that would be restored from cache and
# executed on the host runner before the AWF sandbox is established. Remove all
# non-sample hook files immediately after cache restore to prevent this.
if [ -d .git/hooks ]; then
  find .git/hooks -type f ! -name '*.sample' -delete
fi

# --- Format detection & migration ---
if [ ! -d .git ]; then
  initialize_cache_memory_git_repo
else
  # If restored git metadata is corrupt (for example missing tree objects from a
  # raced or force-pushed cache branch), reset to a clean repo while preserving
  # restored files in the working tree.
  if ! git fsck --connectivity-only --no-progress >"${TMP_DIR}/git-fsck-out" 2>"${TMP_DIR}/git-fsck-err"; then
    echo "WARNING: Detected corrupted cache-memory git repository; reinitializing git metadata"
    cat "${TMP_DIR}/git-fsck-err" 2>/dev/null || true
    rm -rf .git
    IS_CACHE_HIT=false
    initialize_cache_memory_git_repo
  fi

  # Existing repo: disable hooks as belt-and-suspenders after the earlier
  # hook-file cleanup step in this script.
  # If git metadata is malformed enough that config cannot be written (for example
  # missing HEAD), recover by reinitializing while preserving working-tree files.
  _hooks_config_err="$(mktemp)"
  if ! git config core.hooksPath /dev/null 2>"$_hooks_config_err"; then
    echo "WARNING: Detected corrupted cache-memory git repository (cannot configure hooks); reinitializing git metadata"
    cat "$_hooks_config_err" 2>/dev/null || true
    rm -rf .git
    IS_CACHE_HIT=false
    initialize_cache_memory_git_repo
  fi
  rm -f "$_hooks_config_err" 2>/dev/null || true
fi

# --- Security: scrub git config/info state and enforce hardened defaults ---
# Cache restores can carry forward untrusted git configuration and info files
# from prior runs. Remove untrusted info overrides and dangerous config keys
# while preserving repository metadata (remotes/branch sections).
if [ -d .git ]; then
  mkdir -p .git/info
  rm -f .git/info/exclude .git/info/attributes .git/info/grafts .git/info/sparse-checkout

  git config --unset-all core.attributesFile >/dev/null 2>&1 || true
  git config --unset-all core.fsmonitor >/dev/null 2>&1 || true
  git config --unset-all core.sshCommand >/dev/null 2>&1 || true
  git config --unset-all core.hooksPath >/dev/null 2>&1 || true
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
fi

# --- Checkout current integrity branch ---
# Use -q to suppress "Switched to branch" noise
if ! git checkout -q "$INTEGRITY" 2>"${TMP_DIR}/checkout-err"; then
  checkout_exit=$?
  if grep -qiE "unable to read tree|could not parse HEAD|bad object|missing tree" "${TMP_DIR}/checkout-err" 2>/dev/null; then
    echo "WARNING: checkout failed due to cache-memory git corruption; reinitializing git metadata"
    cat "${TMP_DIR}/checkout-err" 2>/dev/null || true
    rm -rf .git
    IS_CACHE_HIT=false
    initialize_cache_memory_git_repo
    git checkout -q "$INTEGRITY"
  else
    echo "ERROR: failed to checkout integrity branch '$INTEGRITY' (exit $checkout_exit):" >&2
    cat "${TMP_DIR}/checkout-err" >&2
    exit "$checkout_exit"
  fi
fi

# --- Merge down from higher-integrity branches ---
# Read semantics: lower-integrity runs see higher-integrity data via merge,
# but higher-integrity runs never see lower-integrity data.
# -X theirs: higher-integrity branch wins conflicts.
for level in "${LEVELS[@]}"; do
  if [ "$level" = "$INTEGRITY" ]; then
    break
  fi
  # Merge higher-integrity branch into the current branch
  if git merge "$level" -X theirs --no-edit -m "merge-from-$level" -q 2>"${TMP_DIR}/merge-err"; then
    echo "Merged integrity branch '$level' into '$INTEGRITY'"
  else
    merge_exit=$?
    # Abort the merge to restore a clean working tree, then hard-reset to the
    # pre-merge state so the agent always starts from a consistent, usable tree.
    git merge --abort 2>/dev/null || git reset --hard HEAD 2>/dev/null || true
    # Ignore "already up-to-date" and "nothing to merge" — fail fast on real errors
    if grep -qiE "already up.to.date|nothing to merge|nothing to commit" "${TMP_DIR}/merge-err" 2>/dev/null; then
      echo "Nothing to merge from '$level' into '$INTEGRITY' (already up-to-date)"
    else
      echo "ERROR: merge from '$level' into '$INTEGRITY' failed (exit $merge_exit):" >&2
      cat "${TMP_DIR}/merge-err" >&2
      exit "$merge_exit"
    fi
  fi
done

echo "Cache memory git setup complete (integrity: $INTEGRITY)"

# --- Security: pre-agent working-tree sanitization ---
# 1. Delete all working-tree symlinks so that a prior run cannot plant links to files
#    outside the cache (e.g. secrets) that would bypass the regular-file checks below.
find . -not -path './.git/*' -type l -delete 2>/dev/null || true
echo "Pre-agent sanitization: deleted all working-tree symlinks"

# 2. Strip execute bits from all working-tree files so that a prior run cannot plant
#    executable scripts (e.g. helper.sh) that the agent or runner could invoke before
#    any validation gate fires.
find . -not -path './.git/*' -type f -exec chmod a-x {} + 2>/dev/null || true
echo "Pre-agent sanitization: stripped execute permissions from all working-tree files"

# 3. If GH_AW_ALLOWED_EXTENSIONS is set (colon-separated, e.g. .json:.md:.txt), remove
#    any restored file whose extension is not in the allowed list. This ensures the agent
#    never encounters unexpected file types planted by a prior compromised run.
if [ -n "${GH_AW_ALLOWED_EXTENSIONS:-}" ]; then
  echo "Pre-agent sanitization: enforcing allowed extensions: ${GH_AW_ALLOWED_EXTENSIONS}"
  # Build a normalized (lowercase, whitespace-trimmed) allowed list for case-insensitive
  # comparison. Pre-computing this once avoids re-parsing it for every file.
  _normalized_allowed=""
  IFS=: read -ra _raw_exts <<< "$GH_AW_ALLOWED_EXTENSIONS"
  for _e in "${_raw_exts[@]}"; do
    # Trim all whitespace and convert to lowercase
    _e="$(printf '%s' "$_e" | tr -d '[:space:]' | tr '[:upper:]' '[:lower:]')"
    if [ -n "$_e" ]; then
      _normalized_allowed="${_normalized_allowed}${_e}:"
    fi
  done
  removed=0
  # Use NUL-delimited output so filenames containing newlines are handled correctly.
  while IFS= read -r -d '' file; do
    filename="$(basename "$file")"
    # Extract the last dot-prefixed segment as the extension, or empty if no dot.
    # Normalize to lowercase for case-insensitive comparison against the allowed list.
    case "$filename" in
      *.*) ext=".$(printf '%s' "${filename##*.}" | tr '[:upper:]' '[:lower:]')" ;;
      *)   ext="" ;;
    esac
    # Check whether this extension appears in the normalized allowed list
    found=0
    IFS=: read -ra _ALLOWED_EXTS <<< "${_normalized_allowed%:}"
    for _a in "${_ALLOWED_EXTS[@]}"; do
      if [ "$ext" = "$_a" ]; then
        found=1
        break
      fi
    done
    if [ "$found" -eq 0 ]; then
      echo "Removing disallowed file: $file (extension: '${ext:-none}')"
      rm -f "$file"
      removed=$((removed + 1))
    fi
  done < <(find . -not -path './.git/*' -type f -print0)
  echo "Pre-agent sanitization complete: removed ${removed} file(s) with disallowed extensions"
fi

# --- Log cache directory contents after full setup ---
echo "=== Cache directory: non-git files available for agent after setup ==="
_post_files=$(find . -not -path './.git/*' -type f 2>/dev/null | sort || true)
if [ -n "$_post_files" ]; then
  echo "$_post_files"
  _post_file_count=$(echo "$_post_files" | wc -l | tr -d ' ')
else
  echo "(no non-git files)"
  _post_file_count=0
fi

# --- Track hit history ---
# On a cache hit, record the run ID, timestamp, and file count in a small JSON file
# so that future runs (and humans reviewing logs) can see when the last successful
# restore occurred.  The file is committed by commit_cache_memory_git.sh and therefore
# persisted into the saved cache for the next run to restore.
if [ "$IS_CACHE_HIT" = "true" ]; then
  _timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u)
  _run_id="${GITHUB_RUN_ID:-unknown}"
  printf '{"last_hit":{"run_id":"%s","timestamp":"%s","cache_files":%s}}\n' \
    "$_run_id" "$_timestamp" "$_post_file_count" > "cache-hit-history.json"
  echo "Cache hit history updated (run: $_run_id, files: $_post_file_count)"
fi

# Preflight write checks for known cache-memory paths required by daily planners.
# Fail fast here so agent runs do not continue after a hidden permission problem.
ensure_writable_dir "$CACHE_DIR" "cache-memory root directory"
ensure_writable_dir "${CACHE_DIR}/spdd-daily" "Daily SPDD rotation cache directory"
echo "Cache memory preflight write checks passed"
