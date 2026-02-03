package workflow

import "github.com/github/gh-aw/pkg/logger"

var workflowNameLog = logger.New("workflow:workflow_name")

// SanitizeIdentifier sanitizes a workflow name to create a safe identifier
// suitable for use as a user agent string or similar context.
//
// This is a SANITIZE function (character validity pattern). Use this when creating
// identifiers that must be purely alphanumeric with hyphens, with no special characters
// preserved. Unlike SanitizeWorkflowName which preserves dots and underscores, this
// function removes ALL special characters except hyphens.
//
// The function:
//   - Converts to lowercase
//   - Replaces spaces and underscores with hyphens
//   - Removes non-alphanumeric characters (except hyphens)
//   - Consolidates multiple hyphens into a single hyphen
//   - Trims leading and trailing hyphens
//   - Returns "github-agentic-workflow" if the result would be empty
//
// Example inputs and outputs:
//
//	SanitizeIdentifier("My Workflow")         // returns "my-workflow"
//	SanitizeIdentifier("test_workflow")       // returns "test-workflow"
//	SanitizeIdentifier("@@@")                 // returns "github-agentic-workflow" (default)
//	SanitizeIdentifier("Weekly v2.0")         // returns "weekly-v2-0"
//
// This function uses the unified SanitizeName function with options configured
// to trim leading/trailing hyphens and return a default value for empty results.
// Hyphens are preserved by default in SanitizeName, not via PreserveSpecialChars.
//
// See package documentation for guidance on when to use sanitize vs normalize patterns.
func SanitizeIdentifier(name string) string {
	workflowNameLog.Printf("Sanitizing workflow identifier: %s", name)
	result := SanitizeName(name, &SanitizeOptions{
		PreserveSpecialChars: []rune{},
		TrimHyphens:          true,
		DefaultValue:         "github-agentic-workflow",
	})
	if result != name {
		workflowNameLog.Printf("Sanitized identifier: %s -> %s", name, result)
	}
	return result
}
