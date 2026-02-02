// This file provides error aggregation utilities for validation.
//
// # Error Aggregation
//
// This file implements error collection and aggregation for validation
// functions, allowing users to see all validation errors in a single run
// instead of discovering them one at a time.
//
// # Error Aggregation Functions
//
//   - NewErrorCollector() - Creates a new error collector
//   - ErrorCollector.Add() - Adds an error to the collection
//   - ErrorCollector.HasErrors() - Checks if any errors were collected
//   - ErrorCollector.Error() - Returns aggregated error using errors.Join
//   - ErrorCollector.Count() - Returns the number of collected errors
//
// # Usage Pattern
//
// Use error collectors in validation functions to collect multiple errors:
//
//	func validateMultipleThings(config Config, failFast bool) error {
//	    collector := NewErrorCollector(failFast)
//
//	    if err := validateThing1(config); err != nil {
//	        if returnErr := collector.Add(err); returnErr != nil {
//	            return returnErr // Fail-fast mode
//	        }
//	    }
//
//	    if err := validateThing2(config); err != nil {
//	        if returnErr := collector.Add(err); returnErr != nil {
//	            return returnErr // Fail-fast mode
//	        }
//	    }
//
//	    return collector.Error()
//	}
//
// # Fail-Fast Mode
//
// When failFast is true, the collector returns immediately on the first error.
// When false, it collects all errors and returns them joined with errors.Join.

package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var errorAggregationLog = logger.New("workflow:error_aggregation")

// ErrorCollector collects multiple validation errors
type ErrorCollector struct {
	errors   []error
	failFast bool
}

// NewErrorCollector creates a new error collector
// If failFast is true, the collector will stop at the first error
func NewErrorCollector(failFast bool) *ErrorCollector {
	errorAggregationLog.Printf("Creating error collector: fail_fast=%v", failFast)
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

	errorAggregationLog.Printf("Adding error to collector: %v", err)

	if c.failFast {
		errorAggregationLog.Print("Fail-fast enabled, returning error immediately")
		return err
	}

	c.errors = append(c.errors, err)
	return nil
}

// HasErrors returns true if any errors have been collected
func (c *ErrorCollector) HasErrors() bool {
	return len(c.errors) > 0
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

	errorAggregationLog.Printf("Aggregating %d errors", len(c.errors))

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

	errorAggregationLog.Printf("Formatting %d errors for category: %s", len(c.errors), category)

	if len(c.errors) == 1 {
		return c.errors[0]
	}

	// Build formatted error with count header
	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d %s errors:", len(c.errors), category)
	for _, err := range c.errors {
		sb.WriteString("\n  • ")
		sb.WriteString(err.Error())
	}

	return fmt.Errorf("%s", sb.String())
}

// FormatAggregatedError formats aggregated errors with a summary header
// Returns a formatted error with count and categorization if multiple errors exist
func FormatAggregatedError(err error, category string) error {
	if err == nil {
		return nil
	}

	// Check if this is a joined error by looking for newlines
	errStr := err.Error()
	lines := strings.Split(errStr, "\n")

	if len(lines) <= 1 {
		return err
	}

	// Format with count and category
	header := fmt.Sprintf("Found %d %s errors:", len(lines), category)

	// Reconstruct with header
	var sb strings.Builder
	sb.WriteString(header)
	for _, line := range lines {
		if line != "" {
			sb.WriteString("\n  • ")
			sb.WriteString(line)
		}
	}

	return fmt.Errorf("%s", sb.String())
}

// SplitJoinedErrors splits a joined error into individual error strings
func SplitJoinedErrors(err error) []error {
	if err == nil {
		return nil
	}

	// errors.Join formats errors separated by newlines
	errStr := err.Error()
	lines := strings.Split(errStr, "\n")

	result := make([]error, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			result = append(result, fmt.Errorf("%s", line))
		}
	}

	if len(result) == 0 {
		return []error{err}
	}

	return result
}
