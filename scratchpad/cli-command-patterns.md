# CLI Command Patterns and Conventions

This document provides guidance for developing CLI commands in GitHub Agentic Workflows. It covers command structure, naming conventions, flag patterns, error handling, testing requirements, and help text standards.

## Table of Contents

- [Command Structure](#command-structure)
- [Naming Conventions](#naming-conventions)
- [Flag Patterns](#flag-patterns)
- [Error Handling](#error-handling)
- [Console Output](#console-output)
- [Testing Requirements](#testing-requirements)
- [Help Text Standards](#help-text-standards)
- [Anti-Patterns](#anti-patterns)
- [Examples](#examples)

---

## Command Structure

### Standard File Structure

Every command should follow this standard structure:

```go
package cli

import (
    // Standard library imports
    "fmt"
    "os"
    
    // Internal imports
    "github.com/github/gh-aw/pkg/console"
    "github.com/github/gh-aw/pkg/logger"
    
    // External imports
    "github.com/spf13/cobra"
)

// Logger instance with namespace following cli:command_name pattern
var commandLog = logger.New("cli:command_name")

// NewCommandNameCommand creates the command-name command
func NewCommandNameCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "command-name [args]",
        Short: "Brief one-line description under 80 chars",
        Long:  `Detailed description with examples...`,
        Args:  cobra.ExactArgs(1), // or other validator
        RunE: func(cmd *cobra.Command, args []string) error {
            // Parse flags
            flagValue, _ := cmd.Flags().GetString("flag-name")
            verbose, _ := cmd.Flags().GetBool("verbose")
            
            // Call main function
            return RunCommandName(args[0], flagValue, verbose)
        },
    }
    
    // Add flags
    cmd.Flags().StringP("flag-name", "f", "default", "Flag description")
    addVerboseFlag(cmd)
    
    // Register completions
    RegisterDirFlagCompletion(cmd, "output")
    
    return cmd
}

// RunCommandName executes the command logic
func RunCommandName(arg string, flagValue string, verbose bool) error {
    commandLog.Printf("Starting command: arg=%s, flagValue=%s", arg, flagValue)
    
    // Validate inputs early
    if err := validateInputs(arg, flagValue); err != nil {
        return err
    }
    
    // Execute command logic
    result, err := executeCommand(arg, flagValue)
    if err != nil {
        return fmt.Errorf("failed to execute command: %w", err)
    }
    
    // Output results
    fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(result))
    
    commandLog.Print("Command completed successfully")
    return nil
}

// Internal implementation functions
func validateInputs(arg string, flagValue string) error {
    if arg == "" {
        return fmt.Errorf("argument cannot be empty")
    }
    return nil
}

func executeCommand(arg string, flagValue string) (string, error) {
    // Implementation
    return "Success", nil
}
```

### Key Structure Elements

1. **Logger Namespace**: Always create a logger with format `cli:command_name`
2. **Public API**: Two exported functions:
   - `NewXCommand() *cobra.Command` - Creates the command
   - `RunX(...) error` - Executes command logic (testable)
3. **Internal Functions**: Private helper functions for implementation
4. **Flag Parsing**: Parse flags in `RunE` function
5. **Early Validation**: Validate all inputs before processing
6. **Error Wrapping**: Use `fmt.Errorf` with `%w` for context

### File Organization

Commands can be organized in multiple ways depending on complexity:

#### Single File Pattern (Simple Commands)

For commands with < 500 lines:
- `command_name.go` - All command logic
- `command_name_test.go` - All tests

**Example**: `status_command.go` (368 lines)

#### Multi-File Pattern (Complex Commands)

For commands with > 500 lines, split into focused files:
- `command_name_command.go` - Command definition
- `command_name_config.go` - Configuration types
- `command_name_helpers.go` - Utility functions
- `command_name_validation.go` - Validation logic
- `command_name_orchestrator.go` - Main orchestration

**Example**: Compile command split into multiple files:
- `compile_command.go` - Command definition (minimal)
- `compile_config.go` - Configuration types
- `compile_helpers.go` - Utility functions
- `compile_validation.go` - Validation logic
- `compile_orchestrator.go` - Main orchestration

---

## Naming Conventions

### File Names

| File Type | Pattern | Example |
|-----------|---------|---------|
| Command file | `*_command.go` | `audit_command.go` |
| Test file | `*_command_test.go` | `audit_command_test.go` |
| Integration test | `*_integration_test.go` | `compile_integration_test.go` |
| Helper functions | `*_helpers.go` | `compile_helpers.go` |
| Configuration | `*_config.go` | `compile_config.go` |

### Logger Namespaces

Format: `cli:command_name`

```go
// ✅ CORRECT
var auditLog = logger.New("cli:audit")
var compileLog = logger.New("cli:compile_command")
var statusLog = logger.New("cli:status_command")

// ❌ INCORRECT
var log = logger.New("audit")       // Missing cli: prefix
var logger = logger.New("compile")  // Missing cli: prefix, conflicts with package
```

### Public Functions

| Function Type | Pattern | Example |
|--------------|---------|---------|
| Command creator | `NewXCommand()` | `NewAuditCommand()` |
| Command runner | `RunX(...)` | `RunAuditWorkflowRun(...)` |
| Helper functions | Descriptive names | `validateInputs()`, `fetchWorkflows()` |

### Configuration Structs

Configuration struct names should end with `Config`:

```go
// ✅ CORRECT
type CompileConfig struct {
    WorkflowFile string
    OutputDir    string
    Verbose      bool
}

type AuditConfig struct {
    RunID   int64
    Verbose bool
}

// ❌ INCORRECT
type CompileOptions struct { ... }  // Use Config suffix
type AuditParams struct { ... }     // Use Config suffix
```

---

## Flag Patterns

### Global Flags

Global flags are available to all commands:

```go
// Defined in commands.go
rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
rootCmd.PersistentFlags().BoolVar(&noBanner, "no-banner", false, "Disable banner messages")
```

### Common Flags

Common flags with helper functions (defined in `flags.go`):

```go
// Engine flag - Override AI engine
addEngineFlag(cmd)           // --engine/-e
addEngineFilterFlag(cmd)     // --engine/-e for filtering

// Repository flag
addRepoFlag(cmd)             // --repo/-r

// Output directory flag
addOutputFlag(cmd, defaultDir)  // --output/-o

// JSON output flag
addJSONFlag(cmd)             // --json/-j
```

### Standard Short Flags

Reserve these short flags for consistent meanings:

| Short Flag | Meaning | Long Flag |
|-----------|---------|-----------|
| `-v` | Verbose output | `--verbose` |
| `-e` | Engine selection/filter | `--engine` |
| `-r` | Repository | `--repo` |
| `-o` | Output directory | `--output` |
| `-j` | JSON output | `--json` |
| `-f` | Force/file | `--force`/`--file` |
| `-w` | Watch mode | `--watch` |

### Flag Naming

- Use **kebab-case** for flag names: `--output-dir`, `--run-id`
- Use descriptive names that indicate purpose with noun phrases and conventional prefixes
- Avoid abbreviations unless universally understood

### Flag Validation

Validate flags early in the `RunE` function:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    // Parse flags
    engine, _ := cmd.Flags().GetString("engine")
    runID, _ := cmd.Flags().GetInt64("run-id")
    
    // Validate flags
    if engine != "" && !isValidEngine(engine) {
        return fmt.Errorf("invalid engine: %s (must be copilot, claude, codex, or custom)", engine)
    }
    
    if runID <= 0 {
        return fmt.Errorf("run-id must be positive")
    }
    
    // Continue with validated values
    return RunCommand(engine, runID)
},
```

### Flag Completion

Register flag completions for better UX:

```go
// Directory completion
RegisterDirFlagCompletion(cmd, "output")

// File completion
RegisterFileFlagCompletion(cmd, "file", "*.md")

// Custom completion
cmd.RegisterFlagCompletionFunc("engine", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
    return []string{"copilot", "claude", "codex", "custom"}, cobra.ShellCompDirectiveNoFileComp
})
```

---

## Error Handling

### User-Facing Errors

Always use `console.FormatErrorMessage()` for user-facing errors:

```go
// ✅ CORRECT - Formatted error message
if err != nil {
    fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
    return err
}

// ❌ INCORRECT - Plain error message
if err != nil {
    fmt.Fprintln(os.Stderr, err)
    return err
}

// ❌ INCORRECT - Writing to stdout
if err != nil {
    fmt.Println(err)  // Should use stderr
    return err
}
```

### Error Wrapping

Use `fmt.Errorf` with `%w` to provide context:

```go
// ✅ CORRECT - Error wrapping with context
result, err := fetchData(url)
if err != nil {
    return fmt.Errorf("failed to fetch data from %s: %w", url, err)
}

// ❌ INCORRECT - No context
result, err := fetchData(url)
if err != nil {
    return err
}
```

### Actionable Error Messages

Provide clear, actionable error messages:

```go
// ✅ CORRECT - Actionable error with suggestion
if !workflowExists {
    return fmt.Errorf("workflow '%s' not found. Use 'gh aw list' to see available workflows", name)
}

// ✅ CORRECT - Validation error with expected format
if !strings.Contains(input, "=") {
    return fmt.Errorf("invalid input format '%s': expected key=value", input)
}

// ❌ INCORRECT - Vague error message
if !workflowExists {
    return fmt.Errorf("not found")
}
```

### Error Logging

Log errors with context before returning:

```go
func RunCommand(arg string) error {
    commandLog.Printf("Starting command with arg: %s", arg)
    
    result, err := processArg(arg)
    if err != nil {
        commandLog.Printf("Failed to process arg: %v", err)
        return fmt.Errorf("failed to process argument: %w", err)
    }
    
    commandLog.Print("Command completed successfully")
    return nil
}
```

---

## Console Output

### Output Guidelines

1. **ALL output goes to stderr**: `fmt.Fprintln(os.Stderr, ...)`
2. **Use console formatting**: Never use plain `fmt.Println()`
3. **Consistent styling**: Use appropriate message types

### Message Types

```go
import "github.com/github/gh-aw/pkg/console"

// Success messages
fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Workflow compiled successfully"))

// Info messages
fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Fetching workflow status..."))

// Warning messages
fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Workflow has unstaged changes"))

// Error messages
fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))

// Command messages (CLI commands being executed)
fmt.Fprintln(os.Stderr, console.FormatCommandMessage("gh workflow run workflow.yml"))

// Progress messages (operations in progress)
fmt.Fprintln(os.Stderr, console.FormatProgressMessage("Downloading artifacts..."))

// Prompt messages (user prompts)
fmt.Fprintln(os.Stderr, console.FormatPromptMessage("Select a workflow:"))

// Count messages (numeric counts)
fmt.Fprintln(os.Stderr, console.FormatCountMessage(fmt.Sprintf("Found %d workflows", count)))

// Verbose messages (debug info, only shown with --verbose)
if verbose {
    fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Detailed debug information"))
}

// Location messages (file paths, URLs)
fmt.Fprintln(os.Stderr, console.FormatLocationMessage(filepath))
```

### Structured Output

For structured data display, use `console.RenderStruct()`:

```go
type WorkflowStatus struct {
    Workflow      string `json:"workflow" console:"header:Workflow"`
    Engine        string `json:"engine" console:"header:Engine"`
    Compiled      string `json:"compiled" console:"header:Compiled"`
    Status        string `json:"status" console:"header:Status"`
}

statuses := []WorkflowStatus{...}

// Render table
fmt.Print(console.RenderStruct(statuses))
```

### JSON Output

Support JSON output with `--json` flag:

```go
if jsonOutput {
    jsonBytes, err := json.MarshalIndent(data, "", "  ")
    if err != nil {
        return fmt.Errorf("failed to marshal JSON: %w", err)
    }
    fmt.Println(string(jsonBytes))  // JSON goes to stdout
    return nil
}

// Normal output goes to stderr
fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Operation complete"))
```

### Verbose Output

Verbose messages should provide additional detail:

```go
if verbose {
    fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(
        fmt.Sprintf("Processing workflow: %s", workflowName)))
    fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(
        fmt.Sprintf("Engine: %s", engine)))
}
```

---

## Testing Requirements

### Test File Structure

Every command must have corresponding test files:

```
command_name_command.go
command_name_command_test.go         # Unit tests
command_name_integration_test.go     # Integration tests (optional)
```

### Table-Driven Tests

Use table-driven tests for testing multiple scenarios:

```go
func TestRunCommand(t *testing.T) {
    tests := []struct {
        name      string
        input     string
        expected  string
        shouldErr bool
    }{
        {
            name:      "valid input",
            input:     "test-workflow",
            expected:  "Success",
            shouldErr: false,
        },
        {
            name:      "empty input",
            input:     "",
            expected:  "",
            shouldErr: true,
        },
        {
            name:      "invalid format",
            input:     "invalid@workflow",
            expected:  "",
            shouldErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := RunCommand(tt.input)
            
            if tt.shouldErr {
                assert.Error(t, err, "Expected error for input: %s", tt.input)
            } else {
                assert.NoError(t, err, "Should not error for input: %s", tt.input)
                assert.Equal(t, tt.expected, result, "Incorrect result for input: %s", tt.input)
            }
        })
    }
}
```

### Assert vs Require

Use `testify` assertions appropriately:

```go
import (
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestWorkflowCompilation(t *testing.T) {
    // Use require for critical setup (stops test if fails)
    tmpDir := t.TempDir()
    testFile := filepath.Join(tmpDir, "test.md")
    err := os.WriteFile(testFile, []byte(content), 0644)
    require.NoError(t, err, "Failed to write test file")
    
    // Use assert for test validations (continues checking)
    result, err := CompileWorkflow(testFile)
    assert.NoError(t, err, "Should compile valid workflow")
    assert.NotNil(t, result, "Result should not be nil")
    assert.Equal(t, "expected", result.Field, "Field value incorrect")
}
```

### Test Coverage

Tests should cover:

1. **Valid inputs**: Normal operation with valid data
2. **Invalid inputs**: Error handling with invalid data
3. **Edge cases**: Empty strings, nil values, boundary conditions
4. **Flag handling**: All flag combinations
5. **Error paths**: All error conditions

### Test Naming

Use descriptive test names:

```go
// ✅ CORRECT - Descriptive test names
func TestAuditCommand_ValidRunID(t *testing.T) { ... }
func TestAuditCommand_InvalidRunID(t *testing.T) { ... }
func TestAuditCommand_MissingArtifacts(t *testing.T) { ... }

// ❌ INCORRECT - Vague test names
func TestAudit1(t *testing.T) { ... }
func TestAudit2(t *testing.T) { ... }
```

---

## Help Text Standards

### Short Description

- Under 80 characters
- Action-oriented (start with verb)
- No period at the end
- Clear and concise

```go
// ✅ CORRECT
Short: "Investigate an agentic workflow run and generate a detailed report"
Short: "Show status of agentic workflow files"
Short: "Compile Markdown workflows to GitHub Actions YAML"

// ❌ INCORRECT
Short: "This command investigates workflows."  // Too wordy, has period
Short: "Status"                                // Not descriptive enough
Short: "A very long description that goes well over the eighty character limit for short descriptions"  // Too long
```

### Long Description

Structure long descriptions with:

1. **Overview**: What the command does (1-2 sentences)
2. **Context**: When to use it, what it's for
3. **Details**: Specific behavior, options, features
4. **Examples**: Practical usage examples (minimum 3)

```go
Long: `Audit a single workflow run by downloading artifacts and logs, detecting errors,
analyzing MCP tool usage, and generating a concise Markdown report suitable for AI agents.

This command accepts:
- A numeric run ID (e.g., 1234567890)
- A GitHub Actions run URL (e.g., https://github.com/owner/repo/actions/runs/1234567890)
- A GitHub Actions job URL (e.g., https://github.com/owner/repo/actions/runs/1234567890/job/9876543210)

This command:
- Downloads artifacts and logs for the specified run ID
- Detects errors and warnings in the logs
- Analyzes MCP tool usage statistics
- Generates a concise Markdown report

Examples:
  gh aw audit 1234567890                    # Audit run with ID 1234567890
  gh aw audit https://...                    # Audit from run URL
  gh aw audit 1234567890 -o ./reports        # Custom output directory
  gh aw audit 1234567890 -v                  # Verbose output
  gh aw audit 1234567890 --parse             # Parse agent and firewall logs`,
```

### Example Guidelines

Provide at least 3 practical examples:

1. **Basic usage**: Simplest form of the command
2. **Common options**: With frequently used flags
3. **Advanced usage**: Complex scenarios

```go
Examples:
  gh aw compile workflow.md                  # Compile single workflow
  gh aw compile workflow.md -v               # Verbose output
  gh aw compile -d .github/workflows         # Compile directory
  gh aw compile workflow.md --watch          # Watch mode
  gh aw compile --all                        # Compile all workflows
```

### Including WorkflowIDExplanation

For commands that accept workflow IDs, include the standard explanation:

```go
import "github.com/github/gh-aw/pkg/cli"

Long: `Description of command...

` + cli.WorkflowIDExplanation + `

Examples:
  ...`,
```

---

## Anti-Patterns

### What NOT to Do

#### 1. Don't Write to Stdout (Except JSON)

```go
// ❌ INCORRECT
fmt.Println("Success")
fmt.Printf("Status: %s\n", status)

// ✅ CORRECT
fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Success"))
fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Status: %s", status)))
```

#### 2. Don't Use Plain Error Messages

```go
// ❌ INCORRECT
fmt.Fprintln(os.Stderr, err)

// ✅ CORRECT
fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
```

#### 3. Don't Skip Error Context

```go
// ❌ INCORRECT
if err != nil {
    return err
}

// ✅ CORRECT
if err != nil {
    return fmt.Errorf("failed to parse workflow: %w", err)
}
```

#### 4. Don't Create Monolithic Command Files

```go
// ❌ INCORRECT - Single file with 2000+ lines
compile_command.go  // 2000 lines

// ✅ CORRECT - Split into focused files
compile_command.go      // Command definition (50 lines)
compile_config.go       // Configuration (100 lines)
compile_validation.go   // Validation (300 lines)
compile_orchestrator.go // Main logic (500 lines)
```

#### 5. Don't Use Inconsistent Logger Namespaces

```go
// ❌ INCORRECT
var log = logger.New("audit")
var logger = logger.New("compile")

// ✅ CORRECT
var auditLog = logger.New("cli:audit")
var compileLog = logger.New("cli:compile_command")
```

#### 6. Don't Skip Input Validation

```go
// ❌ INCORRECT
func RunCommand(arg string) error {
    // Directly use arg without validation
    result := processArg(arg)
    return nil
}

// ✅ CORRECT
func RunCommand(arg string) error {
    if arg == "" {
        return fmt.Errorf("argument cannot be empty")
    }
    if !isValid(arg) {
        return fmt.Errorf("invalid argument format: %s", arg)
    }
    result := processArg(arg)
    return nil
}
```

#### 7. Don't Forget Flag Completions

```go
// ❌ INCORRECT - No completions
cmd.Flags().StringP("output", "o", "", "Output directory")

// ✅ CORRECT - With completions
cmd.Flags().StringP("output", "o", "", "Output directory")
RegisterDirFlagCompletion(cmd, "output")
```

#### 8. Don't Create Vague Help Text

```go
// ❌ INCORRECT
Short: "Compile files"
Long:  `This compiles files.`

// ✅ CORRECT
Short: "Compile Markdown workflows to GitHub Actions YAML"
Long: `Compile agentic workflow Markdown files to GitHub Actions YAML with validation.

This command:
- Parses Markdown frontmatter and content
- Validates workflow configuration
- Generates GitHub Actions YAML
- Supports watch mode for automatic recompilation

Examples:
  gh aw compile workflow.md
  gh aw compile --all
  gh aw compile workflow.md --watch`,
```

---

## Examples

### Complete Command Example

Here's a complete example following all patterns:

```go
package cli

import (
    "fmt"
    "os"
    "path/filepath"
    
    "github.com/github/gh-aw/pkg/console"
    "github.com/github/gh-aw/pkg/logger"
    "github.com/spf13/cobra"
)

var validateLog = logger.New("cli:validate_command")

// NewValidateCommand creates the validate command
func NewValidateCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "validate <workflow-file>",
        Short: "Validate agentic workflow Markdown files",
        Long: `Validate agentic workflow Markdown files for correctness and compatibility.

This command:
- Validates frontmatter syntax and required fields
- Checks GitHub Actions compatibility
- Validates tool configurations
- Reports errors and warnings with actionable suggestions

` + WorkflowIDExplanation + `

Examples:
  gh aw validate workflow.md              # Validate single workflow
  gh aw validate workflow.md -v           # Verbose output
  gh aw validate --all                    # Validate all workflows
  gh aw validate workflow.md --strict     # Strict mode validation`,
        Args: cobra.MaximumNArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            verbose, _ := cmd.Flags().GetBool("verbose")
            strict, _ := cmd.Flags().GetBool("strict")
            all, _ := cmd.Flags().GetBool("all")
            
            var workflowFile string
            if len(args) > 0 {
                workflowFile = args[0]
            }
            
            return RunValidate(workflowFile, all, strict, verbose)
        },
    }
    
    cmd.Flags().Bool("all", false, "Validate all workflow files")
    cmd.Flags().Bool("strict", false, "Enable strict mode validation")
    
    return cmd
}

// RunValidate validates workflow files
func RunValidate(workflowFile string, all bool, strict bool, verbose bool) error {
    validateLog.Printf("Starting validation: file=%s, all=%v, strict=%v", workflowFile, all, strict)
    
    // Validate inputs
    if !all && workflowFile == "" {
        return fmt.Errorf("workflow file required (or use --all to validate all workflows)")
    }
    
    if all && workflowFile != "" {
        return fmt.Errorf("cannot specify both workflow file and --all flag")
    }
    
    // Get files to validate
    var files []string
    if all {
        var err error
        files, err = findAllWorkflowFiles()
        if err != nil {
            validateLog.Printf("Failed to find workflow files: %v", err)
            return fmt.Errorf("failed to find workflow files: %w", err)
        }
    } else {
        files = []string{workflowFile}
    }
    
    if verbose {
        fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
            fmt.Sprintf("Validating %d workflow file(s)...", len(files))))
    }
    
    // Validate each file
    var hasErrors bool
    for _, file := range files {
        if err := validateWorkflowFile(file, strict, verbose); err != nil {
            hasErrors = true
            fmt.Fprintln(os.Stderr, console.FormatErrorMessage(
                fmt.Sprintf("%s: %s", filepath.Base(file), err.Error())))
        } else {
            if verbose {
                fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(
                    fmt.Sprintf("%s: Valid", filepath.Base(file))))
            }
        }
    }
    
    if hasErrors {
        return fmt.Errorf("validation failed for one or more workflows")
    }
    
    fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(
        fmt.Sprintf("All %d workflow(s) validated successfully", len(files))))
    
    validateLog.Print("Validation completed successfully")
    return nil
}

func findAllWorkflowFiles() ([]string, error) {
    pattern := filepath.Join(".github", "workflows", "*.md")
    return filepath.Glob(pattern)
}

func validateWorkflowFile(file string, strict bool, verbose bool) error {
    if verbose {
        fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(
            fmt.Sprintf("Validating %s...", file)))
    }
    
    // Validation logic here
    return nil
}
```

### Test Example

```go
package cli

import (
    "os"
    "path/filepath"
    "testing"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestRunValidate(t *testing.T) {
    tests := []struct {
        name         string
        workflowFile string
        all          bool
        strict       bool
        shouldErr    bool
        errorMsg     string
    }{
        {
            name:         "valid workflow file",
            workflowFile: "test-workflow.md",
            all:          false,
            strict:       false,
            shouldErr:    false,
        },
        {
            name:         "no file and no all flag",
            workflowFile: "",
            all:          false,
            strict:       false,
            shouldErr:    true,
            errorMsg:     "workflow file required",
        },
        {
            name:         "both file and all flag",
            workflowFile: "test-workflow.md",
            all:          true,
            strict:       false,
            shouldErr:    true,
            errorMsg:     "cannot specify both",
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Setup test environment
            tmpDir := t.TempDir()
            originalDir, err := os.Getwd()
            require.NoError(t, err)
            defer os.Chdir(originalDir)
            
            err = os.Chdir(tmpDir)
            require.NoError(t, err)
            
            // Create test workflow file if specified
            if tt.workflowFile != "" {
                workflowsDir := filepath.Join(".github", "workflows")
                err := os.MkdirAll(workflowsDir, 0755)
                require.NoError(t, err)
                
                testFile := filepath.Join(workflowsDir, tt.workflowFile)
                err = os.WriteFile(testFile, []byte("---\nengine: copilot\n---\n# Test"), 0644)
                require.NoError(t, err)
            }
            
            // Run validation
            err = RunValidate(tt.workflowFile, tt.all, tt.strict, false)
            
            // Assert results
            if tt.shouldErr {
                assert.Error(t, err, "Expected error for test: %s", tt.name)
                if tt.errorMsg != "" {
                    assert.Contains(t, err.Error(), tt.errorMsg, 
                        "Error message should contain expected text")
                }
            } else {
                assert.NoError(t, err, "Should not error for test: %s", tt.name)
            }
        })
    }
}

func TestValidateCommand_FlagParsing(t *testing.T) {
    cmd := NewValidateCommand()
    
    // Test flag parsing
    cmd.SetArgs([]string{"workflow.md", "--strict", "--verbose"})
    err := cmd.ParseFlags([]string{"workflow.md", "--strict", "--verbose"})
    assert.NoError(t, err, "Should parse flags successfully")
    
    strict, _ := cmd.Flags().GetBool("strict")
    assert.True(t, strict, "Strict flag should be true")
    
    verbose, _ := cmd.Flags().GetBool("verbose")
    assert.True(t, verbose, "Verbose flag should be true")
}
```

---

## Command Development Checklist

Use this checklist when developing a new command:

- [ ] File created with `*_command.go` suffix
- [ ] Logger created with `cli:command_name` namespace
- [ ] `NewXCommand()` function defined
- [ ] `RunX()` function defined (testable)
- [ ] Short description under 80 chars
- [ ] Long description with context and examples
- [ ] Minimum 3 usage examples provided
- [ ] Flags added with appropriate short flags
- [ ] Flag completions registered
- [ ] Input validation implemented
- [ ] Error messages use `console.FormatErrorMessage()`
- [ ] Success messages use `console.FormatSuccessMessage()`
- [ ] All output goes to stderr (except JSON)
- [ ] JSON output supported if applicable
- [ ] Test file created (`*_command_test.go`)
- [ ] Table-driven tests implemented
- [ ] Valid input tests added
- [ ] Invalid input tests added
- [ ] Edge case tests added
- [ ] Flag handling tests added
- [ ] Documentation updated if needed

---

## Related Documentation

- **Testing Framework**: See `scratchpad/testing.md` for testing guidelines
- **Console Rendering**: See `skills/console-rendering/SKILL.md` for console output details
- **Error Messages**: See `skills/error-messages/SKILL.md` for error message style guide
- **Code Organization**: See `scratchpad/code-organization.md` for file organization patterns
- **Breaking Changes**: See `scratchpad/breaking-cli-rules.md` for breaking change guidelines

---

**Last Updated**: 2026-01-01
