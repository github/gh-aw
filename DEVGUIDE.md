# Developer Guide

## Development Environment Setup

### 1. Clone and Setup Repository

```bash
# Clone the repository
git clone https://github.com/github/gh-aw.git
cd gh-aw
```

### 2. Install Development Dependencies

```bash
# Install basic Go dependencies
make deps

# For full development (including linter)
make deps-dev
```

### 3. Build and Verify Development Environment

```bash
# Verify GitHub CLI is authenticated
gh auth status

# Run all tests to ensure everything works
make test

# Check code formatting
make fmt-check

# Run linter (may require golangci-lint installation)
make lint

# Build and test the binary
make build
./gh-aw --help
```

### 4. Install the Extension Locally for Testing

```bash
# Install the local version of gh-aw extension
make install

# Verify installation
gh aw --help
```

## Common Development Tasks

This section provides quick answers to common development scenarios. The repository has 75+ Makefile targets - this guide helps you find the right command quickly.

### I want to...

#### Test my code changes quickly
```bash
make test-unit  # Fast unit tests (~25s, recommended for development)
```
**When to use**: During active development for rapid feedback on your changes.

#### Run all tests before committing
```bash
make test  # All tests including integration (~30s)
```
**When to use**: Before creating a PR to ensure comprehensive validation.

#### Build the gh-aw binary
```bash
make build  # Includes sync-templates and sync-action-pins (~1.5s)
```
**When to use**: After making code changes to compile the binary.

**Note**: The build automatically syncs:
- Templates from `.github/` to `pkg/cli/templates/`
- Action pins from `.github/aw/actions-lock.json` to `pkg/workflow/data/action_pins.json`

#### Validate everything before committing
```bash
make agent-finish  # Complete validation (~10-15s)
```
**What it does**: Runs deps-dev, fmt, lint, build, test-all, fix, recompile, dependabot, generate-schema-docs, generate-agent-factory, and security-scan.

**When to use**: Before committing to ensure everything passes CI checks.

#### Format my code
```bash
make fmt  # Format Go, JavaScript, and JSON files
```
**When to use**: Before committing or when linter reports formatting issues.

#### Run the linter
```bash
make lint  # Full linting (includes format check) (~5.5s)
```
**When to use**: To catch code quality issues before committing.

#### Run linter on only changed files (faster)
```bash
make golint-incremental BASE_REF=origin/main  # 50-75% faster on PRs
```
**When to use**: During development to get quick feedback on your changes.

#### Compile a specific workflow
```bash
./gh-aw compile .github/workflows/my-workflow.md
```
**When to use**: Testing individual workflow compilation.

#### Watch and auto-compile workflows on changes
```bash
make watch  # Or: ./gh-aw compile --watch
```
**When to use**: Developing workflows with live reload.

#### Recompile all workflows after code changes
```bash
make recompile  # Recompile all .md workflows to .lock.yml
```
**When to use**: After modifying compiler code or workflow templates.

**Critical**: Always run this after changing workflow compilation logic.

#### Install dependencies for the first time
```bash
make deps      # Install Go and npm dependencies (~1.5min first run)
make deps-dev  # Add development tools like linter (~5-8min)
```
**When to use**: Fresh clone setup or after dependency changes.

#### Clean build artifacts
```bash
make clean  # Remove binaries, coverage files, security reports, etc.
```
**When to use**: To start fresh or troubleshoot build issues.

#### Run security scans
```bash
make security-scan  # Run gosec, govulncheck, and trivy
```
**When to use**: Before releases or when checking for vulnerabilities.

#### Run performance benchmarks
```bash
make bench               # Run all benchmarks (~30s)
make bench-compare       # Run with more iterations for benchstat comparison
make bench-memory        # Memory profiling with pprof output
```
**When to use**: Measuring compilation performance, detecting regressions, or optimizing.

**Interpreting results**:
- **ns/op**: Nanoseconds per operation (lower is better)
- **B/op**: Bytes allocated per operation (lower is better)
- **allocs/op**: Number of allocations per operation (lower is better)

**Performance baselines**:
- Simple workflows: <100ms compilation time
- Complex workflows: <500ms compilation time
- MCP-heavy workflows: <1s compilation time

**Comparing benchmark results**:
```bash
make bench                    # Baseline run, saves to bench_results.txt
# Make code changes
make bench-compare            # Comparison run, saves to bench_compare.txt
benchstat bench_results.txt bench_compare.txt  # Compare results
```

**Memory profiling**:
```bash
make bench-memory                # Generate mem.prof and cpu.prof
go tool pprof -http=:8080 mem.prof  # Interactive memory analysis
go tool pprof -http=:8080 cpu.prof  # Interactive CPU analysis
```

#### Check for slow tests
```bash
make test-perf  # Shows 10 slowest tests with timing
```
**When to use**: Optimizing test suite performance.

#### Validate workflows with actionlint
```bash
make actionlint  # Depends on build
```
**When to use**: Ensure compiled workflows are valid GitHub Actions.

#### Update GitHub Actions to latest versions
```bash
make update  # Update actions, sync pins, rebuild
```
**When to use**: Updating action versions in workflows.

### The Golden Path

For most development work, follow this sequence:

```bash
# 1. First time only - install dependencies
make deps deps-dev  # ~6-10min first time

# 2. After making code changes - build
make build  # ~1.5s

# 3. During development - fast feedback
make test-unit  # ~25s

# 4. Before committing - comprehensive validation
make agent-finish  # ~10-15s
```

### When to Use Each Test Target

The project has several test targets optimized for different scenarios:

| Target | Speed | What It Tests | Use When |
|--------|-------|---------------|----------|
| `test-unit` | ~25s | Unit tests only (excludes integration) | **Recommended** for rapid iteration during development |
| `test` | ~30s | Unit + all integration tests | Before committing, comprehensive validation |
| `test-integration-compile` | Varies | Workflow compilation integration tests | Testing compiler changes specifically |
| `test-integration-mcp-playwright` | Varies | MCP Playwright integration | Testing Playwright MCP functionality |
| `test-integration-mcp-other` | Varies | Other MCP integration tests | Testing GitHub/Config MCP features |
| `test-integration-logs` | Varies | Log parsing and analysis | Testing log-related functionality |
| `test-integration-workflow` | Varies | Workflow package integration | Testing workflow compilation end-to-end |
| `test-all` | ~30s | Go + JavaScript tests | Complete test coverage |
| `test-js` | Varies | JavaScript-only tests | Testing JS action code |
| `test-security` | Varies | Security regression tests | Validating security fixes |
| `test-coverage` | Varies | Tests with coverage report | Analyzing test coverage |
| `test-perf` | Varies | All tests + timing analysis | Finding slow tests |

**Quick decision guide**:
- **Developing a feature?** → `make test-unit`
- **Ready to commit?** → `make test` or `make agent-finish`
- **Changed compiler code?** → `make test-integration-compile`
- **Working on JavaScript?** → `make test-js`
- **Security-sensitive change?** → `make test-security`

### Expected Output and Timing

| Command | Approximate Time | Expected Output |
|---------|-----------------|-----------------|
| `make build` | ~1.5s | Binary created: `./gh-aw` |
| `make test-unit` | ~25s | All unit tests pass |
| `make test` | ~30s | All tests pass (unit + integration) |
| `make lint` | ~5.5s | Code quality checks pass |
| `make fmt` | ~2s | Code formatted successfully |
| `make deps` | ~1.5min (first run) | Dependencies installed |
| `make deps-dev` | ~5-8min (first run) | Dev tools installed |
| `make agent-finish` | ~10-15s | Complete validation passes |
| `make recompile` | Varies | All workflows compiled |
| `make clean` | ~5s | Build artifacts removed |

### Common Error Scenarios

#### "golangci-lint is not installed"
**Solution**: Run `make deps-dev` to install development dependencies.

#### "Node.js version X is not supported"
**Solution**: Install Node.js 20+ (see [CONTRIBUTING.md](CONTRIBUTING.md#prerequisites)).

#### Test failures after `git pull`
**Solution**: Rebuild dependencies and binary:
```bash
make deps
make build
make test
```

#### Workflows fail to compile
**Solution**: Ensure you've built the latest binary and synced templates:
```bash
make build
make recompile
```

#### "cannot find package" errors
**Solution**: Clean and reinstall dependencies:
```bash
make clean
make deps
make build
```

## Workflow Compilation

### Workflow Types and Compilation Expectations

The repository contains two types of workflow files:

#### 1. Standalone Workflows (Main Workflows)
- Located in `.github/workflows/*.md`
- **Must have** an `on:` trigger that defines when they run
- Can be compiled directly with `./gh-aw compile <workflow>.md`
- **Expected compilation rate**: 100% of standalone workflows should compile successfully

Example:
```yaml
---
on:
  issues:
    types: [opened]
engine: copilot
---
```

#### 2. Shared Workflow Components
- Located in `.github/workflows/shared/**/*.md`
- **Do NOT have** an `on:` trigger (they are partial configurations)
- Meant to be imported using the `imports:` field in other workflows
- **Cannot be compiled directly** - this is expected behavior

Example shared workflow:
```yaml
---
safe-outputs:
  app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
---
```

To use a shared workflow:
```yaml
---
on:
  issues:
    types: [opened]
imports:
  - .github/workflows/shared/app-config.md
engine: copilot
---
```

### Compilation Success Rate

As of the latest audit:
- **Total workflow files**: 175
- **Standalone workflows**: 127 (100% compile successfully)
- **Shared components**: 48 (intentionally cannot be compiled directly)

### Why Shared Workflows Don't Compile

Shared workflows are reusable configuration fragments that:
1. Provide common configuration (e.g., safe-outputs, MCP server configs)
2. Are imported and merged into standalone workflows during compilation
3. Don't need triggers since they inherit context from the importing workflow

If you try to compile a shared workflow directly, you'll see a helpful error message explaining this pattern.

## Build Tools

This project uses `tools.go` to track build-time tool dependencies. This ensures everyone uses the same tool versions.

### Install Tools

```bash
make tools
```

This installs all tools listed in `tools.go` at the versions specified in `go.mod`:

- **golangci-lint**: Go linter with comprehensive checks
- **actionlint**: GitHub Actions workflow linter
- **gosec**: Go security linter
- **gopls**: Go language server for IDE support
- **govulncheck**: Go vulnerability scanner

### Adding a New Tool

1. Add blank import to `tools.go`:
   ```go
   _ "github.com/example/tool/cmd/tool"
   ```

2. Update dependencies:
   ```bash
   go get github.com/example/tool/cmd/tool@latest
   go mod tidy
   ```

3. Install: `make tools`

### Tool Version Management

Tool versions are locked in `go.mod` and `go.sum`, ensuring:

- **Consistency**: Same tool versions in CI and local development
- **Reproducibility**: Tool versions are version-controlled
- **Simplicity**: Single command to install all tools
- **Discoverability**: `tools.go` shows all build tools at a glance

```bash
# Install the local version of gh-aw extension
make install

# Verify installation
gh aw --help
```

## Testing

### Test Structure

The project has comprehensive testing at multiple levels:

#### Unit Tests
```bash
# Run specific package tests
go test ./pkg/cli -v
go test ./pkg/parser -v  
go test ./pkg/workflow -v

# Run all unit tests
make test
```

#### End-to-End Tests
```bash
# Comprehensive test validation
make test-script
```

### Adding New Tests

1. **Unit tests**: Add to `pkg/*/package_test.go`
2. **Follow existing patterns**: Look at current tests for structure

### CI Test Artifacts

The CI workflow generates JSON test result artifacts with timing information that can be downloaded and analyzed:

#### Available Artifacts

- **test-result-unit.json**: Unit test results with timing data
- **test-result-integration-*.json**: Integration test results for each test group

#### JSON Format

Each test result file contains newline-delimited JSON (ndjson) with test events:

```json
{"Time":"2025-12-12T13:17:30Z","Action":"pass","Package":"github.com/github/gh-aw/pkg/logger","Elapsed":0.022}
```

Key fields:
- `Time`: ISO 8601 timestamp
- `Action`: Test event (`start`, `run`, `pass`, `fail`, `output`)
- `Package`: Go package being tested
- `Test`: Test name (if applicable)
- `Elapsed`: Test duration in seconds
- `Output`: Test output (for `output` actions)

#### Analyzing Test Timing

To extract timing information from artifacts:

```bash
# Download artifacts from a workflow run
gh run download <run-id>

# Extract slowest tests
cat test-result-unit.json | jq -r 'select(.Action == "pass" and .Test != null) | "\(.Elapsed)s \(.Test)"' | sort -rn | head -20

# Get package-level timing
cat test-result-unit.json | jq -r 'select(.Action == "pass" and .Test == null) | "\(.Elapsed)s \(.Package)"' | sort -rn
```

#### Mining Test Data

The JSON format enables various analyses:
- Identify slow tests across multiple runs
- Track test performance trends over time
- Detect flaky tests by comparing results
- Generate test execution reports

## CLI Command Development

When developing new CLI commands for `gh aw`, follow these established patterns and conventions to maintain consistency across the codebase.

### Quick Start Guide

1. **Create command file**: `pkg/cli/command_name_command.go`
2. **Create test file**: `pkg/cli/command_name_command_test.go`
3. **Follow the structure pattern** (see below)
4. **Use standard flags** from `flags.go`
5. **Implement comprehensive tests**
6. **Run validation**: `make agent-finish`

### Standard Command Structure

```go
package cli

import (
    "fmt"
    "os"
    
    "github.com/github/gh-aw/pkg/console"
    "github.com/github/gh-aw/pkg/logger"
    "github.com/spf13/cobra"
)

// Logger with namespace following cli:command_name convention
var commandLog = logger.New("cli:command_name")

// NewCommandNameCommand creates the command
func NewCommandNameCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "command-name <arg>",
        Short: "Brief description under 80 chars (no period)",
        Long: `Detailed description with context and examples.

This command:
- Does something useful
- Validates inputs
- Provides helpful feedback

Examples:
  gh aw command-name arg              # Basic usage
  gh aw command-name arg -v           # Verbose output
  gh aw command-name arg --flag val   # With options`,
        Args: cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            // Parse flags
            verbose, _ := cmd.Flags().GetBool("verbose")
            flagValue, _ := cmd.Flags().GetString("flag-name")
            
            // Call main function
            return RunCommandName(args[0], flagValue, verbose)
        },
    }
    
    // Add flags
    cmd.Flags().StringP("flag-name", "f", "default", "Flag description")
    
    return cmd
}

// RunCommandName executes the command logic (testable)
func RunCommandName(arg string, flagValue string, verbose bool) error {
    commandLog.Printf("Starting: arg=%s, flag=%s", arg, flagValue)
    
    // Validate inputs early
    if arg == "" {
        return fmt.Errorf("argument cannot be empty")
    }
    
    // Execute logic
    result, err := processCommand(arg, flagValue)
    if err != nil {
        commandLog.Printf("Failed: %v", err)
        return fmt.Errorf("failed to process: %w", err)
    }
    
    // Output results
    fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(result))
    
    commandLog.Print("Completed successfully")
    return nil
}
```

### Naming Conventions

| Element | Pattern | Example |
|---------|---------|---------|
| **Command file** | `*_command.go` | `audit_command.go` |
| **Test file** | `*_command_test.go` | `audit_command_test.go` |
| **Logger** | `cli:command_name` | `logger.New("cli:audit")` |
| **Constructor** | `NewXCommand()` | `NewAuditCommand()` |
| **Runner** | `RunX(...)` | `RunAuditWorkflowRun(...)` |
| **Config** | `XConfig` | `AuditConfig` |

### Standard Flags

Use helper functions from `flags.go` for consistency:

```go
import "github.com/github/gh-aw/pkg/cli"

// Add common flags
addEngineFlag(cmd)          // --engine/-e (Override AI engine)
addRepoFlag(cmd)            // --repo/-r (Target repository)
addOutputFlag(cmd, dir)     // --output/-o (Output directory)
addJSONFlag(cmd)            // --json/-j (JSON output)
```

**Reserved short flags**: `-v` (verbose), `-e` (engine), `-r` (repo), `-o` (output), `-j` (json), `-f` (force/file), `-w` (watch)

### Output and Error Handling

**All output must go to stderr** (except JSON):

```go
// ✅ CORRECT - Console formatted, stderr
fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Success"))
fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Processing..."))
fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Warning"))
fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))

// ❌ INCORRECT - Plain output, stdout
fmt.Println("Success")
fmt.Printf("Status: %s\n", status)
```

**Error wrapping with context**:

```go
// ✅ CORRECT - Context + wrapping
if err != nil {
    return fmt.Errorf("failed to process workflow: %w", err)
}

// ❌ INCORRECT - No context
if err != nil {
    return err
}
```

### Testing Requirements

Every command needs comprehensive table-driven tests:

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
            input:     "test",
            expected:  "Success",
            shouldErr: false,
        },
        {
            name:      "empty input",
            input:     "",
            shouldErr: true,
        },
        {
            name:      "invalid format",
            input:     "invalid@",
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

**Test coverage requirements**:
- Valid inputs
- Invalid inputs (empty, malformed, out-of-range)
- Edge cases (nil, empty arrays, boundary values)
- Flag combinations
- Error conditions

### Help Text Guidelines

**Short description**:
- Under 80 characters
- Action-oriented (starts with verb)
- No period at the end
- Clear and concise

**Long description**:
- Overview (what it does)
- Context (when to use)
- Details (behavior, options)
- Minimum 3 practical examples

**Include WorkflowIDExplanation** for workflow commands:

```go
import "github.com/github/gh-aw/pkg/cli"

Long: `Description...

` + cli.WorkflowIDExplanation + `

Examples:
  ...`,
```

### Command Development Checklist

When creating a new command, verify:

- [ ] File named `*_command.go` in `pkg/cli/`
- [ ] Logger: `logger.New("cli:command_name")`
- [ ] `NewXCommand()` and `RunX()` functions defined
- [ ] Short description < 80 chars, no period
- [ ] Long description with 3+ examples
- [ ] Standard flags used where applicable
- [ ] Input validation implemented early
- [ ] Console formatting for all output
- [ ] All output to stderr (except JSON)
- [ ] Errors wrapped with context
- [ ] Test file `*_command_test.go` created
- [ ] Table-driven tests with multiple scenarios
- [ ] Valid, invalid, and edge case tests
- [ ] `make test-unit` passes
- [ ] `make agent-finish` passes

### File Organization Patterns

**Simple commands** (< 500 lines): Single file
```
command_name_command.go
command_name_command_test.go
```

**Complex commands** (> 500 lines): Split into focused files
```
command_name_command.go       # Command definition
command_name_config.go        # Configuration types
command_name_helpers.go       # Utility functions
command_name_validation.go    # Validation logic
command_name_orchestrator.go  # Main orchestration
```

### Comprehensive Documentation

For complete command development patterns including:
- Anti-patterns to avoid
- Complete examples with tests
- Error handling best practices
- Advanced patterns for complex commands

See: **[scratchpad/cli-command-patterns.md](scratchpad/cli-command-patterns.md)**

### Related Guidelines

- **Testing**: [scratchpad/testing.md](scratchpad/testing.md) - Comprehensive testing framework
- **Console Output**: [skills/console-rendering/SKILL.md](skills/console-rendering/SKILL.md) - Output formatting
- **Error Messages**: [skills/error-messages/SKILL.md](skills/error-messages/SKILL.md) - Error message style
- **Code Organization**: [scratchpad/code-organization.md](scratchpad/code-organization.md) - File structure patterns

## Debugging and Troubleshooting

### Common Development Issues

#### Build Failures
```bash
# Clean and rebuild
make clean
make deps-dev  # Use deps-dev for full development dependencies
make build
```

#### Test Failures
```bash
# Run specific test with verbose output
go test ./pkg/cli -v -run TestSpecificFunction

# Check test dependencies
go mod verify
go mod tidy
```

#### Linter Issues
```bash
# Fix formatting issues
make fmt

# Address linter warnings
make lint

# Validate workflows with actionlint
make actionlint
```

### Local Incremental Linting

Speed up linting by only checking changed files:

```bash
# Lint changes since origin/main
make golint-incremental BASE_REF=origin/main

# This is what CI uses on PRs - 50-75% faster!
```

This runs the same incremental linting strategy as CI, checking only files changed since the base reference. It's particularly useful when working on pull requests where you want quick feedback on your changes without waiting for a full repository scan.

The incremental approach uses `golangci-lint --new-from-rev` to analyze only the files that differ from the specified base reference, providing significant performance improvements:
- **Full lint** (`make lint`): Scans entire repository
- **Incremental lint** (`make golint-incremental`): Scans only changed files - typically 50-75% faster on PRs

**When to use each approach:**
- Use `make golint-incremental BASE_REF=origin/main` during development for fast feedback
- Use `make lint` before final commits to ensure comprehensive coverage

## Security Scanning

The project includes automated security scanning to detect vulnerabilities, code smells, and dependency issues.

### Running Security Scans Locally

```bash
# Run all security scans (gosec, govulncheck, trivy)
make security-scan

# Run individual scans
make security-gosec      # Go security linter
make security-govulncheck # Go vulnerability database check
make security-trivy       # Filesystem/dependency scanner (requires trivy)
```

### Security Scan Tools

- **gosec**: Static analysis tool for Go that detects security issues in source code
- **govulncheck**: Official Go tool that checks for known vulnerabilities in dependencies
- **trivy**: Comprehensive scanner for filesystem vulnerabilities, misconfigurations, and secrets

### Interpreting Results

#### Gosec Results
- Results are saved to `gosec-report.json`
- Review findings by severity (HIGH, MEDIUM, LOW)
- False positives can be suppressed with `// #nosec G<rule-id>` comments

#### Govulncheck Results
- Shows vulnerabilities in direct and indirect dependencies
- Indicates if vulnerable code paths are actually called
- Update affected dependencies to resolve issues

#### Trivy Results
- Displays HIGH and CRITICAL severity findings
- Covers Go dependencies, npm packages, and configuration files
- Shows CVE details and available fix versions

### Suppressing False Positives

#### Gosec
```go
// Suppress a specific rule
// #nosec G104
err := someFunction() // Error explicitly ignored

// Suppress multiple rules
// #nosec G101 G102
secret := "example" // Known test value
```

#### Govulncheck
- No inline suppression available
- Update dependencies or document accepted risks in security review

#### Trivy
- Use `.trivyignore` file to exclude specific CVEs:
```text
# .trivyignore
CVE-2023-XXXXX  # False positive: not exploitable in our usage
```

### CI/CD Integration

Security scans run automatically on:
- Daily scheduled scan (6:00 AM UTC)
- Manual workflow dispatch

Results are uploaded to the GitHub Security tab in SARIF format.

### Security Scanning Exclusions

For comprehensive documentation of gosec security exclusions, see **<a>Gosec Security Exclusions</a>**.

This documentation provides:
- Complete list of global and file-specific exclusions
- CWE mappings for compliance tracking
- Detailed rationale and mitigation strategies
- Suppression guidelines for `#nosec` annotations
- Compliance and audit trail information

### Development Tips

1. **Use verbose testing**: `go test -v` for detailed output
2. **Run tests frequently**: Ensure changes don't break existing functionality
3. **Check formatting**: Run `make fmt` before committing
4. **Validate thoroughly**: Use `go run test_validation.go` before pull requests

## Release Process

## Architectural Patterns

Understanding the architectural patterns used in this codebase will help you make consistent contributions.

### Core Design Patterns

#### 1. Create Functions Pattern

The codebase uses a consistent pattern for GitHub API operations:

- **One file per entity type**: `create_issue.go`, `create_pull_request.go`, `create_discussion.go`
- **Consistent structure**: Configuration parsing, validation, job generation
- **Parallel development**: Each creation type is independent

**Example Structure**:
```go
// In create_issue.go
type CreateIssuesConfig struct { ... }
func (c *Compiler) parseIssuesConfig(...) *CreateIssuesConfig
func (c *Compiler) generateCreateIssuesJob(...) map[string]any
```

#### 2. Engine Architecture

Each AI engine follows a consistent pattern:

- **Separate files**: `copilot_engine.go`, `claude_engine.go`, `codex_engine.go`
- **Shared utilities**: `engine_helpers.go` contains common functionality
- **Clear interfaces**: All engines implement common methods

**Key Files**:
- `agentic_engine.go` - Base engine interface
- `<engine>_engine.go` - Engine-specific implementation
- `engine_helpers.go` - Shared helper functions
- `engine_helpers_test.go` - Common test utilities

#### 3. Compiler Architecture

The compiler is organized by responsibility:

- `compiler.go` - Main compilation orchestration
- `compiler_yaml.go` - YAML generation logic
- `compiler_jobs.go` - Job generation logic
- `compiler_test.go` - Comprehensive test coverage

This separation allows working on different aspects without conflicts.

#### 4. Expression Building

The expression system (`expressions.go`) demonstrates cohesive design:

- All expression-related logic in one file
- Tree-based structure for complex conditions
- Clean abstractions (ConditionNode interface)
- Comprehensive tests in `expressions_test.go`

### File Organization Best Practices

#### ✅ Good Patterns

1. **Focused files**: Each file has a clear, single responsibility
2. **Descriptive names**: File names clearly indicate their purpose
3. **Collocated tests**: Tests live next to implementation
4. **Reasonable size**: Most files under 500 lines

#### ❌ Anti-Patterns to Avoid

1. **God files**: Single file doing too many things
2. **Vague naming**: `utils.go`, `helpers.go` without context
3. **Mixed concerns**: Unrelated functionality in one file
4. **Massive tests**: All tests in one huge file

### When to Create New Files

Use this decision tree:

1. **New safe output type?** → `create_<entity>.go`
2. **New AI engine?** → `<engine>_engine.go`
3. **New domain feature?** → `<feature>.go`
4. **File over 800 lines?** → Consider splitting
5. **Independent functionality?** → Create new file

### Code Organization Guidelines

#### File Size Targets

- **Small** (50-200 lines): Simple utilities, helpers
- **Medium** (200-500 lines): Feature implementations
- **Large** (500-800 lines): Complex features
- **Very Large** (800+ lines): Core infrastructure only

#### Naming Conventions

- **Create operations**: `create_<entity>.go`
- **Engines**: `<engine>_engine.go`
- **Features**: `<feature>.go`
- **Helpers**: `<subsystem>_helpers.go`
- **Tests**: `<feature>_test.go`, `<feature>_integration_test.go`

### Package Structure

```text
pkg/workflow/
├── create_*.go              # GitHub entity creation
├── *_engine.go              # AI engine implementations
├── engine_helpers.go        # Shared engine utilities
├── compiler*.go             # Compilation logic
├── expressions.go           # Expression building
├── validation.go            # Schema validation
├── strings.go               # String utilities
└── *_test.go                # Tests alongside code
```

### Testing Architecture

#### Test Organization

- **Unit tests**: `feature_test.go` - Fast, focused tests
- **Integration tests**: `feature_integration_test.go` - Cross-component tests
- **Scenario tests**: `feature_scenario_test.go` - Specific use cases

#### Test Naming

Use descriptive names that explain what's being tested:
- ✅ `create_issue_assignees_test.go` - Clear purpose
- ✅ `engine_error_patterns_infinite_loop_test.go` - Specific scenario
- ❌ `test_utils.go` - Too vague

### Contributing to Architecture

When adding new features:

1. **Follow existing patterns** - Look for similar features first
2. **Keep files focused** - One responsibility per file
3. **Use descriptive names** - Future you will thank present you
4. **Write tests alongside** - Don't defer testing
5. **Document patterns** - Update this guide when introducing new patterns

For complete details, see [Code Organization Patterns](scratchpad/code-organization.md).



### Prerequisites for Releases

Before creating a release, ensure you have:

- **Maintainer access** to the GitHub repository
- **Push permissions** to create tags
- **Write access** to GitHub releases
- **All tests passing** on the main branch

### Release Types

The project uses semantic versioning (semver):
- **Major** (v2.0.0): Breaking API changes, incompatible updates
- **Minor** (v1.1.0): New features, backward compatible
- **Patch** (v1.0.1): Bug fixes, backward compatible

### Official Release Process

Releases are **automatically handled by GitHub Actions** when you create a git tag. The process is:

#### 1. Prepare for Release

```bash
# Ensure you're on the main branch with latest changes
git checkout main
git pull origin main

# Run all tests to ensure stability
make test
make lint
make fmt-check

# Test build locally
make build-all
```

#### 2. Create and Push Release Tag

For patch releases (bug fixes), you can use the automated make target:

```bash
# Automated patch release - finds current version and increments patch number
make patch-release

# Automated patch release - finds current version and increments minor number
make minor-release
```

Or create the tag manually:

```bash
# Create a new tag following semantic versioning
# Replace x.y.z with the actual version number
git tag -a v1.0.0 -m "Release v1.0.0"

# Push the tag to trigger the release workflow
git push origin v1.0.0
```

#### 3. Automated Release Process

When you push a tag matching `v*.*.*`, GitHub Actions automatically:

1. **Runs tests** to ensure code quality
2. **Builds cross-platform binaries** using `gh-extension-precompile`
3. **Creates GitHub release** with:
   - Pre-compiled binaries for Linux (amd64, arm64)
   - Pre-compiled binaries for macOS (amd64, arm64) 
   - Pre-compiled binaries for Windows (amd64)
   - Automatic changelog generation

#### 4. Verify Release

After the GitHub Actions workflow completes:

```bash
# Check the release was created successfully
gh release list

# Remove any existing extension
gh extension remove gh-aw || true

# Test installation as a GitHub CLI extension
gh extension install github/gh-aw@v1.0.0
gh aw --help
```

### Release Workflow Details

The release is orchestrated by `.github/workflows/release.yml` which:

- **Triggers on**: Git tags matching `v*.*.*` pattern or manual workflow dispatch
- **Runs on**: Ubuntu latest with Go version from `go.mod`
- **Permissions**: Contents (write), packages (write), ID token (write)
- **Artifacts**: Cross-platform binaries, Docker images, checksums

### Rollback Process

If a release has critical issues:

1. **Immediate**: Delete the problematic release from GitHub
   ```bash
   gh release delete v1.0.0 --yes
   git tag -d v1.0.0
   git push origin :refs/tags/v1.0.0
   ```

2. **Long-term**: Create a new release with fixes

### Current Release Infrastructure Status

The project has a complete automated release system in place:

- ✅ **GitHub Actions workflow** (`.github/workflows/release.yml`)
- ✅ **Cross-platform binary builds** via `gh-extension-precompile`
- ✅ **Semantic versioning** with git tags

The release system is **production-ready** and uses GitHub's official `gh-extension-precompile` action, which is the recommended approach for GitHub CLI extensions.

### Release Notes and Changelog

Release notes are automatically generated from:
- **Commit messages** between releases
- **Pull request titles** and descriptions
- **Conventional commit format** is recommended for better changelog generation

To improve changelog quality, use conventional commit messages:
```bash
git commit -m "feat: add new workflow command"
git commit -m "fix: resolve path handling on Windows"
git commit -m "docs: update installation instructions"
```

### Version Management

- **Version information** is automatically injected at build time
- **Current version** comes from git tags (`git describe --tags`)
- **No manual version files** need to be updated
- **Build metadata** includes commit hash and build date

