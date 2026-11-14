#!/usr/bin/env bash
set +o histexpand

PATCH_DIR="${GH_AW_TMP_DIR:-/tmp/gh-aw}"
path_has_symlink_component() {
  local path="$1"
  case "$path" in
    /*) ;;
    *) path="$PWD/$path" ;;
  esac
  while [ "$path" != "/" ] && [[ "$path" == */ ]]; do
    path="${path%/}"
  done
  while [ "$path" != "/" ]; do
    if [ -L "$path" ]; then return 0; fi
    path="$(dirname "$path")"
  done
  return 1
}

if path_has_symlink_component "$PATCH_DIR"; then
  echo "::error::Refusing to use a symlinked patch directory" >&2
  exit 1
fi
mkdir -p "$PATCH_DIR"
if path_has_symlink_component "$PATCH_DIR" || [ ! -d "$PATCH_DIR" ]; then
  echo "::error::Patch path is not a real directory" >&2
  exit 1
fi
if [ -n "${GITHUB_STEP_SUMMARY:-}" ] && path_has_symlink_component "$GITHUB_STEP_SUMMARY"; then
  echo "::error::Refusing to append to a symlinked step summary" >&2
  exit 1
fi

write_patch_file() {
  local target="$1"
  local error_message="$2"
  local revision_range="$3"
  local staged_patch

  if [ -L "$target" ]; then
    echo "::error::Refusing to replace a symlinked patch file: $target" >&2
    return 1
  fi
  if ! staged_patch="$(mktemp "${PATCH_DIR}/.aw-patch.XXXXXX")"; then
    echo "::error::Could not create a private patch staging file" >&2
    return 1
  fi
  if ! git format-patch "$revision_range" --stdout > "$staged_patch"; then
    printf '%s\n' "$error_message" > "$staged_patch"
  fi
  if [ -L "$target" ]; then
    rm -f -- "$staged_patch"
    echo "::error::Refusing to replace a symlinked patch file: $target" >&2
    return 1
  fi
  mv -f -- "$staged_patch" "$target"
}

# Diagnostic logging: Show environment information
echo "=== Diagnostic: Environment Information ==="
echo "GITHUB_SHA: ${GITHUB_SHA@Q}"
echo "DEFAULT_BRANCH: ${DEFAULT_BRANCH@Q}"
echo "Current HEAD: $(git rev-parse HEAD 2>/dev/null || echo 'unknown')"
echo "Current branch: $(git branch --show-current 2>/dev/null || echo 'detached HEAD')"

# Diagnostic logging: Show recent commits before patch generation
echo ""
echo "=== Diagnostic: Recent commits (last 10) ==="
git log --oneline -10 || echo "Failed to show git log"

# Check current git status
echo ""
echo "=== Diagnostic: Current git status ==="
git status

# Sanitize a branch name for use as a patch filename
# Matches the JavaScript sanitizeBranchNameForPatch function
sanitize_branch_name_for_patch() {
  echo "$1" | tr '[:upper:]' '[:lower:]' | sed 's|[/\\:*?"<>|]|-|g' | sed 's/-\{2,\}/-/g' | sed 's/^-//' | sed 's/-$//'
}

# Get the patch file path for a branch name
get_patch_path() {
  local sanitized
  sanitized="$(sanitize_branch_name_for_patch "$1")"
  if [ -z "$sanitized" ]; then
    sanitized="unknown"
  fi
  echo "${PATCH_DIR}/aw-${sanitized}.patch"
}

# Extract all branch names from JSONL output (for all create_pull_request and push_to_pull_request_branch entries)
BRANCH_NAMES=()
if [ -f "$GH_AW_SAFE_OUTPUTS" ] && ! path_has_symlink_component "$GH_AW_SAFE_OUTPUTS"; then
  echo ""
  echo "Checking for branch names in JSONL output..."
  echo "JSONL file path: $GH_AW_SAFE_OUTPUTS"
  while IFS= read -r line; do
    if [ -n "$line" ]; then
      # Extract branch from create-pull-request or push_to_pull_request_branch lines
      # Note: types use underscores (normalized by safe-outputs MCP server)
      if echo "$line" | grep -qE '"type"[[:space:]]*:[[:space:]]*"(create_pull_request|push_to_pull_request_branch)"'; then
        BRANCH_NAME="$(echo "$line" | sed -n 's/.*"branch"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
        if [ -n "$BRANCH_NAME" ]; then
          echo "Found branch name: ${BRANCH_NAME@Q}"
          BRANCH_NAMES+=("$BRANCH_NAME")
        fi
      fi
    fi
  done < "$GH_AW_SAFE_OUTPUTS"
else
  echo ""
  echo "GH_AW_SAFE_OUTPUTS file not found at: ${GH_AW_SAFE_OUTPUTS@Q}"
fi

# If no branches found in JSONL, log it but don't give up yet
if [ "${#BRANCH_NAMES[@]}" -eq 0 ]; then
  echo ""
  echo "No branch names found in JSONL output"
  echo "Will check for commits made to current HEAD instead"
fi

# Strategy 1: If we have branch names, generate a patch for each one
PATCH_GENERATED=false
declare -A PROCESSED_BRANCHES
if [ "${#BRANCH_NAMES[@]}" -gt 0 ]; then
  echo ""
  echo "=== Strategy 1: Using named branches from JSONL ==="
  echo "Found ${#BRANCH_NAMES[@]} branch name(s): ${BRANCH_NAMES[*]}"

  for BRANCH_NAME in "${BRANCH_NAMES[@]}"; do
    # Skip duplicate branches (already processed)
    if [ "${PROCESSED_BRANCHES[$BRANCH_NAME]+_}" ]; then
      echo "Skipping duplicate branch: ${BRANCH_NAME@Q}"
      continue
    fi
    PROCESSED_BRANCHES[$BRANCH_NAME]=1

    PATCH_PATH="$(get_patch_path "$BRANCH_NAME")"
    echo ""
    echo "Looking for branch: ${BRANCH_NAME@Q}"
    echo "Patch path: ${PATCH_PATH@Q}"

    # Check if the branch exists
    if git show-ref --verify --quiet "refs/heads/$BRANCH_NAME"; then
      echo "Branch ${BRANCH_NAME@Q} exists, generating patch from branch changes"

      # Check if origin/$BRANCH_NAME exists to use as base
      if git show-ref --verify --quiet "refs/remotes/origin/$BRANCH_NAME"; then
        echo "Using origin/${BRANCH_NAME@Q} as base for patch generation"
        BASE_REF="origin/$BRANCH_NAME"
      else
        echo "origin/${BRANCH_NAME@Q} does not exist, using merge-base with default branch"
        echo "Default branch: ${DEFAULT_BRANCH@Q}"
        git fetch origin "$DEFAULT_BRANCH"
        BASE_REF="$(git merge-base "origin/$DEFAULT_BRANCH" "$BRANCH_NAME")"
        echo "Using merge-base as base: ${BASE_REF@Q}"
      fi

      # Diagnostic logging: Show diff stats before generating patch
      echo ""
      echo "=== Diagnostic: Diff stats for patch generation ==="
      echo "Command: git diff --stat ${BASE_REF@Q}..${BRANCH_NAME@Q}"
      git diff --stat "$BASE_REF".."$BRANCH_NAME" || echo "Failed to show diff stats"

      # Diagnostic logging: Count commits to be included
      echo ""
      echo "=== Diagnostic: Commits to be included in patch ==="
      COMMIT_COUNT="$(git rev-list --count "$BASE_REF".."$BRANCH_NAME" 2>/dev/null || echo "0")"
      echo "Number of commits: $COMMIT_COUNT"
      if [ "$COMMIT_COUNT" -gt 0 ]; then
        echo "Commit SHAs:"
        git log --oneline "$BASE_REF".."$BRANCH_NAME" || echo "Failed to list commits"
      fi

      # Diagnostic logging: Show the exact command being used
      echo ""
      echo "=== Diagnostic: Generating patch ==="
      echo "Command: git format-patch ${BASE_REF@Q}..${BRANCH_NAME@Q} --stdout > ${PATCH_PATH@Q}"

      # Generate patch from the determined base to the branch
      write_patch_file "$PATCH_PATH" "Failed to generate patch from branch" "$BASE_REF..$BRANCH_NAME" || exit 1
      echo "Patch file created from branch: ${BRANCH_NAME@Q} (base: ${BASE_REF@Q})"
      PATCH_GENERATED=true
    else
      echo "Branch ${BRANCH_NAME@Q} does not exist locally"
    fi
  done
fi

# Strategy 2: Check if commits were made to current HEAD since checkout
if [ "$PATCH_GENERATED" = false ]; then
  echo ""
  echo "=== Strategy 2: Checking for commits on current HEAD ==="

  # Get current HEAD SHA
  CURRENT_HEAD="$(git rev-parse HEAD 2>/dev/null || echo '')"
  echo "Current HEAD: ${CURRENT_HEAD@Q}"
  echo "Checkout SHA (GITHUB_SHA): ${GITHUB_SHA@Q}"

  if [ -z "$CURRENT_HEAD" ]; then
    echo "ERROR: Could not determine current HEAD SHA"
  elif [ -z "$GITHUB_SHA" ]; then
    echo "ERROR: GITHUB_SHA environment variable is not set"
  elif [ "$CURRENT_HEAD" = "$GITHUB_SHA" ]; then
    echo "No commits have been made since checkout (HEAD == GITHUB_SHA)"
    echo "No patch will be generated"
  else
    echo "HEAD has moved since checkout - checking if commits were added"

    # Check if GITHUB_SHA is an ancestor of current HEAD
    if git merge-base --is-ancestor "$GITHUB_SHA" HEAD 2>/dev/null; then
      echo "GITHUB_SHA is an ancestor of HEAD - commits were added"

      # Count commits between GITHUB_SHA and HEAD
      COMMIT_COUNT="$(git rev-list --count "${GITHUB_SHA}..HEAD" 2>/dev/null || echo "0")"
      echo ""
      echo "=== Diagnostic: Commits added since checkout ==="
      echo "Number of commits: $COMMIT_COUNT"

      if [ "$COMMIT_COUNT" -gt 0 ]; then
        echo "Commit SHAs:"
        git log --oneline "${GITHUB_SHA}..HEAD" || echo "Failed to list commits"

        # Show diff stats
        echo ""
        echo "=== Diagnostic: Diff stats for patch generation ==="
        echo "Command: git diff --stat ${GITHUB_SHA@Q}..HEAD"
        git diff --stat "${GITHUB_SHA}..HEAD" || echo "Failed to show diff stats"

        # Detect current branch for patch filename
        CURRENT_BRANCH="$(git branch --show-current 2>/dev/null || echo '')"
        if [ -z "$CURRENT_BRANCH" ]; then
          CURRENT_BRANCH="head"
        fi
        PATCH_PATH="$(get_patch_path "$CURRENT_BRANCH")"

        # Generate patch from GITHUB_SHA to HEAD
        echo ""
        echo "=== Diagnostic: Generating patch ==="
        echo "Command: git format-patch ${GITHUB_SHA@Q}..HEAD --stdout > ${PATCH_PATH@Q}"
        write_patch_file "$PATCH_PATH" "Failed to generate patch from HEAD" "${GITHUB_SHA}..HEAD" || exit 1
        echo "Patch file created from commits on HEAD (base: ${GITHUB_SHA@Q})"
        PATCH_GENERATED=true
      else
        echo "No commits found between GITHUB_SHA and HEAD"
      fi
    else
      echo "GITHUB_SHA is not an ancestor of HEAD - repository state has diverged"
      echo "This may indicate a rebase or other history rewriting operation"
      echo "Will not generate patch due to ambiguous history"
    fi
  fi
fi

# Final status
echo ""
if [ "$PATCH_GENERATED" = true ]; then
  echo "=== Patch generation completed successfully ==="
else
  echo "=== No patch generated ==="
  echo "Reason: No commits found via branch name or HEAD analysis"
fi

# Show patch info for all aw-*.patch files
for patch_file in "$PATCH_DIR"/aw-*.patch; do
  if [ -f "$patch_file" ] && [ ! -L "$patch_file" ]; then
    echo ""
    echo "=== Diagnostic: Patch file information: ${patch_file} ==="
    ls -lh "$patch_file"

    # Get patch file size in KB
    PATCH_SIZE="$(du -k "$patch_file" | cut -f1)"
    echo "Patch file size: ${PATCH_SIZE} KB"

    # Count lines in patch
    PATCH_LINES="$(wc -l < "$patch_file")"
    echo "Patch file lines: $PATCH_LINES"

    # Extract and count commits from patch file (each commit starts with "From <sha>")
    PATCH_COMMITS="$(grep -c "^From [0-9a-f]\{40\}" "$patch_file" 2>/dev/null || echo "0")"
    echo "Commits included in patch: $PATCH_COMMITS"

    # List commit SHAs in the patch
    if [ "$PATCH_COMMITS" -gt 0 ]; then
      echo "Commit SHAs in patch:"
      grep "^From [0-9a-f]\{40\}" "$patch_file" | sed 's/^From \([0-9a-f]\{40\}\).*/  \1/' || echo "Failed to extract commit SHAs"
    fi

    # Show the first 500 lines of the patch for review
    {
      echo "## Git Patch: $(basename "$patch_file")"
      echo ''
      echo '```diff'
      head -500 "$patch_file" || echo "Could not display patch contents"
      echo '...'
      echo '```'
      echo ''
    } >> "$GITHUB_STEP_SUMMARY"
  fi
done
