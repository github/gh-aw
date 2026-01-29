# String Sanitization vs Normalization Patterns

This document clarifies the distinction between "sanitize" and "normalize" string processing functions in the gh-aw codebase and provides guidance on when to use each pattern.

## Overview

The codebase uses two distinct patterns for string processing with different purposes:

### Sanitize Pattern: Character Validity

**Purpose**: Remove or replace invalid characters to create valid identifiers, file names, or artifact names.

**When to use**: When you need to ensure a string contains only valid characters for a specific context (identifiers, YAML artifact names, filesystem paths).

**What it does**:
- Removes special characters that are invalid in the target context
- Replaces separators (colons, slashes, spaces) with hyphens
- Converts to lowercase for consistency
- May preserve certain characters (dots, underscores) based on configuration

### Normalize Pattern: Format Standardization

**Purpose**: Standardize format by removing extensions, converting between conventions, or applying consistent formatting rules.

**When to use**: When you need to convert between different representations of the same logical entity (e.g., file extensions, naming conventions).

**What it does**:
- Removes file extensions (.md, .lock.yml)
- Converts between naming conventions (dashes to underscores)
- Standardizes identifiers to a canonical form
- Does NOT validate character validity (assumes input is already valid)

## Function Reference

### Sanitize Functions

#### `SanitizeName(name string, opts *SanitizeOptions) string`
**Location**: `pkg/workflow/strings.go`

Configurable sanitization function that serves as the foundation for other sanitize functions.

**Example**:
```go
// Preserve dots and underscores
opts := &SanitizeOptions{
    PreserveSpecialChars: []rune{'.', '_'},
}
result := SanitizeName("My.Workflow_Name:Test", opts)
// Returns: "my.workflow_name-test"

// Trim hyphens and use default for empty results
opts := &SanitizeOptions{
    TrimHyphens:  true,
    DefaultValue: "default-name",
}
result := SanitizeName("@@@", opts)
// Returns: "default-name"
```text

**Use case**: When you need custom sanitization behavior with specific character preservation rules.

#### `SanitizeWorkflowName(name string) string`
**Location**: `pkg/workflow/strings.go`

Sanitizes workflow names for use in artifact names and file paths.

**Example**:
```go
result := SanitizeWorkflowName("My Workflow: Test/Build")
// Returns: "my-workflow-test-build"
```text

**Use case**: Artifact names, file paths where dots and underscores are valid.

#### `SanitizeIdentifier(name string) string`
**Location**: `pkg/workflow/workflow_name.go`

Creates clean identifiers for user agent strings and similar contexts.

**Example**:
```go
result := SanitizeIdentifier("My Workflow")
// Returns: "my-workflow"

result := SanitizeIdentifier("@@@")
// Returns: "github-agentic-workflow" (default)
```text

**Use case**: User agent strings, identifiers that must be purely alphanumeric with hyphens.

### Normalize Functions

#### `normalizeWorkflowName(name string) string`
**Location**: `pkg/workflow/resolve.go`

Removes file extensions to get the base workflow identifier.

**Example**:
```go
result := normalizeWorkflowName("weekly-research.md")
// Returns: "weekly-research"

result := normalizeWorkflowName("weekly-research.lock.yml")
// Returns: "weekly-research"

result := normalizeWorkflowName("weekly-research")
// Returns: "weekly-research"
```text

**Use case**: Converting between workflow file names and workflow IDs.

#### `normalizeSafeOutputIdentifier(identifier string) string`
**Location**: `pkg/workflow/safe_outputs.go`

Converts dashes to underscores for safe output identifiers.

**Example**:
```go
result := normalizeSafeOutputIdentifier("create-issue")
// Returns: "create_issue"

result := normalizeSafeOutputIdentifier("add-comment")
// Returns: "add_comment"
```text

**Use case**: Ensuring consistency in safe output identifiers while remaining resilient to LLM-generated variations.

## Decision Tree

```text
Need to process a string?
│
├─ Need to ensure character validity? → Use SANITIZE
│  ├─ Artifact name / file path → SanitizeWorkflowName()
│  ├─ Identifier / user agent → SanitizeIdentifier()
│  └─ Custom requirements → SanitizeName() with options
│
└─ Need to standardize format? → Use NORMALIZE
   ├─ Remove file extensions → normalizeWorkflowName()
   └─ Convert conventions → normalizeSafeOutputIdentifier()
```text

## Common Patterns

### Pattern 1: User Input to Valid Identifier

When accepting user input that needs to become a valid identifier:

```go
// User provides: "My-Project: Feature/Test"
sanitized := SanitizeIdentifier("My-Project: Feature/Test")
// Result: "my-project-feature-test"
```text

### Pattern 2: Workflow File Resolution

When converting between workflow file names and IDs:

```go
// User provides: "weekly-research.md" or "weekly-research"
normalized := normalizeWorkflowName(userInput)
// Result: "weekly-research" (base identifier)

// Use normalized ID to find files:
// - .github/workflows/weekly-research.md
// - .github/workflows/weekly-research.lock.yml
```text

### Pattern 3: Safe Output Identifier Consistency

When handling safe output identifiers that may use different conventions:

```go
// From YAML: "create-issue" or "create_issue"
normalized := normalizeSafeOutputIdentifier(identifier)
// Result: "create_issue" (consistent internal format)
```text

## Anti-Patterns

### ❌ Don't sanitize already-normalized strings

```go
// BAD: Sanitizing a normalized workflow name
normalized := normalizeWorkflowName("weekly-research.md")
// normalized = "weekly-research"
sanitized := SanitizeWorkflowName(normalized) // Unnecessary!
```text

**Why**: Normalization produces valid identifiers. Sanitizing again adds unnecessary processing and may produce unexpected results if the normalize function's output changes.

### ❌ Don't normalize for character validity

```go
// BAD: Using normalize for invalid characters
userInput := "My Workflow: Test/Build"
normalized := normalizeWorkflowName(userInput) // Wrong tool!
// normalized = "My Workflow: Test/Build" (unchanged - invalid chars remain)
```text

**Why**: Normalize functions don't validate or fix character validity. Use sanitize functions for this purpose.

### ❌ Don't mix sanitize and normalize without understanding

```go
// QUESTIONABLE: Mixing both without clear purpose
input := "my-workflow.md"
normalized := normalizeWorkflowName(input) // "my-workflow"
sanitized := SanitizeWorkflowName(normalized) // "my-workflow"
// Result is same as just normalizing - sanitize was unnecessary
```text

**Why**: If the input is already a valid workflow file name, normalizing is sufficient. Only sanitize if the input might contain invalid characters.

## Best Practices

1. **Choose the right tool**: Use sanitize for character validity, normalize for format standardization.

2. **Don't double-process**: If normalize produces a valid identifier, don't sanitize it again.

3. **Document intent**: When using these functions, add comments explaining which pattern you're using and why.

4. **Validate assumptions**: If you assume input is already valid, document that assumption.

5. **Consider defaults**: Use `SanitizeIdentifier` when you need a fallback default value for empty results.

## Testing Guidelines

When testing string processing:

1. **Test character validity separately from format**: 
   - Sanitize tests should check invalid character removal
   - Normalize tests should check format conversion

2. **Test edge cases**:
   - Empty strings
   - Strings with only special characters
   - Already-valid strings
   - Strings with multiple consecutive separators

3. **Test idempotency**:
   - Sanitizing an already-sanitized string should produce the same result
   - Normalizing an already-normalized string should produce the same result

## Examples from Codebase

### Example 1: Workflow Name Resolution (resolve.go)

```go
// ResolveWorkflowName converts user input to workflow name
func ResolveWorkflowName(workflowInput string) (string, error) {
    // First: normalize to remove extensions and get base ID
    normalizedName := normalizeWorkflowName(workflowInput)
    // normalizedName is now a clean identifier like "weekly-research"
    
    // Then: use normalized name to locate files
    mdFile := filepath.Join(workflowsDir, normalizedName+".md")
    lockFile := filepath.Join(workflowsDir, normalizedName+".lock.yml")
    
    // No sanitization needed - normalizeWorkflowName output is already valid
    // ...
}
```text

**Pattern used**: NORMALIZE (format standardization)  
**Why**: Input is a file name or workflow ID that needs extension removal, not character validation.

### Example 2: User Agent Creation (workflow_name.go)

```go
// SanitizeIdentifier creates a user agent string from workflow name
func SanitizeIdentifier(name string) string {
    return SanitizeName(name, &SanitizeOptions{
        PreserveSpecialChars: []rune{},      // No special chars allowed
        TrimHyphens:          true,          // Clean edges
        DefaultValue:         "github-agentic-workflow", // Fallback
    })
}
```text

**Pattern used**: SANITIZE (character validity)  
**Why**: Input might contain invalid characters that need to be removed for a valid identifier.

### Example 3: Safe Output Processing (safe_outputs.go)

```go
// normalizeSafeOutputIdentifier ensures consistent format
func normalizeSafeOutputIdentifier(identifier string) string {
    // Convert dashes to underscores: "create-issue" -> "create_issue"
    return strings.ReplaceAll(identifier, "-", "_")
}
```text

**Pattern used**: NORMALIZE (format standardization)  
**Why**: Both formats are valid, but internal representation uses underscores for consistency.

## Related Documentation

- `pkg/workflow/strings.go` - Core sanitization functions and configuration
- `pkg/workflow/workflow_name.go` - Identifier sanitization
- `pkg/workflow/resolve.go` - Workflow name resolution and normalization
- `pkg/workflow/safe_outputs.go` - Safe output identifier normalization

## History

- Issue #4184: Semantic analysis identified overlapping string processing concerns
- This specification created to clarify the distinction between sanitize and normalize patterns
- Documentation added to provide clear guidance for future contributors
