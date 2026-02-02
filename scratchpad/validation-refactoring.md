# Validation Refactoring Guide

## Overview

This guide provides step-by-step instructions for refactoring large validation files into smaller, focused validators. It uses the `bundler_validation.go` split as a reference implementation.

## When to Refactor

### Complexity Thresholds

- **Target size**: 100-200 lines per validator
- **Hard limit**: 300 lines (refactor if exceeded)
- **Single responsibility**: Each file should validate one domain

### Indicators for Splitting

Refactor a validation file when:
- File exceeds 300 lines
- File contains 2+ unrelated validation domains
- Complex cross-dependencies require separate testing
- Error messages span multiple concern areas
- Adding new validation would push file over 300 lines

### Decision Tree: When to Split

```text
┌─────────────────────────────────────┐
│  Validation File > 300 lines?       │
└──────────────┬──────────────────────┘
               │
               ▼
        ┌──────────────┐
        │ Does it mix  │
        │ 2+ distinct  │     YES
        │ domains?     ├──────────► Should split
        └──────┬───────┘
               │ NO
               ▼
        ┌──────────────┐
        │ Is it hard   │
        │ to maintain  │     YES
        │ or test?     ├──────────► Should split
        └──────┬───────┘
               │ NO
               ▼
           Keep as-is
```

## Refactoring Process

### Step 1: Analyze Current Structure

1. **Identify function groups**: List all functions and group by domain
2. **Count lines per group**: Calculate approximate lines for each domain
3. **Map dependencies**: Identify which functions call each other
4. **Review tests**: Understand test coverage and organization

**Example from bundler_validation.go**:

```bash
# List functions with line numbers
awk '/^func / {print NR": "$0}' bundler_validation.go

# Output:
# 65: func validateNoLocalRequires(bundledContent string) error {
# 99: func validateNoModuleReferences(bundledContent string) error {
# 145: func ValidateEmbeddedResourceRequires(sources map[string]string) error {
# 221: func validateNoExecSync(scriptName string, content string, mode RuntimeMode) error {
# 263: func validateNoGitHubScriptGlobals(scriptName string, content string, mode RuntimeMode) error {
# 320: func validateNoRuntimeMixing(mainScript string, sources map[string]string, targetMode RuntimeMode) error {
# 331: func validateRuntimeModeRecursive(content string, currentPath string, sources map[string]string, targetMode RuntimeMode, checked map[string]bool) error {
# 399: func detectRuntimeMode(content string) RuntimeMode {
# 439: func normalizePath(path string) string {
```

### Step 2: Group by Domain

Organize functions into logical domains based on:
- What they validate (safety, content, runtime)
- When they run (compile-time, registration, bundling)
- Their error semantics (hard errors vs warnings)

**Example grouping**:

| Domain | Functions | Line Range | Purpose |
|--------|-----------|------------|---------|
| **Safety** | `validateNoLocalRequires`<br>`validateNoModuleReferences`<br>`ValidateEmbeddedResourceRequires`<br>`normalizePath` | 65-216 | Bundle safety checks |
| **Script Content** | `validateNoExecSync`<br>`validateNoGitHubScriptGlobals` | 221-315 | Script API usage |
| **Runtime Mode** | `validateNoRuntimeMixing`<br>`validateRuntimeModeRecursive`<br>`detectRuntimeMode` | 320-436 | Runtime compatibility |

### Step 3: Create New Files

For each domain, create a new file following naming convention:

**Naming convention**: `{domain}_{subdomain}_validation.go`

**File structure**:
```go
// Package workflow provides {domain} validation for agentic workflows.
//
// # {Domain} Validation
//
// This file validates {what it validates} to ensure {goal}.
//
// # Validation Functions
//
//   - Function1() - Description
//   - Function2() - Description
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - Criteria 1
//   - Criteria 2
//
// For related validation, see {related_files}.
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md
package workflow

import (
	"fmt"
	"github.com/github/gh-aw/pkg/logger"
)

var {domain}Log = logger.New("workflow:{domain}_validation")

// Validation functions here
```

### Step 4: Move Functions

1. **Copy functions** to new file
2. **Preserve signatures**: Don't change function names or parameters
3. **Update logger**: Create domain-specific logger instance
4. **Move helpers**: Include helper functions used only by this domain
5. **Shared helpers**: Keep shared utilities in original file or create common file

**Example moves**:

```go
// bundler_safety_validation.go
var bundlerSafetyLog = logger.New("workflow:bundler_safety_validation")

func validateNoLocalRequires(bundledContent string) error {
	bundlerSafetyLog.Printf("Validating bundled JavaScript: %d bytes", len(bundledContent))
	// ... rest of function unchanged
}

// bundler_script_validation.go  
var bundlerScriptLog = logger.New("workflow:bundler_script_validation")

func validateNoExecSync(scriptName string, content string, mode RuntimeMode) error {
	bundlerScriptLog.Printf("Validating no execSync in GitHub Script: %s", scriptName)
	// ... rest of function unchanged
}
```

### Step 5: Reorganize Tests

Split test files to match new structure:

1. **Create test files**: One per new validation file
2. **Move test functions**: Group by domain
3. **Preserve test logic**: Don't change test behavior
4. **Update imports**: Ensure tests can access validation functions

**Example test split**:

```go
// bundler_script_validation_test.go
package workflow

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestValidateNoExecSync_GitHubScriptMode(t *testing.T) {
	// Tests for validateNoExecSync
}

func TestValidateNoGitHubScriptGlobals_NodeJSMode(t *testing.T) {
	// Tests for validateNoGitHubScriptGlobals
}
```

### Step 6: Update Documentation

Update these files to reference new validators:

1. **validation.go**: Add new files to package documentation
2. **validation-architecture.md**: Add new validators to architecture section
3. **AGENTS.md**: Update complexity guidelines (if adding new patterns)

**Example documentation update**:

```go
// pkg/workflow/validation.go
//   - bundler_safety_validation.go: JavaScript bundle safety (require/module checks)
//   - bundler_script_validation.go: JavaScript script content (execSync, GitHub globals)
//   - bundler_runtime_validation.go: JavaScript runtime mode compatibility
```

### Step 7: Verify No Functional Changes

1. **Build**: `make build`
2. **Run tests**: `go test ./pkg/workflow`
3. **Check imports**: Verify all callers still work
4. **Test manually**: Run CLI commands that use validation

```bash
# Build and test
make build
go test -v ./pkg/workflow

# Verify specific tests
go test -v ./pkg/workflow -run "TestValidateNo"
```

## Common Patterns

### Pattern 1: Shared Helper Functions

**Problem**: Multiple domains need the same helper function

**Solutions**:

1. **Keep in one domain**: If primarily used by one domain, keep it there
2. **Create common file**: If used equally, create `{domain}_helpers.go`
3. **Move to parent**: If used across packages, move to `pkg/utils/`

**Example**: `normalizePath()` stayed in `bundler_safety_validation.go` because it's only used by `ValidateEmbeddedResourceRequires()` and `validateRuntimeModeRecursive()`, and the latter needs path normalization for safety checks.

### Pattern 2: Pre-compiled Regexes

**Problem**: Regexes compiled at package init need to move

**Solution**: Keep regex compilation in new file where it's used

```go
// Before (bundler_validation.go)
var (
	moduleExportsRegex = regexp.MustCompile(`\bmodule\.exports\b`)
	exportsRegex = regexp.MustCompile(`\bexports\.\w+`)
)

// After (bundler_safety_validation.go)
var (
	moduleExportsRegex = regexp.MustCompile(`\bmodule\.exports\b`)
	exportsRegex = regexp.MustCompile(`\bexports\.\w+`)
)
```

### Pattern 3: Cross-Domain Validation

**Problem**: Validation spans multiple domains

**Solution**: Keep orchestrator in one domain, call helpers from others

```go
// bundler.go (orchestrator)
func BundleJavaScriptWithMode(...) (string, error) {
	// Runtime validation (bundler_runtime_validation.go)
	if err := validateNoRuntimeMixing(mainContent, sources, mode); err != nil {
		return "", err
	}
	
	// Bundle code...
	
	// Safety validation (bundler_safety_validation.go)
	if err := validateNoLocalRequires(bundled); err != nil {
		return "", err
	}
	if err := validateNoModuleReferences(bundled); err != nil {
		return "", err
	}
	
	return bundled, nil
}
```

## Validation Complexity Guidelines

### Target Size

- **100-200 lines**: Ideal size for focused validator
- **200-300 lines**: Acceptable but consider splitting
- **300+ lines**: Should be refactored

### Documentation Standards

- **Minimum 30% comment coverage**: At least 30% of lines should be comments/docs
- **File header**: Describe domain, functions, and when to add validation
- **Function comments**: Explain what, when, and error conditions

### Naming Conventions

- **Files**: `{domain}_{subdomain}_validation.go`
- **Loggers**: `{domain}Log = logger.New("workflow:{domain}_validation")`
- **Functions**: `validate{WhatIsValidated}()`
- **Tests**: `Test{FunctionName}_{Scenario}`

## Example: bundler_validation.go Split

### Before (460 lines)

Single file with 3 domains:
- Bundle safety (152 lines)
- Script content (95 lines)
- Runtime mode (141 lines)

### After (3 files, 580 lines total)

1. **bundler_safety_validation.go** (230 lines)
   - `validateNoLocalRequires()`
   - `validateNoModuleReferences()`
   - `ValidateEmbeddedResourceRequires()`
   - `normalizePath()`

2. **bundler_script_validation.go** (160 lines)
   - `validateNoExecSync()`
   - `validateNoGitHubScriptGlobals()`

3. **bundler_runtime_validation.go** (190 lines)
   - `validateNoRuntimeMixing()`
   - `validateRuntimeModeRecursive()`
   - `detectRuntimeMode()`

### Benefits

- Each file under 250 lines (target: 100-200)
- Clear single responsibility per file
- Easier to test each domain independently
- Simpler to add new validation (clear where it belongs)
- Better documentation organization

## Checklist

When refactoring a large validation file:

- [ ] Analyze current structure (functions, line counts, dependencies)
- [ ] Group functions by domain
- [ ] Create new files with proper headers
- [ ] Move functions preserving signatures
- [ ] Update logger instances
- [ ] Split test files to match new structure
- [ ] Update validation.go documentation
- [ ] Update validation-architecture.md
- [ ] Build project successfully
- [ ] Run all tests successfully
- [ ] Verify no functional changes

## References

- **Implementation**: `pkg/workflow/bundler_*_validation.go`
- **Tests**: `pkg/workflow/bundler_*_validation_test.go`
- **Architecture**: `scratchpad/validation-architecture.md`
- **Guidelines**: `AGENTS.md` (Validation Architecture section)

---

**Last Updated**: 2025-01-02
**Reference Issue**: #8635
