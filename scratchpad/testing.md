# Testing Guidelines and Framework

This document covers testing patterns, conventions, and the testing framework for GitHub Agentic Workflows.

## Testing Guidelines for Contributors

GitHub Agentic Workflows has extensive testing practices (699 test files, 1,061+ table-driven tests). Understanding these patterns helps maintain code quality and consistency.

### Test Organization

Tests are co-located with implementation files:

- **Unit tests**: `feature.go` + `feature_test.go`
- **Integration tests**: `feature_integration_test.go` (marked with `//go:build integration`)
- **Security tests**: `feature_security_regression_test.go`
- **Fuzz tests**: `feature_fuzz_test.go`

### Assert vs Require

Use **testify** assertions appropriately:

- **`require.*`** - For critical setup steps that make the test invalid if they fail
  - Stops test execution immediately on failure
  - Use for: creating test files, parsing input, setting up test data
  
- **`assert.*`** - For actual test validations
  - Allows test to continue checking other conditions
  - Use for: verifying behavior, checking output values, testing multiple conditions

**Example from the codebase:**

```go
func TestSafeOutputsAppConfiguration(t *testing.T) {
    compiler := NewCompilerWithVersion("1.0.0")
    
    // Create test file - use require (setup step)
    tmpDir := t.TempDir()
    testFile := filepath.Join(tmpDir, "test.md")
    err := os.WriteFile(testFile, []byte(markdown), 0644)
    require.NoError(t, err, "Failed to write test file")
    
    // Parse file - use require (critical for test to continue)
    workflowData, err := compiler.ParseWorkflowFile(testFile)
    require.NoError(t, err, "Failed to parse markdown content")
    require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
    
    // Verify behavior - use assert (actual test validations)
    assert.Equal(t, "${{ vars.APP_ID }}", workflowData.SafeOutputs.App.AppID)
    assert.Equal(t, "${{ secrets.APP_PRIVATE_KEY }}", workflowData.SafeOutputs.App.PrivateKey)
    assert.Equal(t, []string{"repo1", "repo2"}, workflowData.SafeOutputs.App.Repositories)
}
```

### Table-Driven Tests

Use table-driven tests with `t.Run()` for testing multiple scenarios:

```go
func TestSortStrings(t *testing.T) {
    tests := []struct {
        name     string
        input    []string
        expected []string
    }{
        {
            name:     "already sorted",
            input:    []string{"a", "b", "c"},
            expected: []string{"a", "b", "c"},
        },
        {
            name:     "reverse order",
            input:    []string{"c", "b", "a"},
            expected: []string{"a", "b", "c"},
        },
        {
            name:     "empty slice",
            input:    []string{},
            expected: []string{},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := make([]string, len(tt.input))
            copy(result, tt.input)
            
            SortStrings(result)
            
            if len(result) != len(tt.expected) {
                t.Errorf("length = %d, want %d", len(result), len(tt.expected))
                return
            }
            
            for i := range result {
                if result[i] != tt.expected[i] {
                    t.Errorf("at index %d = %q, want %q", i, result[i], tt.expected[i])
                }
            }
        })
    }
}
```

**Key principles:**
- Use descriptive test case names (e.g., "already sorted", "empty slice", "invalid input")
- Structure: Define test cases → Loop with `t.Run()` → Test logic
- Each sub-test runs independently (supports parallel execution with `t.Parallel()`)

### Writing Good Tests

**Use specific assertions:**

```go
// ✅ GOOD - Specific assertions with context
assert.NotEmpty(t, result, "Result should not be empty")
assert.Contains(t, output, "expected text", "Output should contain expected text")
assert.Error(t, err, "Should return error for invalid input")
assert.NoError(t, err, "Failed to parse valid input")

// ❌ BAD - Generic checks without context
if result == "" {
    t.Error("empty")
}
```

**Always include helpful assertion messages:**
- Explain what failed and why it matters
- Include relevant context (input values, expected behavior)
- Make failures immediately understandable

**Test structure (Arrange-Act-Assert):**

```go
func TestFeature(t *testing.T) {
    // Arrange - Set up test data
    input := "test input"
    expected := "expected output"
    
    // Act - Execute the code being tested
    result := ProcessInput(input)
    
    // Assert - Verify the results
    assert.Equal(t, expected, result, "ProcessInput should transform input correctly")
}
```

### Why No Mocks or Test Suites?

This project **intentionally avoids** mocking frameworks and test suites:

**No mocks because:**
- **Simplicity**: Tests use real component interactions
- **Reliability**: Tests verify actual behavior, not mock behavior
- **Maintainability**: No mock setup/teardown boilerplate
- **Confidence**: Tests catch real integration issues

**No test suites (testify/suite) because:**
- **Parallel execution**: Standard Go tests run in parallel by default
- **Simplicity**: No suite lifecycle methods to understand
- **Explicitness**: Setup is visible in each test
- **Compatibility**: Compatible with standard `go test` tooling

This approach keeps tests focused, fast, and maintainable. Tests verify real component interactions rather than mocked behavior.

### Running Tests

```bash
# Fast unit tests (recommended during development)
make test-unit       # ~25s - Unit tests only

# Full test suite
make test            # ~30s - All tests including integration

# Specific tests
go test -v ./pkg/workflow/...                    # Test specific package
go test -run TestSafeOutputs ./pkg/workflow/...  # Run specific test

# Security regression tests
make test-security   # Run security-focused tests

# With coverage
make test-coverage   # Generate coverage report

# Benchmarks
make bench          # Run performance benchmarks

# Fuzz testing
make fuzz           # Run fuzz tests for 30 seconds

# Linting (includes test quality checks)
make lint           # Runs golangci-lint with testifylint rules

# Complete validation (before committing)
make agent-finish   # Runs build, test, recompile, fmt, lint
```

**Note**: The project uses testifylint (via golangci-lint) to enforce consistent test assertion usage. Common rules enforced:
- Prefer specific assertions (`NotEmpty`, `NotNil`) over generic ones
- Use `require` for setup, `assert` for validations
- Include helpful assertion messages

### Additional Resources

- **[testify documentation](https://github.com/stretchr/testify)** - Assertion library reference
- **[Go testing package](https://pkg.go.dev/testing)** - Official Go testing documentation
- **[Table-driven tests in Go](https://dave.cheney.net/2019/05/07/prefer-table-driven-tests)** - Best practices

---

## Testing Framework

This section describes the testing framework added to ensure the Go implementation of gh-aw matches the bash version exactly and maintains high quality standards.

### Overview

The testing framework implements **Phase 6 (Quality Assurance)** of the Go reimplementation project, providing validation that the Go implementation behaves identically to the bash version while maintaining code quality and reliability.

### Testing Structure

### 1. Unit Tests (`pkg/*/`)

### 2. Fuzz Tests (`pkg/*/_fuzz_test.go`)

Fuzz tests use Go's built-in fuzzing support to test functions with randomly generated inputs, helping discover edge cases and security vulnerabilities that traditional tests might miss.

**Running Fuzz Tests:**
```bash
# Run expression parser fuzz test for 10 seconds
go test -fuzz=FuzzExpressionParser -fuzztime=10s ./pkg/workflow/

# Run for extended duration (1 minute)
go test -fuzz=FuzzExpressionParser -fuzztime=1m ./pkg/workflow/

# Run seed corpus only (no fuzzing)
go test -run FuzzExpressionParser ./pkg/workflow/
```text

**Available Fuzz Tests:**
- **FuzzParseFrontmatter** (`pkg/parser/frontmatter_fuzz_test.go`): Tests YAML frontmatter parsing for edge cases and malformed input
- **FuzzScheduleParser** (`pkg/parser/schedule_parser_fuzz_test.go`): Tests cron schedule parsing for edge cases
- **FuzzExpressionParser** (`pkg/workflow/expression_parser_fuzz_test.go`): Tests GitHub expression validation against injection attacks
  - 59 seed cases covering allowed expressions, malicious injections, and edge cases
  - Validates security controls against secret injection, script tags, command injection
  - Ensures parser handles malformed input without panic
- **FuzzMentionsFiltering** (`pkg/workflow/mentions_fuzz_test.go`): Tests mention sanitization with 80+ seed corpus entries
- **FuzzSanitizeOutput** (`pkg/workflow/sanitize_output_fuzz_test.go`): Tests output sanitization against injection attacks
- **FuzzSanitizeIncomingText** (`pkg/workflow/sanitize_incoming_text_fuzz_test.go`): Tests incoming text sanitization
- **FuzzSanitizeLabelContent** (`pkg/workflow/sanitize_label_fuzz_test.go`): Tests label content sanitization
- **FuzzWrapExpressionsInTemplateConditionals** (`pkg/workflow/template_fuzz_test.go`): Tests template expression wrapping
- **FuzzYAMLParsing** (`pkg/workflow/security_fuzz_test.go`): Tests YAML parsing for DoS and malformed input handling
- **FuzzTemplateRendering** (`pkg/workflow/security_fuzz_test.go`): Tests template rendering for injection attacks
- **FuzzInputValidation** (`pkg/workflow/security_fuzz_test.go`): Tests input validation functions for edge cases
- **FuzzNetworkPermissions** (`pkg/workflow/security_fuzz_test.go`): Tests network permission parsing for injection
- **FuzzSafeJobConfig** (`pkg/workflow/security_fuzz_test.go`): Tests safe job configuration parsing

**Fuzz Test Results:**
- Seed corpus includes authorized and unauthorized expression patterns
- Fuzzer generates thousands of variations per second
- Typical coverage: 87+ test cases in baseline, discovers additional interesting cases during fuzzing
- All inputs should be handled without panic, unauthorized expressions properly rejected

**Continuous Integration:**
Fuzz tests can be run in CI with time limits:
```yaml
- name: Fuzz test expression parser
  run: go test -fuzz=FuzzExpressionParser -fuzztime=30s ./pkg/workflow/
```text

### 3. Security Regression Tests (`*_security_regression_test.go`)

Security regression tests ensure that security fixes remain effective over time and prevent reintroduction of vulnerabilities.

**Running Security Tests:**
```bash
# Run all security regression tests
make test-security

# Run security tests manually
go test -v -run '^TestSecurity' ./pkg/workflow/... ./pkg/cli/...

# Run specific security test category
go test -v -run 'TestSecurityTemplate' ./pkg/workflow/
go test -v -run 'TestSecurityDoS' ./pkg/workflow/
go test -v -run 'TestSecurityCLI' ./pkg/cli/
```text

**Security Test Categories:**

#### Injection Attack Prevention (`pkg/workflow/security_regression_test.go`)
- **Template Injection**: Tests that GitHub expression injection (e.g., `${{ secrets.TOKEN }}`) is blocked
- **Command Injection**: Tests that shell command injection patterns are handled safely
- **YAML Injection**: Tests that YAML-based injection attacks are prevented
- **XSS Prevention**: Tests that script injection patterns don't leak sensitive data

#### DoS Prevention (`pkg/workflow/security_regression_test.go`)
- **Large Input Handling**: Tests that excessively large inputs don't cause resource exhaustion
- **Nested YAML**: Tests that deeply nested structures don't cause stack overflow
- **Billion Laughs Attack**: Tests protection against YAML entity expansion attacks

#### Authorization (`pkg/workflow/security_regression_test.go`)
- **Unauthorized Access**: Tests that unauthorized expression contexts are rejected
- **Token Leakage**: Tests that tokens cannot be leaked through various expression paths
- **Safe Outputs System**: Tests that safe-outputs properly restricts operations

#### CLI Security (`pkg/cli/security_regression_test.go`)
- **Command Injection Prevention**: Tests that CLI commands sanitize inputs properly
- **Path Traversal Prevention**: Tests that file paths are sanitized
- **Input Size Limits**: Tests that large inputs are handled without DoS
- **Environment Variable Sanitization**: Tests safe handling of environment variables
- **Output Directory Safety**: Tests that output directories are validated

**Security Test Patterns:**
- Use `t.Run()` for sub-tests to organize test cases
- Use table-driven tests with clear descriptions
- Include both positive (should block) and negative (should allow) test cases
- Document the security vulnerability being prevented

### 4. Benchmarks (`pkg/*/_benchmark_test.go`)

Performance benchmarks measure the speed of critical operations. Run benchmarks to:
- Detect performance regressions
- Identify optimization opportunities
- Track performance trends over time

**Running Benchmarks:**
```bash
# Run all benchmarks with make (optimized for CI, runs in ~6 seconds)
make bench

# Run all benchmarks manually
go test -bench=. -benchtime=3x -run=^$ ./pkg/...

# Run benchmarks with more iterations for comparison
make bench-compare

# Run benchmarks for specific package
go test -bench=. -benchtime=3x -run=^$ ./pkg/workflow/

# Run specific benchmark
go test -bench=BenchmarkCompileWorkflow -benchtime=3x -run=^$ ./pkg/workflow/

# Run with custom iterations (default is 1 second per benchmark)
go test -bench=. -benchtime=100x -run=^$ ./pkg/workflow/

# Run with memory profiling
go test -bench=. -benchmem -benchtime=3x -run=^$ ./pkg/...

# Compare benchmark results over time
go test -bench=. -benchtime=3x -run=^$ ./pkg/... > bench_baseline.txt
# ... make changes ...
go test -bench=. -benchtime=3x -run=^$ ./pkg/... > bench_new.txt
benchstat bench_baseline.txt bench_new.txt
```text

**Note**: Benchmarks use `-benchtime=3x` (3 iterations) for fast CI execution. For more accurate measurements, use `-benchtime=100x` or longer durations.

**Benchmark Coverage:**
- **Workflow Compilation**: Basic, with MCP, with imports, with validation, complex workflows
- **Frontmatter Parsing**: Simple, complex, minimal, with arrays, schema validation
- **Expression Validation**: Single expressions, complex expressions, full markdown validation, parsing
- **Log Processing**: Claude, Copilot, Codex log parsing, aggregation, JSON metrics extraction
- **MCP Configuration**: Playwright config, Docker args, expression extraction
- **Tool Processing**: Simple and complex tool configurations, safe outputs, network permissions

**Performance Baselines** (approximate, machine-dependent):
- Workflow compilation: ~100μs - 2ms depending on complexity
- Frontmatter parsing: ~10μs - 250μs depending on complexity
- Expression validation: ~700ns - 10μs per expression
- Log parsing: ~50μs - 1ms depending on log size
- Schema validation: ~35μs - 130μs depending on complexity

### 5. Test Validation Framework (`test_validation.go`)

Validation system that ensures:

#### Unit Test Validation
- All package tests pass
- Test coverage information is available
- No test failures or build errors

#### Sample Workflow Validation
- At least 5 sample workflows are available
- All sample files are readable and valid
- Workflow structure meets expectations

#### Test Coverage Validation  
- Coverage reports are generated correctly
- All packages have test coverage
- Tests execute and pass consistently

#### CLI Behavior Validation
- Go binary builds successfully
- Basic commands execute without crashing
- Help system works correctly
- Command interface is stable

## Test Execution

### Running All Tests
```bash
# Run all unit tests
go test ./pkg/... -v

# Run security regression tests
make test-security

# Run validation
go run test_validation.go
```text

### Test Results Summary
- **Unit Tests**: ⚠️ Partial - Parser & Workflow packages pass, CLI package has known failures (see #48)
- **Sample Workflows**: ✅ 5 sample files validated
- **Test Coverage**: ✅ Coverage reporting functional
- **CLI Behavior**: ✅ Binary builds and executes correctly
- **Security Regression Tests**: ✅ Injection, DoS, and authorization scenarios covered

## Security Testing Strategy

### Defense in Depth
Security tests are organized in layers:

1. **Input Validation Layer**: Fuzz tests and input validation tests ensure all user inputs are handled safely
2. **Expression Safety Layer**: Expression parser tests prevent secret and token leakage
3. **Compilation Layer**: Workflow compilation tests ensure secure YAML generation
4. **Output Layer**: Safe output tests ensure operations are properly restricted

### Test-Driven Security
When adding new features:
1. First, add security regression tests for the feature
2. Then, implement the feature with security controls
3. Finally, verify all security tests pass

### Continuous Security Validation
Security tests are integrated into:
- CI/CD pipeline (via `make test` which includes security tests)
- Pre-commit validation (via `make agent-finish`)
- Fuzz testing job (via the `fuzz` CI job)

## Testing Philosophy

### Current Implementation Status
The tests are designed to work with the current implementation state:
- **Completed functionality**: Tested with high coverage
- **Stub implementations**: Interface stability testing to ensure future compatibility
- **Missing functionality**: Framework prepared for when implementation is complete

### Future Expansion
As the Go implementation develops:
1. **Stub tests** will be enhanced with full behavioral validation
3. **Edge case tests** will be expanded based on real usage patterns

## Test Coverage Areas

### ✅ Fully Tested
- Markdown frontmatter parsing (100% coverage)
- YAML extraction and processing
- CLI interface structure and stability
- Basic workflow compilation interface
- Error handling for malformed inputs
- **Performance benchmarks** for critical operations (62+ benchmarks)
- **Security regression tests** for injection, DoS, and authorization scenarios

### 🔄 Interface Testing (Ready for Implementation)
- CLI command execution (stubs tested)
- Workflow compilation (interface validated)
- Management commands (add, remove, enable, disable)

### 📋 Ready for Enhancement
- Bash-Go output comparison (when compiler is complete)
- **Performance regression tracking** (baseline established)
- Cross-platform compatibility testing
- Real workflow execution testing

## Quality Assurance

This testing framework ensures:

1. **Regression Prevention**: Any changes that break existing functionality will be caught
2. **Interface Stability**: CLI and API interfaces remain consistent
3. **Behavioral Compatibility**: Go implementation will match bash behavior exactly
4. **Code Quality**: High test coverage and validation
5. **Future Readiness**: Testing infrastructure scales with implementation progress
6. **Security Assurance**: Security fixes remain effective over time

## Test Maintenance

The testing framework is designed to be:
- **Self-validating**: The validation script ensures all tests work correctly  
- **Complete**: Covers all aspects of functionality and interface design
- **Maintainable**: Clear structure and documentation for future updates
- **Scalable**: Tests can be added incrementally as functionality is implemented
- **Security-focused**: Security regression tests prevent reintroduction of vulnerabilities

## Conclusion

This testing framework provides a solid foundation for ensuring the Go implementation of gh-aw maintains compatibility with the bash version while providing high-quality, reliable, and secure functionality. The framework is immediately useful for current development and ready to scale as implementation progresses.