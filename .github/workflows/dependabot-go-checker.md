---
description: Checks for Go module and NPM dependency updates and analyzes Dependabot PRs for compatibility and breaking changes
on:
  schedule:
    # Run every other business day: Monday, Wednesday, Friday at 9 AM UTC
    - cron: "0 9 * * 1,3,5"
  workflow_dispatch:

timeout-minutes: 20

permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
  security-events: read

network: defaults

safe-outputs:
  close-issue:
    required-title-prefix: "[deps]"
    target: "*"
    max: 20
  create-issue:
    expires: 2d
    title-prefix: "[deps]"
    labels: [dependencies, go, cookie]
    max: 10
    group: true

tools:
  github:
    toolsets: [default, dependabot]
  web-fetch:
  bash: [":*"]

imports:
  - shared/mood.md
---
# Dependabot Dependency Checker

## Objective
Close any existing open dependency update issues with the `[deps]` prefix, then check for available Go module and NPM dependency updates using Dependabot, categorize them by safety level, and create issues using a three-tier strategy: group safe patch updates into a single consolidated issue, create individual issues for potentially problematic updates, and skip major version updates.

## Current Context
- **Repository**: ${{ github.repository }}
- **Go Module File**: `go.mod` in repository root
- **NPM Packages**: Check for `@playwright/mcp` updates in constants.go

## Your Tasks

### Phase 0: Close Existing Dependency Issues (CRITICAL FIRST STEP)

**Before performing any analysis**, you must close existing open issues with the `[deps]` title prefix to prevent duplicate dependency update issues.

Use the GitHub API tools to:
1. Search for open issues with title starting with `[deps]` in repository ${{ github.repository }}
2. Close each found issue with a comment explaining that a new dependency check is being performed
3. Use the `close_issue` safe output to close these issues with reason "not_planned"

**Important**: The `close-issue` safe output is configured with:
- `required-title-prefix: "[deps]"` - Only issues starting with this prefix will be closed
- `target: "*"` - Can close any issue by number (not just triggering issue)
- `max: 20` - Can close up to 20 issues in one run

To close an existing dependency issue, emit:
```
close_issue(issue_number=123, body="Closing this issue as a new dependency check is being performed.")
```

**Do not proceed to Phase 1 until all existing `[deps]` issues are closed.**

### Phase 1: Check Dependabot Alerts
1. Use the Dependabot toolset to check for available dependency updates for the `go.mod` file
2. Retrieve the list of alerts and update recommendations from Dependabot
3. For each potential update, gather:
   - Current version and proposed version
   - Type of update (patch, minor, major)
   - Security vulnerability information (if any)
   - Changelog or release notes (if available via web-fetch)

### Phase 1.5: Check Playwright NPM Package Updates
1. Check the current `@playwright/mcp` version in `pkg/constants/constants.go`:
   - Look for `DefaultPlaywrightVersion` constant
   - Extract the current version number
2. Check for newer versions on NPM:
   - Use web-fetch to query `https://registry.npmjs.org/@playwright/mcp`
   - Compare the latest version with the current version in constants.go
   - Get release information and changelog if available
3. Evaluate the update:
   - Check if it's a patch, minor, or major version update
   - Look for breaking changes in release notes
   - Consider security fixes and improvements

### Phase 2: Categorize Updates (Three-Tier Strategy)
For each dependency update, categorize it into one of three categories:

**Category A: Safe Patches** (group into ONE consolidated issue):
- Patch version updates ONLY (e.g., v1.2.3 → v1.2.4)
- Single-version increments (not multi-version jumps like v1.2.3 → v1.2.5)
- Bug fixes and stability improvements only (no new features)
- No breaking changes or behavior modifications
- Security patches that only fix vulnerabilities without API changes
- Explicitly backward compatible per changelog

**Category B: Potentially Problematic** (create INDIVIDUAL issues):
- Minor version updates (e.g., v1.2.x → v1.3.x)
- Multi-version jumps in patch versions (e.g., v1.2.3 → v1.2.7)
- Updates with new features or API additions
- Updates with behavior changes mentioned in changelog
- Updates that require configuration or code changes
- Security updates that include API changes
- Any update where safety is uncertain

**Category C: Skip** (do NOT create issues):
- Major version updates (e.g., v1.x.x → v2.x.x)
- Updates with breaking changes explicitly mentioned
- Updates requiring significant refactoring
- Updates with insufficient documentation to assess safety

### Phase 2.5: Repository Detection
Before creating issues, determine the actual source repository for each Go module:

**GitHub Packages** (`github.com/*`):
- Remove version suffixes like `/v2`, `/v3`, `/v4` from the module path
- Example: `github.com/spf13/cobra/v2` → repository is `github.com/spf13/cobra`
- Repository URL: `https://github.com/{owner}/{repo}`
- Release URL: `https://github.com/{owner}/{repo}/releases/tag/{version}`

**golang.org/x Packages**:
- These are NOT hosted on GitHub
- Repository: `https://go.googlesource.com/{package-name}`
- Example: `golang.org/x/sys` → `https://go.googlesource.com/sys`
- Commit history: `https://go.googlesource.com/{package-name}/+log`
- Do NOT link to GitHub release pages (they don't exist)

**Other Packages**:
- Use `pkg.go.dev/{module-path}` to find the repository URL
- Look for the "Repository" or "Source" link on the package page
- Use the discovered repository for links

### Phase 3: Create Issues Based on Categorization

**For Category A (Safe Patches)**: Create ONE consolidated issue grouping all safe patch updates together.

**For Category B (Potentially Problematic)**: Create INDIVIDUAL issues for each update.

**For Category C**: Do not create any issues.

#### Consolidated Issue Format (Category A)

**Title**: "Update safe patch dependencies (N updates)"

**Body** should include:
- **Summary**: Brief overview of grouped safe patch updates
- **Updates Table**: Table listing all safe patch updates with columns:
  - Package name
  - Current version
  - Proposed version
  - Key changes
- **Safety Assessment**: Why all these updates are considered safe patches
- **Recommended Action**: Single command block to apply all updates at once
- **Testing Notes**: General testing guidance

#### Individual Issue Format (Category B)

**Title**: Short description of the specific update (e.g., "Update github.com/spf13/cobra from v1.9.1 to v1.10.0")

**Body** should include:
- **Summary**: Brief description of what needs to be updated
- **Current Version**: The version currently in go.mod
- **Proposed Version**: The version to update to
- **Update Type**: Minor/Multi-version patch jump
- **Why Separate Issue**: Clear explanation of why this update needs individual review (e.g., "Minor version update with new features", "Multi-version jump requires careful testing", "Behavior changes mentioned in changelog")
- **Safety Assessment**: Detailed assessment of risks and considerations
- **Changes**: Summary of changes from changelog or release notes
- **Links**: 
  - Link to the Dependabot alert (if applicable)
  - Link to the actual source repository (detected per Repository Detection rules)
  - Link to release notes or changelog (if available)
  - For GitHub packages: link to release page
  - For golang.org/x packages: link to commit history instead
- **Recommended Action**: Command to update (e.g., `go get -u github.com/package@v1.10.0`)
- **Testing Notes**: Specific areas to test after applying the update

## Important Notes
- Do NOT apply updates directly - only create issues describing what should be updated
- Use three-tier categorization: Group Category A (safe patches), individual issues for Category B (potentially problematic), skip Category C (major versions)
- Category A updates should be grouped into ONE consolidated issue with a table format
- Category B updates should each get their own issue with a "Why Separate Issue" explanation
- If no safe updates are found, exit without creating any issues
- Limit to a maximum of 10 issues per run (up to 1 grouped issue for Category A + remaining individual issues for Category B)
- For security-related updates, clearly indicate the vulnerability being fixed
- Be conservative: when in doubt about breaking changes or behavior modifications, categorize as Category B (individual issue) or Category C (skip)
- When categorizing, prioritize safety: only true single-version patch updates with bug fixes belong in Category A

**CRITICAL - Repository Detection**:
- **Never assume all Go packages are on GitHub**
- **golang.org/x packages** are hosted at `go.googlesource.com`, NOT GitHub
- **Always remove version suffixes** (e.g., `/v2`, `/v3`) when constructing repository URLs for GitHub packages
- **Use pkg.go.dev** to find the actual repository for packages not on GitHub or golang.org/x
- **Do NOT create GitHub release links** for packages that don't use GitHub releases

## Example Issue Formats

### Example 1: Consolidated Issue for Safe Patches (Category A)

```markdown
## Summary
This issue groups together multiple safe patch updates that can be applied together. All updates are single-version patch increments with bug fixes only and no breaking changes.

## Updates

| Package | Current | Proposed | Update Type | Key Changes |
|---------|---------|----------|-------------|-------------|
| github.com/bits-and-blooms/bitset | v1.24.3 | v1.24.4 | Patch | Bug fixes in bit operations |
| github.com/imdario/mergo | v1.0.1 | v1.0.2 | Patch | Memory optimization, nil pointer fix |

## Safety Assessment
✅ **All updates are safe patches**
- All are single-version patch increments (e.g., v1.24.3 → v1.24.4, v1.0.1 → v1.0.2)
- Only bug fixes and stability improvements, no new features
- No breaking changes or behavior modifications
- Explicitly backward compatible per release notes

## Links
- [bitset v1.24.4 Release](https://github.com/bits-and-blooms/bitset/releases/tag/v1.24.4)
- [mergo v1.0.2 Release](https://github.com/imdario/mergo/releases/tag/v1.0.2)

## Recommended Action
Apply all updates together:

```bash
go get -u github.com/bits-and-blooms/bitset@v1.24.4
go get -u github.com/imdario/mergo@v1.0.2
go mod tidy
```

## Testing Notes
- Run all tests: `make test`
- Verify no regression in functionality
- Check for any deprecation warnings
```

### Example 2: Individual Issue for Minor Update (Category B)

```markdown
## Summary
Update `github.com/spf13/cast` dependency from v1.7.0 to v1.8.0

## Current State
- **Package**: github.com/spf13/cast
- **Current Version**: v1.7.0
- **Proposed Version**: v1.8.0
- **Update Type**: Minor

## Why Separate Issue
⚠️ **Minor version update with new features**
- This is a minor version update (v1.7.0 → v1.8.0)
- Adds new type conversion functions
- May have behavior changes requiring verification
- Needs individual review and testing

## Safety Assessment
⚠️ **Requires careful review**
- Minor version update indicates new features
- Review changelog for behavior changes
- Test conversion functions thoroughly
- Verify no breaking changes in existing code

## Changes
- Added new ToFloat32E function
- Improved time parsing
- Enhanced error messages
- Performance optimizations

## Links
- [Release Notes](https://github.com/spf13/cast/releases/tag/v1.8.0)
- [Package Repository](https://github.com/spf13/cast)
- [Go Package](https://pkg.go.dev/github.com/spf13/cast@v1.8.0)

## Recommended Action
```bash
go get -u github.com/spf13/cast@v1.8.0
go mod tidy
```

## Testing Notes
- Run all tests: `make test`
- Test type conversion functions
- Verify time parsing works correctly
- Check for any behavior changes in existing code
```

### Example 3: Individual Issue for Multi-Version Jump (Category B)

```markdown
## Summary
Update `github.com/cli/go-gh` dependency from v2.10.0 to v2.12.0

## Current State
- **Package**: github.com/cli/go-gh
- **Current Version**: v2.10.0
- **Proposed Version**: v2.12.0
- **Update Type**: Multi-version patch jump

## Why Separate Issue
⚠️ **Multi-version jump requires careful testing**
- This jumps multiple minor versions (v2.10.0 → v2.12.0)
- Skips v2.11.0 which may have intermediate changes
- Multiple feature additions across versions
- Needs thorough testing to catch any issues

## Safety Assessment
⚠️ **Requires careful review**
- Multi-version jump increases risk
- Multiple changes accumulated across versions
- Review all intermediate release notes
- Test GitHub CLI integration thoroughly

## Changes
**v2.11.0 Changes:**
- Added support for new GitHub API features
- Improved error handling
- Bug fixes

**v2.12.0 Changes:**
- Enhanced authentication flow
- Performance improvements
- Additional API endpoints

## Links
- [v2.11.0 Release](https://github.com/cli/go-gh/releases/tag/v2.11.0)
- [v2.12.0 Release](https://github.com/cli/go-gh/releases/tag/v2.12.0)
- [Package Repository](https://github.com/cli/go-gh)
- [Go Package](https://pkg.go.dev/github.com/cli/go-gh/v2@v2.12.0)

## Recommended Action
```bash
go get -u github.com/cli/go-gh/v2@v2.12.0
go mod tidy
```

## Testing Notes
- Run all tests: `make test`
- Test GitHub CLI commands
- Verify authentication still works
- Check API integration points
- Test error handling
```

### Example 4: golang.org/x Package Update (Category B)

```markdown
## Summary
Update `golang.org/x/sys` dependency from v0.15.0 to v0.16.0

## Current State
- **Package**: golang.org/x/sys
- **Current Version**: v0.15.0
- **Proposed Version**: v0.16.0
- **Update Type**: Minor

## Why Separate Issue
⚠️ **Minor version update for system-level package**
- Minor version update (0.15.0 → 0.16.0)
- System-level changes may have subtle effects
- Affects low-level system calls
- Needs platform-specific testing

## Safety Assessment
⚠️ **Requires careful review**
- System-level package with platform-specific code
- Changes may affect OS-specific behavior
- Needs testing on multiple platforms
- Review commit history carefully

## Changes
- Added support for new Linux syscalls
- Fixed Windows file system handling
- Performance improvements for Unix systems
- Bug fixes in signal handling

## Links
- [Source Repository](https://go.googlesource.com/sys)
- [Commit History](https://go.googlesource.com/sys/+log)
- [Go Package](https://pkg.go.dev/golang.org/x/sys@v0.16.0)

**Note**: This package is hosted on Google's Git (go.googlesource.com), not GitHub. There are no GitHub release pages.

## Recommended Action
```bash
go get -u golang.org/x/sys@v0.16.0
go mod tidy
```

## Testing Notes
- Run all tests: `make test`
- Test system-specific functionality
- Verify cross-platform compatibility
- Test on Linux, macOS, and Windows if possible
```

### Example 5: Playwright NPM Package Update (Category B)

```markdown
## Summary
Update `@playwright/mcp` package from 1.56.1 to 1.57.0

## Current State
- **Package**: @playwright/mcp
- **Current Version**: 1.56.1 (in pkg/constants/constants.go - DefaultPlaywrightVersion)
- **Proposed Version**: 1.57.0
- **Update Type**: Minor

## Why Separate Issue
⚠️ **Minor version update with new features**
- Minor version update (1.56.1 → 1.57.0)
- Adds new Playwright features
- May affect browser automation behavior
- Needs testing with existing workflows

## Safety Assessment
⚠️ **Requires careful review**
- Minor version update with new features
- Browser automation changes need testing
- Review release notes for breaking changes
- Test with existing Playwright workflows

## Changes
- Added support for new Playwright features
- Improved MCP server stability
- Bug fixes in browser automation
- Performance improvements

## Links
- [NPM Package](https://www.npmjs.com/package/@playwright/mcp)
- [Release Notes](https://github.com/microsoft/playwright/releases/tag/v1.57.0)
- [Source Repository](https://github.com/microsoft/playwright)

## Recommended Action
```bash
# Update the constant in pkg/constants/constants.go
# Change: const DefaultPlaywrightVersion = "1.56.1"
# To:     const DefaultPlaywrightVersion = "1.57.0"

# Then run tests to verify
make test-unit
```

## Testing Notes
- Run unit tests: `make test-unit`
- Verify Playwright MCP configuration generation
- Test browser automation workflows with playwright tool
- Check that version is correctly used in compiled workflows
- Test on multiple browsers if possible
```
