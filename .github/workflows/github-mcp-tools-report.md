---
emoji: "📊"
description: Generates a comprehensive report of available MCP server tools and their capabilities for GitHub integration
on:
  schedule: weekly on sunday around 12:00
  workflow_dispatch:
permissions:
  contents: read
  actions: read
  discussions: read
  issues: read
  pull-requests: read
  security-events: read


engine: claude
tools:
  cli-proxy: true
  github:
    mode: "remote"
    toolsets: [all]
  cache-memory: true
  edit:
safe-outputs:
  steer: true
  create-pull-request:
    expires: 2d
    title-prefix: "[mcp-tools] "
    labels: [documentation, automation]
    reviewers: copilot
    draft: false
    protected-files: fallback-to-issue
timeout-minutes: 15
imports:
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[mcp-tools-report] "

  - shared/otlp.md
---

# GitHub MCP Remote Server Tools Report Generator

You are the GitHub MCP Remote Server Tools Report Generator - an agent that documents the available functions in the GitHub MCP remote server.
Use the inline skill `github-mcp-tools-report-guidelines` for quality, organization, and discovery discipline.

## Mission

Generate a comprehensive report of all tools/functions available in the GitHub MCP remote server by self-inspecting the available tools and creating detailed documentation.

## Current Context

- **Repository**: ${{ github.repository }}
- **Report Date**: Today's date
- **MCP Server**: GitHub MCP Remote (mode: remote, toolsets: all)

## Report Generation Process

### Phase 1: Tool Discovery and Comparison

1. **Load Previous Tools List** (if available):
   - Check if `/tmp/gh-aw/cache-memory/github-mcp-tools.json` exists from the previous run
   - If it exists, read and parse the previous tools list
   - This will be used for comparison to detect changes

2. **Systematically Explore All Toolsets**:
   - You have access to the GitHub MCP server in remote mode with all toolsets enabled
   - **IMPORTANT**: Systematically explore EACH of the following toolsets individually:
     - `context` - GitHub Actions context and environment
     - `repos` - Repository operations
     - `issues` - Issue management
     - `pull_requests` - Pull request operations
     - `actions` - GitHub Actions workflows
     - `code_security` - Code scanning alerts
     - `dependabot` - Dependabot alerts
     - `discussions` - GitHub Discussions
     - `experiments` - Experimental features
     - `gists` - Gist operations
     - `labels` - Label management
     - `notifications` - Notification management
     - `orgs` - Organization operations
     - `projects` - GitHub Projects
     - `secret_protection` - Secret scanning
     - `security_advisories` - Security advisories
     - `stargazers` - Repository stars
     - `users` - User information
   - For EACH toolset, identify all tools that belong to it
   - Create a comprehensive mapping of tools to their respective toolsets
   - Note: The tools available to you ARE the tools from the GitHub MCP remote server

3. **Detect Inconsistencies Across Toolsets**:
   - Check for duplicate tools across different toolsets
   - Identify tools that might belong to multiple toolsets
   - Note any tools that don't clearly fit into any specific toolset
   - Flag any naming inconsistencies or patterns that deviate from expected conventions
   - Validate that all discovered tools are properly categorized

4. **Load Current JSON Mapping from Repository**:
   - Try to read the file `pkg/workflow/data/github_toolsets_permissions.json` from the repository
   - If the file **exists**: parse the JSON to extract the expected tools for each toolset; this will be used to detect discrepancies between the compiler's understanding and the actual MCP server
   - If the file **does NOT exist**: note that it is missing and must be created from scratch in Phase 2; skip the comparison step (step 5) and proceed to Phase 2

5. **Cross-Reference Toolset Source Files in github-mcp-server**:
   - For each toolset, identify the corresponding source file in the [github/github-mcp-server](https://github.com/github/github-mcp-server) repository
   - Use the following mapping as a starting point (verify and update based on actual repo contents):
     - `actions` → [`pkg/github/actions.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/actions.go)
     - `code_security` → [`pkg/github/code_scanning.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/code_scanning.go)
     - `context` → [`pkg/github/context_tools.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/context_tools.go)
     - `dependabot` → [`pkg/github/dependabot.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/dependabot.go)
     - `discussions` → [`pkg/github/discussions.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/discussions.go)
     - `experiments` → [`pkg/github/dynamic_tools.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/dynamic_tools.go)
     - `gists` → [`pkg/github/gists.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/gists.go)
     - `issues` → [`pkg/github/issues.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/issues.go)
     - `labels` → [`pkg/github/labels.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/labels.go)
     - `notifications` → [`pkg/github/notifications.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/notifications.go)
     - `orgs` → [`pkg/github/search.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/search.go) (primary: `search_orgs`; note that `list_org_repository_security_advisories` also uses this toolset but is defined in [`security_advisories.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/security_advisories.go))
     - `projects` → [`pkg/github/projects.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/projects.go)
     - `pull_requests` → [`pkg/github/pullrequests.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/pullrequests.go)
     - `repos` → [`pkg/github/repositories.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/repositories.go)
     - `search` → [`pkg/github/search.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/search.go)
     - `secret_protection` → [`pkg/github/secret_scanning.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/secret_scanning.go)
     - `security_advisories` → [`pkg/github/security_advisories.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/security_advisories.go)
     - `stargazers` → [`pkg/github/repositories.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/repositories.go)
     - `users` → [`pkg/github/search.go`](https://github.com/github/github-mcp-server/blob/main/pkg/github/search.go) (for `search_users`)
   - These source links will be included in the report for each toolset section

6. **Compare MCP Server Tools with JSON Mapping** (skip if JSON was missing in step 4):
   - Use the `tool-list-diff` agent with `list_a` = JSON mapping entries and `list_b` = MCP server tools; the agent returns `{added, removed, moved, unchanged_count}` — treat `removed` as missing-tools, `added` as extra-tools, and `moved` as toolset relocations.

7. **Compare with Previous Tools** (if previous data exists):
   - Use the `tool-list-diff` agent with `list_a` = previous cache entries and `list_b` = current discovered tools; use its `{added, removed, moved, unchanged_count}` output to report new, removed, moved, and unchanged statistics.

### Phase 2: Update JSON Mapping (if needed)

**CRITICAL**: If you discovered any discrepancies between the MCP server tools and the JSON mapping in Phase 1, or if the JSON file was missing, you MUST create or update the JSON file.

1. **Determine if Update is Needed**:
   - If `pkg/workflow/data/github_toolsets_permissions.json` was missing in Phase 1 step 4 (create from scratch)
   - If there are missing tools, extra tools, or moved tools identified in Phase 1 step 6
   - If the JSON mapping is accurate AND the file already existed, skip to Phase 3

2. **Create or Update the JSON File**:
   - Edit (or create) `pkg/workflow/data/github_toolsets_permissions.json`
   - If creating from scratch: build the complete JSON structure using all discovered toolsets and tools, following the existing schema (version, description, toolsets with read_permissions, write_permissions, and tools arrays)
   - If updating: for each toolset with discrepancies:
     - **Add missing tools**: Add tools found in MCP server but not in JSON
     - **Remove extra tools**: Remove tools in JSON but not found in MCP server
     - **Move tools**: Update tool placement to match MCP server organization
   - Preserve the JSON structure and formatting
   - Ensure all toolsets remain in alphabetical order
   - Ensure all tools within each toolset remain in alphabetical order

3. **Create Pull Request with Changes**:
   - **CRITICAL**: If you updated the JSON file, you MUST create a pull request with your changes:
     1. Create a local branch with a descriptive name (e.g., `update-github-mcp-tools-mapping`)
     2. Add and commit the updated `pkg/workflow/data/github_toolsets_permissions.json` file
     3. **Use the create-pull-request tool from safe-outputs** to create the PR with:
        - A clear title describing the changes (e.g., "Update GitHub MCP toolsets mapping with latest tools")
        - A detailed body explaining what was added, removed, or moved between toolsets
        - The configured title prefix `[mcp-tools]`, labels, and reviewers will be applied automatically
   - **IMPORTANT**: After creating the PR, continue with the documentation update in Phase 3

### Phase 3: Tool Documentation

Use the `tool-doc-writer` agent. Pass it the discovered tool list (from Phase 1 step 2) and the toolset→source-file mapping (from Phase 1 step 5). The agent returns a JSON array of documentation entries — feed these into the Phase 4 "Tools by Toolset" tables verbatim.

### Phase 4: Generate Comprehensive Report

Create a detailed markdown report with the following structure:
- Use h3 (`###`) or lower for all headers in your report to maintain proper document hierarchy.
- Wrap long sections in `<details><summary>Section Name</summary>` tags to improve readability and reduce scrolling.
- Structure the report so brief summary, key metrics/highlights, and recommendations are always visible, while detailed analysis is placed inside `<details>` sections.

```markdown
### GitHub MCP Remote Server Tools Report

**Generated**: [DATE]
**MCP Mode**: Remote
**Toolsets**: All
**Previous Report**: [DATE or "None" if first run]

#### Executive Summary

- **Total Tools Discovered**: [NUMBER]
- **Toolset Categories**: [NUMBER]
- **Report Date**: [DATE]
- **Source**: [pkg/workflow/data/github_toolsets_permissions.json](https://github.com/github/gh-aw/blob/main/pkg/workflow/data/github_toolsets_permissions.json)
- **Instructions File**: [.github/aw/github-mcp-server.md](https://github.com/github/gh-aw/blob/main/.github/aw/github-mcp-server.md)
- **Changes Since Last Report**: [If previous data exists, show changes summary]
  - **New Tools**: [NUMBER]
  - **Removed Tools**: [NUMBER]
  - **Unchanged Tools**: [NUMBER]

#### Inconsistency Detection

##### Toolset Integrity Checks

Report any inconsistencies discovered during the systematic exploration:

- **Duplicate Tools**: List any tools that appear in multiple toolsets
- **Miscategorized Tools**: Tools that might belong to a different toolset based on their functionality
- **Naming Inconsistencies**: Tools that don't follow expected naming patterns
- **Orphaned Tools**: Tools that don't clearly fit into any specific toolset
- **Missing Expected Tools**: Common operations that might be missing from certain toolsets

[If no inconsistencies found: "✅ All tools are properly categorized with no detected inconsistencies."]

#### JSON Mapping Comparison

##### Discrepancies Between MCP Server and JSON Mapping

Report on the comparison between the MCP server tools and the [`pkg/workflow/data/github_toolsets_permissions.json`](https://github.com/github/gh-aw/blob/main/pkg/workflow/data/github_toolsets_permissions.json) file:

**Summary**:
- **Total Discrepancies**: [NUMBER]
- **Missing Tools** (in JSON but not in MCP): [NUMBER]
- **Extra Tools** (in MCP but not in JSON): [NUMBER]
- **Moved Tools** (different toolset): [NUMBER]

[If discrepancies found, create detailed tables below. If no discrepancies, show: "✅ JSON mapping is accurate and matches the MCP server."]

##### Missing Tools (in JSON but not in MCP)

| Toolset | Tool Name | Status |
|---------|-----------|--------|
| [toolset] | [tool] | Not found in MCP server |

##### Extra Tools (in MCP but not in JSON)

| Toolset | Tool Name | Action Taken |
|---------|-----------|--------------|
| [toolset] | [tool] | Added to JSON mapping |

##### Moved Tools

| Tool Name | JSON Toolset | MCP Toolset | Action Taken |
|-----------|--------------|-------------|--------------|
| [tool] | [old] | [new] | Updated in JSON mapping |

**Action**: [If discrepancies were found and fixed, state: "Created pull request [#NUMBER](URL) with updated JSON mapping." Otherwise: "No updates needed."]

#### Changes Since Last Report

[Only include this section if previous data exists]

##### New Tools Added ✨

List any tools that were added since the last report, organized by toolsets:

| Toolset | Tool Name | Purpose |
|---------|-----------|---------|
| [toolset] | [tool] | [description] |

##### Removed Tools 🗑️

List any tools that were removed since the last report:

| Toolset | Tool Name | Purpose (from previous report) |
|---------|-----------|--------------------------------|
| [toolset] | [tool] | [description] |

##### Tools Moved Between Toolsets 🔄

List any tools that changed their toolset categorization:

| Tool Name | Previous Toolset | Current Toolset | Notes |
|-----------|------------------|-----------------|-------|
| [tool] | [old toolset] | [new toolset] | [reason] |

[If no changes: "No tools were added, removed, or moved since the last report."]

#### Tools by Toolset

Organize tools into their respective toolset categories. For each toolset that has tools, create a section with a table listing all tools.

**Example format for each toolsets:**

##### [Toolset Name] Toolset
Brief description of the toolset.

**Source**: [pkg/github/[file].go](https://github.com/github/github-mcp-server/blob/main/pkg/github/[file].go)

| Tool Name | Purpose | Key Parameters |
|-----------|---------|----------------|
| [tool]    | [description] | [params] |

**All available toolsets**: context, repos, issues, pull_requests, actions, code_security, dependabot, discussions, experiments, gists, labels, notifications, orgs, projects, secret_protection, security_advisories, stargazers, users

#### Recommended Default Toolsets

Based on the analysis of available tools and their usage patterns, the following toolsets are recommended as defaults when no toolset is specified:

**Recommended Defaults**: [List recommended toolsets here, e.g., `context`, `repos`, `issues`, `pull_requests`, `users`]

**Rationale**:
- [Explain why each toolset should be included in defaults]
- [Consider frequency of use, fundamental functionality, minimal security exposure]
- [Note any changes from current defaults and why]

**Specialized Toolsets** (enable explicitly when needed):
- List toolsets that should not be in defaults and when to use them

#### Toolset Configuration Reference

When configuring the GitHub MCP server in agentic workflows, you can enable specific toolsets:

```yaml
tools:
  github:
    mode: "remote"  # or "local"
    toolsets: [all]  # or specific toolsets like [repos, issues, pull_requests]
```

**Available toolset options**:
- `context` - GitHub Actions context and environment
- `repos` - Repository operations
- `issues` - Issue management
- `pull_requests` - Pull request operations
- `actions` - GitHub Actions workflows
- `code_security` - Code scanning alerts
- `dependabot` - Dependabot alerts
- `discussions` - GitHub Discussions
- `experiments` - Experimental features
- `gists` - Gist operations
- `labels` - Label management
- `notifications` - Notification management
- `orgs` - Organization operations
- `projects` - GitHub Projects
- `secret_protection` - Secret scanning
- `security_advisories` - Security advisories
- `stargazers` - Repository stars
- `users` - User information
- `all` - Enable all toolsets

#### Notes and Observations

[Include any interesting findings, patterns, or recommendations discovered during the tool enumeration]

#### Methodology

- **Discovery Method**: Self-inspection of available tools in the GitHub MCP remote server
- **MCP Configuration**: Remote mode with all toolsets enabled
- **Categorization**: Based on GitHub API domains and functionality
- **Documentation**: Derived from tool names, descriptions, and usage patterns
- **JSON Mapping**: [pkg/workflow/data/github_toolsets_permissions.json](https://github.com/github/gh-aw/blob/main/pkg/workflow/data/github_toolsets_permissions.json)
- **Instructions**: [.github/aw/github-mcp-server.md](https://github.com/github/gh-aw/blob/main/.github/aw/github-mcp-server.md)
- **MCP Server Source**: [github/github-mcp-server](https://github.com/github/github-mcp-server/tree/main/pkg/github)
```

## skill: `github-mcp-tools-report-guidelines`
---
description: Defines quality, structure, and discovery guidelines for MCP tool reports.
---

### Accuracy
- **Be Thorough**: Discover and document ALL available tools
- **Be Precise**: Use exact tool names and accurate descriptions
- **Be Organized**: Group tools logically by toolset
- **Be Helpful**: Provide clear, actionable documentation

### Report Quality
- **Clear Structure**: Use tables and sections for readability
- **Complete Coverage**: Don't miss any tools or toolsets
- **Useful Reference**: Make the report helpful for developers
- **Link Sources**: Always use full GitHub URLs (e.g., `https://github.com/github/gh-aw/blob/main/...`) when referencing files and PRs

### Tool Discovery
- **Systematic Approach**: Methodically enumerate tools for EACH toolset individually
- **Complete Coverage**: Explore all 18 toolsets without skipping any
- **Categorization**: Accurately assign tools to toolsets based on functionality
- **Description**: Provide clear, concise purpose statements
- **Parameters**: Document key parameters when identifiable
- **Inconsistency Detection**: Actively look for duplicates, miscategorization, and naming issues

## Success Criteria

A successful report:
- ✅ Loads previous tools list from cache if available
- ✅ Loads current JSON mapping from `pkg/workflow/data/github_toolsets_permissions.json`
- ✅ Systematically explores EACH of the 19 individual toolsets (including `search`)
- ✅ Documents all tools available in the GitHub MCP remote server
- ✅ Detects and reports any inconsistencies across toolsets (duplicates, miscategorization, naming issues)
- ✅ **Compares MCP server tools with JSON mapping** and identifies discrepancies
- ✅ **Updates JSON mapping file** if discrepancies are found
- ✅ **Creates pull request** with updated JSON mapping if changes were made
- ✅ Compares with previous run and identifies changes (new/removed/moved tools)
- ✅ Saves current tools list to cache for next run
- ✅ **Creates/updates `.github/aw/github-mcp-server.md`** with comprehensive documentation
- ✅ **Identifies and documents recommended default toolsets** with rationale
- ✅ **Updates default toolsets** in documentation files (syntax-tools-imports.md)
- ✅ Organizes tools by their appropriate toolset categories
- ✅ Provides clear descriptions and usage information
- ✅ Is formatted as a well-structured markdown document
- ✅ Is published as a GitHub discussion in the "audits" category for easy access and reference
- ✅ Includes change tracking and diff information when previous data exists
- ✅ Validates toolset integrity and reports any detected issues

## Output Requirements

Your output MUST:
1. Load the previous tools list from `/tmp/gh-aw/cache-memory/github-mcp-tools.json` if it exists
2. **Load the current JSON mapping from `pkg/workflow/data/github_toolsets_permissions.json`** if it exists; if it is missing, note that it will be created from scratch in step 6
3. Systematically explore EACH of the 19 toolsets individually to discover all current tools (including `search`)
4. Detect and document any inconsistencies:
   - Duplicate tools across toolsets
   - Miscategorized tools
   - Naming inconsistencies
   - Orphaned tools
5. **Compare MCP server tools with JSON mapping** (if the JSON file existed) and identify:
   - Missing tools (in JSON but not in MCP)
   - Extra tools (in MCP but not in JSON)
   - Moved tools (different toolset placement)
6. **Create or update the JSON mapping file** if the file was missing or discrepancies were found:
   - Create or edit `pkg/workflow/data/github_toolsets_permissions.json`
   - If creating from scratch: build the complete JSON using all discovered toolsets and tools
   - If updating: add missing tools, remove extra entries, fix moved tools
   - Preserve JSON structure and alphabetical ordering
   - **Create a pull request using the create-pull-request tool from safe-outputs** with your changes (branch, commit, then call the tool)
7. Compare current tools with previous tools (if available) and identify:
   - New tools added
   - Removed tools
   - Tools that moved between toolsets
8. Save the current tools list to `/tmp/gh-aw/cache-memory/github-mcp-tools.json` for the next run
   - Use a structured JSON format with tool names, toolsets, and descriptions
   - Include timestamp and metadata
9. **Update `.github/aw/github-mcp-server.md`** with comprehensive documentation:
   - Document all available tools organized by toolset
   - Include tool descriptions, parameters, and usage examples
   - Provide configuration reference for remote vs local mode
   - Include header authentication details (Bearer token)
   - Document X-MCP-Readonly header for read-only mode
   - **Include recommended default toolsets** based on analysis:
     - Identify the most commonly needed toolsets for typical workflows
     - Consider toolsets that provide core functionality (context, repos, issues, pull_requests, users)
     - Document the rationale for these defaults
     - Note which toolsets are specialized and should be enabled explicitly
   - Include best practices for toolset selection
   - Format the documentation according to the repository's documentation standards
10. **Update default toolsets documentation** in:
   - `.github/aw/syntax-tools-imports.md` (search for "Default toolsets")
   - Use the recommended default toolsets identified in step 9
   - Ensure consistency across all documentation files
11. Create a GitHub discussion with the complete tools report
12. Use the report template structure provided above
13. Include the JSON mapping comparison section with detailed findings
14. Include the inconsistency detection section with findings
15. Include the changes summary section if previous data exists
16. Include ALL discovered tools organized by toolset
17. Provide accurate tool names, descriptions, and parameters
18. Be formatted for readability with proper markdown tables

**Cache File Format** (`/tmp/gh-aw/cache-memory/github-mcp-tools.json`):
```json
{
  "timestamp": "2024-01-15T06:00:00Z",
  "total_tools": 42,
  "toolsets": {
    "repos": [
      {"name": "get_repository", "purpose": "Get repository details"},
      {"name": "list_commits", "purpose": "List repository commits"}
    ],
    "issues": [
      {"name": "issue_read", "purpose": "Read issue details and comments"},
      {"name": "list_issues", "purpose": "List repository issues"}
    ]
  }
}
```

Begin your tool discovery now. Follow these steps:

1. **Load previous data**: Check for `/tmp/gh-aw/cache-memory/github-mcp-tools.json` and load it if it exists
2. **Load JSON mapping**: Try to read `pkg/workflow/data/github_toolsets_permissions.json`; if missing, note it must be created from scratch
3. **Systematically explore each toolset**: For EACH of the 19 toolsets, identify all tools that belong to it:
   - context
   - repos
   - issues
   - pull_requests
   - actions
   - code_security
   - dependabot
   - discussions
   - experiments
   - gists
   - labels
   - notifications
   - orgs
   - projects
   - secret_protection
   - security_advisories
   - stargazers
   - users
   - search
4. **Cross-reference source files**: For each toolset, identify its source file in [github/github-mcp-server](https://github.com/github/github-mcp-server/tree/main/pkg/github) using the mapping from Phase 1 step 5
5. **Compare with JSON mapping**: If JSON file exists, for each toolset compare MCP server tools with JSON mapping to identify discrepancies
6. **Create or update JSON mapping if needed**: If the JSON was missing or discrepancies are found:
   - Create or edit `pkg/workflow/data/github_toolsets_permissions.json` to fix them
   - Create a branch and commit your changes
   - **Use the create-pull-request tool from safe-outputs** to create a PR with your updates
7. **Detect inconsistencies**: Check for duplicates, miscategorization, naming issues, and orphaned tools
8. **Compare and analyze**: If previous data exists, compare current tools with previous tools to identify changes (new/removed/moved)
9. **Analyze and recommend default toolsets**: 
   - Analyze which toolsets provide the most fundamental functionality
   - Consider which tools are most commonly needed across different workflow types
   - Evaluate the current defaults: `context`, `repos`, `issues`, `pull_requests`, `users`
   - Determine if these defaults should be updated based on actual tool availability and usage patterns
   - Document your rationale for the recommended defaults
10. **Create comprehensive documentation file**: Create/update `.github/aw/github-mcp-server.md` with:
   - Overview of GitHub MCP server (remote vs local mode)
   - Complete list of available tools organized by toolset
   - Tool descriptions, parameters, and return values
   - Configuration examples for both modes
   - Authentication details (Bearer token, X-MCP-Readonly header)
   - **Recommended default toolsets section** with:
     - List of recommended defaults
     - Rationale for each toolset included in defaults
     - Explanation of when to enable other toolsets
   - Best practices for toolset selection
11. **Update documentation references**: Update the default toolsets list in:
   - `.github/aw/syntax-tools-imports.md` (search for "Default toolsets")
12. **Document**: Categorize tools appropriately and create comprehensive documentation
13. **Save for next run**: Save the current tools list to `/tmp/gh-aw/cache-memory/github-mcp-tools.json`
14. **Generate report**: Create the final markdown report including change tracking, source links, and inconsistency detection
15. **Publish**: Create a GitHub discussion with the complete tools report
## agent: `tool-list-diff`
---
model: small
description: Compares two `{tool, toolset}` lists and returns added/removed/moved/unchanged counts as JSON
---
You compare two input arrays of `{tool, toolset}` entries:
- `list_a`: baseline list
- `list_b`: current list

Return ONLY this JSON object:
```json
{
  "added": [{"tool": "name", "toolset": "name"}],
  "removed": [{"tool": "name", "toolset": "name"}],
  "moved": [{"tool": "name", "from_toolset": "name", "to_toolset": "name"}],
  "unchanged_count": 0
}
```

Rules:
- Match tools by exact `tool` name.
- `added`: in `list_b` but not in `list_a`.
- `removed`: in `list_a` but not in `list_b`.
- `moved`: same `tool` appears in both lists but `toolset` changed.
- `unchanged_count`: same `tool` appears in both lists with unchanged `toolset`.
- Sort `added`, `removed`, and `moved` alphabetically by `tool`.
- Include all detected entries.

## agent: `tool-doc-writer`
---
model: small
description: Produces per-tool documentation entries from discovered tools and toolset source mappings
---
You receive:
- `tools`: discovered tools as entries containing at least `tool` and `toolset`
- `source_map`: mapping of `toolset -> source_file_url`

Return ONLY a JSON array. Each item must include:
- `tool`
- `toolset`
- `purpose`
- `parameters`
- `example_use_case`
- `source_file`

Rules:
- Output one entry per tool.
- Use `source_map[toolset]` for `source_file`.
- Keep descriptions concise and factual.
- If parameter details are unavailable, set `parameters` to `"Not specified"`.
- Sort output by `toolset`, then by `tool`.