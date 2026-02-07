---
description: Monitors and updates agentic CLI tools (Claude Code, GitHub Copilot CLI, OpenAI Codex, GitHub MCP Server, Playwright MCP, Playwright Browser, Sandbox Runtime, MCP Gateway) for new versions
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  pull-requests: read
  issues: read
strict: true
engine: claude
network: 
   allowed: [defaults, node, "api.github.com", "ghcr.io"]
imports:
  - shared/mood.md
  - shared/jqschema.md
  - shared/reporting.md
tools:
  web-fetch:
  cache-memory: true
  bash:
    - "*"
  edit:
safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[ca] "
    labels: [automation, dependencies, cookie]
timeout-minutes: 45
---

# CLI Version Checker

Monitor and update agentic CLI tools: Claude Code, GitHub Copilot CLI, OpenAI Codex, GitHub MCP Server, Playwright MCP, Playwright Browser, Sandbox Runtime, and MCP Gateway.

**Repository**: ${{ github.repository }} | **Run**: ${{ github.run_id }}

## Report Formatting Guidelines

When creating version update issues, follow these markdown formatting standards for improved readability:

### Header Levels
**Use h3 (###) or lower for all headers in update issue reports to maintain proper document hierarchy.**

The issue title is already h1, so all internal sections should use h3 (###) or h4 (####) to maintain proper hierarchy. This ensures accessibility and proper document structure.

### Progressive Disclosure
**Wrap detailed changelog sections in `<details><summary><b>Section Name</b></summary>` tags to improve readability.**

Changelogs can be very long, especially for major version bumps. The summary and breaking changes should be visible, but full changelogs should be collapsible.

Example:
```markdown
<details>
<summary><b>View Full Changelog</b></summary>

[Complete changelog with all commits, PRs, and detailed changes]

</details>
```

### Report Structure Pattern
Use this structure for version update issues:

```markdown
### Update Summary
- **Current Version**: v1.2.3
- **Latest Version**: v1.3.0
- **Breaking Changes**: Yes/No
- **Update Priority**: High/Medium/Low

### Breaking Changes
[Always visible if present - critical for planning updates]

### Key Features
[Highlight 2-3 most important new features - keep visible]

<details>
<summary><b>View Full Changelog</b></summary>

[Complete release notes, all changes, commit history]

</details>

<details>
<summary><b>View Migration Guide</b></summary>

[Step-by-step update instructions, code changes needed]

</details>

### Recommendations
[Update priority, testing strategy, rollout plan]
```

**Design Principles**: Version update reports should:
- **Build trust through clarity**: Breaking changes and update priority immediately visible
- **Exceed expectations**: Include migration guides, testing recommendations, impact analysis
- **Create delight**: Use progressive disclosure for lengthy changelogs while keeping summary scannable
- **Maintain consistency**: Follow the same patterns as other update/monitoring workflows

## Process

**EFFICIENCY FIRST**: Before starting:
1. Check cache-memory at `/tmp/gh-aw/cache-memory/` for previous version checks and help outputs
2. If cached versions exist and are recent (< 24h), verify if updates are needed before proceeding
3. If no version changes detected, exit early with success

**CRITICAL**: If ANY version changes are detected, you MUST create an issue using safe-outputs.create-issue. Do not skip issue creation even for minor updates.

For each CLI/MCP server:
1. Fetch latest version from NPM registry or GitHub releases (use npm view commands for package metadata)
2. Compare with current version in `./pkg/constants/constants.go`
3. If newer version exists, research changes and prepare update

### Version Sources
- **Claude Code**: Use `npm view @anthropic-ai/claude-code version` (faster than web-fetch)
  - No public GitHub repository
- **Copilot CLI**: Use `npm view @github/copilot version`
  - Repository: https://github.com/github/copilot-cli
  - **CRITICAL**: Always attempt to fetch and deeply analyze Copilot repository content
  - Release Notes: https://github.com/github/copilot-cli/releases
  - Changelog: https://github.com/github/copilot-cli/blob/main/CHANGELOG.md (or similar)
  - README: https://github.com/github/copilot-cli/blob/main/README.md
- **Codex**: Use `npm view @openai/codex version`
  - Repository: https://github.com/openai/codex
  - Release Notes: https://github.com/openai/codex/releases
- **GitHub MCP Server**: `https://api.github.com/repos/github/github-mcp-server/releases/latest`
  - Release Notes: https://github.com/github/github-mcp-server/releases
- **Playwright MCP**: Use `npm view @playwright/mcp version`
  - Repository: https://github.com/microsoft/playwright
  - Package: https://www.npmjs.com/package/@playwright/mcp
- **Playwright Browser**: `https://api.github.com/repos/microsoft/playwright/releases/latest`
  - Release Notes: https://github.com/microsoft/playwright/releases
  - Docker Image: `mcr.microsoft.com/playwright:v{VERSION}`
- **Sandbox Runtime**: Use `npm view @anthropic-ai/sandbox-runtime version`
  - Repository: https://github.com/anthropic-experimental/sandbox-runtime
  - Package: https://www.npmjs.com/package/@anthropic-ai/sandbox-runtime
- **MCP Gateway**: `https://api.github.com/repos/github/gh-aw-mcpg/releases/latest`
  - Repository: https://github.com/github/gh-aw-mcpg
  - Release Notes: https://github.com/github/gh-aw-mcpg/releases
  - Docker Image: `ghcr.io/github/gh-aw-mcpg:v{VERSION}`
  - Used as default sandbox.agent container (see `pkg/constants/constants.go`)

**Optimization**: Fetch all versions in parallel using multiple npm view or WebFetch calls in a single turn.

### Research & Analysis
For each update, analyze intermediate versions:
- Categorize changes: Breaking, Features, Fixes, Security, Performance
- Assess impact on gh-aw workflows
- Document migration requirements
- Assign risk level (Low/Medium/High)

**GitHub Release Notes (when available)**:
- **Codex**: Fetch release notes from https://github.com/openai/codex/releases/tag/rust-v{VERSION}
  - Parse the "Highlights" section for key changes
  - Parse the "PRs merged" or "Merged PRs" section for detailed changes
  - **CRITICAL**: Convert PR/issue references (e.g., `#6211`) to full URLs since they refer to external repositories (e.g., `https://github.com/openai/codex/pull/6211`)
- **GitHub MCP Server**: Fetch release notes from https://github.com/github/github-mcp-server/releases/tag/v{VERSION}
  - Parse release body for changelog entries
  - **CRITICAL**: Convert PR/issue references (e.g., `#1105`) to full URLs since they refer to external repositories (e.g., `https://github.com/github/github-mcp-server/pull/1105`)
- **Playwright Browser**: Fetch release notes from https://github.com/microsoft/playwright/releases/tag/v{VERSION}
  - Parse release body for changelog entries
  - **CRITICAL**: Convert PR/issue references to full URLs (e.g., `https://github.com/microsoft/playwright/pull/12345`)
- **Copilot CLI**: **ALWAYS attempt deep analysis** - Repository: https://github.com/github/copilot-cli
  - **CRITICAL**: Thoroughly read and analyze all available documentation:
    1. **Release Notes**: Fetch from https://github.com/github/copilot-cli/releases/tag/v{VERSION}
       - Parse release highlights and feature descriptions
       - Extract breaking changes and deprecation notices
       - Note new commands, flags, and configuration options
    2. **CHANGELOG.md**: Read from https://github.com/github/copilot-cli/blob/main/CHANGELOG.md (or equivalent)
       - Compare versions to identify all changes between current and new version
       - Categorize changes: Breaking, Features, Fixes, Security, Performance
    3. **README.md**: Review https://github.com/github/copilot-cli/blob/main/README.md
       - Check for updated usage patterns and examples
       - Note new capabilities or configuration options
    4. **Documentation Changes**: Look for changes in documentation files that indicate new features
  - If repository is inaccessible (private), document the access limitation in the issue but still:
    - Use `npm view @github/copilot --json` for detailed package metadata
    - Compare CLI help output between versions (see "Tool Installation & Discovery" section)
    - Check for any publicly available release announcements or blog posts
  - **CRITICAL**: Convert PR/issue references to full URLs (e.g., `https://github.com/github/copilot-cli/pull/123`)
- **Claude Code**: No public repository, rely on NPM metadata and CLI help output
- **Playwright MCP**: Uses Playwright versioning, check NPM package metadata for changes
- **MCP Gateway**: Fetch release notes from https://github.com/github/gh-aw-mcpg/releases/tag/{VERSION}
  - Parse release body for changelog entries
  - **CRITICAL**: Convert PR/issue references to full URLs (e.g., `https://github.com/github/gh-aw-mcpg/pull/123`)
  - Note: Used as default sandbox.agent container in MCP Gateway configuration

**NPM Metadata Fallback**: When GitHub release notes are unavailable, use:
- `npm view <package> --json` for package metadata
- Compare CLI help outputs between versions
- Check for version changelog in package description

### Tool Installation & Discovery
**CACHE OPTIMIZATION**: 
- Before installing, check cache-memory for previous help outputs (main and subcommands)
- Only install and run --help if version has changed
- Store main help outputs in cache-memory at `/tmp/gh-aw/cache-memory/[tool]-[version]-help.txt`
- Store subcommand help outputs at `/tmp/gh-aw/cache-memory/[tool]-[version]-[subcommand]-help.txt`

For each CLI tool update:
1. Install the new version globally (skip if already installed from cache check):
   - Claude Code: `npm install -g @anthropic-ai/claude-code@<version>`
   - Copilot CLI: `npm install -g @github/copilot@<version>`
   - Codex: `npm install -g @openai/codex@<version>`
   - Playwright MCP: `npm install -g @playwright/mcp@<version>`
   - Sandbox Runtime: `npm install -g @anthropic-ai/sandbox-runtime@<version>`
2. Invoke help to discover commands and flags (compare with cached output if available):
   - Run `claude-code --help`
   - Run `copilot --help` or `copilot help copilot`
   - Run `codex --help`
   - Run `npx @playwright/mcp@<version> --help` (if available)
   - Sandbox Runtime is a library, check NPM package metadata for API changes
3. **Explore subcommand help** for each tool (especially Copilot CLI):
   - Identify all available subcommands from main help output
   - For each subcommand, run its help command (e.g., `copilot help config`, `copilot help environment`, `copilot config --help`)
   - Store each subcommand help output in cache-memory at `/tmp/gh-aw/cache-memory/[tool]-[version]-[subcommand]-help.txt`
   - **Priority subcommands for Copilot CLI**: `config`, `environment` (explicitly requested)
   - Example commands:
     - `copilot help copilot`
     - `copilot help config` or `copilot config --help`
     - `copilot help environment` or `copilot environment --help`
4. Compare help output with previous version to identify:
   - New commands or subcommands
   - New command-line flags or options
   - Deprecated or removed features
   - Changed default behaviors
   - **NEW**: Changes in subcommand functionality or flags
5. Save all help outputs (main and subcommands) to cache-memory for future runs

### Update Process
1. Edit `./pkg/constants/constants.go` with new version(s)
2. **REQUIRED**: Run `make recompile` to update workflows (MUST be run after any constant changes)
3. Verify changes with `git status`
4. **REQUIRED**: Create issue via safe-outputs with detailed analysis (do NOT skip this step)

## Issue Format

**Follow the Report Structure Pattern defined in "Report Formatting Guidelines" section above.**

For each updated CLI, include:
- **Version**: old → new (list intermediate versions if multiple)
- **Release Timeline**: dates and intervals
- **Changes**: Categorized as Breaking/Features/Fixes/Security/Performance
- **Impact Assessment**: Risk level, affected features, migration notes
- **Changelog Links**: Use plain URLs without backticks
- **CLI Changes**: New commands, flags, or removed features discovered via help
- **Subcommand Changes**: Changes in subcommand functionality or flags (especially `config` and `environment` for Copilot CLI)
- **GitHub Release Notes**: Include highlights and PR summaries when available from GitHub releases

**IMPORTANT**: Use h3 (###) or lower for all headers. Wrap full changelogs and migration guides in `<details>` tags as shown in the Report Structure Pattern.

**URL Formatting Rules**:
- Use plain URLs without backticks around package names
- **CORRECT**: https://www.npmjs.com/package/@github/copilot
- **INCORRECT**: `https://www.npmjs.com/package/@github/copilot` (has backticks)
- **INCORRECT**: https://www.npmjs.com/package/`@github/copilot` (package name wrapped in backticks)

**Pull Request Link Formatting**:
- **CRITICAL**: Always use full URLs for pull requests that refer to external repositories
- **CORRECT**: https://github.com/openai/codex/pull/6211
- **INCORRECT**: #6211 (relative reference only works for same repository)
- When copying PR references from release notes, convert `#1234` to full URLs like `https://github.com/owner/repo/pull/1234`

Legacy template reference (adapt to use Report Structure Pattern above):
```
### Update [CLI Name]
- Previous: [version] → New: [version]
- Timeline: [dates and frequency]

### Breaking Changes
[list or "None"]

### Key Features
- [New feature 1]
- [New feature 2]

<details>
<summary><b>View Full Changelog</b></summary>

### Release Highlights (from GitHub)
[Include key highlights from GitHub release notes if available]

### Bug Fixes
[list]

### Security Updates
[CVEs/patches or "None"]

### CLI Discovery
[New commands/flags or "None detected"]

### Subcommand Changes
[Changes in subcommands like config/environment or "None detected"]

### Merged PRs (from GitHub)
[List significant merged PRs from release notes if available]

### Subcommand Help Analysis
[Document changes in subcommand help output, particularly for config and environment commands]

</details>

<details>
<summary><b>View Migration Guide</b></summary>

[Step-by-step update instructions, code changes needed if any]

</details>

### Impact Assessment
- Risk: [Low/Medium/High]
- Affects: [features]

### Recommendations
[Update priority, testing strategy, rollout plan]

### Package Links
- **NPM Package**: https://www.npmjs.com/package/package-name-here
- **Repository**: [GitHub URL if available]
- **Release Notes**: [GitHub releases URL if available]
- **Specific Release**: [Direct link to version's release notes if available]
```

## Guidelines
- Only update stable versions (no pre-releases)
- Prioritize security updates
- Document all intermediate versions
- **USE NPM COMMANDS**: Use `npm view` instead of web-fetch for package metadata queries
- **CHECK CACHE FIRST**: Before re-analyzing versions, check cache-memory for recent results
- **PARALLEL FETCHING**: Fetch all versions in parallel using multiple npm/WebFetch calls in one turn
- **EARLY EXIT**: If no version changes detected, save check timestamp to cache and exit successfully
- **FETCH GITHUB RELEASE NOTES**: For tools with public GitHub repositories, fetch release notes to get detailed changelog information
  - Codex: Always fetch from https://github.com/openai/codex/releases
  - GitHub MCP Server: Always fetch from https://github.com/github/github-mcp-server/releases
  - Playwright Browser: Always fetch from https://github.com/microsoft/playwright/releases
  - MCP Gateway: Always fetch from https://github.com/github/gh-aw-mcpg/releases
  - Copilot CLI: Try to fetch, but may be inaccessible (private repo)
  - Playwright MCP: Check NPM metadata, uses Playwright versioning
- **EXPLORE SUBCOMMANDS**: Install and test CLI tools to discover new features via `--help` and explore each subcommand
  - For Copilot CLI, explicitly check: `config`, `environment` and any other available subcommands
  - Use commands like `copilot help <subcommand>` or `<tool> <subcommand> --help`
- Compare help output between old and new versions (both main help and subcommand help)
- **SAVE TO CACHE**: Store help outputs (main and all subcommands) and version check results in cache-memory
- **REQUIRED**: Always run `make recompile` after updating constants to regenerate workflow lock files
- **DO NOT COMMIT** `*.lock.yml` or `pkg/workflow/js/*.js` files directly

## Common JSON Parsing Issues

When using npm commands or other CLI tools, their output may include informational messages with Unicode symbols that break JSON parsing:

**Problem Patterns**:
- `Unexpected token 'ℹ', "ℹ Timeout "... is not valid JSON`
- `Unexpected token '⚠', "⚠ pip pack"... is not valid JSON`
- `Unexpected token '✓', "✓ Success"... is not valid JSON`

**Solutions**:

### 1. Filter stderr (Recommended)
Redirect stderr to suppress npm warnings/info:
```bash
npm view @github/copilot version 2>/dev/null
npm view @anthropic-ai/claude-code --json 2>/dev/null
```

### 2. Use grep to filter output
Remove lines with Unicode symbols before parsing:
```bash
npm view @github/copilot --json | grep -v "^[ℹ⚠✓]"
```

### 3. Use jq for reliable extraction
Let jq handle malformed input:
```bash
# Extract version field only, ignoring non-JSON lines
npm view @github/copilot --json 2>/dev/null | jq -r '.version'
```

### 4. Check tool output before parsing
Always validate JSON before attempting to parse:
```bash
output=$(npm view package --json 2>/dev/null)
if echo "$output" | jq empty 2>/dev/null; then
  # Valid JSON, safe to parse
  version=$(echo "$output" | jq -r '.version')
else
  # Invalid JSON, handle error
  echo "Warning: npm output is not valid JSON"
fi
```

**Best Practice**: Combine stderr filtering with jq extraction for most reliable results:
```bash
npm view @github/copilot --json 2>/dev/null | jq -r '.version'
```

## Error Handling
- **SAVE PROGRESS**: Before exiting on errors, save current state to cache-memory
- **RESUME ON RESTART**: Check cache-memory on startup to resume from where you left off
- Retry NPM registry failures once after 30s
- Continue if individual changelog fetch fails
- Skip PR creation if recompile fails
- Exit successfully if no updates found
- Document incomplete research if rate-limited
