---
description: Type patterns and best practices for GitHub Agentic Workflows
---

# Go Type Patterns and Best Practices

This document describes the type patterns used throughout the GitHub Agentic Workflows codebase and provides guidance on when and how to use different typing approaches.

## Table of Contents

- [Semantic Type Aliases](#semantic-type-aliases)
- [Dynamic YAML/JSON Handling](#dynamic-yamljson-handling)
- [Interface Patterns](#interface-patterns)
- [Type Safety Guidelines](#type-safety-guidelines)
- [Anti-Patterns](#anti-patterns)

---

## Semantic Type Aliases

Semantic type aliases provide meaningful names for primitive types, improving code clarity and preventing mistakes through type safety.

### Pattern: Semantic Type Alias

**Purpose**: Distinguish different uses of the same primitive type with meaningful names

**Implementation**:
```go
// LineLength represents a line length in characters for expression formatting
type LineLength int

// Version represents a software version string
type Version string
```

**Benefits**:
- Self-documenting code - the type name explains the purpose
- Type safety - prevents mixing different concepts that share the same underlying type
- Clear intent - signals to readers what the value represents
- Easy refactoring - can change underlying implementation without affecting API

### Examples in Codebase

#### LineLength Type

**Location**: `pkg/constants/constants.go`

**Purpose**: Represents character counts for formatting decisions

```go
// LineLength represents a line length in characters for expression formatting
type LineLength int

// String returns the string representation of the line length
func (l LineLength) String() string {
	return fmt.Sprintf("%d", l)
}

// IsValid returns true if the line length is positive
func (l LineLength) IsValid() bool {
	return l > 0
}

// MaxExpressionLineLength is the maximum length for a single line expression
const MaxExpressionLineLength LineLength = 120

// ExpressionBreakThreshold is the threshold for breaking long lines
const ExpressionBreakThreshold LineLength = 100
```

**Usage**:
```go
// Clear intent - these are lengths, not arbitrary integers
if len(expression) > int(constants.MaxExpressionLineLength) {
    // Break into multiple lines
}

// Helper methods provide validation and string conversion
if constants.MaxExpressionLineLength.IsValid() {
    fmt.Println(constants.MaxExpressionLineLength.String()) // "120"
}
```

#### Version Type

**Location**: `pkg/constants/constants.go`

**Purpose**: Represents software version strings

```go
// Version represents a software version string
type Version string

// String returns the string representation of the version
func (v Version) String() string {
	return string(v)
}

// IsValid returns true if the version is non-empty
func (v Version) IsValid() bool {
	return len(v) > 0
}

// DefaultCopilotVersion is the default version of the GitHub Copilot CLI
const DefaultCopilotVersion Version = "0.0.374"

// DefaultClaudeCodeVersion is the default version of the Claude Code CLI
const DefaultClaudeCodeVersion Version = "2.0.76"
```

**Benefits**:
- Distinguishes version strings from arbitrary strings
- Makes version requirements explicit in function signatures
- Enables future validation logic (e.g., semver parsing)
- Provides helper methods for validation and string conversion

#### WorkflowID Type

**Location**: `pkg/constants/constants.go`

**Purpose**: Represents workflow identifiers (basename without .md extension)

```go
// WorkflowID represents a workflow identifier (basename without .md extension)
type WorkflowID string

// String returns the string representation of the workflow ID
func (w WorkflowID) String() string {
	return string(w)
}

// IsValid returns true if the workflow ID is non-empty
func (w WorkflowID) IsValid() bool {
	return len(w) > 0
}
```

**Benefits**:
- Distinguishes workflow identifiers from file paths or arbitrary strings
- Prevents mixing workflow IDs with other string types
- Makes workflow operations explicit in function signatures
- Type-safe workflow identifier handling

**Usage**:
```go
func GetWorkflow(id WorkflowID) (*Workflow, error) { ... }
func CompileWorkflow(id WorkflowID) error { ... }

// Clear intent - this is a workflow identifier, not a file path
workflowID := WorkflowID("ci-doctor")
if workflowID.IsValid() {
    err := CompileWorkflow(workflowID)
}
```

#### EngineName Type

**Location**: `pkg/constants/constants.go`

**Purpose**: Represents AI engine name identifiers (copilot, claude, codex, custom)

```go
// EngineName represents an AI engine name identifier
type EngineName string

// String returns the string representation of the engine name
func (e EngineName) String() string {
	return string(e)
}

// IsValid returns true if the engine name is non-empty
func (e EngineName) IsValid() bool {
	return len(e) > 0
}

// Engine name constants for type safety
const (
	CopilotEngine EngineName = "copilot"
	ClaudeEngine  EngineName = "claude"
	CodexEngine   EngineName = "codex"
	CustomEngine  EngineName = "custom"
)
```

**Benefits**:
- Distinguishes engine names from arbitrary strings
- Prevents typos in engine name references
- Makes engine selection explicit and type-safe
- Provides compile-time validation for engine constants
- Single source of truth for engine identifiers

**Usage**:
```go
func SetEngine(engine EngineName) error { ... }
func ValidateEngine(engine EngineName) bool { ... }

// Type-safe engine selection
engine := CopilotEngine
if engine.IsValid() {
    err := SetEngine(engine)
}

// Prevents mixing engine names with other strings
// engine := "some-random-string" // Would require explicit conversion
engine := EngineName("copilot")   // Explicit conversion when needed
```

#### Feature Flag Constants

**Location**: `pkg/workflow/gateway.go`, `pkg/workflow/safe_inputs.go`

**Purpose**: Named constants for feature flag identifiers

```go
// MCPGatewayFeatureFlag is the feature flag name for enabling MCP gateway
const MCPGatewayFeatureFlag = "mcp-gateway"

// SafeInputsFeatureFlag is the name of the feature flag for safe-inputs
const SafeInputsFeatureFlag = "safe-inputs"
```

**Benefits**:
- Single source of truth for feature flag names
- Prevents typos when checking feature flags
- Supports IDE navigation to find all usages

#### Tool Configuration Types

**Location**: `pkg/workflow/tools_types.go`

**Purpose**: Type-safe tool configuration using semantic types and typed slices

Tool configurations demonstrate the pattern of combining semantic types with typed slices to provide compile-time type safety while maintaining clean APIs.

**Semantic Types for Tool Names**:

```go
// GitHubToolName represents a GitHub tool name (e.g., "issue_read", "create_issue")
type GitHubToolName string

// GitHubToolset represents a GitHub toolset name (e.g., "default", "repos", "issues")
type GitHubToolset string
```

**Typed Slices for Collections**:

```go
// GitHubAllowedTools is a slice of GitHub tool names
type GitHubAllowedTools []GitHubToolName

// ToStringSlice converts GitHubAllowedTools to []string
func (g GitHubAllowedTools) ToStringSlice() []string {
    result := make([]string, len(g))
    for i, tool := range g {
        result[i] = string(tool)
    }
    return result
}

// GitHubToolsets is a slice of GitHub toolset names
type GitHubToolsets []GitHubToolset

// ToStringSlice converts GitHubToolsets to []string
func (g GitHubToolsets) ToStringSlice() []string {
    result := make([]string, len(g))
    for i, toolset := range g {
        result[i] = string(toolset)
    }
    return result
}
```

**Usage in Configuration Structs**:

```go
// GitHubToolConfig represents the configuration for the GitHub tool
type GitHubToolConfig struct {
    Allowed     GitHubAllowedTools `yaml:"allowed,omitempty"`
    Mode        string             `yaml:"mode,omitempty"`
    Version     string             `yaml:"version,omitempty"`
    Args        []string           `yaml:"args,omitempty"`
    ReadOnly    bool               `yaml:"read-only,omitempty"`
    GitHubToken string             `yaml:"github-token,omitempty"`
    Toolset     GitHubToolsets     `yaml:"toolsets,omitempty"`
    Lockdown    bool               `yaml:"lockdown,omitempty"`
}
```

**Benefits**:
- **Type safety**: Prevents mixing tool names with arbitrary strings
- **Self-documenting**: Type names make intent clear (e.g., `GitHubToolName` vs `string`)
- **Conversion helpers**: `ToStringSlice()` methods enable interoperability with legacy code
- **Compile-time validation**: Mismatched types caught at compile time, not runtime
- **IDE support**: Better autocomplete and navigation for tool names

**Migration Pattern - Before/After**:

```go
// ❌ BEFORE - Using []any and map[string]any
type GitHubToolConfig struct {
    Allowed []any          // Could be any type - no compile-time safety
    Toolset []any          // What values are valid?
}

func processTools(config map[string]any) {
    // Need runtime type assertions everywhere
    if allowed, ok := config["allowed"].([]any); ok {
        for _, tool := range allowed {
            if toolStr, ok := tool.(string); ok {
                // Finally have a string, but could be invalid tool name
                processTool(toolStr)
            }
        }
    }
}

// ✅ AFTER - Using semantic types and typed slices
type GitHubToolConfig struct {
    Allowed GitHubAllowedTools  // Clear what this contains
    Toolset GitHubToolsets      // Clear what this contains
}

func processTools(config *GitHubToolConfig) {
    // Type-safe access, no assertions needed
    for _, tool := range config.Allowed {
        // tool is GitHubToolName, not just any string
        processTool(string(tool))
    }
}
```

**When to Use Typed Slices vs `[]any`**:

✅ **Use typed slices (e.g., `GitHubAllowedTools`) when:**
- The slice contains elements of a known, consistent type
- You want compile-time type safety
- The elements represent a specific domain concept (e.g., tool names, toolsets)
- You need helper methods on the slice (e.g., `ToStringSlice()`)
- The slice is part of a configuration struct used across the codebase

❌ **Use `[]any` when:**
- The slice genuinely contains mixed types (e.g., YAML parsing where values can be string, int, bool)
- You're parsing external data with unknown structure
- The values are truly dynamic and can't be typed at compile time
- You're working with legacy APIs that require `[]any`

**Example - Parsing Dynamic to Typed**:

```go
// Parse dynamic YAML input
toolsMap := map[string]any{
    "github": map[string]any{
        "allowed": []any{"issue_read", "create_issue"},  // Dynamic from YAML
    },
}

// Convert to typed configuration
func parseGitHubConfig(data map[string]any) (*GitHubToolConfig, error) {
    config := &GitHubToolConfig{}
    
    if allowed, ok := data["allowed"].([]any); ok {
        // Convert []any to GitHubAllowedTools
        for _, item := range allowed {
            if str, ok := item.(string); ok {
                config.Allowed = append(config.Allowed, GitHubToolName(str))
            }
        }
    }
    
    return config, nil
}

// Now use type-safe configuration
func processConfig(config *GitHubToolConfig) {
    for _, tool := range config.Allowed {
        // Type-safe iteration, no assertions needed
        fmt.Printf("Processing tool: %s\n", tool)
    }
}
```

### When to Use Semantic Type Aliases

✅ **Use semantic type aliases when:**

- You have a primitive type that represents a specific concept (e.g., `LineLength`, `Version`, `WorkflowID`, `EngineName`)
- Multiple unrelated concepts share the same primitive type (prevents confusion)
- You want to prevent mixing incompatible values (type safety)
- The type name adds clarity that a comment alone wouldn't provide
- Future validation logic might be needed
- The concept is used frequently across the codebase (e.g., workflow identifiers, engine names)

❌ **Don't use semantic type aliases when:**

- The primitive type is already clear from context
- It's a one-off usage without reuse
- The type would be overly specific (prefer composition)
- It adds ceremony without clarity

### Best Practices for Type Aliases

1. **Document the purpose**: Always include a comment explaining what the type represents

```go
// ✅ GOOD - Clear purpose
// LineLength represents a line length in characters for expression formatting
type LineLength int

// ❌ BAD - No explanation
type LineLength int
```

2. **Use descriptive names**: The name should indicate both what it is and how it's used

```go
// ✅ GOOD - Indicates both content and purpose
type Version string
type LineLength int

// ❌ BAD - Too generic
type String string
type Number int
```

3. **Provide constants with the type**: Define common values using the type

```go
// ✅ GOOD - Constants use the semantic type
const MaxExpressionLineLength LineLength = 120

// ❌ BAD - Constants use primitive type
const MaxExpressionLineLength = 120  // type: int, should be LineLength
```

4. **Add helper methods where useful**: Provide String() and IsValid() methods for common operations

```go
// ✅ GOOD - Helper methods for common operations
func (v Version) String() string {
	return string(v)
}

func (v Version) IsValid() bool {
	return len(v) > 0
}

// Usage
if version.IsValid() {
	fmt.Println(version.String())
}
```

5. **Convert explicitly**: Make type conversions explicit in code

```go
// ✅ GOOD - Explicit conversion
if len(line) > int(constants.MaxExpressionLineLength) {
    // ...
}

// ❌ BAD - Implicit conversion won't compile
if len(line) > constants.MaxExpressionLineLength {  // Type mismatch
    // ...
}
```

---

## Dynamic YAML/JSON Handling

When parsing YAML or JSON with dynamic/unknown structures, `map[string]any` is the appropriate choice.

### Pattern: Dynamic Data Structures

**Purpose**: Handle configuration or data with unknown structure at compile time

**When to Use**:
- Parsing YAML/JSON frontmatter from markdown files
- Processing user-provided configuration
- Working with GitHub Actions workflow YAML (dynamic fields)
- Intermediate representation during compilation

### Examples in Codebase

#### Frontmatter Parsing

**Location**: `pkg/parser/frontmatter.go`

**Purpose**: Parse workflow frontmatter which has dynamic structure

```go
// ImportInputs aggregates input values from all imports
// Uses map[string]any because input values can be string, number, or boolean
ImportInputs map[string]any // key = input name, value = input value

// ImportSpec represents a single import with optional inputs
type ImportSpec struct {
    Path   string         // Import path (required)
    Inputs map[string]any // Optional input values (string, number, or boolean)
}

// ProcessImportsFromFrontmatter processes dynamic imports field
func ProcessImportsFromFrontmatter(
    frontmatter map[string]any,  // Dynamic frontmatter structure
    baseDir string,
) (mergedTools string, mergedEngines []string, err error) {
    // Parse dynamic YAML structure...
}
```text

**Why `map[string]any`**:
- Frontmatter structure varies by workflow
- Input values can be different types (string, number, boolean)
- Schema validation happens separately
- Allows flexible configuration without code changes

#### Tool Configuration

**Location**: `pkg/workflow/permissions_validator.go`

**Purpose**: Handle dynamic GitHub MCP tool configuration

```go
// ValidatePermissions accepts any type for GitHub tool config
// because the structure varies based on tool configuration
func ValidatePermissions(
    permissions *Permissions,
    githubTool any,  // Could be map, struct, or nil
) *PermissionsValidationResult {
    // Extract toolsets from dynamic configuration...
}
```

**Why `any`**:
- Tool configuration structure not known at compile time
- Different tools have different configuration schemas
- Enables runtime type inspection and extraction

### Best Practices for Dynamic Types

1. **Document why `any` is used**: Explain the dynamic nature

```go
// ✅ GOOD - Explains why any is necessary
// githubTool uses any because the tool configuration structure
// varies based on the engine and toolsets being used
func ValidatePermissions(permissions *Permissions, githubTool any)

// ❌ BAD - No explanation
func ValidatePermissions(permissions *Permissions, githubTool any)
```

2. **Validate early**: Convert from `any` to typed structures ASAP

```go
// ✅ GOOD - Extract and validate immediately
func ProcessConfig(config any) error {
    configMap, ok := config.(map[string]any)
    if !ok {
        return fmt.Errorf("expected map, got %T", config)
    }
    
    // Now work with typed data
    name, _ := configMap["name"].(string)
    // ...
}
```

3. **Use type assertions safely**: Always check the boolean return

```go
// ✅ GOOD - Check assertion success
value, ok := data["key"].(string)
if !ok {
    return fmt.Errorf("expected string")
}

// ❌ BAD - Panic on type mismatch
value := data["key"].(string)  // Can panic!
```

4. **Prefer specific types when structure is known**: Only use `any` when truly dynamic

```go
// ✅ GOOD - Known structure uses typed struct
type ToolConfig struct {
    Name    string
    Version string
    Options map[string]any  // Only options are dynamic
}

// ❌ BAD - Using any when structure is known
type ToolConfig map[string]any
```

---

## Interface Patterns

Interfaces define behavior contracts and enable polymorphism. Several interface patterns exist in the codebase.

### Pattern: Behavior Contract Interface

**Purpose**: Define what a type can do, not what it is

**Example**: `CodingAgentEngine` Interface

**Location**: `pkg/workflow/agentic_engine.go`

```go
// CodingAgentEngine defines the interface for AI coding engines
type CodingAgentEngine interface {
    // GetName returns the engine name (e.g., "copilot", "claude", "codex")
    GetName() string
    
    // GenerateSteps creates workflow steps for this engine
    GenerateSteps(config EngineConfig) ([]Step, error)
}
```text

**Benefits**:
- Multiple engine implementations (Copilot, Claude, Codex)
- New engines can be added by implementing the Engine interface
- Testable with mock implementations
- Clear contract for engine behavior

### Pattern: Configuration Interface

**Purpose**: Allow different configuration sources with common interface

**Example**: `ToolConfig` Interface

**Location**: `pkg/workflow/mcp-config.go`

```go
// ToolConfig represents the common interface for tool configurations
type ToolConfig interface {
    GetName() string
    GetType() string
    Validate() error
}
```text

**Benefits**:
- Different tools can have different configuration structures
- Common validation interface
- Type-safe tool access through interface

### When to Use Interfaces

✅ **Use interfaces when:**

- Multiple types need to implement the same behavior
- You want to enable testing with mocks
- You need polymorphism (different implementations of same contract)
- You want to decouple implementation from usage
- The behavior is more important than the data structure

❌ **Don't use interfaces when:**

- Only one implementation exists and no others are planned
- The data structure itself is the interface (use structs)
- It adds indirection without benefit
- A single-purpose function returning one type would suffice

### Interface Best Practices

1. **Keep interfaces small**: Prefer many small interfaces over large ones

```go
// ✅ GOOD - Small, focused interface
type Validator interface {
    Validate() error
}

type Namer interface {
    GetName() string
}

// ❌ BAD - Kitchen sink interface
type Tool interface {
    Validate() error
    GetName() string
    GetVersion() string
    Execute() error
    Cleanup() error
    // ... many more methods
}
```

2. **Define interfaces where they're used**: Consumers define interfaces they need

```go
// ✅ GOOD - Interface defined where used
// pkg/workflow/compiler.go
type Validator interface {
    Validate() error
}

func Compile(v Validator) error {
    if err := v.Validate(); err != nil {
        return err
    }
    // ...
}

// ❌ BAD - Interface defined far from usage
// pkg/types/interfaces.go
type Validator interface {
    Validate() error
}
```text

3. **Document interface contracts**: Explain what implementations must do

```go
// ✅ GOOD - Clear documentation
// CodingAgentEngine defines the interface for AI coding engines.
// Implementations must:
// - Return a unique lowercase name
// - Generate valid GitHub Actions workflow steps
// - Handle errors gracefully
type CodingAgentEngine interface {
    GetName() string
    GenerateSteps(config EngineConfig) ([]Step, error)
}
```

---

## Type Safety Guidelines

### Use `any` Sparingly

The type `any` (alias for `interface{}`) should be used only when necessary:

**Valid uses of `any`**:
- Parsing dynamic YAML/JSON structures
- Generic utility functions that work with multiple types
- Reflection-based code
- Interfacing with external libraries that require `interface{}`

**Avoid `any` for**:
- Function parameters when the type is known
- Return values when the type is known
- Struct fields when the structure is fixed
- Map values when the value type is consistent

### Type Conversion Safety

Always use safe type assertions:

```go
// ✅ GOOD - Safe type assertion with check
value, ok := data["key"].(string)
if !ok {
    return fmt.Errorf("expected string, got %T", data["key"])
}

// ❌ BAD - Unsafe assertion (can panic)
value := data["key"].(string)
```

### Avoid Type Name Collisions

When creating new types, check for existing types with similar names:

```go
// ✅ GOOD - Distinct, descriptive names
type WorkflowPermissions struct { /* ... */ }
type UserPermissions struct { /* ... */ }
type RepositoryPermissions struct { /* ... */ }

// ❌ BAD - Generic names that might collide
type Permissions struct { /* ... */ }  // Which permissions?
type Config struct { /* ... */ }       // Which config?
```

**Best practices**:
- Use package-qualified access when importing types
- Prefix types with their domain/purpose
- Run `go build` to catch name collisions early
- Use IDE tools to search for existing type names

---

## Anti-Patterns

### ❌ Anti-Pattern 1: Overusing `any`

**Problem**: Using `any` when the type is known leads to runtime errors and poor maintainability

```go
// ❌ BAD - Using any when type is known
func ProcessConfig(config any) error {
    // Have to type assert everywhere
    name := config.(map[string]any)["name"].(string)
    version := config.(map[string]any)["version"].(string)
    // ...
}

// ✅ GOOD - Use typed struct
type Config struct {
    Name    string
    Version string
}

func ProcessConfig(config Config) error {
    // Type-safe access
    name := config.Name
    version := config.Version
    // ...
}
```

### ❌ Anti-Pattern 2: Using Primitive Types for Domain Concepts

**Problem**: Primitive types don't convey meaning and enable mistakes

```go
// ❌ BAD - Callers may confuse timeout and retry count
func CallAPI(url string, timeout int, retries int) error {
    // Which int is which?
    CallAPI("https://api.example.com", 3, 5)  // Wrong order!
}

// ✅ GOOD - Semantic types prevent confusion
type Timeout time.Duration
type RetryCount int

func CallAPI(url string, timeout Timeout, retries RetryCount) error {
    // Type mismatch caught at compile time
    CallAPI("https://api.example.com", Timeout(3), RetryCount(5))
}
```

### ❌ Anti-Pattern 3: God Interfaces

**Problem**: Large interfaces with many methods are hard to implement and test

```go
// ❌ BAD - Too many responsibilities
type WorkflowProcessor interface {
    Parse() error
    Validate() error
    Compile() error
    Deploy() error
    Monitor() error
    Rollback() error
}

// ✅ GOOD - Small, focused interfaces
type Parser interface {
    Parse() error
}

type Validator interface {
    Validate() error
}

type Compiler interface {
    Compile() error
}
```

### ❌ Anti-Pattern 4: Unnecessary Type Aliases

**Problem**: Creating type aliases that don't add value

```go
// ❌ BAD - Doesn't add clarity
type String string
type Integer int
type MyError error

// ✅ GOOD - Semantic meaning
type URL string
type Port int
type ValidationError error
```

### ❌ Anti-Pattern 5: Using `interface{}` Instead of `any`

**Problem**: Using `interface{}` when `any` is clearer and more modern (Go 1.18+)

```go
// ❌ BAD - Using old interface{} syntax
func Process(data interface{}) error {
    // ...
}

// ✅ GOOD - Using modern any alias
func Process(data any) error {
    // ...
}
```

**Note**: The codebase standard is to **always use `any` instead of `interface{}`**

---

## Summary

### Quick Decision Tree

```text
Need to represent a value?
│
├─ Is structure known at compile time?
│  ├─ YES → Use typed struct or specific type
│  └─ NO → Use map[string]any or any
│
├─ Is it a primitive with semantic meaning?
│  ├─ YES → Use semantic type alias (e.g., LineLength, Version)
│  └─ NO → Use primitive type directly
│
├─ Need polymorphism?
│  ├─ YES → Define interface for behavior
│  └─ NO → Use concrete type
│
└─ Handling external data (YAML/JSON)?
   ├─ YES → Use map[string]any initially, validate and convert
   └─ NO → Use specific types
```

### Key Principles

1. **Prefer specific types** over generic ones
2. **Use semantic type aliases** for domain concepts
3. **Use `any` only for truly dynamic data** (YAML/JSON parsing)
4. **Keep interfaces small and focused**
5. **Document type choices**, especially when using `any`
6. **Always use `any` instead of `interface{}`** (Go 1.18+ standard)
7. **Validate and convert** from dynamic types early
8. **Avoid type name collisions** with descriptive names

### Common Patterns Summary

| Pattern | When to Use | Example |
|---------|-------------|---------|
| Semantic Type Alias | Domain-specific primitives | `type LineLength int`, `type WorkflowID string`, `type EngineName string`, `type GitHubToolName string` |
| Typed Slices | Collections of semantic types | `type GitHubAllowedTools []GitHubToolName`, `type GitHubToolsets []GitHubToolset` |
| `map[string]any` | Dynamic YAML/JSON parsing | Frontmatter, tool configs |
| Behavior Interface | Multiple implementations | `CodingAgentEngine` |
| Configuration Interface | Varied config structures | `ToolConfig` |
| Named Constants | Feature flags, identifiers | `MCPGatewayFeatureFlag`, `CopilotEngine` |

### Semantic Type Examples by Domain

| Domain | Type | Example Constants |
|--------|------|-------------------|
| **Measurements** | `LineLength` | `MaxExpressionLineLength`, `ExpressionBreakThreshold` |
| **Versions** | `Version` | `DefaultCopilotVersion`, `DefaultClaudeCodeVersion` |
| **Workflows** | `WorkflowID` | (user-provided workflow identifiers) |
| **AI Engines** | `EngineName` | `CopilotEngine`, `ClaudeEngine`, `CodexEngine`, `CustomEngine` |
| **Feature Flags** | `FeatureFlag` | `SafeInputsFeatureFlag`, `MCPGatewayFeatureFlag` |
| **URLs** | `URL` | `DefaultMCPRegistryURL` |
| **Models** | `ModelName` | `DefaultCopilotDetectionModel` |
| **GitHub Actions** | `JobName`, `StepID` | `AgentJobName`, `CheckMembershipStepID` |
| **CLI** | `CommandPrefix` | `CLIExtensionPrefix` |
| **Tool Configuration** | `GitHubToolName`, `GitHubToolset` | (typed tool names and toolsets) |
| **Tool Collections** | `GitHubAllowedTools`, `GitHubToolsets` | (typed slices with conversion helpers) |

---

**Last Updated**: 2026-01-20
