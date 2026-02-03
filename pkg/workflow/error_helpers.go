package workflow

import (
	"fmt"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var errorHelpersLog = logger.New("workflow:error_helpers")

// WorkflowValidationError represents an error that occurred during input validation
type WorkflowValidationError struct {
	Field      string
	Value      string
	Reason     string
	Suggestion string
	Timestamp  time.Time
}

// Error implements the error interface
func (e *WorkflowValidationError) Error() string {
	var b strings.Builder

	fmt.Fprintf(&b, "[%s] Validation failed for field '%s'",
		e.Timestamp.Format(time.RFC3339), e.Field)

	if e.Value != "" {
		// Truncate long values
		truncatedValue := e.Value
		if len(truncatedValue) > 100 {
			truncatedValue = truncatedValue[:97] + "..."
		}
		fmt.Fprintf(&b, "\n\nValue: %s", truncatedValue)
	}

	fmt.Fprintf(&b, "\nReason: %s", e.Reason)

	if e.Suggestion != "" {
		fmt.Fprintf(&b, "\nSuggestion: %s", e.Suggestion)
	}

	return b.String()
}

// NewValidationError creates a new validation error with context
func NewValidationError(field, value, reason, suggestion string) *WorkflowValidationError {
	errorHelpersLog.Printf("Creating validation error: field=%s, reason=%s", field, reason)
	return &WorkflowValidationError{
		Field:      field,
		Value:      value,
		Reason:     reason,
		Suggestion: suggestion,
		Timestamp:  time.Now(),
	}
}

// OperationError represents an error that occurred during an operation
type OperationError struct {
	Operation  string
	EntityType string
	EntityID   string
	Cause      error
	Suggestion string
	Timestamp  time.Time
}

// Error implements the error interface
func (e *OperationError) Error() string {
	var b strings.Builder

	fmt.Fprintf(&b, "[%s] Failed to %s %s",
		e.Timestamp.Format(time.RFC3339), e.Operation, e.EntityType)

	if e.EntityID != "" {
		fmt.Fprintf(&b, " #%s", e.EntityID)
	}

	if e.Cause != nil {
		fmt.Fprintf(&b, "\n\nUnderlying error: %v", e.Cause)
	}

	if e.Suggestion != "" {
		fmt.Fprintf(&b, "\nSuggestion: %s", e.Suggestion)
	} else {
		// Provide default suggestion
		fmt.Fprintf(&b, "\nSuggestion: Check that the %s exists and you have the necessary permissions.", e.EntityType)
	}

	return b.String()
}

// Unwrap returns the underlying error
func (e *OperationError) Unwrap() error {
	return e.Cause
}

// NewOperationError creates a new operation error with context
func NewOperationError(operation, entityType, entityID string, cause error, suggestion string) *OperationError {
	if errorHelpersLog.Enabled() {
		errorHelpersLog.Printf("Creating operation error: operation=%s, entityType=%s, entityID=%s, cause=%v",
			operation, entityType, entityID, cause)
	}
	return &OperationError{
		Operation:  operation,
		EntityType: entityType,
		EntityID:   entityID,
		Cause:      cause,
		Suggestion: suggestion,
		Timestamp:  time.Now(),
	}
}

// ConfigurationError represents an error in safe-outputs configuration
type ConfigurationError struct {
	ConfigKey  string
	Value      string
	Reason     string
	Suggestion string
	Timestamp  time.Time
}

// Error implements the error interface
func (e *ConfigurationError) Error() string {
	var b strings.Builder

	fmt.Fprintf(&b, "[%s] Configuration error in '%s'",
		e.Timestamp.Format(time.RFC3339), e.ConfigKey)

	if e.Value != "" {
		// Truncate long values
		truncatedValue := e.Value
		if len(truncatedValue) > 100 {
			truncatedValue = truncatedValue[:97] + "..."
		}
		fmt.Fprintf(&b, "\n\nValue: %s", truncatedValue)
	}

	fmt.Fprintf(&b, "\nReason: %s", e.Reason)

	if e.Suggestion != "" {
		fmt.Fprintf(&b, "\nSuggestion: %s", e.Suggestion)
	} else {
		// Provide default suggestion
		fmt.Fprintf(&b, "\nSuggestion: Check the safe-outputs configuration in your workflow frontmatter and ensure '%s' is correctly specified.", e.ConfigKey)
	}

	return b.String()
}

// NewConfigurationError creates a new configuration error with context
func NewConfigurationError(configKey, value, reason, suggestion string) *ConfigurationError {
	errorHelpersLog.Printf("Creating configuration error: configKey=%s, reason=%s", configKey, reason)
	return &ConfigurationError{
		ConfigKey:  configKey,
		Value:      value,
		Reason:     reason,
		Suggestion: suggestion,
		Timestamp:  time.Now(),
	}
}

// EnhanceError adds context to an existing error
func EnhanceError(err error, context, suggestion string) error {
	if err == nil {
		return nil
	}

	errorHelpersLog.Printf("Enhancing error with context: context=%s", context)

	timestamp := time.Now().Format(time.RFC3339)

	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s", timestamp, context)
	fmt.Fprintf(&b, "\n\nOriginal error: %v", err)

	if suggestion != "" {
		fmt.Fprintf(&b, "\nSuggestion: %s", suggestion)
	}

	return fmt.Errorf("%s", b.String())
}

// WrapErrorWithContext wraps an error with additional context using fmt.Errorf %w
// This preserves error unwrapping while adding context
func WrapErrorWithContext(err error, context, suggestion string) error {
	if err == nil {
		return nil
	}

	timestamp := time.Now().Format(time.RFC3339)

	if suggestion != "" {
		return fmt.Errorf("[%s] %s (suggestion: %s): %w", timestamp, context, suggestion, err)
	}

	return fmt.Errorf("[%s] %s: %w", timestamp, context, err)
}
