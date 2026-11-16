//go:build !integration

package workflow

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidationError(t *testing.T) {
	longValue := strings.Repeat("a", 200)
	tests := []struct {
		name       string
		field      string
		value      string
		reason     string
		suggestion string
		contains   []string
		notContain []string
	}{
		{
			name:       "basic validation error",
			field:      "title",
			reason:     "cannot be empty",
			suggestion: "Provide a non-empty title",
			contains: []string{
				"Validation failed for field 'title'",
				"Reason: cannot be empty",
				"Suggestion: Provide a non-empty title",
			},
		},
		{
			name:     "validation error with long value",
			field:    "body",
			value:    longValue,
			reason:   "too long",
			contains: []string{"Value: " + strings.Repeat("a", 97) + "..."},
		},
		{
			name:       "validation error without suggestion",
			field:      "labels",
			value:      "invalid",
			reason:     "not allowed",
			notContain: []string{"Suggestion:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewValidationError(tt.field, tt.value, tt.reason, tt.suggestion)

			require.Error(t, err)
			message := err.Error()
			assertTimestamp(t, err.Timestamp, message)
			for _, expected := range tt.contains {
				assert.Contains(t, message, expected)
			}
			for _, unexpected := range tt.notContain {
				assert.NotContains(t, message, unexpected)
			}
		})
	}
}

func TestOperationError(t *testing.T) {
	cause := errors.New("original error")
	tests := []struct {
		name       string
		operation  string
		entityType string
		entityID   string
		cause      error
		suggestion string
		contains   []string
		notContain []string
		wantIs     error
	}{
		{
			name:       "basic operation error",
			operation:  "update",
			entityType: "issue",
			entityID:   "123",
			cause:      errors.New("API error"),
			suggestion: "Check permissions",
			contains: []string{
				"Failed to update issue #123",
				"Underlying error: API error",
				"Suggestion: Check permissions",
			},
		},
		{
			name:       "operation error without entity ID",
			operation:  "create",
			entityType: "PR",
			cause:      errors.New("Network error"),
			contains:   []string{"Failed to create PR", "Check that the PR exists"},
			notContain: []string{"#"},
		},
		{
			name:       "operation error wraps cause",
			operation:  "delete",
			entityType: "comment",
			entityID:   "456",
			cause:      cause,
			wantIs:     cause,
		},
		{
			name:       "operation error with nil cause",
			operation:  "get",
			entityType: "repository",
			notContain: []string{"Underlying error:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewOperationError(tt.operation, tt.entityType, tt.entityID, tt.cause, tt.suggestion)

			require.Error(t, err)
			message := err.Error()
			assertTimestamp(t, err.Timestamp, message)
			for _, expected := range tt.contains {
				assert.Contains(t, message, expected)
			}
			for _, unexpected := range tt.notContain {
				assert.NotContains(t, message, unexpected)
			}
			if tt.wantIs != nil {
				assert.ErrorIs(t, err, tt.wantIs)
			}
		})
	}
}

func TestConfigurationError(t *testing.T) {
	longValue := strings.Repeat("x", 200)
	tests := []struct {
		name       string
		configKey  string
		value      string
		reason     string
		suggestion string
		contains   []string
		notContain []string
	}{
		{
			name:       "basic configuration error",
			configKey:  "safe-outputs.max",
			value:      "abc",
			reason:     "must be an integer",
			suggestion: "Use a numeric value",
			contains: []string{
				"Configuration error in 'safe-outputs.max'",
				"Value: abc",
				"Reason: must be an integer",
				"Suggestion: Use a numeric value",
			},
		},
		{
			name:      "configuration error with default suggestion",
			configKey: "safe-outputs.target",
			value:     "invalid",
			reason:    "not a valid target",
			contains:  []string{"Configuration error", "Check the safe-outputs configuration"},
		},
		{
			name:      "configuration error with long value",
			configKey: "config.field",
			value:     longValue,
			reason:    "invalid",
			contains:  []string{"Value: " + strings.Repeat("x", 97) + "..."},
		},
		{
			name:       "configuration error with empty value",
			configKey:  "config.field",
			reason:     "missing",
			notContain: []string{"Value:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewConfigurationError(tt.configKey, tt.value, tt.reason, tt.suggestion)

			require.Error(t, err)
			message := err.Error()
			assertTimestamp(t, err.Timestamp, message)
			for _, expected := range tt.contains {
				assert.Contains(t, message, expected)
			}
			for _, unexpected := range tt.notContain {
				assert.NotContains(t, message, unexpected)
			}
		})
	}
}

func assertTimestamp(t *testing.T, timestamp time.Time, message string) {
	t.Helper()
	assert.False(t, timestamp.IsZero())
	assert.Contains(t, message, timestamp.Format(time.RFC3339))
}
