# Code Organization Patterns

This document provides guidance on file organization patterns and best practices for maintaining code quality in the GitHub Agentic Workflows project.

## Table of Contents

- [Recommended Patterns to Follow](#recommended-patterns-to-follow)
- [File Organization Principles](#file-organization-principles)
- [When to Create New Files](#when-to-create-new-files)
- [File Size Guidelines](#file-size-guidelines)
- [Domain-Specific Patterns](#domain-specific-patterns)
- [Anti-Patterns to Avoid](#anti-patterns-to-avoid)
- [Decision Trees](#decision-trees)

## Recommended Patterns to Follow

The codebase exhibits several well-organized patterns that should be emulated:

### 1. Create Functions Pattern (`create_*.go`)

**Pattern**: One file per GitHub entity creation operation

**Examples**:
- `create_issue.go` - GitHub issue creation logic
- `create_pull_request.go` - Pull request creation logic
- `create_discussion.go` - Discussion creation logic
- `create_code_scanning_alert.go` - Code scanning alert creation
- `create_agent_task.go` - Agent session creation logic
- `create_pr_review_comment.go` - PR review comment creation

**Why it works**:
- Clear separation of concerns
- Enables quick location of specific functionality
- Prevents files from becoming too large
- Facilitates parallel development
- Simplifies test organization and coverage

**When to use**:
- Creating handlers for GitHub API operations
- Implementing safe output processors
- Building distinct feature modules

### 2. Engine Separation Pattern

**Pattern**: Each AI engine has its own file with shared helpers in `engine_helpers.go`

**Examples**:
- `copilot_engine.go` (971 lines) - GitHub Copilot engine
- `claude_engine.go` (340 lines) - Claude engine
- `codex_engine.go` (639 lines) - Codex engine
- `custom_engine.go` (300 lines) - Custom engine support
- `agentic_engine.go` (450 lines) - Base agentic engine interface
- `engine_helpers.go` (424 lines) - Shared engine utilities

**Why it works**:
- Engine-specific logic is isolated
- Shared code is centralized in `engine_helpers.go`
- Allows addition of new engines without affecting existing ones
- Clear boundaries reduce merge conflicts

**When to use**:
- Implementing new AI engines
- Adding engine-specific features
- Refactoring engine functionality

### 3. Expression Builder Pattern (`expressions.go`)

**Pattern**: Cohesive functionality organized in a single, focused file

**Examples**:
- `expressions.go` (948 lines) - Expression tree building and rendering
- `strings.go` (153 lines) - String utility functions
- `artifacts.go` (60 lines) - Artifact handling
- `args.go` (65 lines) - Argument parsing

**Why it works**:
- All related functionality in one place
- Provides clear view of the complete feature
- Reduces navigation between files
- Promotes cohesive design

**When to use**:
- Building domain-specific utilities
- Implementing self-contained features
- Creating reusable components

### 4. Test Organization Pattern

**Pattern**: Tests live alongside implementation files with descriptive names

**Examples**:
- Feature tests: `feature.go` + `feature_test.go`
- Integration tests: `feature_integration_test.go`
- Specific scenario tests: `feature_scenario_test.go`

**Examples from codebase**:
- `create_issue.go` + `create_issue_assignees_test.go` + `create_issue_backward_compat_test.go`
- `copilot_engine.go` + `copilot_engine_test.go`
- `engine_helpers.go` + `engine_helpers_test.go` + `engine_helpers_shared_test.go`

**Why it works**:
- Tests are co-located with implementation
- Clear test purpose from filename
- Supports test coverage requirements
- Separates integration from unit tests

## File Organization Principles

### 1. Prefer Many Small Files Over Large Ones

**Good**: Multiple focused files (100-500 lines each)
```text
create_issue.go (160 lines)
create_pull_request.go (238 lines)
create_discussion.go (118 lines)
```text

**Avoid**: Single large file with all creation logic (600+ lines)

### 2. Group by Functionality, Not by Type

**Good**: Feature-based organization
```text
create_issue.go            # Issue creation logic
create_issue_test.go       # Issue creation tests
add_comment.go             # Comment addition logic
add_comment_test.go        # Comment tests
```text

**Avoid**: Type-based organization
```text
models.go                  # All structs
logic.go                   # All business logic
tests.go                   # All tests
```text

### 3. Use Descriptive File Names

**Good**:
- `create_pull_request_reviewers_test.go` - Clear what's being tested
- `engine_error_patterns_infinite_loop_test.go` - Specific scenario
- `copilot_mcp_http_integration_test.go` - Clear scope and type

**Avoid**:
- `utils.go` - Too vague
- `helpers.go` - Too generic (unless truly shared like `engine_helpers.go`)
- `misc.go` - Indicates poor organization

### 4. Keep Related Code Together

When implementing a feature:
1. Create main implementation file (`feature.go`)
2. Add unit tests (`feature_test.go`)
3. Add integration tests if needed (`feature_integration_test.go`)
4. Add scenario-specific tests (`feature_scenario_test.go`)

## When to Create New Files

### Create a New File When:

1. **Implementing a new safe output type**
   - Pattern: `create_<entity>.go`
   - Example: Adding `create_gist.go` for gist creation

2. **Adding a new engine**
   - Pattern: `<engine-name>_engine.go`
   - Example: `gemini_engine.go` for Google Gemini support

3. **Building a new domain feature**
   - Pattern: `<feature-name>.go`
   - Example: `webhooks.go` for webhook handling

4. **Current file exceeds 800 lines**
   - Consider splitting by logical boundaries
   - Extract related functionality to new file

5. **Adding significant test coverage**
   - Pattern: `feature_<scenario>_test.go`
   - Example: `create_issue_assignees_test.go`

### Extend Existing Files When:

1. **Adding to existing functionality**
   - Example: Adding a field to `CreateIssuesConfig` in `create_issue.go`

2. **Fixing bugs in existing code**
   - Keep fixes in the same file as the original code

3. **File is still under 500 lines**
   - No need to split unless logic is truly independent

4. **Adding related helper functions**
   - Example: Adding to `strings.go` for string utilities

## File Size Guidelines

### Function Count Threshold

**Guideline**: Consider splitting files when they exceed **50 functions**.

**Note**: This is a guideline, not a hard rule. Domain complexity may justify larger files.

**Monitoring**: Run `make check-file-sizes` to identify files approaching the 50-function threshold.

### Current Large Files

The following files are justified despite their size due to domain complexity:

- `js.go` (41 functions, 914 lines) - JavaScript bundling and execution with many embed directives
- `permissions.go` (37 functions, 945 lines) - Permission handling with many GitHub Actions permission types
- `scripts.go` (37 functions, 397 lines) - Script generation with specialized functions for workflow steps
- `compiler_safe_outputs_consolidated.go` (30 functions, 1267 lines) - Consolidated safe output handling

### Recommended Sizes

- **Small files**: 50-200 lines
  - Utilities, single-purpose functions, helper methods
  - Examples: `args.go` (65 lines), `artifacts.go` (60 lines)

- **Medium files**: 200-500 lines
  - Most feature implementations
  - Examples: `create_issue.go` (160 lines), `add_comment.go` (210 lines)

- **Large files**: 500-800 lines
  - Complex features with many aspects
  - Examples: `permissions.go` (905 lines), `safe_outputs.go` (811 lines)

- **Very large files**: 800+ lines
  - Core infrastructure only
  - Examples: `compiler.go` (1596 lines), `copilot_engine.go` (971 lines)
  - **Consider refactoring** if possible

### Red Flags

⚠️ **Warning signs** that a file should be split:

1. Multiple distinct responsibilities
2. Difficulty naming the file
3. Scrolling excessively to find code
4. Merge conflicts frequently occur
5. Tests are hard to organize

## Domain-Specific Patterns

### Validation Organization

**Current approach**: Centralized `validation.go` (714 lines)

**When to add to `validation.go`**:
- Schema validation logic
- Cross-cutting validation concerns
- Frontmatter field validation

**When to use domain-specific validation**:
- Engine-specific validation in `<engine>_engine.go`
- Feature-specific validation alongside feature code
- Example: Network validation in network-related files

### Extraction Functions

**Centralized extraction** (`validation.go`):
```go
func extractString(data map[string]any, key string) string
func extractBool(data map[string]any, key string) bool
```text

**Domain-specific extraction** (feature files):
```go
// In create_issue.go
func parseTitlePrefixFromConfig(configMap map[string]any) string
func parseLabelsFromConfig(configMap map[string]any) []string
```text

**Guideline**: Use centralized extractors for primitive types, domain-specific parsers for complex types.

### Compiler Organization

The compiler is split across multiple files:

- `compiler.go` (1596 lines) - Main compilation logic
- `compiler_yaml.go` (1020 lines) - YAML generation
- `compiler_jobs.go` (806 lines) - Job generation
- `compiler_test.go` (6058 lines) - Comprehensive tests

This demonstrates that even large subsystems benefit from logical file splits.

## Anti-Patterns to Avoid

### ❌ 1. God Files

**Problem**: Single file doing everything
```text
// Don't create files like this
workflow.go (5000+ lines)  // Everything related to workflows
```text

**Solution**: Split by responsibility
```text
workflow_parser.go
workflow_compiler.go
workflow_validation.go
```text

### ❌ 2. Vague Naming

**Problem**: Non-descriptive file names
```text
utils.go
helpers.go
misc.go
common.go
```text

**Solution**: Use specific names
```text
string_utils.go        // If really needed
engine_helpers.go      // Shared engine utilities
```text

### ❌ 3. Mixed Concerns

**Problem**: Unrelated functionality in one file
```text
// In create_issue.go - DON'T DO THIS
func CreateIssue() {}
func ValidateNetwork() {}  // Unrelated!
func CompileYAML() {}      // Unrelated!
```text

**Solution**: Keep files focused on one domain

### ❌ 4. Test Pollution

**Problem**: All tests in one massive file
```text
workflow_test.go (10000+ lines)  // All tests
```text

**Solution**: Split by scenario
```text
workflow_parser_test.go
workflow_compiler_test.go
workflow_integration_test.go
```text

### ❌ 5. Premature Abstraction

**Problem**: Creating files before patterns emerge
```text
// Don't create these preemptively
future_feature_helpers.go
maybe_needed_utils.go
```text

**Solution**: Wait until you have 2-3 use cases, then extract common patterns

## Decision Trees

### Should I Create a New File?

```text
Is this a new safe output type (create_*)?
├─ YES → Create create_<entity>.go
└─ NO
   │
   Is this a new AI engine?
   ├─ YES → Create <engine>_engine.go
   └─ NO
      │
      Is current file > 800 lines?
      ├─ YES → Consider splitting by logical boundaries
      └─ NO
         │
         Is this functionality independent?
         ├─ YES → Create new file
         └─ NO → Add to existing file
```text

### Should I Split an Existing File?

```text
Is the file > 1000 lines?
├─ YES → SHOULD split
└─ NO
   │
   Is the file > 800 lines?
   ├─ YES → CONSIDER splitting
   └─ NO
      │
      Does it have multiple responsibilities?
      ├─ YES → CONSIDER splitting
      └─ NO
         │
         Are there frequent merge conflicts?
         ├─ YES → CONSIDER splitting
         └─ NO → Keep as is
```text

### What Should I Name This File?

```text
Is it a create operation for GitHub entity?
├─ YES → create_<entity>.go
└─ NO
   │
   Is it an AI engine implementation?
   ├─ YES → <engine>_engine.go
   └─ NO
      │
      Is it shared helpers for a subsystem?
      ├─ YES → <subsystem>_helpers.go
      └─ NO
         │
         Is it a cohesive feature?
         ├─ YES → <feature>.go
         └─ NO → Reconsider the organization
```text

## Examples from the Codebase

### Recommended: Create Functions
```text
pkg/workflow/
├── create_issue.go                    (160 lines)
├── create_issue_test.go               (various test files)
├── create_pull_request.go             (238 lines)
├── create_pull_request_test.go
├── create_discussion.go               (118 lines)
├── create_code_scanning_alert.go      (136 lines)
└── create_agent_task.go               (120 lines)
```text

### Recommended: Engine Organization
```text
pkg/workflow/
├── agentic_engine.go                  (450 lines) - Base interface
├── copilot_engine.go                  (971 lines) - Copilot implementation
├── claude_engine.go                   (340 lines) - Claude implementation
├── codex_engine.go                    (639 lines) - Codex implementation
├── custom_engine.go                   (300 lines) - Custom engine
└── engine_helpers.go                  (424 lines) - Shared utilities
```text

### Recommended: Focused Utilities
```text
pkg/workflow/
├── strings.go                         (153 lines) - String utilities
├── expressions.go                     (948 lines) - Expression handling
├── artifacts.go                       (60 lines) - Artifact management
└── args.go                            (65 lines) - Argument parsing
```text

## Quick Reference

**When adding a feature**, ask yourself:

1. ✅ Does this fit logically in an existing file under 500 lines? → Add there
2. ✅ Is this a new GitHub entity creation? → `create_<entity>.go`
3. ✅ Is this a new engine? → `<engine>_engine.go`
4. ✅ Is this a cohesive, self-contained feature? → `<feature>.go`
5. ❌ Am I creating a "utils" or "helpers" file? → Reconsider the name
6. ❌ Will this file have multiple unrelated responsibilities? → Split it up

**When refactoring**, ask yourself:

1. ✅ Is the file over 800 lines? → Consider splitting
2. ✅ Are there distinct logical sections? → Extract to separate files
3. ✅ Would splitting improve testability? → Do it
4. ❌ Am I just moving code around without improving organization? → Don't do it

## Contributing to Organization Patterns

If you discover new patterns or anti-patterns:

1. Document them in this file
2. Provide concrete examples from the codebase
3. Explain the rationale
4. Update decision trees if needed
5. Submit a pull request with your improvements

Remember: **Good organization emerges from consistent patterns, not rigid rules.**
