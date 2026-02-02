//go:build !integration

package workflow

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/parser"
)

// internalHTTPError is a custom error type used in tests to demonstrate
// proper error wrapping patterns without using deprecated types.
type internalHTTPError struct {
	message string
}

func (e *internalHTTPError) Error() string {
	return e.message
}

// TestUserFacingErrorsDontLeakInternals validates that internal errors are properly
// wrapped and don't leak implementation details to users. This uses testify v1.11.0+'s
// NotErrorAs assertion to ensure error types are hidden from user-facing code.
//
// The compiler should format errors with console.FormatError() and create new errors
// with errors.New(), which prevents internal error types from appearing in the error chain.
func TestUserFacingErrorsDontLeakInternals(t *testing.T) {
	tests := []struct {
		name           string
		operation      func() error
		internalErrors []any
	}{
		{
			name: "workflow compilation YAML parse error",
			operation: func() error {
				// Create a test file with invalid YAML
				tmpDir := t.TempDir()
				testFile := filepath.Join(tmpDir, "invalid.md")
				content := `---
engine: copilot
on:
  - invalid: {{{
---
# Test Workflow`
				err := os.WriteFile(testFile, []byte(content), 0644)
				require.NoError(t, err, "Failed to write test file")

				// Try to compile the invalid workflow
				compiler := NewCompilerWithVersion("1.0.0")
				err = compiler.CompileWorkflow(testFile)
				return err
			},
			internalErrors: []any{
				&yaml.TypeError{},   // YAML parsing error should be wrapped
				&yaml.SyntaxError{}, // YAML syntax error should be wrapped
			},
		},
		{
			name: "workflow file read error",
			operation: func() error {
				// Try to compile a non-existent file
				compiler := NewCompilerWithVersion("1.0.0")
				err := compiler.CompileWorkflow("/nonexistent/file.md")
				return err
			},
			internalErrors: []any{
				&os.PathError{}, // File system errors should be wrapped
				&os.LinkError{}, // Symlink errors should be wrapped
			},
		},
		{
			name: "import resolution error",
			operation: func() error {
				// Create a test file with invalid import
				tmpDir := t.TempDir()
				testFile := filepath.Join(tmpDir, "test.md")
				content := `---
engine: copilot
imports:
  - nonexistent.md
on:
  issues:
    types: [opened]
---
# Test Workflow`
				err := os.WriteFile(testFile, []byte(content), 0644)
				require.NoError(t, err, "Failed to write test file")

				// Try to compile with invalid import
				compiler := NewCompilerWithVersion("1.0.0")
				err = compiler.CompileWorkflow(testFile)
				return err
			},
			internalErrors: []any{
				&parser.ImportError{}, // Import errors are internal implementation details
				&os.PathError{},       // Underlying file errors should be wrapped
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.operation()
			require.Error(t, err, "should return an error")

			// Verify that internal error types are not exposed to users
			// The compiler should use errors.New() after formatting, which breaks the chain
			for _, internalErr := range tt.internalErrors {
				switch e := internalErr.(type) {
				case *yaml.TypeError:
					var target *yaml.TypeError
					assert.NotErrorAs(t, err, &target,
						"internal error type %T should not leak to user - the error chain should be broken by errors.New()", e)
				case *yaml.SyntaxError:
					var target *yaml.SyntaxError
					assert.NotErrorAs(t, err, &target,
						"internal error type %T should not leak to user", e)
				case *os.PathError:
					var target *os.PathError
					assert.NotErrorAs(t, err, &target,
						"internal error type %T should not leak to user", e)
				case *os.LinkError:
					var target *os.LinkError
					assert.NotErrorAs(t, err, &target,
						"internal error type %T should not leak to user", e)
				case *parser.ImportError:
					var target *parser.ImportError
					assert.NotErrorAs(t, err, &target,
						"internal error type %T should not leak to user", e)
				default:
					t.Fatalf("Unknown error type in test: %T", e)
				}
			}

			// Ensure the error message is still meaningful
			errMsg := err.Error()
			assert.NotEmpty(t, errMsg, "error message should not be empty")
			assert.Greater(t, len(errMsg), 10, "error message should be descriptive")
		})
	}
}

// TestErrorMessagesPreserveContext ensures that when we format errors,
// we don't lose important context information even though we break the error chain.
func TestErrorMessagesPreserveContext(t *testing.T) {
	tests := []struct {
		name      string
		operation func() error
		wantInfo  []string // Information that should be preserved in the message
	}{
		{
			name: "file path is preserved in error message",
			operation: func() error {
				tmpDir := t.TempDir()
				testFile := filepath.Join(tmpDir, "my-workflow.md")
				// Don't create the file - let it fail

				compiler := NewCompilerWithVersion("1.0.0")
				err := compiler.CompileWorkflow(testFile)
				return err
			},
			wantInfo: []string{"my-workflow.md"}, // Filename should appear in error
		},
		{
			name: "field names are preserved for validation errors",
			operation: func() error {
				tmpDir := t.TempDir()
				testFile := filepath.Join(tmpDir, "test.md")
				content := `---
engine: copilot
on: 123456
---
# Test Workflow`
				err := os.WriteFile(testFile, []byte(content), 0644)
				require.NoError(t, err)

				compiler := NewCompilerWithVersion("1.0.0")
				err = compiler.CompileWorkflow(testFile)
				return err
			},
			wantInfo: []string{"on"}, // Field name should be mentioned
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.operation()
			require.Error(t, err)

			errMsg := err.Error()
			for _, info := range tt.wantInfo {
				assert.Contains(t, errMsg, info,
					"error should preserve context: '%s'", info)
			}

			// Verify internal error types are not in the chain
			var pathErr *os.PathError
			assert.NotErrorAs(t, err, &pathErr, "os.PathError should not be in error chain")

			var typeErr *yaml.TypeError
			assert.NotErrorAs(t, err, &typeErr, "yaml.TypeError should not be in error chain")
		})
	}
}

// TestStandardLibraryErrorsNotExposed validates that common standard library
// error types don't leak through our error handling.
func TestStandardLibraryErrorsNotExposed(t *testing.T) {
	tests := []struct {
		name       string
		operation  func() error
		notInChain []any
	}{
		{
			name: "path errors from file operations",
			operation: func() error {
				compiler := NewCompilerWithVersion("1.0.0")
				return compiler.CompileWorkflow("/definitely/does/not/exist/workflow.md")
			},
			notInChain: []any{
				&os.PathError{},
			},
		},
		{
			name: "YAML type errors from parsing",
			operation: func() error {
				tmpDir := t.TempDir()
				testFile := filepath.Join(tmpDir, "test.md")
				content := `---
engine: copilot
on:
  issues: not_a_valid_structure_at_all
---
# Test`
				err := os.WriteFile(testFile, []byte(content), 0644)
				require.NoError(t, err)

				compiler := NewCompilerWithVersion("1.0.0")
				return compiler.CompileWorkflow(testFile)
			},
			notInChain: []any{
				&yaml.TypeError{},
				&yaml.SyntaxError{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.operation()
			require.Error(t, err)

			// Verify standard library errors are not exposed
			for _, errType := range tt.notInChain {
				switch e := errType.(type) {
				case *os.PathError:
					var target *os.PathError
					assert.NotErrorAs(t, err, &target,
						"os.PathError should be formatted and not in chain")
				case *yaml.TypeError:
					var target *yaml.TypeError
					assert.NotErrorAs(t, err, &target,
						"yaml.TypeError should be formatted and not in chain")
				case *yaml.SyntaxError:
					var target *yaml.SyntaxError
					assert.NotErrorAs(t, err, &target,
						"yaml.SyntaxError should be formatted and not in chain")
				default:
					t.Fatalf("Unknown error type: %T", e)
				}
			}

			// Ensure error is still informative
			assert.NotEmpty(t, err.Error())
			assert.Greater(t, len(err.Error()), 20, "error message should be descriptive")
		})
	}
}

// TestHTTPErrorsNotExposed is a documentation test showing how HTTP errors
// from external services should be handled. This is more of a guideline test.
func TestHTTPErrorsNotExposed(t *testing.T) {
	t.Run("HTTP errors should be wrapped with user-friendly messages", func(t *testing.T) {
		// Example: if we had HTTP errors from MCP server communication,
		// they should be wrapped like this:
		httpErr := &internalHTTPError{message: "simulated"}

		// WRONG: Don't use %w which exposes the internal error
		// wrongErr := fmt.Errorf("MCP server error: %w", httpErr)

		// RIGHT: Format it and create a new error
		userErr := fmt.Errorf("failed to connect to MCP server: %s", httpErr.Error())

		// Verify the internal error is not in the chain
		var target *internalHTTPError
		assert.NotErrorAs(t, userErr, &target,
			"HTTP internal errors should not be in the error chain")

		// But the message should still be informative
		assert.Contains(t, userErr.Error(), "MCP server")
	})

	t.Run("IO errors should be wrapped with context", func(t *testing.T) {
		// Example: IO errors should not leak as-is
		ioErr := io.ErrUnexpectedEOF

		// WRONG: Don't wrap with %w for internal errors
		// wrongErr := fmt.Errorf("read failed: %w", ioErr)

		// RIGHT: Create a new error with context
		userErr := fmt.Errorf("failed to read workflow file: unexpected end of file")

		// Verify sentinel error is not exposed
		assert.NotErrorIs(t, userErr, ioErr,
			"sentinel IO errors should not be exposed")
	})
}
