# Debugging Action Pinning Version Comments

## Problem Description

When compiling workflows locally, the version comment in action pinning may flip between different versions that resolve to the same SHA. For example:

```yaml
uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd # v8
```

may sometimes change to:

```yaml
uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd # v8.0.0
```

This happens even though both `v8` and `v8.0.0` resolve to the same SHA (`ed597411...`).

## Root Cause

The issue occurs because:

1. **Multiple Version References**: Different workflows may reference the same action with different version tags (e.g., `v8` vs `v8.0.0`)
2. **Dynamic Resolution**: Each version tag is resolved independently via the GitHub API
3. **Cache Storage**: Each unique version tag creates a separate cache entry
4. **Version Comment Source**: The version comment in the compiled output comes from whichever version was requested, not a canonical version

### Example Flow

```
Workflow A: uses actions/github-script@v8
  → Resolves to SHA ed597411...
  → Cache stores: actions/github-script@v8 → ed597411...
  → Lock file: uses: actions/github-script@ed597411... # v8

Workflow B: uses actions/github-script@v8.0.0
  → Resolves to SHA ed597411... (same SHA!)
  → Cache stores: actions/github-script@v8.0.0 → ed597411...
  → Lock file: uses: actions/github-script@ed597411... # v8.0.0

Next compile of Workflow A:
  → Cache has both v8 and v8.0.0
  → Deduplication keeps v8.0.0 (more precise)
  → But if v8 is requested, it gets resolved again
  → Lock file may now show: # v8.0.0 instead of # v8
```

## Debugging Steps

### 1. Enable Debug Logging

Enable debug logging for action pinning components:

```bash
# Enable all action pinning logs
DEBUG=workflow:action_* gh aw compile

# Enable specific components
DEBUG=workflow:action_pins gh aw compile          # Action pin resolution
DEBUG=workflow:action_cache gh aw compile         # Cache operations
DEBUG=workflow:action_resolver gh aw compile      # GitHub API resolution

# Enable multiple components
DEBUG=workflow:action_pins,workflow:action_cache gh aw compile

# Save debug output to file
DEBUG=workflow:action_* gh aw compile 2> debug.log
```

### 2. Inspect the Action Cache

The action cache is stored at `.github/aw/actions-lock.json`. Examine it for duplicate entries:

```bash
# Pretty-print the cache
cat .github/aw/actions-lock.json | jq .

# Find entries for a specific action
cat .github/aw/actions-lock.json | jq '.entries | to_entries[] | select(.value.repo == "actions/github-script")'

# Check for duplicate SHAs
cat .github/aw/actions-lock.json | jq -r '.entries | to_entries[] | "\(.value.sha) \(.key)"' | sort | uniq -d -w 40
```

### 3. Check for Version Aliases

Some actions have multiple version tags pointing to the same commit:

```bash
# Use gh CLI to check what v8 and v8.0.0 point to
gh api /repos/actions/github-script/git/ref/tags/v8 --jq '.object.sha'
gh api /repos/actions/github-script/git/ref/tags/v8.0.0 --jq '.object.sha'
```

### 4. Review Workflow Files

Check your workflow files for inconsistent version references:

```bash
# Find all uses of an action across workflows
grep -r "uses: actions/github-script@" .github/workflows/

# Extract just the version tags
grep -r "uses: actions/github-script@" .github/workflows/ | sed 's/.*@//' | sort | uniq
```

### 5. Debug Log Analysis

When you run with `DEBUG=workflow:action_*`, look for these key messages:

#### Action Pin Resolution Flow

```
workflow:action_pins Resolving action pin: repo=actions/github-script, version=v8
workflow:action_pins Attempting dynamic resolution for actions/github-script@v8
workflow:action_resolver Cache hit for actions/github-script@v8: ed597411...
workflow:action_pins Dynamic resolution succeeded: actions/github-script@v8 → ed597411...
```

#### Cache Operations

```
workflow:action_cache Loading action cache from: .github/aw/actions-lock.json
workflow:action_cache Successfully loaded cache with 15 entries
workflow:action_cache Setting cache entry: key=actions/github-script@v8, sha=ed597411...
workflow:action_cache Deduplicating: keeping actions/github-script@v8.0.0, removing actions/github-script@v8
workflow:action_cache Deduplicated 1 entries, 14 entries remaining
```

#### Version Mismatch Detection

The system logs warnings when cache keys don't match their stored version:

```
workflow:action_pins WARNING: Key/version mismatch in action_pins.json: 
  key=actions/github-script@v8 has version=v8 but pin.Version=v8.0.0
```

## Understanding Deduplication

The action cache automatically deduplicates entries when saving. The deduplication logic:

1. **Groups by repo+SHA**: Identifies entries that resolve to the same commit
2. **Keeps most precise**: Retains the version with more specificity (e.g., `v8.0.0` over `v8`)
3. **Removes others**: Deletes less precise entries

### Precision Ranking

Versions are ranked by precision:

```
v8.0.0     (most precise - 3 components)
v8.0       (medium - 2 components)
v8         (least precise - 1 component)
```

## Solutions and Workarounds

### Option 1: Use Consistent Version Tags (Recommended)

Ensure all workflows use the same version tag format:

```yaml
# ✅ Good - consistent
uses: actions/github-script@v8.0.0
uses: actions/setup-node@v6.1.0

# ❌ Avoid - mixing precision levels
uses: actions/github-script@v8      # Some workflows
uses: actions/github-script@v8.0.0  # Other workflows
```

### Option 2: Clear the Cache

If you suspect cache corruption, clear it and recompile:

```bash
# Remove the cache file
rm .github/aw/actions-lock.json

# Recompile all workflows
gh aw compile
```

### Option 3: Use Exact SHAs

Pin directly to SHAs in your workflow files:

```yaml
uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd
```

This bypasses version tag resolution entirely.

### Option 4: Update action_pins.json

If you control the repository, ensure `pkg/workflow/data/action_pins.json` uses canonical versions:

```json
{
  "entries": {
    "actions/github-script@v8.0.0": {
      "repo": "actions/github-script",
      "version": "v8.0.0",
      "sha": "ed597411d8f924073f98dfc5c65a23a2325f34cd"
    }
  }
}
```

## Common Scenarios

### Scenario 1: Shared Workflows

If you're using shared workflows that reference actions differently than your local workflows:

1. Adopt the version format from shared workflows
2. Or update shared workflows to match your local format
3. Ensure consistency across all workflow sources

### Scenario 2: CI vs Local Compilation

CI environments may use a fresh cache on each run, while local development persists the cache:

1. CI may show different version comments than local
2. Solution: Commit `.github/aw/actions-lock.json` to version control
3. This ensures consistent resolution across environments

### Scenario 3: Upstream Version Changes

Action maintainers may create new tags pointing to existing commits:

1. Example: `v8` and `v8.0.0` both added, pointing to same SHA
2. Cache may need cleanup when this happens
3. Solution: Clear cache or wait for deduplication to converge

## Log Interpretation Guide

### Normal Operation

```
workflow:action_pins Resolving action pin: repo=actions/github-script, version=v8.0.0
workflow:action_cache Cache hit for actions/github-script@v8.0.0: ed597411...
workflow:action_pins Dynamic resolution succeeded
```

**Interpretation**: Action is in cache, no API call needed.

### First Resolution

```
workflow:action_pins Resolving action pin: repo=actions/github-script, version=v8
workflow:action_cache Cache miss for key=actions/github-script@v8
workflow:action_resolver Querying GitHub API: /repos/actions/github-script/git/ref/tags/v8
workflow:action_resolver Successfully resolved actions/github-script@v8 to SHA: ed597411...
workflow:action_cache Setting cache entry: key=actions/github-script@v8, sha=ed597411...
```

**Interpretation**: New version tag being resolved and cached.

### Deduplication

```
workflow:action_cache Deduplicating: keeping actions/github-script@v8.0.0, removing actions/github-script@v8
workflow:action_cache Deduplicated 1 entries, 14 entries remaining
```

**Interpretation**: Cache cleanup removed less precise version tags.

### Fallback to Hardcoded Pins

```
workflow:action_pins Dynamic resolution failed for actions/github-script@v8: API error
workflow:action_pins Falling back to hardcoded pins
workflow:action_pins Exact version match: requested=v8.0.0, found=v8.0.0
```

**Interpretation**: API unavailable, using embedded `action_pins.json`.

## Preventive Measures

### 1. Standardize Version Format

Create a style guide for your team:

```markdown
## Action Version Format

All workflow files must use full semantic versioning for actions:

- ✅ `actions/checkout@v5.0.1`
- ✅ `actions/setup-node@v6.1.0`
- ❌ `actions/checkout@v5`
- ❌ `actions/setup-node@v6`
```

### 2. Commit the Action Cache

Add `.github/aw/actions-lock.json` to version control:

```bash
git add .github/aw/actions-lock.json
git commit -m "chore: add action cache for consistent pinning"
```

This ensures all developers and CI use the same resolved versions.

### 3. Periodic Cache Cleanup

Add to your maintenance workflow:

```bash
# Monthly: clear and regenerate cache
rm .github/aw/actions-lock.json
gh aw compile
git add .github/aw/actions-lock.json
git commit -m "chore: refresh action cache"
```

### 4. Pre-commit Checks

Add a pre-commit hook to detect inconsistent version formats:

```bash
#!/bin/bash
# Check for inconsistent action versions
if git diff --cached --name-only | grep -q '\.md$'; then
  # Extract action uses
  actions=$(git diff --cached --diff-filter=AM | grep -o 'uses: [^@]*@v[0-9]\+\s' || true)
  if [ -n "$actions" ]; then
    echo "Warning: Found actions using short version tags:"
    echo "$actions"
    echo "Consider using full semantic versions (e.g., v8.0.0 instead of v8)"
  fi
fi
```

## Advanced Debugging

### Trace Action Resolution

For deep debugging, trace the complete resolution path:

```bash
DEBUG='workflow:action_*' gh aw compile workflow.md 2>&1 | tee debug.log

# Then analyze:
grep "Resolving action pin" debug.log
grep "Dynamic resolution" debug.log
grep "Deduplicating" debug.log
```

### Compare Cache States

Track cache evolution across compiles:

```bash
# Before
cp .github/aw/actions-lock.json before.json

# Compile
gh aw compile

# After
cp .github/aw/actions-lock.json after.json

# Compare
diff -u before.json after.json
```

### Validate Cache Consistency

Check for unexpected cache entries:

```bash
# List all SHAs with their version tags
jq -r '.entries | to_entries[] | "\(.value.sha) \(.value.version) \(.key)"' .github/aw/actions-lock.json | sort

# Find duplicate SHAs
jq -r '.entries | to_entries[] | .value.sha' .github/aw/actions-lock.json | sort | uniq -d
```

## Related Documentation

- [Action Pinning Architecture](action_pins.go) - Implementation details
- [Action Cache Design](action_cache.go) - Cache structure and deduplication
- [GitHub Actions Security](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions) - Why pin to SHAs

## Troubleshooting Checklist

- [ ] Enabled debug logging: `DEBUG=workflow:action_* gh aw compile`
- [ ] Checked `.github/aw/actions-lock.json` for duplicate entries
- [ ] Verified version tags point to same SHA via GitHub API
- [ ] Searched workflows for inconsistent version formats
- [ ] Cleared cache and recompiled: `rm .github/aw/actions-lock.json && gh aw compile`
- [ ] Checked for upstream version tag changes
- [ ] Reviewed action_pins.json for canonical versions
- [ ] Consulted team on preferred version format
- [ ] Committed action cache to version control
- [ ] Set up pre-commit hooks for version consistency

## Getting Help

If the issue persists after following this guide:

1. Capture debug logs: `DEBUG=workflow:action_* gh aw compile 2> debug.log`
2. Export cache state: `cat .github/aw/actions-lock.json > cache.json`
3. List workflow action references: `grep -r "uses: " .github/workflows/ > actions.txt`
4. Create an issue with these artifacts attached

## Related Issues

This debugging guide addresses:
- Version comment flipping between equivalent tags
- Cache inconsistencies in local development
- Action resolution behavior differences across environments
