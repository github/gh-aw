//go:build !integration

package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidationError(t *testing.T) {
	t.Run("basic validation error", func(t *testing.T) {
		err := NewValidationError("title", "", "cannot be empty", "Provide a non-empty title")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "Validation failed for field 'title'")
		assert.Contains(t, err.Error(), "Reason: cannot be empty")
		assert.Contains(t, err.Error(), "Suggestion: Provide a non-empty title")

		// Check timestamp is included
		assert.Contains(t, err.Error(), "[")
		assert.Contains(t, err.Error(), "T")
	})

	t.Run("validation error with long value", func(t *testing.T) {
		longValue := strings.Repeat("a", 200)
		err := NewValidationError("body", longValue, "too long", "Shorten the body")

		require.Error(t, err)
		// Value should be truncated
		assert.Contains(t, err.Error(), "...")
		assert.Less(t, len(err.Error()), len(longValue)+200)
	})

	t.Run("validation error without suggestion", func(t *testing.T) {
		err := NewValidationError("labels", "invalid", "not allowed", "")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "Validation failed")
		assert.NotContains(t, err.Error(), "Suggestion:")
	})
}

func TestOperationError(t *testing.T) {
	t.Run("basic operation error", func(t *testing.T) {
		cause := errors.New("API error")
		err := NewOperationError("update", "issue", "123", cause, "Check permissions")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "Failed to update issue #123")
		assert.Contains(t, err.Error(), "Underlying error: API error")
		assert.Contains(t, err.Error(), "Suggestion: Check permissions")
	})

	t.Run("operation error without entity ID", func(t *testing.T) {
		cause := errors.New("Network error")
		err := NewOperationError("create", "PR", "", cause, "")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "Failed to create PR")
		assert.NotContains(t, err.Error(), "#")
		// Should have default suggestion
		assert.Contains(t, err.Error(), "Check that the PR exists")
	})

	t.Run("operation error unwrap", func(t *testing.T) {
		cause := errors.New("original error")
		err := NewOperationError("delete", "comment", "456", cause, "")

		unwrapped := errors.Unwrap(err)
		assert.Equal(t, cause, unwrapped)
	})

	t.Run("operation error with timestamp", func(t *testing.T) {
		cause := errors.New("failed")
		err := NewOperationError("operation", "entity", "1", cause, "")

		assert.Contains(t, err.Error(), "[")
		assert.Contains(t, err.Error(), "T")
	})
}

func TestConfigurationError(t *testing.T) {
	t.Run("basic configuration error", func(t *testing.T) {
		err := NewConfigurationError("safe-outputs.max", "abc", "must be an integer", "Use a numeric value")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "Configuration error in 'safe-outputs.max'")
		assert.Contains(t, err.Error(), "Value: abc")
		assert.Contains(t, err.Error(), "Reason: must be an integer")
		assert.Contains(t, err.Error(), "Suggestion: Use a numeric value")
	})

	t.Run("configuration error with default suggestion", func(t *testing.T) {
		err := NewConfigurationError("safe-outputs.target", "invalid", "not a valid target", "")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "Configuration error")
		assert.Contains(t, err.Error(), "Check the safe-outputs configuration")
	})

	t.Run("configuration error with long value", func(t *testing.T) {
		longValue := strings.Repeat("x", 200)
		err := NewConfigurationError("config.field", longValue, "invalid", "")

		require.Error(t, err)
		// Value should be truncated
		assert.Contains(t, err.Error(), "...")
	})
}

func TestEnhanceError(t *testing.T) {
	t.Run("enhance error with context", func(t *testing.T) {
		originalErr := errors.New("original message")
		enhanced := EnhanceError(originalErr, "processing workflow", "Check your configuration")

		require.Error(t, enhanced)
		assert.Contains(t, enhanced.Error(), "processing workflow")
		assert.Contains(t, enhanced.Error(), "Original error: original message")
		assert.Contains(t, enhanced.Error(), "Suggestion: Check your configuration")
	})

	t.Run("enhance nil error returns nil", func(t *testing.T) {
		enhanced := EnhanceError(nil, "context", "suggestion")
		assert.NoError(t, enhanced)
	})

	t.Run("enhance error includes timestamp", func(t *testing.T) {
		originalErr := errors.New("test")
		enhanced := EnhanceError(originalErr, "test context", "")

		require.Error(t, enhanced)
		assert.Contains(t, enhanced.Error(), "[")
		assert.Contains(t, enhanced.Error(), "T")
	})
}

func TestWrapErrorWithContext(t *testing.T) {
	t.Run("wrap error with context", func(t *testing.T) {
		originalErr := errors.New("original")
		wrapped := WrapErrorWithContext(originalErr, "failed to compile", "Check syntax")

		require.Error(t, wrapped)
		assert.Contains(t, wrapped.Error(), "failed to compile")
		assert.Contains(t, wrapped.Error(), "Check syntax")

		// Should be unwrappable
		unwrapped := errors.Unwrap(wrapped)
		assert.Equal(t, originalErr, unwrapped)
	})

	t.Run("wrap nil error returns nil", func(t *testing.T) {
		wrapped := WrapErrorWithContext(nil, "context", "suggestion")
		assert.NoError(t, wrapped)
	})

	t.Run("wrap error without suggestion", func(t *testing.T) {
		originalErr := errors.New("original")
		wrapped := WrapErrorWithContext(originalErr, "processing", "")

		require.Error(t, wrapped)
		assert.Contains(t, wrapped.Error(), "processing")
		assert.NotContains(t, wrapped.Error(), "suggestion:")
	})
}
