//go:build !integration

package workflow

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatCompilerError tests the formatCompilerError helper function
func TestFormatCompilerError(t *testing.T) {
	tests := []struct {
		name        string
		filePath    string
		errType     string
		message     string
		cause       error
		wantContain []string
	}{
		{
			name:     "error type with simple message, no cause",
			filePath: "/path/to/workflow.md",
			errType:  "error",
			message:  "validation failed",
			cause:    nil,
			wantContain: []string{
				"/path/to/workflow.md",
				"1:1",
				"error",
				"validation failed",
			},
		},
		{
			name:     "warning type with detailed message, no cause",
			filePath: "/path/to/workflow.md",
			errType:  "warning",
			message:  "missing required permission",
			cause:    nil,
			wantContain: []string{
				"/path/to/workflow.md",
				"1:1",
				"warning",
				"missing required permission",
			},
		},
		{
			name:     "error with underlying cause",
			filePath: "/path/to/workflow.md",
			errType:  "error",
			message:  "failed to parse YAML",
			cause:    fmt.Errorf("syntax error at line 42"),
			wantContain: []string{
				"/path/to/workflow.md",
				"1:1",
				"error",
				"failed to parse YAML",
			},
		},
		{
			name:     "lock file path",
			filePath: "/path/to/workflow.lock.yml",
			errType:  "error",
			message:  "failed to write lock file",
			cause:    nil,
			wantContain: []string{
				"/path/to/workflow.lock.yml",
				"1:1",
				"error",
				"failed to write lock file",
			},
		},
		{
			name:     "formatted message with error details and cause",
			filePath: "test.md",
			errType:  "error",
			message:  "failed to generate YAML: syntax error",
			cause:    fmt.Errorf("underlying error"),
			wantContain: []string{
				"test.md",
				"1:1",
				"error",
				"failed to generate YAML: syntax error",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := formatCompilerError(tt.filePath, tt.errType, tt.message, tt.cause)
			require.Error(t, err, "formatCompilerError should return an error")

			errStr := err.Error()
			for _, want := range tt.wantContain {
				assert.Contains(t, errStr, want, "Error message should contain: %s", want)
			}

			// If cause is provided, verify error wrapping
			if tt.cause != nil {
				assert.ErrorIs(t, err, tt.cause, "Error should wrap the cause")
			}
		})
	}
}

// TestFormatCompilerError_OutputFormat verifies the output format remains consistent
func TestFormatCompilerError_OutputFormat(t *testing.T) {
	err := formatCompilerError("/test/workflow.md", "error", "test message", nil)
	require.Error(t, err)

	errStr := err.Error()

	// Verify the error format contains the standard compiler error structure
	assert.Contains(t, errStr, "/test/workflow.md", "Should contain file path")
	assert.Contains(t, errStr, "1:1", "Should contain line:column")
	assert.Contains(t, errStr, "error", "Should contain error type")
	assert.Contains(t, errStr, "test message", "Should contain message")
}

// TestFormatCompilerError_ErrorVsWarning tests differentiation between error and warning types
func TestFormatCompilerError_ErrorVsWarning(t *testing.T) {
	errorErr := formatCompilerError("test.md", "error", "error message", nil)
	warningErr := formatCompilerError("test.md", "warning", "warning message", nil)

	require.Error(t, errorErr)
	require.Error(t, warningErr)

	assert.Contains(t, errorErr.Error(), "error", "Error type should be present")
	assert.Contains(t, warningErr.Error(), "warning", "Warning type should be present")

	// Ensure they produce different outputs
	assert.NotEqual(t, errorErr.Error(), warningErr.Error(), "Error and warning should have different outputs")
}

// TestFormatCompilerMessage tests the formatCompilerMessage helper function
func TestFormatCompilerMessage(t *testing.T) {
	tests := []struct {
		name        string
		filePath    string
		msgType     string
		message     string
		wantContain []string
	}{
		{
			name:     "warning message",
			filePath: "/path/to/workflow.md",
			msgType:  "warning",
			message:  "container image validation failed",
			wantContain: []string{
				"/path/to/workflow.md",
				"1:1",
				"warning",
				"container image validation failed",
			},
		},
		{
			name:     "error message as string",
			filePath: "test.md",
			msgType:  "error",
			message:  "validation error",
			wantContain: []string{
				"test.md",
				"1:1",
				"error",
				"validation error",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := formatCompilerMessage(tt.filePath, tt.msgType, tt.message)

			for _, want := range tt.wantContain {
				assert.Contains(t, msg, want, "Message should contain: %s", want)
			}
		})
	}
}

// TestFormatCompilerError_ErrorWrapping verifies that error wrapping preserves error chains
func TestFormatCompilerError_ErrorWrapping(t *testing.T) {
	// Create an underlying error
	underlyingErr := fmt.Errorf("underlying validation error")

	// Wrap it with formatCompilerError
	wrappedErr := formatCompilerError("test.md", "error", "validation failed", underlyingErr)

	require.Error(t, wrappedErr)

	// Verify error chain is preserved
	require.ErrorIs(t, wrappedErr, underlyingErr, "Should preserve error chain with %w")

	// Verify formatted message is in the error string
	assert.Contains(t, wrappedErr.Error(), "test.md")
	assert.Contains(t, wrappedErr.Error(), "validation failed")
}

// TestFormatCompilerError_NilCause verifies that nil cause creates a new error
func TestFormatCompilerError_NilCause(t *testing.T) {
	err := formatCompilerError("test.md", "error", "validation error", nil)

	require.Error(t, err)

	// Verify error message contains expected content
	assert.Contains(t, err.Error(), "test.md")
	assert.Contains(t, err.Error(), "validation error")

	// Verify it's a new error (not wrapping anything)
	// This is a validation error, so it should not wrap
	dummyErr := fmt.Errorf("some other error")
	assert.NotErrorIs(t, err, dummyErr, "Should not wrap any error when cause is nil")
}
