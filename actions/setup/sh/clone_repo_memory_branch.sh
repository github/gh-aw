#!/usr/bin/env bash
set +o histexpand

# Clone repo-memory branch script
# Clones a repo-memory branch or creates an orphan branch if it doesn't exist
#
# Required environment variables:
#   GH_TOKEN: GitHub token for authentication
#   BRANCH_NAME: Name of the branch to clone
#   TARGET_REPO: Repository to clone from (e.g., owner/repo)
#   MEMORY_DIR: Directory to clone into
#   CREATE_ORPHAN: Whether to create orphan branch if it doesn't exist (true/false)
#   GITHUB_SERVER_URL: GitHub server URL (e.g., https://github.com or https://ghe.company.com)

set -e

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

harden_repo_memory_git_state() {
  local repo_root="$1"
  local safe_origin_url="$2"

  if [ ! -d "$repo_root/.git" ] && [ ! -L "$repo_root/.git" ]; then
    return 0
  fi

  if [ -d "$repo_root/.git/hooks" ]; then
    find "$repo_root/.git/hooks" -mindepth 1 -maxdepth 1 \( -type f -o -type l \) ! -name '*.sample' -delete
  fi
  mkdir -p "$repo_root/.git/info"
  rm -f "$repo_root/.git/info/exclude" "$repo_root/.git/info/attributes" "$repo_root/.git/info/grafts" "$repo_root/.git/info/sparse-checkout"

  git -C "$repo_root" config --unset-all core.attributesFile >/dev/null 2>&1 || true
  git -C "$repo_root" config --unset-all core.fsmonitor >/dev/null 2>&1 || true
  git -C "$repo_root" config --unset-all core.sshCommand >/dev/null 2>&1 || true
  git -C "$repo_root" config --unset-all core.hooksPath >/dev/null 2>&1 || true
  (
    cd "$repo_root" || exit 1
    scrub_git_config_entries include
    scrub_git_config_entries includeif
    scrub_git_config_entries credential
    scrub_git_config_entries alias
    scrub_git_config_entries filter
    scrub_git_config_entries merge
  )

  git -C "$repo_root" config user.name "github-actions[bot]"
  git -C "$repo_root" config user.email "github-actions[bot]@users.noreply.github.com"
  git -C "$repo_root" config core.hooksPath /dev/null
  git -C "$repo_root" config core.fsmonitor false

  # Never leave a credential-embedded remote URL persisted in .git/config
  if [ -n "$safe_origin_url" ] && git -C "$repo_root" remote get-url origin >/dev/null 2>&1; then
    git -C "$repo_root" remote set-url origin "$safe_origin_url"
  fi
}

write_clone_output() {
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    printf 'cloned=%s\n' "$1" >> "$GITHUB_OUTPUT"
  fi
}

# Validate required environment variables
if [ -z "$GH_TOKEN" ]; then
  echo "ERROR: GH_TOKEN environment variable is required"
  exit 1
fi

if [ -z "$BRANCH_NAME" ]; then
  echo "ERROR: BRANCH_NAME environment variable is required"
  exit 1
fi

if [ -z "$TARGET_REPO" ]; then
  echo "ERROR: TARGET_REPO environment variable is required"
  exit 1
fi

if [ -z "$MEMORY_DIR" ]; then
  echo "ERROR: MEMORY_DIR environment variable is required"
  exit 1
fi

if [ -z "$CREATE_ORPHAN" ]; then
  echo "ERROR: CREATE_ORPHAN environment variable is required"
  exit 1
fi

# Default to github.com if not set
if [ -z "$GITHUB_SERVER_URL" ]; then
  GITHUB_SERVER_URL="https://github.com"
fi

# Extract host from server URL (remove https:// or http:// prefix)
SERVER_HOST="${GITHUB_SERVER_URL#https://}"
SERVER_HOST="${SERVER_HOST#http://}"
SAFE_ORIGIN_URL="https://${SERVER_HOST}/${TARGET_REPO}.git"

# Authenticate via a transient HTTP extra header instead of embedding the
# token in the remote URL, so the credential is never written to .git/config.
# The header is passed via GIT_CONFIG_* environment variables (rather than
# `git -c ...`) so it never appears in the process argument list.
AUTH_HEADER="Authorization: Basic $(printf 'x-access-token:%s' "$GH_TOKEN" | base64 | tr -d '\n')"
export GIT_CONFIG_COUNT=1
export GIT_CONFIG_KEY_0="http.extraheader"
export GIT_CONFIG_VALUE_0="$AUTH_HEADER"

write_clone_output false

# Verify the branch before cloning so only a confirmed missing branch can
# fall back to an orphan. Authentication and transport failures must fail closed.
if git ls-remote --exit-code --heads "$SAFE_ORIGIN_URL" "refs/heads/$BRANCH_NAME" >/dev/null 2>&1; then
  if ! git clone --depth 1 --single-branch --branch "$BRANCH_NAME" "$SAFE_ORIGIN_URL" "$MEMORY_DIR" 2>/dev/null; then
    echo "ERROR: failed to clone repo-memory branch after it was confirmed to exist" >&2
    exit 1
  fi
  CLONE_SUCCEEDED=true
else
  LS_REMOTE_EXIT_CODE=$?
  if [ "$LS_REMOTE_EXIT_CODE" -ne 2 ]; then
    echo "ERROR: failed to verify repo-memory branch; refusing to create an orphan branch" >&2
    exit 1
  fi
  CLONE_SUCCEEDED=false
fi
unset GIT_CONFIG_COUNT GIT_CONFIG_KEY_0 GIT_CONFIG_VALUE_0

if [ "$CLONE_SUCCEEDED" = "false" ]; then
  # The remote confirmed that the branch does not exist.
  if [ "$CREATE_ORPHAN" = "true" ]; then
    echo "Branch $BRANCH_NAME does not exist, creating orphan branch"
    mkdir -p "$MEMORY_DIR"
    cd "$MEMORY_DIR"
    if has_symlinked_git_metadata "$MEMORY_DIR"; then
      echo "WARNING: Detected symlinked repo-memory git metadata; reinitializing git metadata"
      rm -rf "$MEMORY_DIR/.git"
    fi
    git init
    git checkout --orphan "$BRANCH_NAME"
    git remote remove origin >/dev/null 2>&1 || true
    git remote add origin "$SAFE_ORIGIN_URL"
    harden_repo_memory_git_state "$MEMORY_DIR" "$SAFE_ORIGIN_URL"
  else
    echo "Branch $BRANCH_NAME does not exist and create-orphan is false, skipping"
    mkdir -p "$MEMORY_DIR"
  fi
else
  # Clone succeeded
  echo "Successfully cloned $BRANCH_NAME branch"
  cd "$MEMORY_DIR"
  if has_symlinked_git_metadata "$MEMORY_DIR"; then
    echo "WARNING: Detected symlinked repo-memory git metadata; recloning branch"
    cd ..
    rm -rf "$MEMORY_DIR"
    if ! GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0="http.extraheader" GIT_CONFIG_VALUE_0="$AUTH_HEADER" \
      git clone --depth 1 --single-branch --branch "$BRANCH_NAME" "$SAFE_ORIGIN_URL" "$MEMORY_DIR" 2>/dev/null; then
      echo "ERROR: failed to re-clone repo-memory branch after symlink metadata detection" >&2
      exit 1
    fi
    cd "$MEMORY_DIR"
  fi
  git remote remove origin >/dev/null 2>&1 || true
  git remote add origin "$SAFE_ORIGIN_URL"
  harden_repo_memory_git_state "$MEMORY_DIR" "$SAFE_ORIGIN_URL"
  write_clone_output true
fi
unset AUTH_HEADER

# Ensure memory directory exists
mkdir -p "$MEMORY_DIR"
echo "Repo memory directory ready at $MEMORY_DIR"
