package cli

import (
	"fmt"
	"os"

	"github.com/githubnext/gh-aw/pkg/console"
)

// FormatValidationError formats validation errors for console output
// Preserves structured error content while applying console styling
//
// This function bridges the gap between pure validation logic (plain text errors)
// and CLI presentation layer (styled console output). By keeping validation errors
// as plain text at the validation layer, we maintain testability and reusability
// while providing consistent styled output in CLI contexts.
//
// The function handles both simple single-line errors and complex multi-line
// structured errors (like GitHubToolsetValidationError) by applying console
// formatting to preserve the error structure and readability.
func FormatValidationError(err error) string {
	if err == nil {
		return ""
	}

	errMsg := err.Error()

	// Apply console formatting to the entire error message
	// This preserves structured multi-line errors while adding visual styling
	return console.FormatErrorMessage(errMsg)
}

// PrintValidationError prints a validation error to stderr with console formatting
//
// This is a convenience helper that combines formatting and printing in one call.
// All validation errors should be printed using this function to ensure consistent
// styling across the CLI.
//
// Example usage:
//
//	if err := ValidateWorkflow(config); err != nil {
//	    PrintValidationError(err)
//	    return err
//	}
func PrintValidationError(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, FormatValidationError(err))
}
