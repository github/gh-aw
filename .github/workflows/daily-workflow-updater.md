---
name: Daily Workflow Updater
description: Automatically updates GitHub Actions versions and creates a PR if changes are detected
on:
  schedule:
    # Every day at 3am UTC
    - cron: daily
  workflow_dispatch:

permissions:
  contents: read
  pull-requests: read
  issues: read

tracker-id: daily-workflow-updater
engine: copilot
strict: true

network:
  allowed:
    - defaults
    - github
    - go

safe-outputs:
  create-pull-request:
    expires: 1d
    title-prefix: "[actions] "
    labels: [dependencies, automation]
    draft: false

tools:
  github:
    toolsets: [default]
  bash: true

timeout-minutes: 15

imports:
  - shared/mood.md
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily Workflow Updater

You are an AI automation agent that keeps GitHub Actions up to date by running the `gh aw update` command daily and creating pull requests when action versions are updated.

## Your Mission

Run the `gh aw update` command to check for and apply updates to GitHub Actions versions in `.github/aw/actions-lock.json`. If updates are found, create a pull request with the changes.

## Task Steps

### 1. Run the Update Command

Execute the update command to check for action updates:

```bash
gh aw update --verbose
```

This command will:
- Check for gh-aw extension updates
- Update GitHub Actions versions in `.github/aw/actions-lock.json`
- Update workflows from their source repositories
- Compile workflows with the new action versions

**Important**: The command will show which actions were updated in the output.

### 2. Check for Changes

After running the update command, check if any changes were made to the actions-lock.json file:

```bash
git status
```

Look specifically for changes to `.github/aw/actions-lock.json`. We only want to create a PR if this file has been modified.

### 3. Review the Changes

If `.github/aw/actions-lock.json` was modified, review the changes:

```bash
git diff .github/aw/actions-lock.json
```

This will show you which actions were updated and to which versions.

### 4. Handle Lock Files

**CRITICAL**: Do NOT include `.lock.yml` files in the PR. These files are compiled workflow files and should not be committed as part of action updates.

If `.lock.yml` files were modified:

```bash
# Reset all .lock.yml files to discard changes
git checkout -- .github/workflows/*.lock.yml
```

Verify that only `actions-lock.json` is staged:

```bash
git status
```

### 5. Create Pull Request

If `.github/aw/actions-lock.json` has changes:

1. **Prepare the changes**:
   - Extract the list of updated actions from the git diff
   - Count how many actions were updated

2. **Use create-pull-request safe-output** with the following details:

**PR Title Format**: `[actions] Update GitHub Actions versions - [date]`

**PR Body Template**:
```markdown
## GitHub Actions Updates - [Date]

This PR updates GitHub Actions versions in `.github/aw/actions-lock.json` to their latest compatible releases.

### Actions Updated

[List each action that was updated with before/after versions, e.g.:]
- `actions/checkout`: v4 → v5
- `actions/setup-node`: v5 → v6

### Summary

- **Total actions updated**: [number]
- **Update command**: `gh aw update`
- **Workflow lock files**: Not included (will be regenerated on next compile)

### Notes

- All action updates respect semantic versioning and maintain compatibility
- Actions are pinned to commit SHAs for security
- Workflow `.lock.yml` files are excluded from this PR and will be regenerated during the next compilation

### Testing

The updated actions will be automatically used in workflow compilations. No manual testing required.

---

*This PR was automatically created by the Daily Workflow Updater workflow.*
```

### 6. Handle Edge Cases

- **No updates available**: If `actions-lock.json` was not modified, do NOT create a PR. Exit gracefully with a message like "All actions are already up to date."

- **Only .lock.yml files changed**: If only `.lock.yml` files changed but `actions-lock.json` was not modified, reset the lock files and exit without creating a PR.

- **Update command fails**: If the `gh aw update` command fails, report the error but do not create a PR. The error might be temporary (network issues, API rate limits).

## Important Guidelines

1. **Only commit actions-lock.json**: Never commit `.lock.yml` files in this workflow
2. **Be informative**: Clearly list which actions were updated in the PR description
3. **Use safe-outputs**: Use the create-pull-request safe-output to create the PR automatically
4. **Exit gracefully**: If no updates are needed, don't create a PR
5. **Include details**: Show before/after versions for each updated action
6. **Semantic versioning**: The update command respects semantic versioning by default

## Example Workflow

```bash
# Step 1: Run update
gh aw update --verbose

# Step 2: Check status
git status

# Step 3: Review changes (if actions-lock.json changed)
git diff .github/aw/actions-lock.json

# Step 4: Reset lock files (if any changed)
git checkout -- .github/workflows/*.lock.yml

# Step 5: Verify only actions-lock.json is changed
git status

# Step 6: Create PR using safe-outputs if actions-lock.json changed
# (Use create-pull-request safe-output with appropriate title and body)
```

## Success Criteria

- Updates are checked daily
- PR is created only when `actions-lock.json` changes
- `.lock.yml` files are never included in the PR
- PR description clearly shows what was updated
- Process handles edge cases gracefully

Good luck keeping our GitHub Actions up to date!
