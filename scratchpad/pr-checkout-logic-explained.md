# PR Checkout Logic: Understanding pull_request vs pull_request_target

## Overview

The `checkout_pr_branch.cjs` script handles checking out PR branches for different GitHub event types. The logic differs significantly between `pull_request` and `pull_request_target` events due to their fundamentally different execution contexts.

## Critical Differences

### `pull_request` Event

**Execution Context**: Runs in the **merge commit** context
- The workflow operates on a temporary merge commit created by GitHub
- This merge commit is the result of merging the PR head with the base branch
- The PR branch code is already available in the checked-out repository

**Security Model**: Limited permissions (safe for fork PRs)
- Workflows run with restricted permissions
- Cannot access repository secrets
- Safe to run untrusted code from fork contributors

**Checkout Strategy**: Direct git commands
```bash
git fetch origin <branch-name>
git checkout <branch-name>
```

**Why it works**: The branch exists in the current repository context because GitHub has already set up the merge commit environment.

**Use case**: CI/CD tasks that need to test the merged code (unit tests, linting, builds)

### `pull_request_target` Event

**Execution Context**: Runs in the **base repository** context
- The workflow operates on the base branch (e.g., `main`) of the target repository
- The PR head code is NOT checked out by default
- The workflow has access to the base repository's committed files

**Security Model**: Full write permissions (dangerous with untrusted code)
- Workflows run with full repository permissions
- Can access repository secrets
- **CRITICAL**: Should NOT execute untrusted code from PR without careful validation

**Checkout Strategy**: Must use `gh pr checkout`
```bash
gh pr checkout <pr-number>
```

**Why direct git fails**: For fork PRs, the head branch does NOT exist in the base repository's `origin` remote. Attempting `git fetch origin <fork-branch>` will fail with "remote ref does not exist" because the branch only exists in the fork's repository.

**Why gh pr checkout works**: The GitHub CLI fetches the PR branch from the correct remote (fork or base) using the PR number as a reference. It handles the complexity of determining the correct remote and ref.

**Use case**: Workflows that need write permissions (commenting, labeling, deploying) triggered by PR events but shouldn't execute PR code directly for security.

## Fork PR Detection

The script uses a robust multi-signal approach to detect fork PRs:

### Detection Logic

```javascript
let isFork = false;
let forkReason = "same repository";

if (!pullRequest.head?.repo) {
  // Head repo is null - likely a deleted fork
  isFork = true;
  forkReason = "head repository deleted (was likely a fork)";
} else if (pullRequest.head.repo.fork === true) {
  // GitHub's explicit fork flag
  isFork = true;
  forkReason = "head.repo.fork flag is true";
} else if (pullRequest.head.repo.full_name !== pullRequest.base?.repo?.full_name) {
  // Different repository names
  isFork = true;
  forkReason = "different repository names";
}
```

### Why Multiple Signals?

**1. Deleted Fork Detection** (`!pullRequest.head?.repo`):
- When a fork repository is deleted, `pullRequest.head.repo` becomes `null`
- This is a critical edge case that would cause the workflow to crash without null checks
- The script correctly identifies this as a fork scenario

**2. GitHub Fork Flag** (`pullRequest.head.repo.fork === true`):
- GitHub provides an explicit `fork` boolean property on the repository object
- This is the most reliable signal when the repository still exists
- More authoritative than comparing names alone

**3. Repository Name Comparison** (`full_name` differs):
- Fallback for cases where the fork flag might not be set
- Catches cross-organization PRs that aren't technically forks but behave similarly
- Compares `owner/repo` format strings

### Edge Cases Handled

**Deleted Fork**:
```javascript
// pullRequest.head.repo is null
isFork = true;
forkReason = "head repository deleted (was likely a fork)";
```
✅ Detected as fork → Uses `gh pr checkout` (will fail if repo truly deleted)

**Forked Repository** (same name, different ownership):
```javascript
// pullRequest.head.repo.fork is true
// OR pullRequest.head.repo.full_name = "fork-owner/repo"
// vs pullRequest.base.repo.full_name = "original-owner/repo"
isFork = true;
```
✅ Detected as fork → Uses `gh pr checkout`

**Same-Repo PR** (branch in base repository):
```javascript
// pullRequest.head.repo.fork is false
// AND pullRequest.head.repo.full_name = pullRequest.base.repo.full_name
isFork = false;
```
✅ Not a fork → Still uses `gh pr checkout` for non-`pull_request` events

## Decision Logic

```
Is event === "pull_request"?
├─ YES → Use git fetch + checkout
│         Reason: Running in merge commit context, branch available
│
└─ NO → Use gh pr checkout
          Reason: Running in base repo context
          ├─ For fork PRs: Head branch doesn't exist in origin
          ├─ For deleted forks: May fail but handles gracefully
          └─ For same-repo PRs: Still safer to use gh pr checkout for consistency
```

## Example Scenarios

### Scenario 1: Same-Repo PR with pull_request Event

```yaml
on:
  pull_request:
    branches: [main]

# Context:
# - Running in merge commit (PR head merged with base)
# - github.sha: merge commit SHA
# - Branch: temporary refs/pull/123/merge
```

**Checkout Logic**: 
```javascript
git fetch origin feature-branch
git checkout feature-branch
```

**Result**: ✅ Success - branch exists in origin

### Scenario 2: Fork PR with pull_request Event

```yaml
on:
  pull_request:
    branches: [main]

# Context:
# - Running in merge commit (fork PR head merged with base)
# - github.sha: merge commit SHA
# - Branch: temporary refs/pull/123/merge
```

**Checkout Logic**:
```javascript
git fetch origin fork-feature-branch  // ❌ Branch doesn't exist in origin
git checkout fork-feature-branch
```

**Result**: ⚠️ Would fail, but we're already in merge commit context, so checkout is unnecessary

### Scenario 3: Same-Repo PR with pull_request_target Event

```yaml
on:
  pull_request_target:
    types: [labeled]

# Context:
# - Running in BASE repository context
# - github.sha: base branch SHA (e.g., main)
# - Branch: base branch (e.g., main)
```

**Checkout Logic**:
```javascript
gh pr checkout 123
```

**Result**: ✅ Success - gh CLI fetches from origin and checks out PR branch

### Scenario 4: Fork PR with pull_request_target Event (CRITICAL)

```yaml
on:
  pull_request_target:
    types: [labeled]

# Context:
# - Running in BASE repository context
# - github.sha: base branch SHA (e.g., main)
# - PR head is in fork: fork-owner/repo
```

**Checkout Logic (OLD - BROKEN)**:
```javascript
git fetch origin fork-feature-branch  // ❌ FAILS - branch doesn't exist in origin
git checkout fork-feature-branch      // ❌ Never executes
```

**Checkout Logic (NEW - CORRECT)**:
```javascript
gh pr checkout 123  // ✅ SUCCESS - gh CLI fetches from fork-owner/repo
```

**Result**: ✅ Success - gh CLI knows to fetch from the fork remote

## Logging Enhancements

The enhanced script now logs:

### 1. PR Context Details
```
📋 PR Context Details
  Event type: pull_request_target
  PR number: 123
  PR state: open
  Head ref: feature-branch
  Head SHA: abc123
  Head repo: fork-owner/repo
  Base ref: main
  Base SHA: def456
  Base repo: owner/repo
  Is fork PR: true (head.repo.fork flag is true)
  Current repository: owner/repo
```

**Fork Detection Reasons**:
- `(same repository)` - Not a fork, same base and head repo
- `(head.repo.fork flag is true)` - GitHub's explicit fork flag is set
- `(different repository names)` - Head and base have different full names
- `(head repository deleted (was likely a fork))` - Head repo is null, likely deleted fork

### 2. Checkout Strategy
```
🔄 Checkout Strategy
  Event type: pull_request_target
  Strategy: gh pr checkout
  Reason: pull_request_target runs in base repo context; for fork PRs, head branch doesn't exist in origin
```

### 3. Fork Detection Warning
```
⚠️ Fork PR detected - gh pr checkout will fetch from fork repository
```

### 4. Error Details (on failure)
```
❌ Checkout Error Details
  Event type: pull_request_target
  PR number: 123
  Error message: branch not found
  Attempted to check out: feature-branch
  
  Current git status: ...
  Available remotes: ...
  Current branch: main
```

## Common Issues and Debugging

### Issue: "pathspec 'branch-name' did not match any file(s) known to git"

**Cause**: Trying to use `git checkout <branch>` for a fork PR in `pull_request_target` context

**Solution**: The script now correctly uses `gh pr checkout` for all non-`pull_request` events

**Debug**: Check the "Is fork PR" log to confirm fork detection

### Issue: "remote ref does not exist"

**Cause**: Trying to `git fetch origin <fork-branch>` when the branch only exists in the fork

**Solution**: Use `gh pr checkout <pr-number>` instead

**Debug**: Check the "Head repo" vs "Base repo" logs to confirm repository context

### Issue: "cannot access private repository"

**Cause**: Missing or invalid GitHub token for accessing fork repository

**Solution**: Ensure `GH_TOKEN` environment variable is set with appropriate permissions

**Debug**: Check token permissions with `gh auth status`

## Security Considerations

### pull_request Event (SAFE)
- ✅ No secrets exposed
- ✅ Limited permissions
- ✅ Can safely execute PR code
- ✅ Suitable for CI/CD pipelines testing changes

### pull_request_target Event (DANGEROUS)
- ⚠️ Full repository access
- ⚠️ Access to secrets
- ⚠️ Write permissions
- ❌ Should NEVER execute untrusted code from PR
- ✅ Suitable for workflows that need write access (commenting, labeling)
- ⚠️ If checking out PR code, must carefully validate before execution

### Best Practice for pull_request_target

```yaml
on:
  pull_request_target:
    types: [labeled]

jobs:
  safe-operation:
    runs-on: ubuntu-latest
    steps:
      # This is SAFE - we're in base repo context
      - uses: actions/checkout@v4
      
      # This adds a comment using base repo code (trusted)
      - name: Add comment
        run: gh pr comment ${{ github.event.pull_request.number }} --body "Labeled!"
      
      # ❌ DANGEROUS - Never do this without validation
      # - uses: actions/checkout@v4
      #   with:
      #     ref: ${{ github.event.pull_request.head.sha }}
      # - run: npm install  # Could execute malicious code from PR
```

## Testing

The test suite now covers:

1. **pull_request events**: Direct git fetch + checkout
2. **pull_request_target events**: gh pr checkout with fork detection
3. **Fork PR detection**: Logging and warnings
4. **Context logging**: Detailed PR information
5. **Strategy logging**: Why each strategy is chosen
6. **Error logging**: Enhanced diagnostics on failure
7. **Missing repo handling**: Graceful handling of deleted forks

## References

- [GitHub Actions: Events that trigger workflows](https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#pull_request)
- [GitHub Actions: pull_request_target](https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#pull_request_target)
- [Security hardening for GitHub Actions](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions)
- [Keeping your GitHub Actions and workflows secure: Preventing pwn requests](https://securitylab.github.com/research/github-actions-preventing-pwn-requests/)
