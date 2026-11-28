// This file defines all error types and aggregation utilities for the workflow package.
//
// # Error Types
//
//   - WorkflowValidationError - validation errors for workflow configuration fields
//   - OperationError - errors that occurred during an operation (e.g., fetching a resource)
//   - ConfigurationError - errors in safe-outputs configuration
//   - SharedWorkflowError - signal that a workflow is a shared/importable component
//   - RedirectOnlyWorkflowError - signal that a workflow only has a redirect field and no trigger
//
// # Error Aggregation
//
// ErrorCollector collects multiple validation errors, supporting both fail-fast and
// collect-all modes. Use NewErrorCollector(failFast) to create one, then Add() errors
// and call Error() or FormattedError() to retrieve the aggregated result.

package workflow

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/validationerror"
)

var errorHelpersLog = logger.New("workflow:error_helpers")

// FieldLocation represents a source file location for a validation error.
// File and Line are optional; zero values mean "location unknown".
type FieldLocation = console.ErrorPosition

// WorkflowValidationError represents an error that occurred during input validation.
// It embeds validationerror.Payload so callers can uniformly detect structured
// validation errors from any package via errors.As(err, &validationerror.ValidationError).
type WorkflowValidationError struct {
	validationerror.Payload
	Severity  ErrorSeverity
	Category  string
	Timestamp time.Time
	// Location context (optional — zero values mean location unknown)
	File   string
	Line   int
	Column int
}

// Error implements the error interface
func (e *WorkflowValidationError) Error() string {
	header := fmt.Sprintf("Validation failed for field '%s'", e.Field)
	return validationerror.Format(header, e.Payload, true)
}

// NewValidationError creates a new validation error with context
func NewValidationError(field, value, reason, suggestion string) *WorkflowValidationError {
	if errorHelpersLog.Enabled() {
		errorHelpersLog.Printf("Creating validation error: field=%s, reason=%s", field, reason)
	}
	severity, category := classifyValidationSeverity(field, reason)
	return &WorkflowValidationError{
		Payload: validationerror.Payload{
			Field:      field,
			Value:      value,
			Reason:     reason,
			Suggestion: suggestion,
		},
		Severity:  severity,
		Category:  category,
		Timestamp: time.Now(),
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
		truncatedValue := stringutil.Truncate(e.Value, 100)
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
	if errorHelpersLog.Enabled() {
		errorHelpersLog.Printf("Creating configuration error: configKey=%s, reason=%s", configKey, reason)
	}
	return &ConfigurationError{
		ConfigKey:  configKey,
		Value:      value,
		Reason:     reason,
		Suggestion: suggestion,
		Timestamp:  time.Now(),
	}
}

var errorAggregationLog = logger.New("workflow:error_aggregation")

// ErrorCollector collects multiple validation errors
type ErrorCollector struct {
	errors   []error
	failFast bool
}

// NewErrorCollector creates a new error collector
// If failFast is true, the collector will stop at the first error
func NewErrorCollector(failFast bool) *ErrorCollector {
	if errorAggregationLog.Enabled() {
		errorAggregationLog.Printf("Creating error collector: fail_fast=%v", failFast)
	}
	return &ErrorCollector{
		errors:   make([]error, 0),
		failFast: failFast,
	}
}

// Add adds an error to the collector
// If failFast is enabled, returns the error immediately
// Otherwise, adds it to the collection and returns nil
func (c *ErrorCollector) Add(err error) error {
	if err == nil {
		return nil
	}

	if errorAggregationLog.Enabled() {
		errorAggregationLog.Printf("Adding error to collector: %v", err)
	}

	if c.failFast {
		if errorAggregationLog.Enabled() {
			errorAggregationLog.Print("Fail-fast enabled, returning error immediately")
		}
		return err
	}

	c.errors = append(c.errors, err)
	return nil
}

// Count returns the number of errors collected
func (c *ErrorCollector) Count() int {
	return len(c.errors)
}

// Error returns the aggregated error using errors.Join
// Returns nil if no errors were collected
func (c *ErrorCollector) Error() error {
	if len(c.errors) == 0 {
		return nil
	}

	if errorAggregationLog.Enabled() {
		errorAggregationLog.Printf("Aggregating %d errors", len(c.errors))
	}

	if len(c.errors) == 1 {
		return c.errors[0]
	}

	return errors.Join(c.errors...)
}

// FormattedError returns the aggregated error with a formatted header showing the count
// Returns nil if no errors were collected
// This method is preferred over Error() + FormatAggregatedError for better accuracy
func (c *ErrorCollector) FormattedError(category string) error {
	if len(c.errors) == 0 {
		return nil
	}

	if errorAggregationLog.Enabled() {
		errorAggregationLog.Printf("Formatting %d errors for category: %s", len(c.errors), category)
	}

	if len(c.errors) == 1 {
		return c.errors[0]
	}

	header := fmt.Sprintf("Found %d %s errors:", len(c.errors), category)
	return fmt.Errorf("%s\n%w", header, errors.Join(c.errors...))
}

var sharedWorkflowLog = logger.New("workflow:shared_workflow_error")

// SharedWorkflowError represents a workflow that is missing the 'on' field
// and should be treated as a shared/importable workflow component rather than
// a standalone workflow. This is not an actual error - it's a signal that
// compilation should be skipped with an informational message.
type SharedWorkflowError struct {
	Path string // File path of the shared workflow
}

// Error implements the error interface
// Returns a formatted info message explaining that this is a shared workflow
func (e *SharedWorkflowError) Error() string {
	if sharedWorkflowLog.Enabled() {
		sharedWorkflowLog.Printf("Formatting info message for shared workflow: %s", e.Path)
	}

	filename := filepath.Base(e.Path)

	return fmt.Sprintf(
		"ℹ️  Shared agentic workflow detected: %s\n\n"+
			"This workflow is missing the 'on' field and will be treated as a shared workflow component.\n"+
			"Shared workflows are reusable components meant to be imported by other workflows.\n\n"+
			"To use this shared workflow:\n"+
			"  1. Import it in another workflow's frontmatter:\n"+
			"     ---\n"+
			"     on: issues\n"+
			"     imports:\n"+
			"       - %s\n"+
			"     ---\n\n"+
			"  2. Compile the workflow that imports it\n\n"+
			"Skipping compilation.",
		filename,
		e.Path,
	)
}

// IsSharedWorkflow returns true, indicating this is a shared workflow
func (e *SharedWorkflowError) IsSharedWorkflow() bool {
	return true
}

// RedirectOnlyWorkflowError represents a workflow that has a redirect field but no 'on' trigger.
// This typically occurs when `gh aw add` downloads a redirect-only placeholder that points to
// the workflow's new canonical location. The redirect was not resolved to the full workflow
// content during download.
//
// This is not an actual error - it is a signal that compilation should be skipped with an
// informational message directing the user to run `gh aw update`.
type RedirectOnlyWorkflowError struct {
	Path   string // File path of the redirect-only workflow
	Target string // The redirect target (workflow spec or URL)
}

// Error implements the error interface.
// Returns an informational message explaining that the file is a redirect placeholder
// and telling the user how to resolve it.
func (e *RedirectOnlyWorkflowError) Error() string {
	filename := filepath.Base(e.Path)

	msg := fmt.Sprintf(
		"Redirect-only workflow detected: %s\n\n"+
			"This workflow file is a redirect placeholder and is missing the 'on' trigger field.\n"+
			"The redirect was not resolved to the full workflow content during download.\n\n",
		filename,
	)
	if e.Target != "" {
		msg += fmt.Sprintf("This workflow redirects to: %s\n\n", e.Target)
	}
	msg += "Run 'gh aw update' to follow the redirect and get the full workflow.\n\n" +
		"Skipping compilation."
	return msg
}
