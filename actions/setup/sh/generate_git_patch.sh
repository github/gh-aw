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

# Extract branch name from JSONL output
BRANCH_NAME=""
if [ -f "$GH_AW_SAFE_OUTPUTS" ]; then
  echo ""
  echo "Checking for branch name in JSONL output..."
  echo "JSONL file path: $GH_AW_SAFE_OUTPUTS"
  while IFS= read -r line; do
    if [ -n "$line" ]; then
      # Extract branch from create-pull-request line using simple grep and sed
      # Note: types use underscores (normalized by safe-outputs MCP server)
      if echo "$line" | grep -q '"type"[[:space:]]*:[[:space:]]*"create_pull_request"'; then
        echo "Found create_pull_request line: $line"
        # Extract branch value using sed
        BRANCH_NAME="$(echo "$line" | sed -n 's/.*"branch"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
        if [ -n "$BRANCH_NAME" ]; then
          echo "Extracted branch name from create_pull_request: ${BRANCH_NAME@Q}"
          break
        fi
      # Extract branch from push_to_pull_request_branch line using simple grep and sed
      # Note: types use underscores (normalized by safe-outputs MCP server)
      elif echo "$line" | grep -q '"type"[[:space:]]*:[[:space:]]*"push_to_pull_request_branch"'; then
        echo "Found push_to_pull_request_branch line: $line"
        # Extract branch value using sed
        BRANCH_NAME="$(echo "$line" | sed -n 's/.*"branch"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
        if [ -n "$BRANCH_NAME" ]; then
          echo "Extracted branch name from push_to_pull_request_branch: ${BRANCH_NAME@Q}"
          break
        fi
      fi
    fi
  done < "$GH_AW_SAFE_OUTPUTS"
else
  echo ""
  echo "GH_AW_SAFE_OUTPUTS file not found at: ${GH_AW_SAFE_OUTPUTS@Q}"
fi

# If no branch found in JSONL, log it but don't give up yet
if [ -z "$BRANCH_NAME" ]; then
  echo ""
  echo "No branch name found in JSONL output"
  echo "Will check for commits made to current HEAD instead"
fi

# Strategy 1: If we have a branch name, check if that branch exists and get its diff
PATCH_GENERATED=false
if [ -n "$BRANCH_NAME" ]; then
  echo ""
  echo "=== Strategy 1: Using named branch from JSONL ==="
  echo "Looking for branch: ${BRANCH_NAME@Q}"
  # Check if the branch exists
  if git show-ref --verify --quiet "refs/heads/$BRANCH_NAME"; then
    echo "Branch ${BRANCH_NAME@Q} exists, generating patch from branch changes"
    
    # Check if origin/$BRANCH_NAME exists to use as base
    if git show-ref --verify --quiet "refs/remotes/origin/$BRANCH_NAME"; then
      echo "Using origin/${BRANCH_NAME@Q} as base for patch generation"
      BASE_REF="origin/$BRANCH_NAME"
    else
      echo "origin/${BRANCH_NAME@Q} does not exist, using merge-base with default branch"
      # Use the default branch name from environment variable
      echo "Default branch: ${DEFAULT_BRANCH@Q}"
      # Fetch the default branch to ensure it's available locally
      git fetch origin "$DEFAULT_BRANCH"
      # Find merge base between default branch and current branch
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
    echo "Command: git format-patch ${BASE_REF@Q}..${BRANCH_NAME@Q} --stdout > /tmp/gh-aw/aw.patch"
    
    # Generate patch from the determined base to the branch
    git format-patch "$BASE_REF".."$BRANCH_NAME" --stdout > /tmp/gh-aw/aw.patch || echo "Failed to generate patch from branch" > /tmp/gh-aw/aw.patch
    echo "Patch file created from branch: ${BRANCH_NAME@Q} (base: ${BASE_REF@Q})"
    PATCH_GENERATED=true
  else
    echo "Branch ${BRANCH_NAME@Q} does not exist locally"
  fi
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
        
        # Generate patch from GITHUB_SHA to HEAD
        echo ""
        echo "=== Diagnostic: Generating patch ==="
        echo "Command: git format-patch ${GITHUB_SHA@Q}..HEAD --stdout > /tmp/gh-aw/aw.patch"
        git format-patch "${GITHUB_SHA}..HEAD" --stdout > /tmp/gh-aw/aw.patch || echo "Failed to generate patch from HEAD" > /tmp/gh-aw/aw.patch
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

# Show patch info if it exists
if [ -f /tmp/gh-aw/aw.patch ]; then
  echo ""
  echo "=== Diagnostic: Patch file information ==="
  ls -lh /tmp/gh-aw/aw.patch
  
  # Get patch file size in KB
  PATCH_SIZE="$(du -k /tmp/gh-aw/aw.patch | cut -f1)"
  echo "Patch file size: ${PATCH_SIZE} KB"
  
  # Count lines in patch
  PATCH_LINES="$(wc -l < /tmp/gh-aw/aw.patch)"
  echo "Patch file lines: $PATCH_LINES"
  
  # Extract and count commits from patch file (each commit starts with "From <sha>")
  PATCH_COMMITS="$(grep -c "^From [0-9a-f]\{40\}" /tmp/gh-aw/aw.patch 2>/dev/null || echo "0")"
  echo "Commits included in patch: $PATCH_COMMITS"
  
  # List commit SHAs in the patch
  if [ "$PATCH_COMMITS" -gt 0 ]; then
    echo "Commit SHAs in patch:"
    grep "^From [0-9a-f]\{40\}" /tmp/gh-aw/aw.patch | sed 's/^From \([0-9a-f]\{40\}\).*/  \1/' || echo "Failed to extract commit SHAs"
  fi
  
  # Show the first 50 lines of the patch for review
  {
    echo '## Git Patch'
    echo ''
    echo '```diff'
    head -500 /tmp/gh-aw/aw.patch || echo "Could not display patch contents"
    echo '...'
    echo '```'
    echo ''
  } >> "$GITHUB_STEP_SUMMARY"
fi
