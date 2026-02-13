---
description: Reviews go.mod dependencies daily to detect and remove GPL-licensed transitive dependencies
on:
  schedule: daily
  workflow_dispatch:

timeout-minutes: 30

permissions:
  contents: read
  issues: read
  pull-requests: read

network:
  allowed:
    - "pkg.go.dev"
    - "proxy.golang.org"
    - "sum.golang.org"
    - "go.googlesource.com"
    - "api.github.com"
    - "github.com"

safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[gpl-dependency]"
    labels: [dependency-cleaner]
    max: 1

tools:
  cache-memory: true
  github:
    toolsets: [default]
  web-fetch:
  bash: [":*"]

strict: false

imports:
  - shared/mood.md

# Pre-download SBOM to get accurate dependency information
steps:
  - name: Download SBOM from GitHub Dependency Graph API
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      echo "📦 Downloading SBOM from GitHub Dependency Graph API..."
      
      # Download SBOM using gh CLI (requires contents: read permission)
      gh api \
        -H "Accept: application/vnd.github+json" \
        -H "X-GitHub-Api-Version: 2022-11-28" \
        "/repos/${{ github.repository }}/dependency-graph/sbom" \
        > /tmp/sbom.json
      
      echo "✅ SBOM downloaded successfully to /tmp/sbom.json"
      
      # Show SBOM summary
      if command -v jq &> /dev/null; then
        PACKAGE_COUNT=$(jq '.sbom.packages | length' /tmp/sbom.json 2>/dev/null || echo "unknown")
        echo "📊 SBOM contains ${PACKAGE_COUNT} packages"
      fi
---

# GPL Dependency Cleaner (gpclean)

## Objective

Systematically detect Go dependencies that introduce non-MIT friendly (GPL-type) licenses through transitive dependencies, perform deep research on how they're used, and create actionable issues with removal/replacement plans.

## Current Context
- **Repository**: ${{ github.repository }}
- **Go Module File**: `go.mod` in repository root
- **SBOM Source**: GitHub Dependency Graph API (SPDX format)
- **Cache Memory**: `/tmp/gh-aw/cache-memory/gpclean/` for round-robin module tracking

## Your Tasks

### Phase 0: Use Pre-Downloaded SBOM and Round-Robin Module Selection

Use the repository's SBOM (Software Bill of Materials) to get accurate dependency information, then select one module to analyze in a round-robin fashion.

**IMPORTANT**: The SBOM has been pre-downloaded to `/tmp/sbom.json` in the frontmatter setup step. **Use this file directly** - do NOT try to download it again using curl or gh api (you do not have a GitHub token in the agent environment).

1. **Use Pre-Downloaded SBOM**:
   - The SBOM file is already available at `/tmp/sbom.json`
   - It was downloaded using the GitHub Dependency Graph API with `contents: read` permission
   - Simply read and parse this file in subsequent steps

2. **Extract dependencies from SBOM**:
   - Parse the SBOM JSON file (SPDX format)
   - Look for packages in `sbom.packages[]` array
   - Filter for Go packages (those with `purl` starting with `pkg:golang/`)
   - Extract the package names (module paths) from the `purl` field
   - Focus on direct dependencies (not dev dependencies or build tools)
   - Save the list of dependencies to `/tmp/go-dependencies.txt`

3. **Load tracking state** from `/tmp/gh-aw/cache-memory/gpclean/state.json`:
   - If file doesn't exist, create it with initial state: `{"last_checked_module": "", "checked_modules": []}`
   - State tracks which modules have been checked recently

4. **Select next module to check**:
   - Use the dependencies list from SBOM (`/tmp/go-dependencies.txt`)
   - Find the first module NOT in `checked_modules` list
   - If all modules have been checked, reset `checked_modules` to empty array and start over
   - Update state with selected module and save to cache-memory

5. **Focused analysis**: Analyze only the selected module and its transitive dependencies in this run

### Phase 1: License Detection for Selected Module

For the selected module:

1. **Get full dependency tree**:
   ```bash
   go mod graph | grep "^<selected-module>"
   ```

2. **Check licenses for module and ALL its transitive dependencies**:
   - Use `go mod download -json <module>` to get module info
   - Check license on pkg.go.dev: `https://pkg.go.dev/<module-path>?tab=licenses`
   - Use web-fetch to scrape license information
   - Check the module's repository for LICENSE files

3. **Identify GPL-type licenses**:
   - GPL v2, GPL v3, LGPL, AGPL
   - Any license with "copyleft" restrictions
   - Licenses incompatible with MIT

4. **If GPL dependency found**:
   - Document the full dependency chain: direct module → intermediate modules → GPL module
   - Note the exact GPL license version
   - Continue to Phase 2

5. **If no GPL dependency found**:
   - Update cache-memory state: add module to `checked_modules`
   - Exit successfully (no issue created)

### Phase 2: Deep Research Using Web-Fetch

When a GPL dependency is detected:

1. **Understand usage in codebase**:
   ```bash
   # Find all imports of the GPL module
   grep -r "import.*<gpl-module-path>" . --include="*.go"
   
   # Find all imports of the direct dependency
   grep -r "import.*<direct-module-path>" . --include="*.go"
   ```

2. **Analyze what functionality is used**:
   - Review the code that imports the dependency
   - Identify specific functions/types being used
   - Determine if the GPL module is actually used or just pulled in transitively

3. **Research alternatives**:
   - Use web-fetch to search for:
     - "golang alternative to <direct-module>"
     - "<functionality> golang library MIT license"
     - Check pkg.go.dev for similar packages with MIT/Apache/BSD licenses
   
4. **Research removal options**:
   - Can we remove the dependency entirely?
   - Can we replace it with an MIT-licensed alternative?
   - Can we refactor code to avoid the dependency?
   - Can we use a different version that doesn't have GPL dependencies?

5. **Assess impact**:
   - What parts of the codebase use this dependency?
   - How complex would removal/replacement be?
   - Are there any breaking changes required?

### Phase 3: Create Actionable Issue

Create a detailed issue with:

**Title**: "Remove GPL dependency: <direct-module> (pulls in <gpl-module>)"

**Body**:

```markdown
## Summary

The dependency `<direct-module>` introduces a GPL-licensed transitive dependency `<gpl-module>` which is incompatible with our MIT license.

## Dependency Chain

```
<direct-module> 
  → <intermediate-module-1>
    → <intermediate-module-2>
      → <gpl-module> (GPL v3)
```

## GPL License Details

- **Module**: `<gpl-module>`
- **Version**: `v1.2.3`
- **License**: GPL v3
- **License URL**: [Link to license on pkg.go.dev]
- **Repository**: [Link to repository]

## Current Usage

### Where It's Used

The direct dependency `<direct-module>` is imported in:
- `path/to/file1.go` - [brief description of usage]
- `path/to/file2.go` - [brief description of usage]

### Functionality Required

We use `<direct-module>` for:
- [Specific feature 1]
- [Specific feature 2]
- [Specific feature 3]

### Direct GPL Usage

Analysis shows the GPL module `<gpl-module>`:
- ✅ IS directly used by our code imports
OR
- ❌ Is NOT directly used, only pulled in transitively

## Removal/Replacement Plan

### Option 1: Remove Dependency [RECOMMENDED/NOT RECOMMENDED]

**Approach**: [Brief description]

**Steps**:
1. [Step 1]
2. [Step 2]
3. [Step 3]

**Complexity**: Low/Medium/High

**Breaking Changes**: Yes/No - [Details if yes]

### Option 2: Replace with MIT-Licensed Alternative

**Alternative Package**: `<alternative-module>`
- **License**: MIT
- **Repository**: [Link]
- **Stars**: X.XK
- **Last Updated**: [Date]
- **pkg.go.dev**: [Link]

**Approach**: [Brief description]

**Steps**:
1. Add `<alternative-module>` to go.mod
2. Replace imports from `<direct-module>` to `<alternative-module>`
3. Update function calls (breaking changes: [details])
4. Run tests and verify behavior
5. Remove `<direct-module>` from go.mod

**Complexity**: Low/Medium/High

**Breaking Changes**: Yes/No - [Details if yes]

### Option 3: Refactor to Avoid Dependency

**Approach**: [Brief description of refactoring approach]

**Steps**:
1. [Step 1]
2. [Step 2]
3. [Step 3]

**Complexity**: Low/Medium/High

**Breaking Changes**: Yes/No - [Details if yes]

## Recommended Action

[Clear recommendation with rationale]

**Priority**: High/Medium/Low

**Rationale**: [Explain why this priority and which option is best]

## Testing Requirements

After implementing the chosen solution:

1. Run full test suite: `make test`
2. Verify specific functionality: [List specific tests]
3. Check for any new GPL dependencies: `go mod graph | [check licenses]`
4. Validate license compliance: `make license-check`

## Research Links

- [Link to pkg.go.dev for direct module]
- [Link to pkg.go.dev for GPL module]
- [Link to alternative packages researched]
- [Link to relevant GitHub discussions/issues]
- [Link to license compatibility resources]

## Additional Notes

[Any other relevant information, caveats, or considerations]

---

**Auto-expires in 2 days** - This issue will auto-close if not addressed within 2 days. A new GPL dependency may be checked in the next daily run.
```

### Phase 4: Update Cache Memory

After creating the issue:

1. **Update state file** `/tmp/gh-aw/cache-memory/gpclean/state.json`:
   ```json
   {
     "last_checked_module": "<module-path>",
     "last_check_date": "2024-01-15",
     "checked_modules": ["<module1>", "<module2>", "<current-module>"],
     "gpl_found": true,
     "gpl_module": "<gpl-module-path>",
     "issue_created": true
   }
   ```

2. **Save issue metadata** to `/tmp/gh-aw/cache-memory/gpclean/issues.jsonl`:
   ```json
   {
     "date": "2024-01-15",
     "module": "<direct-module>",
     "gpl_dependency": "<gpl-module>",
     "license": "GPL v3",
     "issue_number": 123,
     "recommended_action": "replace"
   }
   ```

## Important Guidelines

### SBOM Usage

- **SBOM is pre-downloaded** - The SBOM has been downloaded in the frontmatter setup step and is available at `/tmp/sbom.json`
- **Do NOT try to download SBOM again** - You do not have a GitHub token in the agent environment. Use the pre-downloaded file at `/tmp/sbom.json`
- SBOM is in SPDX format with packages listed in `sbom.packages[]` array
- Go packages have `purl` (Package URL) in format: `pkg:golang/github.com/org/repo@version`
- Parse the SBOM to extract all Go dependencies before license checking
- SBOM provides a comprehensive view including transitive dependencies
- The SBOM was downloaded using `gh api` with the workflow's `contents: read` permission in the frontmatter setup step

### Focus on One Dependency

- **Only analyze ONE direct dependency per run** (round-robin via cache-memory)
- This ensures thorough research without overwhelming the issue tracker
- Allows time for each issue to be addressed before creating new ones

### License Detection Accuracy

- Check multiple sources for license information (pkg.go.dev, repository, LICENSE files)
- Be conservative: if unsure, treat as potentially problematic and investigate
- GPL v2 and GPL v3 are incompatible with MIT - must be removed
- LGPL may be acceptable in some cases (dynamic linking) - research carefully
- AGPL is highly restrictive - must be removed

### Research Thoroughness

- Use web-fetch extensively to research alternatives
- Check pkg.go.dev for download stats, last update, and community trust
- Prefer well-maintained, popular alternatives (1K+ stars, recent updates)
- Consider migration complexity when recommending options
- Minimize breaking changes - prefer drop-in replacements

### Issue Quality

- Provide clear, actionable steps for each option
- Include specific code locations and usage patterns
- Link to all relevant resources for decision-making
- Be realistic about complexity and breaking changes
- Give a clear recommendation based on research

### Cache-Memory Usage

- Use cache-memory to track which modules have been checked
- Rotate through all direct dependencies systematically
- Prevent checking the same module repeatedly
- Store metadata about GPL dependencies found
- Enable trend tracking across runs

### Go Domain Access

- The workflow has access to essential Go domains:
  - `pkg.go.dev` - Package documentation and licenses
  - `proxy.golang.org` - Go module proxy
  - `sum.golang.org` - Checksum database
  - `go.googlesource.com` - Go source repositories
  - `api.github.com` - GitHub API (for repository research)
  - `github.com` - GitHub repositories

## Error Handling

- If the SBOM file `/tmp/sbom.json` is missing or corrupted, report the error and exit (this should not happen as it's pre-downloaded in frontmatter)
- If `go mod graph` fails, report the error and exit
- If license detection fails for a module, document it in the issue and recommend manual review
- If no direct dependencies exist, exit successfully
- If cache-memory state is corrupted, reinitialize it

## Example Module Selection Flow

**Run 1**: Use pre-downloaded SBOM → Extract Go dependencies → Check `github.com/spf13/cobra` → No GPL found → Add to checked_modules
**Run 2**: Check `github.com/spf13/viper` (from SBOM) → No GPL found → Add to checked_modules
**Run 3**: Check `github.com/cli/go-gh` (from SBOM) → GPL found in transitive dep → Create issue, add to checked_modules
**Run 4**: Check `gopkg.in/yaml.v3` (from SBOM) → No GPL found → Add to checked_modules
**Run 5**: All modules from SBOM checked → Reset checked_modules, start from beginning

This ensures systematic coverage without duplicate work.
