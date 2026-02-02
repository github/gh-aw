package workflow

import (
	"fmt"
	"path/filepath"

	"github.com/github/gh-aw/pkg/logger"
)

var sharedWorkflowLog = logger.New("workflow:shared_workflow_error")

// SharedWorkflowError represents a workflow that is missing the 'on' field
// and should be treated as a shared/importable workflow component rather than
// a standalone workflow. This is not an actual error - it's a signal that
// compilation should be skipped with an informational message.
type SharedWorkflowError struct {
	Path string // File path of the shared workflow
}

// NewSharedWorkflowError creates a new shared workflow error
func NewSharedWorkflowError(path string) *SharedWorkflowError {
	sharedWorkflowLog.Printf("Creating shared workflow info for: %s", path)
	return &SharedWorkflowError{
		Path: path,
	}
}

// Error implements the error interface
// Returns a formatted info message explaining that this is a shared workflow
func (e *SharedWorkflowError) Error() string {
	sharedWorkflowLog.Printf("Formatting info message for shared workflow: %s", e.Path)

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
