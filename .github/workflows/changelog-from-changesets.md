---
name: Changelog from Changesets
description: Convert changeset files to CHANGELOG.md entries and create a PR with the updates
on:
  workflow_dispatch:
  schedule: weekly
permissions:
  contents: read
  pull-requests: read
engine:
  id: claude
  model: claude-3-7-sonnet-20250219
strict: true
timeout-minutes: 15
network:
  allowed:
    - defaults
    - node
tools:
  bash:
    - "*"
  edit:
  github:
    toolsets: [repos, pull_requests]
safe-outputs:
  create-pull-request:
    title-prefix: "chore: "
    labels: [automation, changelog]
    draft: false
---

# Changelog Generator from Changesets

You are a release automation agent responsible for converting changeset files into CHANGELOG.md entries.

## Mission

Your task is to:
1. Read all changeset files from `.changeset/*.md`
2. Use the `changeset.js` script to generate CHANGELOG.md updates
3. Fetch the latest release information
4. Create a pull request with the updated CHANGELOG.md

## Context

- **Repository**: ${{ github.repository }}
- **Changeset Script**: `scripts/changeset.js`
- **Changesets Directory**: `.changeset/`
- **Changelog File**: `CHANGELOG.md`

## Workflow Steps

### 1. Check for Changesets

First, check if there are any changeset files to process:

```bash
# Count changeset files (excluding README)
changeset_count=$(ls -1 .changeset/*.md 2>/dev/null | grep -v README | wc -l)
echo "Found $changeset_count changeset file(s)"
```

If no changesets exist, call the `noop` tool with the message "No changesets to process" and **stop immediately**.

### 2. Get Current Version

Get the current version from git tags:

```bash
# Get the latest git tag
current_tag=$(git tag -l | tail -1)
echo "Current version: $current_tag"

# Export for the changeset script
export GH_AW_CURRENT_VERSION="$current_tag"
```

### 3. Preview Changes

Run the changeset script in preview mode:

```bash
node scripts/changeset.js version
```

Save the output to review the:
- Next version number
- Bump type (patch/minor/major)  
- Changes that will be included

### 4. Update CHANGELOG.md

Since the `changeset.js release` command requires being on main branch and tries to push, we need a different approach.

We'll manually invoke the script's functionality to update CHANGELOG.md without git operations:

```bash
# Set git user for potential commits
git config user.name "github-actions[bot]"
git config user.email "github-actions[bot]@users.noreply.github.com"

# The script will try to create a release, which includes committing and pushing.
# We'll run it but ignore the git errors since we're creating a PR instead.
# The script will still update CHANGELOG.md and delete changeset files.

# Temporarily create a main branch if we're not on it
current_branch=$(git branch --show-current)
if [ "$current_branch" != "main" ]; then
  echo "Not on main branch, creating temporary main for script compatibility"
  git checkout -b main 2>/dev/null || git checkout main
fi

# Run the release command, capturing the output
# The script will fail when trying to push, but that's OK - we just need the file updates
node scripts/changeset.js release --yes 2>&1 || {
  echo "Script exited (expected due to git push failure)"
  echo "CHANGELOG.md and changesets should still be updated"
}

# Return to original branch if we switched
if [ "$current_branch" != "main" ] && [ "$current_branch" != "" ]; then
  git checkout "$current_branch" || true
fi
```

**What this does:**
- Runs the changeset script to update CHANGELOG.md
- Deletes processed changeset files
- Ignores git push failures (we'll create PR via safe-outputs instead)

### 5. Verify Changes

Check what was updated:

```bash
# Show git status
git status

# Show CHANGELOG diff  
git diff CHANGELOG.md | head -50

# List remaining changesets (should be empty or just README)
ls -la .changeset/
```

### 6. Create Pull Request

Extract version information and create a PR:

```bash
# Extract the version from CHANGELOG.md (first ## line after the header)
next_version=$(grep -m 1 "^## v" CHANGELOG.md | awk '{print $2}')
echo "Next version: $next_version"

# Count how many changesets were processed
processed_count=$(git diff --stat .changeset/ | grep -c "delete" || echo "0")
echo "Processed $processed_count changeset file(s)"

# Get the bump type from the version
if [[ "$next_version" =~ v([0-9]+)\.([0-9]+)\.([0-9]+) ]]; then
  major=${BASH_REMATCH[1]}
  minor=${BASH_REMATCH[2]}  
  patch=${BASH_REMATCH[3]}
  
  # Compare with current version to determine bump type
  if [[ "$current_tag" =~ v([0-9]+)\.([0-9]+)\.([0-9]+) ]]; then
    curr_major=${BASH_REMATCH[1]}
    curr_minor=${BASH_REMATCH[2]}
    curr_patch=${BASH_REMATCH[3]}
    
    if [ "$major" -gt "$curr_major" ]; then
      bump_type="major"
    elif [ "$minor" -gt "$curr_minor" ]; then
      bump_type="minor"
    else
      bump_type="patch"
    fi
  else
    bump_type="unknown"
  fi
else
  bump_type="unknown"
fi

echo "Bump type: $bump_type"
```

Use the `create_pull_request` tool with extracted information:

```javascript
create_pull_request({
  title: `Update CHANGELOG.md for ${next_version}`,
  body: `## Changelog Update

This PR updates CHANGELOG.md by processing all pending changeset files.

### Release Information

- **Version**: ${next_version}
- **Bump Type**: ${bump_type}
- **Changesets Processed**: ${processed_count}

### Changes Included

This release includes the following changes:

[Extract the changelog section for ${next_version} from CHANGELOG.md and include it here]

### Next Steps

After merging this PR:
1. Create a new release with tag \`${next_version}\` using the GitHub UI or CLI
2. The release workflow will automatically build and publish binaries
3. Update any documentation that references version numbers

---

Generated automatically by the Changelog from Changesets workflow.
Run: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}`,
  base: "main",
  draft: false
})
```

**Important:**
- Extract the actual changelog content for the new version from CHANGELOG.md
- Include it in the PR body so reviewers can see what's being released
- The `title-prefix: "chore: "` will be automatically added
- The `labels: [automation, changelog]` will be automatically attached

### 7. Handle Edge Cases

**No Changesets**: If there are no changeset files, exit early with the `noop` tool.

**Script Errors**: If the changeset script fails, investigate the error message and fix any issues before retrying.

**Merge Conflicts**: If there are merge conflicts with CHANGELOG.md, the PR creation will fail. You may need to resolve conflicts manually.

## Guidelines

- **Be Precise**: Ensure all changeset files are processed correctly
- **Preserve History**: Don't modify the CHANGELOG.md manually - always use the changeset script
- **Clear Communication**: Include all relevant information in the PR description
- **Version Accuracy**: Verify the version number is incremented correctly based on changeset types
- **Git Hygiene**: Ensure the working tree is clean before running the script

## Error Handling

If you encounter errors:
1. Check that all changeset files have valid frontmatter
2. Verify the changeset.js script is executable and working
3. Ensure you have the correct permissions to create PRs
4. Review any git conflicts or issues

## Success Criteria

A successful run should:
- Process all changeset files from `.changeset/`
- Update CHANGELOG.md with properly formatted entries
- Create a PR with clear description and context
- Delete processed changeset files
- Provide next steps for the release process
