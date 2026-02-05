# GitHub Agentic Workflows (gh-aw)

GitHub Agentic Workflows is a Go-based GitHub CLI extension for writing agentic workflows in natural language using markdown files, running them as GitHub Actions.

## Important Note: gh-aw vs GitHub Copilot CLI

**gh-aw** is a GitHub CLI extension (`gh aw`) that compiles markdown workflows into GitHub Actions. It is **not** the GitHub Copilot CLI (`copilot` command). While workflows can use the Copilot CLI as an AI engine, gh-aw itself is a separate tool for workflow management and compilation.

- Use `gh aw` commands (e.g., `gh aw compile`, `gh aw run`) to work with agentic workflows
- Use `/agent` in GitHub Copilot Chat to invoke the unified `agentic-workflows` custom agent (specify your intent: create/debug/update/upgrade)
- The `copilot` CLI command is only used internally within workflows when specified as the engine

## Important: Using Skills

**BE LAZY**: Skills in `skills/` provide detailed, specialized knowledge about specific topics. **Only reference skills when you actually need their specialized knowledge**. Do not load or reference skills preemptively.

**When to use skills:**
- You encounter a specific technical challenge that requires specialized knowledge
- You need detailed guidance on a particular aspect of the codebase (e.g., console rendering, error messages)
- You're working with a specific technology integration (e.g., GitHub MCP server, Copilot CLI)

**When NOT to use skills:**
- For general coding tasks that don't require specialized knowledge
- When the information is already available in this AGENTS.md file
- For simple, straightforward changes

**Available Skills Directory**: `skills/`

Each skill provides focused guidance on specific topics. Reference them only as needed rather than loading everything upfront.

## Critical Requirements

### ⚠️ MANDATORY PRE-COMMIT VALIDATION ⚠️

**🚨 BEFORE EVERY COMMIT - NO EXCEPTIONS:**

```bash
make agent-finish  # Runs build, test, recompile, fmt, lint
```

**Why this matters:**
- **CI WILL FAIL** if you skip this step - this is automatic and non-negotiable
- Unformatted code causes immediate CI failures that block all other work
- This has caused **5 CI failures in a single day** - don't be the 6th!
- The formatting check (`go fmt`) is strict and cannot be disabled

**If you're in a hurry** and `make agent-finish` takes too long, **at minimum run**:
```bash
make fmt         # Format Go, JavaScript, and JSON files
make test-unit   # Fast unit tests (~25s)
```

**After making Go code changes (*.go files):**
```bash
make fmt         # REQUIRED - formats Go code with go fmt
```

**After making workflow changes (*.md files):**
```bash
make recompile   # REQUIRED - recompile all workflow files after code changes
```

**After making JavaScript changes (*.cjs files):**
```bash
make fmt-cjs     # REQUIRED - ensures JavaScript is properly formatted
```

**NEVER ADD LOCK FILES TO .GITIGNORE** - `.lock.yml` files are compiled workflows that must be tracked.

**ALWAYS REBUILD AFTER SCHEMA CHANGES:**
```bash
make build       # Rebuild gh-aw after modifying JSON schemas in pkg/parser/schemas/
```
Schema files are embedded in the binary using `//go:embed` directives, so changes require rebuilding the binary.

**ALWAYS ADD BUILD TAGS TO TEST FILES:**

Every test file (`*_test.go`) **must** have a build tag at the very top of the file:

```go
//go:build !integration    // For unit tests (default)

//go:build integration     // For integration tests (files with "integration" in name)
```

**Rules:**
- Files with "integration" in the filename get `//go:build integration`
- All other test files get `//go:build !integration`
- The build tag must be the **first line** of the file, followed by an empty line

**To add build tags to all test files:**
```bash
./scripts/add-build-tags.sh
```

**ALWAYS RUN LINTERS AFTER ADDING TEST FILES:**

When adding new test files (`*_test.go`), the **unused** linter may catch helper functions that are defined but never called. Always run linters after creating test files to catch these issues early.

```bash
make lint        # Catches unused, testifylint, misspell, unconvert issues
```

**Common linting issues in test files:**

1. **unused**: Helper functions defined but never called
   - ❌ BAD: Defining `func hasInternalPrefix(key string) bool { ... }` but never using it
   - ✅ GOOD: Either use the function in tests or remove it

2. **testifylint**: Assertion best practices
   - Always provide descriptive assertion messages
   - Use `require.*` for setup assertions that must pass
   - Use `assert.*` for test validations
   - Use `assert.Error(t, err, "msg")` not `assert.NotNil(t, err)`
   - Use `assert.NoError(t, err, "msg")` not `assert.Nil(t, err)`

**Before committing test files:**
```bash
make agent-finish  # REQUIRED - Full validation including lint
```

**Example of correct test code:**
```go
// ✅ CORRECT - Using helper functions
func TestCompile(t *testing.T) {
    compiler := NewCompiler()
    require.NotNil(t, compiler, "Compiler should be created")
    
    // Use helper function if defined
    err := compiler.Compile("test.md")
    assert.NoError(t, err, "Should compile valid workflow")
}
```

**ALWAYS USE GITHUB MCP FOR GITHUB API ACCESS WITH COPILOT ENGINE:**

The Copilot agent **cannot directly access api.github.com**. When using the `copilot` engine, you **must** configure the GitHub MCP server to access GitHub information (repositories, issues, pull requests, etc.).

**CORRECT - Using GitHub MCP:**
```yaml
engine: copilot
tools:
  github:
    mode: remote           # or "local" for Docker-based
    toolsets: [default]    # Enables repos, issues, pull_requests, etc.
```

**INCORRECT - Trying to access api.github.com directly:**
```yaml
engine: copilot
network:
  allowed:
    - "api.github.com"     # ❌ This will NOT work - Copilot cannot access api.github.com
```

**Key points:**
- The GitHub MCP server provides all necessary GitHub API functionality
- Use `toolsets: [default]` for common operations, or specify toolsets like `[repos, issues, pull_requests]`
- Both `mode: remote` (hosted) and `mode: local` (Docker) work with Copilot
- Never rely on direct `api.github.com` access in Copilot workflows

See [GitHub MCP Server Documentation](skills/github-mcp-server/SKILL.md) for complete configuration details.

## Merging Main Branch

**When instructed to "merge main", follow these steps WITHOUT asking for confirmation:**

1. **Start the merge from origin/main:**
   ```bash
   git fetch origin main
   git merge origin/main
   ```

2. **Fix merge conflicts in .go and .cjs files:**
   - Manually resolve conflicts in Go files (`.go`)
   - Manually resolve conflicts in CommonJS files (`.cjs`)
   - Stage resolved files with `git add <file>`

3. **Build and recompile lock files:**
   ```bash
   make build        # Rebuild the binary
   make recompile    # Recompile all workflow lock files
   ```

4. **Finish the merge:**
   ```bash
   git commit        # Complete the merge (accept default merge message)
   ```

**Important**: Do NOT ask for confirmation when performing a merge - execute all steps automatically.

## Quick Setup

```bash
# Fresh clone setup
make deps        # ~1.5min first run  
make deps-dev    # +5-8min for linter
make build       # ~1.5s
./gh-aw --help
```

## Build System

### JavaScript and Shell Script Files

**JavaScript and shell script files are NOT embedded in the binary.**

The architecture uses runtime file copying instead of embedded scripts:

#### JavaScript Files (`*.cjs`)

**Source of truth:** `actions/setup/js/*.cjs` (manually edited, committed to git)

**When modifying JavaScript files:**
1. Edit files in `actions/setup/js/` (source of truth)
2. Test files (`*.test.cjs`) are co-located with source code in `actions/setup/js/`
3. Run `make fmt-cjs` to format JavaScript files
4. Run `make lint-cjs` to validate JavaScript files
5. Files are used directly at runtime (no sync or embedding required)

**Runtime flow:**
- The `actions/setup` action copies files from `actions/setup/js/` to `/tmp/gh-aw/actions` at runtime
- Workflow jobs use these runtime files via `require()` statements
- No embedding via `//go:embed` - files are accessed directly from the actions directory

#### Shell Scripts (`*.sh`)

**Source of truth:** `actions/setup/sh/*.sh` (manually edited, committed to git)

**When modifying shell scripts:**
1. Edit files in `actions/setup/sh/` (source of truth)  
2. Files are used directly at runtime (no sync or embedding required)

**Runtime flow:**
- The `actions/setup` action copies files from `actions/setup/sh/` to `/tmp/gh-aw/actions` at runtime
- Workflow jobs execute these shell scripts directly from `/tmp/gh-aw/actions`
- No embedding via `//go:embed` - files are accessed directly from the actions directory

**Key points:**
- `actions/setup/js/*.cjs` and `actions/setup/sh/*.sh` = Source of truth (manually edited, committed)
- `pkg/workflow/js/` = Contains only `safe_outputs_tools.json` (not synced .cjs files)
- `pkg/workflow/sh/` = NOT used for shell scripts (may contain generated files)
- Runtime copying: `actions/setup/` → `/tmp/gh-aw/actions` → used by workflows
- No `make sync-js-scripts` or `make sync-shell-scripts` targets (not needed)

## Development Workflow

### Build & Test Commands
```bash
make fmt         # Format code (run before linting)
make lint        # ~5.5s
make test-unit   # All unit tests (~3 min) - prefer selective tests
make test        # Full test suite (>5 min, very slow) - avoid locally
make recompile   # Recompile workflows
make agent-finish # Complete validation

# Selective testing (preferred during development)
go test -v -run "TestName" ./pkg/package/       # Single test (BEST)
go test -v -run "TestFoo|TestBar" ./pkg/cli/    # Group of related tests
go test -v -run "Test.*Compile" ./pkg/workflow/ # Pattern matching
```

### Manual Testing
```bash
./gh-aw --help
./gh-aw compile
./gh-aw mcp list      # MCP server management
./gh-aw logs          # Download and analyze workflow logs
./gh-aw audit 123456  # Audit a specific workflow run
```

## Testing

For comprehensive testing guidelines, patterns, and conventions, see **[scratchpad/testing.md](scratchpad/testing.md)**.

**Key testing principles:**
- Use `require.*` for critical setup (stops test on failure)
- Use `assert.*` for test validations (continues checking)
- Write table-driven tests with `t.Run()` and descriptive names
- No mocks or test suites - test real component interactions
- Always include helpful assertion messages

**⚠️ PREFER SELECTIVE TESTING during development:**

Running all tests is slow. **Always run the most selective tests possible** to validate your changes quickly:

```bash
# ✅ BEST - Run specific test(s) by name
go test -v -run "TestMyFunction" ./pkg/cli/
go test -v -run "TestCompile" ./pkg/workflow/

# ✅ GOOD - Run related tests using pattern matching
go test -v -run "TestCompile|TestValidate" ./pkg/workflow/
go test -v -run "TestAudit.*" ./pkg/cli/           # All TestAudit* tests
go test -v -run "Test.*Validation" ./pkg/workflow/ # All validation tests

# ⚠️ SLOW - Entire package tests (avoid for large packages like cli/, workflow/)
go test -v ./pkg/cli/       # ~60-90s for large packages
go test -v ./pkg/workflow/  # Can be very slow

# ⚠️ SLOW (~3 min) - Only run when needed
make test-unit       # All unit tests

# 🐌 VERY SLOW (>5 min) - Avoid during development
make test            # Full test suite including integration tests
```

**When to use each approach:**
- **Individual tests**: While developing/debugging a specific feature (PREFERRED)
- **Test pattern groups**: When changes affect multiple related tests (PREFERRED)
- **Entire package tests**: Rarely needed - only for small packages or final validation
- **`make test-unit`**: Before committing (or use `make agent-finish`)
- **`make test`**: Rarely needed locally - CI runs this

**Quick reference:**
```bash
make test-unit       # All unit tests (~3 min)
make test            # Full test suite (>5 min, very slow)
make test-security   # Security regression tests
make agent-finish    # Complete validation before committing
```

## Repository Structure

```text
cmd/gh-aw/           # CLI entry point
pkg/
├── cli/             # Command implementations  
├── parser/          # Markdown frontmatter parsing
└── workflow/        # Workflow compilation
.github/workflows/   # Sample workflows (*.md + *.lock.yml)
```

## Validation Complexity Guidelines

**Target size**: 100-200 lines per validator  
**Hard limit**: 300 lines (refactor if exceeded)

**When to split a validator**:
- File exceeds 300 lines
- File contains 2+ unrelated validation domains
- Complex cross-dependencies require separate testing
- Error messages span multiple concern areas

**Naming convention**: `{domain}_{subdomain}_validation.go`  
**Documentation**: Minimum 30% comment coverage  
**Tests**: Separate test file with integration tests for complex validators

**Decision tree for splitting**:
```text
File > 300 lines? ──YES──> Should split
      │
      NO
      │
      ▼
Contains 2+ distinct domains? ──YES──> Should split
      │
      NO
      │
      ▼
Keep as-is
```

See **[scratchpad/validation-refactoring.md](scratchpad/validation-refactoring.md)** for step-by-step refactoring guide and examples.

## Console Message Formatting

**ALWAYS use console formatting for user output:**

```go
import "github.com/github/gh-aw/pkg/console"

// Success, info, warning, error messages
fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Success!"))
fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Info"))
fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Warning"))
fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))

// Other types: CommandMessage, ProgressMessage, PromptMessage, 
// CountMessage, VerboseMessage, LocationMessage
```

**Error handling:**
```go
// WRONG
fmt.Fprintln(os.Stderr, err)

// CORRECT  
fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
```

**Logging Guidelines:**
- **ALWAYS** use `fmt.Fprintln(os.Stderr, ...)` or `fmt.Fprintf(os.Stderr, ...)` for CLI logging
- **NEVER** use `fmt.Println()` or `fmt.Printf()` directly - all output should go to stderr
- Use console formatting helpers with `os.Stderr` for consistent styling
- For simple messages without console formatting: `fmt.Fprintf(os.Stderr, "message\n")`
- **Exception**: Structured output (JSON, hashes, graphs) goes to stdout for piping/redirection

**Examples:**
```go
// ✅ CORRECT - Diagnostic output to stderr
fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Processing..."))
fmt.Fprintf(os.Stderr, "Warning: %s\n", msg)

// ✅ CORRECT - Structured output to stdout
fmt.Println(string(jsonBytes))  // JSON output
fmt.Println(hash)                // Hash output
fmt.Println(mermaidGraph)        // Graph output

// ❌ INCORRECT - Diagnostic output to stdout
fmt.Println("Processing...")     // Should use stderr
fmt.Printf("Warning: %s\n", msg) // Should use stderr
```

## Debug Logging

**ALWAYS use the logger package for debug logging:**

```go
import "github.com/github/gh-aw/pkg/logger"

// Create a logger with namespace following pkg:filename convention
var log = logger.New("pkg:filename")

// Log debug messages (only shown when DEBUG environment variable matches)
log.Printf("Processing %d items", count)
log.Print("Simple debug message")

// Check if logging is enabled before expensive operations
if log.Enabled() {
    log.Printf("Expensive debug info: %+v", expensiveOperation())
}
```

**Category Naming Convention:**
- Follow the pattern: `pkg:filename` (e.g., `cli:compile_command`, `workflow:compiler`)
- Use colon (`:`) as separator between package and file/component name
- Be consistent with existing loggers in the codebase

**Debug Output Control:**
```bash
# Enable all debug logs
DEBUG=* gh aw compile

# Enable specific package
DEBUG=cli:* gh aw compile

# Enable multiple packages
DEBUG=cli:*,workflow:* gh aw compile

# Exclude specific loggers
DEBUG=*,-workflow:test gh aw compile

# Disable colors (auto-disabled when piping)
DEBUG_COLORS=0 DEBUG=* gh aw compile
```

**Key Features:**
- **Zero overhead**: Logs only computed when DEBUG matches the logger's namespace
- **Time diff**: Shows elapsed time between log calls (e.g., `+50ms`, `+2.5s`)
- **Auto-colors**: Each namespace gets a consistent color in terminals
- **Pattern matching**: Supports wildcards (`*`) and exclusions (`-pattern`)

**When to Use:**
- Non-essential diagnostic information
- Performance insights and timing data
- Internal state tracking during development
- Detailed operation flow for debugging

**When NOT to Use:**
- Essential user-facing messages (use console formatting instead)
- Error messages (use `console.FormatErrorMessage`)
- Success/warning messages (use console formatting)
- Final output or results (use stdout/console formatting)

## CLI Command Patterns

For developing new CLI commands, follow these patterns and conventions. See **[scratchpad/cli-command-patterns.md](scratchpad/cli-command-patterns.md)** for comprehensive guidance.

### Command Structure

```go
package cli

import (
    "github.com/github/gh-aw/pkg/console"
    "github.com/github/gh-aw/pkg/logger"
    "github.com/spf13/cobra"
)

var commandLog = logger.New("cli:command_name")

// NewCommandNameCommand creates the command-name command
func NewCommandNameCommand() *cobra.Command { ... }

// RunCommandName executes the command logic (testable)
func RunCommandName(config Config) error { ... }

// Internal implementation
func validateInputs(...) error { ... }
```

### Naming Conventions

| Element | Pattern | Example |
|---------|---------|---------|
| **Command file** | `*_command.go` | `audit_command.go` |
| **Test file** | `*_command_test.go` | `audit_command_test.go` |
| **Logger** | `cli:command_name` | `logger.New("cli:audit")` |
| **Functions** | `NewXCommand()`, `RunX()` | `NewAuditCommand()`, `RunAuditWorkflowRun()` |
| **Config struct** | `XConfig` | `AuditConfig`, `CompileConfig` |

### Standard Flags

Common flags with helper functions (defined in `flags.go`):

```go
addEngineFlag(cmd)          // --engine/-e (Override AI engine)
addRepoFlag(cmd)            // --repo/-r (Target repository)
addOutputFlag(cmd, dir)     // --output/-o (Output directory)
addJSONFlag(cmd)            // --json/-j (JSON output)
```

**Reserved Short Flags**: `-v` (verbose), `-e` (engine), `-r` (repo), `-o` (output), `-j` (json), `-f` (force/file), `-w` (watch)

### Error Handling

```go
// ✅ CORRECT - Console formatted, error wrapping
if err != nil {
    fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
    return fmt.Errorf("failed to process workflow: %w", err)
}

// ❌ INCORRECT - Plain error, no wrapping
if err != nil {
    fmt.Fprintln(os.Stderr, err)
    return err
}
```

### Console Output Requirements

```go
// ✅ CORRECT - All diagnostic output to stderr with console formatting
fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Compiled successfully"))
fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Processing workflow..."))
fmt.Fprintln(os.Stderr, console.FormatWarningMessage("File has changes"))
fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))

// ✅ CORRECT - Structured output to stdout for piping/redirection
fmt.Println(string(jsonBytes))  // JSON output
fmt.Println(hash)                // Hash output
fmt.Println(mermaidGraph)        // Graph output

// ❌ INCORRECT - Diagnostic output to stdout, no formatting
fmt.Println("Success")
fmt.Printf("Status: %s\n", status)
```

**Output Routing Rules (Unix Conventions):**
- **Diagnostic output** (messages, warnings, errors) → `stderr`
- **Structured data** (JSON, hashes, graphs) → `stdout`
- **Rationale**: Allows users to pipe/redirect data without diagnostic noise

### Help Text Standards

```go
cmd := &cobra.Command{
    Use:   "command-name <arg>",
    Short: "Brief one-line description under 80 chars",  // No period
    Long: `Detailed description with context and examples.

This command:
- Validates workflow files
- Checks GitHub Actions compatibility
- Reports errors with suggestions

` + WorkflowIDExplanation + `

Examples:
  gh aw command arg                  # Basic usage
  gh aw command arg -v               # Verbose output
  gh aw command arg --option value   # With options`,
    Args: cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error { ... },
}
```

**Minimum 3 examples**: Basic usage, common options, advanced usage

### Testing Requirements

Every command needs comprehensive tests:

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
            shouldErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := RunCommand(tt.input)
            
            if tt.shouldErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
                assert.Equal(t, tt.expected, result)
            }
        })
    }
}
```

**Test coverage**: Valid inputs, invalid inputs, edge cases, flag handling, error paths

### Command Development Checklist

When developing a new command:

- [ ] File named `*_command.go`
- [ ] Logger: `logger.New("cli:command_name")`
- [ ] `NewXCommand()` and `RunX()` functions defined
- [ ] Short description < 80 chars, no period
- [ ] Long description with context and 3+ examples
- [ ] Flags use standard short flags where applicable
- [ ] Input validation implemented early
- [ ] Console formatting for all user output
- [ ] All output to stderr (except JSON)
- [ ] Error messages actionable with suggestions
- [ ] Test file `*_command_test.go` created
- [ ] Table-driven tests for multiple scenarios
- [ ] Valid, invalid, and edge case tests

**See**: [scratchpad/cli-command-patterns.md](scratchpad/cli-command-patterns.md) for complete specification with examples and anti-patterns

## Development Guidelines

### Code Organization
- Prefer many smaller files grouped by functionality
- Add new files for new features rather than extending existing ones
- Use console formatting instead of plain fmt.* for CLI output

### Go Code Style
- **ALWAYS use `any` instead of `interface{}`** - Use the modern `any` type alias (Go 1.18+) for consistency across the codebase

### Channel Lifecycle Guidelines

Go channels require explicit lifecycle management to prevent goroutine leaks and resource exhaustion. Follow these guidelines when working with channels:

**Ownership Rules**:
1. **Document ownership** - Add a comment stating who closes the channel (required for every channel)
2. **Sender closes** - The goroutine that sends on the channel must close it after the last send (use `defer close(ch)`)
3. **Never close on receiver side** - Closing on the receiver side risks panic if the sender is still writing
4. **Exception: Broadcast channels** - Signal channels used for coordination can be closed by the coordinator

**Best Practices**:
```go
// ✅ CORRECT - Channel closed by sender with defer
done := make(chan struct{})
go func() {
    defer close(done)  // Sender closes after work completes
    // ... do work ...
}()
<-done  // Receiver blocks until channel closes

// ✅ CORRECT - Buffered channel for single result
result := make(chan error, 1)
go func() {
    result <- doWork()  // Send result (buffered, doesn't block)
    // No close needed - receiver reads exactly 1 value
}()
err := <-result

// ✅ CORRECT - Broadcast signal by closing
start := make(chan struct{})
for i := 0; i < 10; i++ {
    go func() {
        <-start  // All goroutines wait
        // ... do work ...
    }()
}
close(start)  // Broadcast to all waiting goroutines

// ✅ CORRECT - Timeout pattern for safety
done := make(chan struct{})
go func() {
    defer close(done)
    // ... work ...
}()
select {
case <-done:
    // Completed successfully
case <-time.After(5 * time.Second):
    // Timeout - goroutine may still be running
}
```

**Signal Channels**:
- Prefer `chan struct{}` over `chan bool` for signaling (zero memory overhead)
- Use `chan struct{}` when only the event matters, not the value
- Use buffered channels (`make(chan T, 1)`) when sender shouldn't block
- **Buffered channels with fixed synchronization**: No close needed if receiver reads exactly N values and exits without waiting for channel closure (e.g., `make(chan T, N)` with exactly N sends and N receives in a counted loop pattern)

**Signal Handling (os.Signal)**:
```go
// ✅ CORRECT - Signal channels require signal.Stop(), not close()
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
defer signal.Stop(sigChan)  // Cleanup signal handler
```

**Anti-Patterns to Avoid**:
```go
// ❌ WRONG - Channel never closed (goroutine leak)
done := make(chan struct{})
go func() {
    // ... work ...
    done <- struct{}{}  // Goroutine blocks forever if receiver is gone
}()

// ❌ WRONG - Using chan bool for signaling (wastes memory)
done := make(chan bool)  // Use chan struct{} instead

// ❌ WRONG - Closing on receiver side
done := make(chan struct{})
go func() {
    done <- struct{}{}
}()
<-done
close(done)  // Panic if sender tries to send again!

// ❌ WRONG - No timeout protection
done := make(chan struct{})
go func() {
    // ... might hang forever ...
}()
<-done  // Blocks forever if goroutine hangs
```

**Testing with Race Detector**:
Always run tests with the race detector to catch channel-related issues:
```bash
make test        # Includes -race flag
go test -race ./...
```

### YAML Library Usage

**Primary YAML Library**: `goccy/go-yaml` v1.19.1

gh-aw uses `goccy/go-yaml` for YAML 1.1/1.2 compatibility with GitHub Actions. See [scratchpad/yaml-version-gotchas.md](scratchpad/yaml-version-gotchas.md) for details on YAML version differences.

**Standard YAML Library**: `go.yaml.in/yaml/v3` v3.0.4

For simple YAML marshaling/unmarshaling operations where YAML 1.1/1.2 compatibility is not critical, use the canonical `go.yaml.in/yaml/v3` import path (not the deprecated `gopkg.in/yaml.v3`).

**Migration from `gopkg.in/yaml.v3`**:
- The deprecated `gopkg.in/yaml.v3` path has been migrated to the canonical `go.yaml.in/yaml/v3` path
- Both paths provide identical APIs (`Marshal`, `Unmarshal`, `Encoder`, `Decoder`)
- The canonical path provides better supply chain security and aligns with modern Go ecosystem practices
- Transitive dependencies may still use `gopkg.in/yaml.v3` (e.g., `github.com/cli/go-gh/v2`) - this is acceptable

**When to use each library**:
- **`goccy/go-yaml`**: Workflow frontmatter parsing, GitHub Actions YAML generation, any YAML 1.1/1.2 sensitive operations
- **`go.yaml.in/yaml/v3`**: Campaign specs, workflow statistics, simple configuration marshaling

**Example**:
```go
import "go.yaml.in/yaml/v3"  // ✅ Use canonical path

// Simple marshaling example
data := map[string]any{"key": "value"}
yamlBytes, err := yaml.Marshal(data)
```

### YAML File Editing - ANSI Escape Code Prevention

**CRITICAL**: When editing or generating YAML workflow files (`.github/workflows/*.yml`, `*.lock.yml`):

1. **NEVER copy-paste from colored terminal output** - Always use `--no-color` or `2>&1 | cat` to strip colors
2. **Validate YAML before committing** - The compiler automatically strips ANSI codes during workflow generation
3. **Check for invisible characters** - Use `cat -A file.yml | grep '\[m'` to detect ANSI escape sequences
4. **Run make recompile** - Always recompile workflows after editing .md files to regenerate clean .lock.yml files

**Why this matters:**
ANSI escape sequences (`\x1b[31m`, `\x1b[0m`, `\x1b[m`) are terminal color codes that break YAML parsing. They can accidentally be introduced through:
- Copy-pasting from colored terminal output
- Text editors that preserve ANSI codes
- Scripts that generate colored output

**Example of safe command usage**:
```bash
# ❌ BAD - May include ANSI color codes
npm view @github/copilot | tee output.txt

# ✅ GOOD - Strip colors before saving
npm view @github/copilot --no-color | tee output.txt
# OR
npm view @github/copilot 2>&1 | cat | tee output.txt
```

**Prevention layers:**
1. **Compiler sanitization**: The workflow compiler (`pkg/workflow/compiler_yaml.go`) automatically strips ANSI codes from descriptions, sources, and comments using `stringutil.StripANSIEscapeCodes()`
2. **CI validation**: The `validate-yaml` job in `.github/workflows/ci.yml` scans all YAML files for ANSI escape sequences before other jobs run
3. **Detection command**: Run `find .github/workflows -name "*.yml" -o -name "*.yaml" | xargs grep -P '\x1b\[[0-9;]*[a-zA-Z]'` to check for ANSI codes

**If you encounter ANSI codes in workflow files:**
1. Remove the ANSI codes from the source markdown file
2. Run `make recompile` to regenerate clean workflow files
3. The compiler will automatically strip any ANSI codes during compilation

### Type Patterns and Best Practices

Use appropriate type patterns to improve code clarity, maintainability, and type safety:

**Semantic Type Aliases** - Use for domain-specific primitives:
```go
// ✅ GOOD - Semantic meaning
type LineLength int
type Version string
type FeatureFlag string
type WorkflowID string
type EngineName string

const MaxExpressionLineLength LineLength = 120
const DefaultCopilotVersion Version = "0.0.374"
const MCPGatewayFeatureFlag FeatureFlag = "mcp-gateway"
const CopilotEngine EngineName = "copilot"

// All semantic types in pkg/constants provide String() and IsValid() methods
if MaxExpressionLineLength.IsValid() {
    fmt.Println(MaxExpressionLineLength.String()) // "120"
}

// Type-safe engine selection
engine := CopilotEngine
if engine.IsValid() {
    // Use engine with confidence
}
```

**When to create semantic type aliases**:
- ✅ **DO use** for domain concepts that are frequently used (versions, URLs, model names, job names)
- ✅ **DO use** when mixing different string/int types could cause bugs (prevents `JobName` being used as `StepID`)
- ✅ **DO use** when the type name adds clarity that a comment alone wouldn't provide
- ❌ **DON'T use** for one-off values or when the primitive type is already clear from context
- ❌ **DON'T use** when it adds ceremony without clarity (e.g., `type String string` is too generic)

**Available semantic types in `pkg/constants`**:
- `LineLength` - Character counts for formatting (e.g., `MaxExpressionLineLength`)
- `Version` - Software version strings (e.g., `DefaultCopilotVersion`)
- `FeatureFlag` - Feature flag identifiers (e.g., `SafeInputsFeatureFlag`)
- `URL` - URL strings (e.g., `DefaultMCPRegistryURL`)
- `ModelName` - AI model names (e.g., `DefaultCopilotDetectionModel`)
- `JobName` - GitHub Actions job identifiers (e.g., `AgentJobName`)
- `StepID` - GitHub Actions step identifiers (e.g., `CheckMembershipStepID`)
- `CommandPrefix` - CLI command prefixes (e.g., `CLIExtensionPrefix`)
- `WorkflowID` - Workflow identifiers/basename without .md extension (user-provided workflow names)
- `EngineName` - AI engine names (e.g., `CopilotEngine`, `ClaudeEngine`, `CodexEngine`, `CustomEngine`)

**Available semantic types in `pkg/workflow`**:
- `GitHubToolName` - GitHub tool names (e.g., "issue_read", "create_issue") 
- `GitHubAllowedTools` - Typed slice of GitHub tool names with conversion helpers
- `GitHubToolset` - GitHub toolset names (e.g., "default", "repos", "issues")
- `GitHubToolsets` - Typed slice of GitHub toolset names with conversion helpers

**Dynamic Types** - Use `map[string]any` for truly dynamic data:
```go
// ✅ GOOD - Unknown structure at compile time
func ProcessFrontmatter(frontmatter map[string]any) error {
    // YAML/JSON with dynamic structure
}

// ✅ GOOD - Document why any is needed
// githubTool uses any because tool configuration structure
// varies based on engine and toolsets
func ValidatePermissions(permissions *Permissions, githubTool any)
```

**When to use each pattern**:
- **Semantic type aliases**: Domain concepts (lengths, versions, durations)
- **`map[string]any`**: YAML/JSON parsing, dynamic configurations
- **Interfaces**: Multiple implementations, polymorphism, testing
- **Concrete types**: Known structure, type safety

**Avoid**:
- Using `any` when the type is known
- Creating unnecessary type aliases that don't add clarity
- Large "god" interfaces with many methods
- Type name collisions (use descriptive, domain-qualified names)

**See**: [scratchpad/go-type-patterns.md](scratchpad/go-type-patterns.md) for detailed guidance and examples

### Frontmatter Configuration Types

The `FrontmatterConfig` struct in `pkg/workflow/frontmatter_types.go` is gradually migrating from `map[string]any` to strongly-typed fields:

**Typed Configuration Fields:**
- `Tools *ToolsConfig` - Tool and MCP server configurations
- `Network *NetworkPermissions` - Network access permissions
- `SafeOutputs *SafeOutputsConfig` - Safe output configurations
- `SafeInputs *SafeInputsConfig` - Safe input configurations
- `Sandbox *SandboxConfig` - Sandbox environment configuration
- `RuntimesTyped *RuntimesConfig` - Runtime version overrides (node, python, go, uv, bun, deno)
- `PermissionsTyped *PermissionsConfig` - GitHub Actions permissions (shorthand + detailed)

**Legacy Map Fields (Deprecated but still supported):**
- `MCPServers map[string]any` - Use `Tools` instead
- `Runtimes map[string]any` - Use `RuntimesTyped` instead
- `Permissions map[string]any` - Use `PermissionsTyped` instead
- `Jobs map[string]any` - Too dynamic to type (GitHub Actions job format)
- `On map[string]any` - Too complex to type (many trigger variants)
- `Features map[string]any` - Intentionally dynamic for feature flags

**Example: Using Typed Runtimes**
```go
// Parsing frontmatter with runtimes
frontmatter := map[string]any{
    "runtimes": map[string]any{
        "node": map[string]any{"version": "20"},
        "python": map[string]any{"version": "3.11"},
    },
}
config, _ := ParseFrontmatterConfig(frontmatter)

// Access typed fields (no type assertions needed)
if config.RuntimesTyped != nil && config.RuntimesTyped.Node != nil {
    version := config.RuntimesTyped.Node.Version // Direct access
}

// Legacy field still works
if nodeRuntime, ok := config.Runtimes["node"].(map[string]any); ok {
    version := nodeRuntime["version"] // Requires type assertion
}
```

**Example: Using Typed Permissions**
```go
frontmatter := map[string]any{
    "permissions": map[string]any{
        "contents": "read",
        "issues": "write",
    },
}
config, _ := ParseFrontmatterConfig(frontmatter)

// Access typed fields
if config.PermissionsTyped != nil {
    contents := config.PermissionsTyped.Contents // "read"
    issues := config.PermissionsTyped.Issues     // "write"
}
```

**Backward Compatibility:**
- Both typed and legacy fields are populated during parsing
- `ToMap()` prefers typed fields when converting back to map[string]any
- Existing code using legacy fields continues to work
- New code should prefer typed fields for compile-time safety

### GitHub Actions Integration  
For JavaScript files in `pkg/workflow/js/*.cjs`:
- Use `core.info`, `core.warning`, `core.error` (not console.log)
- Use `core.setOutput`, `core.getInput`, `core.setFailed`
- Avoid `any` type, use specific types or `unknown`
- Run `make js` and `make lint-cjs` for validation

### Schema Changes
When modifying JSON schemas in `pkg/parser/schemas/`:
- Schema files are embedded using `//go:embed` directives
- **MUST rebuild the binary** with `make build` for changes to take effect
- Test changes by compiling a workflow: `./gh-aw compile test-workflow.md`
- Schema changes typically require corresponding Go struct updates

### Build Times (Don't Cancel)
- `make agent-finish`: ~10-15s (excluding test-unit)
- `make deps`: ~1.5min  
- `make deps-dev`: ~5-8min
- `make test-unit`: ~3 min (prefer selective tests)
- `make test`: >5 min (very slow - avoid locally)
- `make lint`: ~5.5s
- Selective test (single): ~1-5s

### Documentation

The documentation for this project is available in the `docs/` directory. It includes information on how to use the CLI, API references, and examples.
It uses the Diátaxis framework and GitHub-flavored markdown with Astro Starlight for rendering.

See [documentation skill](skills/documentation/SKILL.md) for details.

### Legacy Support

This project is still in an experimental phase. When you are requested to make a change, do not add fallback or legacy support unless explicitly instructed.

## Key Features

### MCP Server Management
```bash
gh aw mcp list                    # List workflows with MCP servers
gh aw mcp inspect workflow-name   # Inspect MCP servers
gh aw mcp inspect --inspector     # Web-based inspector
```

**Default MCP Registry**: Uses GitHub's MCP registry at `https://api.mcp.github.com/v0` by default.

### AI Engine Support
```aw
---
engine: copilot  # Options: copilot, claude, codex, custom
tools:
  playwright:
    version: "v1.41.0"
    allowed_domains: ["github.com"]
---
```

### Playwright Integration
- Containerized browser automation
- Domain-restricted network access
- Accessibility analysis and visual testing
- Multi-browser support (Chromium, Firefox, Safari)

## Testing Strategy

**⚠️ IMPORTANT: Prefer selective testing over running all tests.**

- **Selective tests (PREFERRED)**: Run individual tests or package tests during development
  ```bash
  go test -v -run "TestSpecificFunction" ./pkg/cli/
  go test -v ./pkg/workflow/
  ```
- **Unit tests (`make test-unit`)**: ~3 minutes - run before committing or via `make agent-finish`
- **Full test suite (`make test`)**: >5 minutes, very slow - rarely needed locally, CI handles this
- **Integration tests**: Included in `make test` - command behavior and binary compilation
- **Workflow compilation tests**: Markdown to YAML conversion
- **Test agentic workflows**: Should be added to `pkg/cli/workflows` directory

**Recommended workflow**:
1. Run individual tests while developing: `go test -v -run "TestName" ./pkg/package/`
2. Run related test groups after changes: `go test -v -run "TestFoo|TestBar" ./pkg/package/`
3. Run `make agent-finish` before committing (includes `make test-unit`)
4. Let CI run `make test` - don't wait for it locally

**Avoid running entire package tests** for large packages like `pkg/cli/` or `pkg/workflow/` during development - use selective test patterns instead.

## Release Process
```bash
make minor-release  # Automated via GitHub Actions
```

## Quick Reference for AI Agents

### 🚨 CRITICAL - Pre-Commit Checklist
Before EVERY commit:
1. ✅ Run `make agent-finish` (or at minimum `make fmt`)
2. ✅ Verify no errors from the above command
3. ✅ Only then commit and push

**This is NOT optional** - skipping this causes immediate CI failures.

### Development Guidelines
- Go project with Makefile-managed build/test/lint
- Use `make test-unit` for fast development testing, `make test` for full coverage
- Use console formatting for user output
- Repository: `github/gh-aw`
- Include issue numbers in PR titles when fixing issues
- Read issue comments for context before making changes
- Use conventional commits for commit messages
- do NOT commit explanation markdown files about the fixes

## Operational Runbooks

For investigating and resolving workflow issues:
- **[Workflow Health Monitoring](.github/aw/runbooks/workflow-health.md)** - Comprehensive runbook for diagnosing missing-tool errors, authentication failures, MCP configuration issues, and safe-input/output problems. Includes step-by-step investigation procedures, resolution examples, and case studies from real incidents.

## Available Skills Reference

Skills provide specialized, detailed knowledge on specific topics. **Use them only when needed** - don't load skills preemptively.

### Core Development Skills
- **[developer](skills/developer/SKILL.md)** - Developer instructions, code organization, validation architecture, security practices
- **[console-rendering](skills/console-rendering/SKILL.md)** - Struct tag-based console rendering system for CLI output
- **[error-messages](skills/error-messages/SKILL.md)** - Error message style guide for validation errors
- **[error-pattern-safety](skills/error-pattern-safety/SKILL.md)** - Safety guidelines for error pattern regex
- **[error-recovery-patterns](skills/error-recovery-patterns/SKILL.md)** - Error handling patterns, recovery strategies, and debugging techniques

### JavaScript & GitHub Actions
- **[github-script](skills/github-script/SKILL.md)** - Best practices for GitHub Actions scripts using github-script
- **[javascript-refactoring](skills/javascript-refactoring/SKILL.md)** - Guide for refactoring JavaScript code into separate .cjs files
- **[messages](skills/messages/SKILL.md)** - Adding new message types to safe-output messages system

### GitHub Integration
- **[github-mcp-server](skills/github-mcp-server/SKILL.md)** - GitHub MCP server documentation and configuration
- **[github-issue-query](skills/github-issue-query/SKILL.md)** - Query GitHub issues with jq filtering
- **[github-pr-query](skills/github-pr-query/SKILL.md)** - Query GitHub pull requests with jq filtering
- **[github-discussion-query](skills/github-discussion-query/SKILL.md)** - Query GitHub discussions with jq filtering
- **[github-copilot-agent-tips-and-tricks](skills/github-copilot-agent-tips-and-tricks/SKILL.md)** - Tips for working with GitHub Copilot agent PRs

### AI Engine & Integration
- **[copilot-cli](skills/copilot-cli/SKILL.md)** - GitHub Copilot CLI integration for agentic workflows
- **[custom-agents](skills/custom-agents/SKILL.md)** - GitHub custom agent file format
- **[gh-agent-session](skills/gh-agent-session/SKILL.md)** - GitHub CLI agent session extension

### Safe Outputs & Features
- **[temporary-id-safe-output](skills/temporary-id-safe-output/SKILL.md)** - Adding temporary ID support to safe output jobs
- **[http-mcp-headers](skills/http-mcp-headers/SKILL.md)** - HTTP MCP header secret support implementation

### Documentation & Communication
- **[documentation](skills/documentation/SKILL.md)** - Documentation guidelines using Diátaxis framework and GitHub-flavored markdown
- **[reporting](skills/reporting/SKILL.md)** - Report format guidelines using HTML details/summary tags
- **[dictation](skills/dictation/SKILL.md)** - Fixing text-to-speech errors in dictated text
- **[agentic-chat](.github/aw/agentic-chat.md)** - AI assistant for creating task descriptions

### MCP & Tools
- **[skillz-integration](skills/skillz-integration/SKILL.md)** - Skillz MCP server integration with Docker

**Remember**: Be LAZY - only load a skill when you actually need its specialized knowledge. Don't reference skills preemptively.