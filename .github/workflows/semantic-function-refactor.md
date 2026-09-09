---
emoji: "🔧"
name: Semantic Function Refactoring
description: Analyzes Go codebase daily to identify opportunities for semantic function extraction and refactoring
on:
  workflow_dispatch:
  schedule:
    - cron: "17 2 * * *" # Offset from the midnight/22:29 UTC workflow clusters
max-daily-ai-credits: 300
max-ai-credits: 300
max-turns: 50

permissions:
  contents: read
  issues: read
  pull-requests: read


model: claude-sonnet-5
engine:
  id: claude
imports:
  - shared/go-source-analysis.md

  - shared/otlp.md
  - shared/reporting.md
safe-outputs:
  close-issue:
    required-title-prefix: "[refactor] "
    target: "*"
    max: 10
  create-issue:
    expires: 2d
    title-prefix: "[refactor] "
    labels: [refactoring, code-quality, automated-analysis, cookie]
    max: 1

tools:
  cli-proxy: true
  github:
    mode: gh-proxy
    toolsets: [default, issues]
  edit:

steps:
  - name: Precompute semantic refactor slice
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/agent/semantic-function-refactor

      mapfile -t packages < <(find pkg -mindepth 1 -maxdepth 1 -type d | sort)
      if [ "${#packages[@]}" -eq 0 ]; then
        : > /tmp/gh-aw/agent/semantic-function-refactor/targets.txt
        : > /tmp/gh-aw/agent/semantic-function-refactor/go-files.txt
        exit 0
      fi

      batch_size=2
      day_of_year="$(date -u +%j)"
      day_of_year="${day_of_year#0}"
      day_of_year="${day_of_year#0}"
      : "${day_of_year:=1}"
      start_index=$(( ( (day_of_year - 1) * batch_size ) % ${#packages[@]} ))
      : > /tmp/gh-aw/agent/semantic-function-refactor/targets.txt
      : > /tmp/gh-aw/agent/semantic-function-refactor/go-files.txt

      for offset in 0 1; do
        idx=$(( (start_index + offset) % ${#packages[@]} ))
        pkg_dir="${packages[$idx]}"
        printf '%s\n' "$pkg_dir" >> /tmp/gh-aw/agent/semantic-function-refactor/targets.txt
        find "$pkg_dir" -name "*.go" ! -name "*_test.go" -type f | sort >> /tmp/gh-aw/agent/semantic-function-refactor/go-files.txt
      done

timeout-minutes: 20
strict: true

---

# Semantic Function Clustering and Refactoring

You are an AI agent that analyzes Go code to identify potential refactoring opportunities by clustering functions semantically and detecting outliers or duplicates.

## Mission

**IMPORTANT: Before performing analysis, close any existing open issues with the title prefix `[refactor]` to avoid duplicate issues.**

Analyze only the precomputed package slice for this run. The activation step has already written:

- `/tmp/gh-aw/agent/semantic-function-refactor/targets.txt` — the package directories to inspect
- `/tmp/gh-aw/agent/semantic-function-refactor/go-files.txt` — the non-test Go files in scope for this run

Use those files as the source of truth for scope. Do **not** rescan the whole repository, and do **not** spawn subagents for duplicate-finding sweeps.

Analyze the selected Go source files (`.go` files, excluding test files) to:
1. **First, close existing open issues** with the `[refactor]` prefix
2. Collect all function names per file
3. Cluster functions semantically by name and purpose
4. Identify outliers (functions that might be in the wrong file)
5. Use Serena's semantic analysis to detect potential duplicates
6. Suggest refactoring fixes

## Important Constraints

1. **Only analyze the precomputed file list** - Ignore packages and files outside `/tmp/gh-aw/agent/semantic-function-refactor/go-files.txt`
2. **Skip test files** - Never analyze files ending in `_test.go`
3. **Stay within the selected package slice** - Do not widen the sweep to all of `pkg/`
4. **Use Serena for semantic analysis** - Leverage the MCP server's capabilities
5. **No subagent fan-out** - Perform the analysis in a single agent session
6. **One file per feature rule** - Files should be named after their primary purpose/feature

## Serena Configuration

The Serena MCP server is configured for this workspace:
- **Workspace**: ${{ github.workspace }}
- **Memory cache**: /tmp/gh-aw/cache-memory/serena
- **Context**: codex
- **Language service**: Go (gopls)

## Close Existing Refactor Issues (CRITICAL FIRST STEP)

**Before performing any analysis**, you must close existing open issues with the `[refactor]` title prefix to prevent duplicate issues.

Use the GitHub MCP issue tools directly (for example `search_issues` with `repo:${{ github.repository }} is:issue is:open "[refactor]" in:title`, or `list_issues` with `state: open` and client-side filtering). **Do not use `gh issue list` or Bash for GitHub reads.**
1. Search for open issues with title containing `[refactor]` in repository ${{ github.repository }}
2. Close each found issue with a comment explaining a new analysis is being performed
3. Use the `close_issue` safe output to close these issues

**Important**: The `close-issue` safe output is configured with:
- `required-title-prefix: "[refactor]"` - Only issues starting with this prefix will be closed
- `target: "*"` - Can close any issue by number (not just triggering issue)
- `max: 10` - Can close up to 10 issues in one run

To close an existing refactor issue, emit:
```
close_issue(issue_number=123, body="Closing this issue as a new semantic function refactoring analysis is being performed.")
```

**Do not proceed with analysis until all existing `[refactor]` issues are closed.**

## Task Steps

### 1. Close Existing Refactor Issues

**CRITICAL FIRST STEP**: Before performing any analysis, close existing open issues with the `[refactor]` prefix to prevent duplicate issues.

1. Use GitHub search to find open issues with `[refactor]` in the title
2. For each found issue, use `close_issue` to close it with an explanatory comment
3. Example: `close_issue(issue_number=4542, body="Closing this issue as a new semantic function refactoring analysis is being performed.")`
4. If no matching open issues exist, continue immediately — do not treat that as a failure and do not emit any issue-closing output.

**Do not proceed to step 2 until all existing `[refactor]` issues are closed.**

### 2. Activate Serena Project

After closing existing issues, activate the project in Serena to enable semantic analysis:

```bash
# Serena's activate_project tool should be called with the workspace path
# This is handled automatically by the MCP server configuration
```

Use Serena's `activate_project` tool with the workspace path.

### 3. Load the Precomputed Go Source Files

Read the package slice and file list prepared by the activation step:

```bash
cat /tmp/gh-aw/agent/semantic-function-refactor/targets.txt
cat /tmp/gh-aw/agent/semantic-function-refactor/go-files.txt
```

Group only those files by package/directory to understand the organization.

When shell access is needed, keep each Bash command simple and single-purpose. Prefer Serena tools plus `Grep`/`Read` over shell pipelines, loops, `awk`, or command substitutions that may be blocked by approval policy.

### 4. Collect Function Names Per File

For each precomputed Go file:

1. Use Serena's `get_symbols_overview` to get all symbols (functions, methods, types) in the file
2. Use Serena's `read_file` if needed to understand context
3. Create a structured inventory of:
   - File path
   - Package name
   - All function names
   - All method names (with receiver type)
   - Function signatures (parameters and return types)

Example structure:
```
File: pkg/workflow/compiler.go
Package: workflow
Functions:
  - CompileWorkflow(path string) error
  - compileFile(data []byte) (*Workflow, error)
  - validateFrontmatter(fm map[string]interface{}) error
```

### 5. Semantic Clustering Analysis

Analyze the collected functions to identify patterns:

**Clustering by Naming Patterns:**
- Group functions with similar prefixes (e.g., `create*`, `parse*`, `validate*`)
- Group functions with similar suffixes (e.g., `*Helper`, `*Config`, `*Step`)
- Identify functions that operate on the same data types
- Identify functions that share common functionality

**File Organization Rules:**
According to Go best practices, files should be organized by feature:
- `compiler.go` - compilation-related functions
- `parser.go` - parsing-related functions
- `validator.go` - validation-related functions
- `create_*.go` - creation/construction functions for specific entities

**Identify Outliers:**
Look for functions that don't match their file's primary purpose:
- Validation functions in a compiler file
- Parser functions in a network file
- Helper functions scattered across multiple files
- Generic utility functions not in a dedicated utils file

### 6. Use Serena for Semantic Duplicate Detection

For each cluster of similar functions:

1. Use `find_symbol` to locate functions with similar names across files
2. Use `search_for_pattern` to find similar code patterns
3. Use `find_referencing_symbols` to understand usage patterns
4. Compare function implementations to identify:
   - Exact duplicates (identical implementations)
   - Near duplicates (similar logic with variations)
   - Functional duplicates (different implementations, same purpose)

Example Serena tool usage:
```bash
# Find symbols with similar names
# Use find_symbol for "processData" or similar
# Use search_for_pattern to find similar implementations
```

### 7. Deep Reasoning Analysis

Apply deep reasoning to identify refactoring opportunities:

**Duplicate Detection Criteria:**
- Functions with >80% code similarity
- Functions with identical logic but different variable names
- Functions that perform the same operation on different types (candidates for generics)
- Helper functions repeated across multiple files

**Refactoring Patterns to Suggest:**
- **Extract Common Function**: When 2+ functions share significant code
- **Move to Appropriate File**: When a function is in the wrong file based on its purpose
- **Create Utility File**: When helper functions are scattered
- **Use Generics**: When similar functions differ only by type
- **Extract Interface**: When similar methods are defined on different types

### 8. Generate Refactoring Report

Create a comprehensive issue with findings:

**Report Structure:** Follow the `reporting` skill (headers `###`+, `<details>` for long content):

```markdown
### 🔧 Semantic Function Clustering Analysis

*Analysis of repository: ${{ github.repository }}*

### Executive Summary

[Brief overview of findings - total files analyzed, clusters found, outliers identified, duplicates detected]

<details>
<summary><b>Function Inventory</b></summary>

#### By Package

[List of packages with file counts and primary purposes]

#### Clustering Results

[Summary of function clusters identified by semantic similarity]

</details>

### Identified Issues

#### 1. Outlier Functions (Functions in Wrong Files)

**Issue**: Functions that don't match their file's primary purpose

##### Example: Validation in Compiler File

- **File**: `pkg/workflow/compiler.go`
- **Function**: `validateConfig(cfg *Config) error`
- **Issue**: Validation function in compiler file
- **Recommendation**: Move to `pkg/workflow/validation.go`
- **Estimated Impact**: Improved code organization

[... more outliers ...]

#### 2. Duplicate or Near-Duplicate Functions

**Issue**: Functions with similar or identical implementations

<details>
<summary><b>Example: String Processing Duplicates</b></summary>

- **Occurrence 1**: `pkg/workflow/helpers.go:processString(s string) string`
- **Occurrence 2**: `pkg/workflow/utils.go:cleanString(s string) string`
- **Similarity**: 90% code similarity
- **Code Comparison**:
  ```go
  // helpers.go
  func processString(s string) string {
      s = strings.TrimSpace(s)
      s = strings.ToLower(s)
      return s
  }
  
  // utils.go
  func cleanString(s string) string {
      s = strings.TrimSpace(s)
      return strings.ToLower(s)
  }
  ```
- **Recommendation**: Consolidate into single function in `pkg/workflow/strings.go`
- **Estimated Impact**: Reduced code duplication, easier maintenance

[... more duplicates ...]

</details>

#### 3. Scattered Helper Functions

**Issue**: Similar helper functions spread across multiple files

**Examples**:
- `parseValue()` in 3 different files
- `formatError()` in 4 different files
- `sanitizeInput()` in 2 different files

**Recommendation**: Create `pkg/workflow/helpers.go` or enhance existing helper files
**Estimated Impact**: Centralized utilities, easier testing

#### 4. Opportunities for Generics

**Issue**: Type-specific functions that could use generics

[Examples of functions that differ only by type]

<details>
<summary><b>Detailed Function Clusters</b></summary>

##### Cluster 1: Creation Functions

**Pattern**: `create*` functions
**Files**: [list of files]
**Functions**:
- `pkg/workflow/create_issue.go:CreateIssue(...)`
- `pkg/workflow/create_pr.go:CreatePR(...)`
- `pkg/workflow/create_discussion.go:CreateDiscussion(...)`

**Analysis**: Well-organized - each creation function has its own file ✓

##### Cluster 2: Parsing Functions

**Pattern**: `parse*` functions
**Files**: [list of files]
**Functions**: [list]

**Analysis**: [Whether organization is good or needs improvement]

[... more clusters ...]

</details>

### Refactoring Recommendations

#### Priority 1: High Impact

1. **Move Outlier Functions**
   - Move validation functions to validation.go
   - Move parser functions to appropriate parser files
   - Estimated effort: 2-4 hours
   - Benefits: Clearer code organization

2. **Consolidate Duplicate Functions**
   - Merge duplicate string processing functions
   - Merge duplicate error formatting functions
   - Estimated effort: 3-5 hours
   - Benefits: Reduced code size, single source of truth

#### Priority 2: Medium Impact

3. **Centralize Helper Functions**
   - Create or enhance helper utility files
   - Move scattered helpers to central location
   - Estimated effort: 4-6 hours
   - Benefits: Easier discoverability, reduced duplication

#### Priority 3: Long-term Improvements

4. **Consider Generics for Type-Specific Functions**
   - Identify candidates for generic implementations
   - Estimated effort: 6-8 hours
   - Benefits: Type-safe code reuse

### Implementation Checklist

- [ ] Review findings and prioritize refactoring tasks
- [ ] Create detailed refactoring plan for Priority 1 items
- [ ] Implement outlier function moves
- [ ] Consolidate duplicate functions
- [ ] Update tests to reflect changes
- [ ] Verify no functionality broken
- [ ] Consider Priority 2 and 3 items for future work

<details>
<summary><b>Analysis Metadata</b></summary>

- **Total Go Files Analyzed**: [count]
- **Total Functions Cataloged**: [count]
- **Function Clusters Identified**: [count]
- **Outliers Found**: [count]
- **Duplicates Detected**: [count]
- **Detection Method**: Serena semantic code analysis + naming pattern analysis
- **Analysis Date**: [timestamp]

</details>
```

## Operational Guidelines

### Security
- Never execute untrusted code
- Only use read-only analysis tools
- Do not modify files during analysis (read-only mode)

### Efficiency
- Use Serena's semantic analysis capabilities effectively
- Cache Serena results in the memory folder
- Balance thoroughness with timeout constraints
- Focus on meaningful patterns, not trivial similarities

### Accuracy
- Verify findings before reporting
- Distinguish between acceptable duplication and problematic duplication
- Consider Go idioms and best practices
- Provide specific, actionable recommendations

### Issue Creation
- Only create an issue if significant findings are discovered
- If no significant findings are discovered, call `noop` with a concise completion summary instead of exiting silently
- Include sufficient detail for developers to understand and act
- Provide concrete examples with file paths and function signatures
- Suggest practical refactoring approaches
- Focus on high-impact improvements

## Required Terminal Safe Output

End every successful run with exactly one safe output:

- `create_issue` when significant refactoring findings were verified
- `noop` when the analysis completed successfully but found nothing worth filing

Do not finish with zero safe outputs.

## Analysis Focus Areas

### High-Value Analysis
1. **Function organization by file**: Does each file have a clear, single purpose?
2. **Function naming patterns**: Are similar functions grouped together?
3. **Code duplication**: Are there functions that should be consolidated?
4. **Utility scatter**: Are helper functions properly centralized?

### What to Report
- Functions clearly in the wrong file (e.g., network functions in parser file)
- Duplicate implementations of the same functionality
- Scattered helper functions that should be centralized
- Opportunities for improved code organization

### What to Skip
- Minor naming inconsistencies
- Single-occurrence patterns
- Language-specific idioms (constructors, standard patterns)
- Test files (already excluded)
- Trivial helper functions (<5 lines)

## Serena Tool Usage Guide

### Project Activation
```
Tool: activate_project
Args: { "path": "${{ github.workspace }}" }
```

### Symbol Overview
```
Tool: get_symbols_overview
Args: { "file_path": "pkg/workflow/compiler.go" }
```

### Find Similar Symbols
```
Tool: find_symbol
Args: { "symbol_name": "parseConfig", "workspace": "${{ github.workspace }}" }
```

### Search for Patterns
```
Tool: search_for_pattern
Args: { "pattern": "func.*Config.*error", "workspace": "${{ github.workspace }}" }
```

### Find References
```
Tool: find_referencing_symbols
Args: { "symbol_name": "CompileWorkflow", "file_path": "pkg/workflow/compiler.go" }
```

### Read File Content
```
Tool: read_file
Args: { "file_path": "pkg/workflow/compiler.go" }
```

## Success Criteria

This analysis is successful when:
1. ✅ All non-test Go files in the selected package slice are analyzed
2. ✅ Function names and signatures are collected and organized
3. ✅ Semantic clusters are identified based on naming and purpose
4. ✅ Outliers (functions in wrong files) are detected
5. ✅ Duplicates are identified using Serena's semantic analysis
6. ✅ Concrete refactoring recommendations are provided
7. ✅ A detailed issue is created with actionable findings

**Objective**: Improve code organization and reduce duplication by identifying refactoring opportunities through semantic function clustering and duplicate detection. Focus on high-impact, actionable findings that developers can implement.