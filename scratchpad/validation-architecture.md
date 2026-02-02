# Validation Architecture

## Overview

The validation system in GitHub Agentic Workflows ensures that workflow configurations are correct, secure, and compatible with GitHub Actions before compilation. Validation is organized into two main patterns:

1. **Centralized validation** - General-purpose validation in `validation.go`
2. **Domain-specific validation** - Specialized validation in dedicated files

This architecture balances maintainability with domain expertise, allowing validation logic to live close to the code it validates while keeping common patterns centralized.

## File Organization

### Centralized Validation: `validation.go`

**Location**: `pkg/workflow/validation.go` (782 lines)

**Purpose**: General-purpose validation that applies across the entire workflow system

**Validation Functions**:
- `validateExpressionSizes()` - Ensures GitHub Actions expression size limits
- `validateContainerImages()` - Verifies Docker images exist and are accessible
- `validateRuntimePackages()` - Validates runtime package dependencies
- `validateGitHubActionsSchema()` - Validates against GitHub Actions YAML schema
- `validateNoDuplicateCacheIDs()` - Ensures unique cache identifiers
- `validateSecretReferences()` - Validates secret reference syntax
- `validateRepositoryFeatures()` - Checks repository capabilities (issues, discussions)
- `validateHTTPTransportSupport()` - Validates HTTP transport configuration for engines
- `validateMaxTurnsSupport()` - Validates max-turns engine compatibility
- `validateWebSearchSupport()` - Validates web search tool support
- `validateAgentFile()` - Validates custom agent file configuration
- `validateWorkflowRunBranches()` - Validates workflow run branch configuration

**When to add validation here**:
- ✅ Cross-cutting concerns that span multiple domains
- ✅ Core workflow integrity checks
- ✅ GitHub Actions compatibility validation
- ✅ General schema and configuration validation
- ✅ Repository-level feature detection

### Domain-Specific Validation Files

Domain-specific validation is organized into separate files based on functional area:

#### 1. **Strict Mode Validation**: `strict_mode_validation.go`

**Location**: `pkg/workflow/strict_mode_validation.go` (190 lines)

**Purpose**: Enforces security and safety constraints in strict mode

**Validation Functions**:
- `validateStrictMode()` - Main strict mode orchestrator
- `validateStrictPermissions()` - Refuses write permissions
- `validateStrictNetwork()` - Requires explicit network configuration
- `validateStrictMCPNetwork()` - Requires network config on custom MCP servers
- `validateStrictBashTools()` - Refuses bash wildcard tools

**Pattern**: Security policy enforcement with progressive validation

**Architecture**: All strict mode validation logic is consolidated in a single file following the `*_validation.go` naming pattern used throughout the codebase

**When to add validation here**:
- ✅ Strict mode security policies
- ✅ Permission restrictions
- ✅ Network access controls
- ✅ Tool usage restrictions

#### 2. **Python Package Validation**: `pip.go`

**Location**: `pkg/workflow/pip.go` (225 lines)

**Purpose**: Validates Python package availability on PyPI

**Validation Functions**:
- `validatePythonPackagesWithPip()` - Generic pip validation helper
- `validatePipPackages()` - Validates pip packages
- `validateUvPackages()` - Validates uv packages
- `validateUvPackagesWithPip()` - Validates uv packages using pip

**Pattern**: External registry validation with fallback to warnings

**When to add validation here**:
- ✅ Python/pip ecosystem validation
- ✅ PyPI package existence checks
- ✅ Python version compatibility

#### 3. **NPM Package Validation**: `npm.go`

**Location**: `pkg/workflow/npm.go` (90 lines)

**Purpose**: Validates NPX package availability on npm registry

**Validation Functions**:
- `validateNpxPackages()` - Validates npm packages used with npx

**Pattern**: External registry validation with error reporting

**When to add validation here**:
- ✅ Node.js/npm ecosystem validation
- ✅ NPM registry package checks
- ✅ NPX launcher validation

#### 4. **Expression Safety**: `expression_safety.go`

**Location**: `pkg/workflow/expression_safety.go` (169 lines)

**Purpose**: Validates GitHub Actions expression security

**Validation Functions**:
- `validateExpressionSafety()` - Validates allowed GitHub expressions
- `validateSingleExpression()` - Validates individual expression syntax

**Pattern**: Security-focused allowlist validation with detailed error reporting

**When to add validation here**:
- ✅ GitHub Actions expression parsing
- ✅ Expression security validation
- ✅ Injection prevention

#### 5. **Engine Validation**: `engine.go`

**Location**: `pkg/workflow/engine.go` (383 lines)

**Purpose**: Validates AI engine configuration

**Validation Functions**:
- `validateEngine()` - Validates engine ID is supported
- `validateSingleEngineSpecification()` - Ensures single engine per workflow

**Pattern**: Configuration validation with backward compatibility

**When to add validation here**:
- ✅ Engine configuration parsing
- ✅ Engine compatibility checks
- ✅ Engine-specific feature validation

#### 6. **MCP Configuration**: `mcp-config.go`

**Location**: `pkg/workflow/mcp-config.go` (1121 lines)

**Purpose**: Validates Model Context Protocol configurations

**Validation Functions**:
- `validateStringProperty()` - Validates string configuration properties
- `validateMCPRequirements()` - Validates MCP server requirements

**Pattern**: Schema validation with type checking

**When to add validation here**:
- ✅ MCP server configuration validation
- ✅ MCP protocol compliance
- ✅ Tool configuration validation

#### 7. **Docker Image Validation**: `docker.go`

**Location**: `pkg/workflow/docker.go` (140 lines)

**Purpose**: Validates Docker image accessibility

**Validation Functions**:
- `validateDockerImage()` - Validates Docker image exists and is pullable

**Pattern**: External resource validation with caching

**When to add validation here**:
- ✅ Docker image existence checks
- ✅ Container registry validation
- ✅ Image tag validation

#### 8. **Template Validation**: `template.go`

**Location**: `pkg/workflow/template.go`

**Purpose**: Validates workflow template structure

**Validation Functions**:
- `validateNoIncludesInTemplateRegions()` - Validates template region integrity

**Pattern**: Structural validation preventing invalid configurations

**When to add validation here**:
- ✅ Template syntax validation
- ✅ Include/import validation
- ✅ Template region validation

#### 9. **JavaScript Bundle Safety Validation**: `bundler_safety_validation.go`

**Location**: `pkg/workflow/bundler_safety_validation.go` (230 lines)

**Purpose**: Validates JavaScript bundle safety to prevent runtime module errors

**Validation Functions**:
- `validateNoLocalRequires()` - Ensures all local require() statements are bundled (GitHub Script mode)
- `validateNoModuleReferences()` - Ensures no module.exports or exports remain (GitHub Script mode)
- `ValidateEmbeddedResourceRequires()` - Validates embedded JavaScript dependencies exist
- `normalizePath()` - Path normalization utility

**Pattern**: Compile-time bundle correctness validation

**When to add validation here**:
- ✅ JavaScript bundling correctness
- ✅ Missing module dependencies
- ✅ CommonJS require() statement resolution

#### 10. **JavaScript Script Content Validation**: `bundler_script_validation.go`

**Location**: `pkg/workflow/bundler_script_validation.go` (160 lines)

**Purpose**: Validates JavaScript script content for runtime mode API compatibility

**Validation Functions**:
- `validateNoExecSync()` - Ensures GitHub Script mode scripts use exec instead of execSync
- `validateNoGitHubScriptGlobals()` - Ensures Node.js scripts don't use GitHub Actions globals (core.*, exec.*, github.*)

**Pattern**: Registration-time script content validation with panic on violation

**Validation Enforcement**:
- Registration-time: Triggered during script registration in `RegisterWithMode()`
- Scripts violating rules cause panics during package initialization
- Catches errors during development/testing rather than at runtime

**When to add validation here**:
- ✅ JavaScript code content validation based on runtime mode
- ✅ API usage patterns (execSync, GitHub Actions globals)
- ✅ Script compatibility with execution environment

**Design Rationale**:
The script content validation enforces two key constraints:
1. **GitHub Script mode**: Should not use `execSync` (use async `exec` from `@actions/exec` instead)
2. **Node.js mode**: Should not use GitHub Actions globals (`core.*`, `exec.*`, `github.*`)

These rules ensure that scripts follow platform conventions:
- GitHub Script mode runs inline in GitHub Actions YAML with GitHub-specific globals available
- Node.js mode runs as standalone scripts with standard Node.js APIs only

#### 11. **JavaScript Runtime Mode Validation**: `bundler_runtime_validation.go`

**Location**: `pkg/workflow/bundler_runtime_validation.go` (190 lines)

**Purpose**: Validates JavaScript runtime mode compatibility to prevent mixing incompatible scripts

**Validation Functions**:
- `validateNoRuntimeMixing()` - Prevents mixing nodejs-only scripts with github-script scripts
- `validateRuntimeModeRecursive()` - Recursively validates runtime compatibility
- `detectRuntimeMode()` - Detects the intended runtime mode of a JavaScript file

**Pattern**: Bundling-time runtime compatibility validation

**When to add validation here**:
- ✅ Runtime mode compatibility checks
- ✅ Detecting script mixing issues
- ✅ Runtime-specific API detection

## Decision Tree: Where to Add New Validation

Use this decision tree to determine where to place new validation logic:

```text
┌─────────────────────────────────────┐
│  New Validation Requirement         │
└──────────────┬──────────────────────┘
               │
               ▼
       ┌───────────────┐
       │ Is it about   │
       │ security or   │     YES
       │ strict mode?  ├──────────► strict_mode_validation.go
       └───────┬───────┘
               │ NO
               ▼
       ┌───────────────┐
       │ Does it only  │
       │ apply to one  │     YES    ┌──────────────────────┐
       │ specific      ├───────────►│ Is there a domain-   │
       │ domain?       │            │ specific file?       │
       └───────┬───────┘            └────┬────────┬────────┘
               │ NO                      │ YES    │ NO
               │                         ▼        ▼
               │                    Add to     Create new
               │                    domain     domain file
               │                    file
               ▼
       ┌───────────────┐
       │ Is it a       │
       │ cross-cutting │     YES
       │ concern?      ├──────────► validation.go
       └───────┬───────┘
               │ NO
               ▼
       ┌───────────────┐
       │ Does it       │
       │ validate      │     YES
       │ external      ├──────────► Domain-specific file
       │ resources?    │            (e.g., pip.go, npm.go,
       └───────┬───────┘             docker.go)
               │ NO
               ▼
          validation.go
```text

## Validation Patterns

### Pattern 1: Allowlist Validation

**Used in**: `expression_safety.go`

**Purpose**: Validate against a known set of allowed values

**Example**:
```go
func validateExpressionSafety(markdownContent string) error {
    matches := expressionRegex.FindAllStringSubmatch(markdownContent, -1)
    var unauthorizedExpressions []string
    
    for _, match := range matches {
        expression := strings.TrimSpace(match[1])
        
        // Check if expression is in allowlist
        if !isAllowed(expression) {
            unauthorizedExpressions = append(unauthorizedExpressions, expression)
        }
    }
    
    if len(unauthorizedExpressions) > 0 {
        return fmt.Errorf("unauthorized expressions found: %v", unauthorizedExpressions)
    }
    return nil
}
```text

**When to use**:
- Security-sensitive validation
- Limited set of valid options
- Preventing injection attacks

### Pattern 2: External Resource Validation

**Used in**: `docker.go`, `pip.go`, `npm.go`

**Purpose**: Validate external resources exist before runtime

**Example**:
```go
func validateDockerImage(image string, verbose bool) error {
    cmd := exec.Command("docker", "inspect", image)
    output, err := cmd.CombinedOutput()
    
    if err != nil {
        // Try pulling the image
        pullCmd := exec.Command("docker", "pull", image)
        if pullErr := pullCmd.Run(); pullErr != nil {
            return fmt.Errorf("docker image not found: %s", image)
        }
    }
    return nil
}
```text

**When to use**:
- Validating external dependencies
- Package registry checks
- Container image availability
- Network resource validation

### Pattern 3: Schema Validation

**Used in**: `validation.go`, `mcp-config.go`

**Purpose**: Validate configuration against a defined schema

**Example**:
```go
func (c *Compiler) validateGitHubActionsSchema(yamlContent string) error {
    // Load JSON Schema
    schema := loadGitHubActionsSchema()
    
    // Parse YAML as JSON
    var data interface{}
    if err := yaml.Unmarshal([]byte(yamlContent), &data); err != nil {
        return err
    }
    
    // Validate against schema
    if err := schema.Validate(data); err != nil {
        return fmt.Errorf("schema validation failed: %w", err)
    }
    return nil
}
```text

**When to use**:
- Configuration file validation
- YAML/JSON structure validation
- Type checking
- Required field validation

### Pattern 4: Progressive Validation

**Used in**: `strict_mode_validation.go`

**Purpose**: Apply multiple validation checks in sequence

**Example**:
```go
func (c *Compiler) validateStrictMode(frontmatter map[string]any, networkPermissions *NetworkPermissions) error {
    if !c.strictMode {
        return nil
    }
    
    // 1. Refuse write permissions
    if err := c.validateStrictPermissions(frontmatter); err != nil {
        return err
    }
    
    // 2. Require network configuration
    if err := c.validateStrictNetwork(networkPermissions); err != nil {
        return err
    }
    
    // 3. Validate MCP network
    if err := c.validateStrictMCPNetwork(frontmatter); err != nil {
        return err
    }
    
    return nil
}
```text

**When to use**:
- Multiple related validation steps
- Security policy enforcement
- Layered validation requirements
- Early exit on first failure

### Pattern 5: Warning vs Error Validation

**Used in**: `pip.go`

**Purpose**: Distinguish between hard failures and soft warnings

**Example**:
```go
func (c *Compiler) validatePythonPackagesWithPip(packages []string, packageType string, pipCmd string) {
    for _, pkg := range packages {
        cmd := exec.Command(pipCmd, "index", "versions", pkg)
        output, err := cmd.CombinedOutput()
        
        if err != nil {
            // Warning: Don't fail compilation
            fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
                fmt.Sprintf("%s package '%s' validation failed - skipping verification", packageType, pkg)))
        } else {
            // Success: Optional verbose output
            if c.verbose {
                fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
                    fmt.Sprintf("✓ %s package validated: %s", packageType, pkg)))
            }
        }
    }
}
```text

**When to use**:
- Optional dependency validation
- Best-effort external checks
- Non-critical validations
- Developer-friendly warnings

## Validation Helper Patterns

### Error Collection

Multiple validation errors should be collected and reported together:

```go
var errors []string

for _, item := range items {
    if err := validateItem(item); err != nil {
        errors = append(errors, err.Error())
    }
}

if len(errors) > 0 {
    return fmt.Errorf("validation failed:\n  - %s", strings.Join(errors, "\n  - "))
}
```text

### Verbose Logging

Use the logger package for debugging validation logic:

```go
var validationLog = logger.New("workflow:validation")

func validateSomething() error {
    validationLog.Print("Starting validation...")
    // validation logic
    validationLog.Printf("Validated %d items", count)
    return nil
}
```text

Enable with: `DEBUG=workflow:validation gh aw compile`

### Console Output

Use console formatting for user-facing messages:

```go
import "github.com/github/gh-aw/pkg/console"

// Success
fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Validation passed"))

// Warning
fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Package validation skipped"))

// Error
fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))

// Info (verbose mode)
if c.verbose {
    fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Validating configuration..."))
}
```text

## Testing Validation

All validation functions should have corresponding tests:

### Unit Tests

Test individual validation functions in isolation:

```go
func TestValidateExpressionSafety(t *testing.T) {
    tests := []struct {
        name        string
        content     string
        expectError bool
    }{
        {
            name:        "allowed expression",
            content:     "${{ github.event.issue.number }}",
            expectError: false,
        },
        {
            name:        "unauthorized expression",
            content:     "${{ secrets.GITHUB_TOKEN }}",
            expectError: true,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validateExpressionSafety(tt.content)
            if (err != nil) != tt.expectError {
                t.Errorf("expected error: %v, got: %v", tt.expectError, err)
            }
        })
    }
}
```text

### Integration Tests

Test validation in the context of full workflow compilation:

```go
func TestStrictModeValidation(t *testing.T) {
    compiler := NewCompiler()
    compiler.strictMode = true
    
    workflowData := &WorkflowData{
        Frontmatter: map[string]any{
            "permissions": map[string]any{
                "contents": "write", // Should fail in strict mode
            },
        },
    }
    
    err := compiler.validateStrictMode(workflowData.Frontmatter, nil)
    if err == nil {
        t.Error("expected strict mode to reject write permissions")
    }
}
```text

## Contributing: Adding New Validation

When adding new validation logic, follow these guidelines:

### 1. Determine the Right Location

Use the decision tree above to determine whether validation belongs in:
- `validation.go` - Cross-cutting concerns
- Domain-specific file - Specialized validation
- New file - New domain area

### 2. Follow Naming Conventions

- Function names: `validate<WhatIsValidated>()`
- Receiver methods: `(c *Compiler) validate<WhatIsValidated>()`
- Test functions: `TestValidate<WhatIsValidated>()`

### 3. Add Documentation

Each validation function should have a comment explaining:
- What it validates
- When it runs
- What errors it can return

```go
// validateDockerImage validates that a Docker image exists and is accessible
// by attempting to inspect it locally, and pulling it if not found.
// Returns an error if the image cannot be pulled or accessed.
func validateDockerImage(image string, verbose bool) error {
    // Implementation
}
```text

### 4. Include Tests

Add both unit tests and integration tests:
- Unit tests: Test the validation function in isolation
- Integration tests: Test validation in compilation flow

### 5. Use Consistent Error Messages

Format error messages for readability:

```go
return fmt.Errorf("validation failed for %s: %w", item, err)
```text

Collect multiple errors:

```go
var errors []string
// ... collect errors
return fmt.Errorf("validation failed:\n  - %s", strings.Join(errors, "\n  - "))
```text

### 6. Add Logging

Use the logger package for debug output:

```go
var myLog = logger.New("workflow:myarea")

func validateSomething() error {
    myLog.Print("Starting validation")
    // ...
    myLog.Printf("Validated %d items", count)
    return nil
}
```text

### 7. Update Documentation

When adding new validation:
- Add function to this document's relevant section
- Update the validation function list
- Add examples if introducing a new pattern
- Update CONTRIBUTING.md if adding new guidelines

## Common Validation Scenarios

### Validating Configuration Fields

```go
func validateRequired(value interface{}, fieldName string) error {
    if value == nil || value == "" {
        return fmt.Errorf("field %s is required", fieldName)
    }
    return nil
}
```text

### Validating Against Allowlist

```go
var allowedEngines = []string{"copilot", "claude", "codex", "custom"}

func validateEngine(engine string) error {
    for _, allowed := range allowedEngines {
        if engine == allowed {
            return nil
        }
    }
    return fmt.Errorf("unsupported engine: %s (allowed: %v)", engine, allowedEngines)
}
```text

### Validating External Resources

```go
func validateResourceExists(url string) error {
    resp, err := http.Head(url)
    if err != nil {
        return fmt.Errorf("resource not accessible: %w", err)
    }
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("resource returned status: %d", resp.StatusCode)
    }
    return nil
}
```text

### Validating Repository Features

```go
func (c *Compiler) validateRepositoryFeatures(workflowData *WorkflowData) error {
    features := getRepositoryFeatures(repo)
    
    if needsDiscussions(workflowData) && !features.HasDiscussions {
        return fmt.Errorf("workflow requires discussions but repository has them disabled")
    }
    
    return nil
}
```text

## References

- **Implementation**: `pkg/workflow/validation.go` and domain-specific files
- **Tests**: `pkg/workflow/*_validation_test.go`
- **Contributing Guide**: `CONTRIBUTING.md`
- **Development Guide**: `DEVGUIDE.md`

---

**Last Updated**: 2025-11-03
**Related Issues**: #3030
