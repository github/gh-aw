package stringutil

import (
	"path/filepath"
	"strings"
)

// NormalizeWorkflowName removes .md and .lock.yml extensions from workflow names.
// This is used to standardize workflow identifiers regardless of the file format.
//
// The function checks for extensions in order of specificity:
// 1. Removes .lock.yml extension (the compiled workflow format)
// 2. Removes .md extension (the markdown source format)
// 3. Returns the name unchanged if no recognized extension is found
//
// This function performs normalization only - it assumes the input is already
// a valid identifier and does NOT perform character validation or sanitization.
//
// Examples:
//
//	NormalizeWorkflowName("weekly-research")           // returns "weekly-research"
//	NormalizeWorkflowName("weekly-research.md")        // returns "weekly-research"
//	NormalizeWorkflowName("weekly-research.lock.yml")  // returns "weekly-research"
//	NormalizeWorkflowName("my.workflow.md")            // returns "my.workflow"
func NormalizeWorkflowName(name string) string {
	// Remove .lock.yml extension first (longer extension)
	if strings.HasSuffix(name, ".lock.yml") {
		return strings.TrimSuffix(name, ".lock.yml")
	}

	// Remove .md extension
	if strings.HasSuffix(name, ".md") {
		return strings.TrimSuffix(name, ".md")
	}

	return name
}

// NormalizeSafeOutputIdentifier converts dashes to underscores for safe output identifiers.
// This standardizes identifier format from the user-facing dash-separated format
// to the internal underscore-separated format used in safe outputs configuration.
//
// Both dash-separated and underscore-separated formats are valid inputs.
// This function simply standardizes to the internal representation.
//
// This function performs normalization only - it assumes the input is already
// a valid identifier and does NOT perform character validation or sanitization.
//
// Examples:
//
//	NormalizeSafeOutputIdentifier("create-issue")      // returns "create_issue"
//	NormalizeSafeOutputIdentifier("create_issue")      // returns "create_issue" (unchanged)
//	NormalizeSafeOutputIdentifier("add-comment")       // returns "add_comment"
//	NormalizeSafeOutputIdentifier("update-pr")         // returns "update_pr"
func NormalizeSafeOutputIdentifier(identifier string) string {
	return strings.ReplaceAll(identifier, "-", "_")
}

// MarkdownToLockFile converts a workflow markdown file path to its compiled lock file path.
// This is the standard transformation for agentic workflow files.
//
// The function removes the .md extension and adds .lock.yml extension.
// If the input already has a .lock.yml extension, it returns the path unchanged.
//
// Examples:
//
//	MarkdownToLockFile("weekly-research.md")                    // returns "weekly-research.lock.yml"
//	MarkdownToLockFile(".github/workflows/test.md")             // returns ".github/workflows/test.lock.yml"
//	MarkdownToLockFile("workflow.lock.yml")                     // returns "workflow.lock.yml" (unchanged)
//	MarkdownToLockFile("my.workflow.md")                        // returns "my.workflow.lock.yml"
func MarkdownToLockFile(mdPath string) string {
	// If already a lock file, return unchanged
	if strings.HasSuffix(mdPath, ".lock.yml") {
		return mdPath
	}

	cleaned := filepath.Clean(mdPath)
	return strings.TrimSuffix(cleaned, ".md") + ".lock.yml"
}

// LockFileToMarkdown converts a compiled lock file path back to its markdown source path.
// This is used when navigating from compiled workflows back to source files.
//
// The function removes the .lock.yml extension and adds .md extension.
// If the input already has a .md extension, it returns the path unchanged.
//
// Examples:
//
//	LockFileToMarkdown("weekly-research.lock.yml")              // returns "weekly-research.md"
//	LockFileToMarkdown(".github/workflows/test.lock.yml")       // returns ".github/workflows/test.md"
//	LockFileToMarkdown("workflow.md")                           // returns "workflow.md" (unchanged)
//	LockFileToMarkdown("my.workflow.lock.yml")                  // returns "my.workflow.md"
func LockFileToMarkdown(lockPath string) string {
	// If already a markdown file, return unchanged
	if strings.HasSuffix(lockPath, ".md") {
		return lockPath
	}

	cleaned := filepath.Clean(lockPath)
	return strings.TrimSuffix(cleaned, ".lock.yml") + ".md"
}

// IsAgenticWorkflow returns true if the file path is an agentic workflow file.
// Agentic workflows end with .md.
//
// Examples:
//
//	IsAgenticWorkflow("test.md")                                // returns true
//	IsAgenticWorkflow("weekly-research.md")                     // returns true
//	IsAgenticWorkflow(".github/workflows/workflow.md")          // returns true
//	IsAgenticWorkflow("test.lock.yml")                          // returns false
func IsAgenticWorkflow(path string) bool {
	// Must end with .md
	return strings.HasSuffix(path, ".md")
}

// IsLockFile returns true if the file path is a compiled lock file.
// Lock files end with .lock.yml and are compiled from agentic workflows.
//
// Examples:
//
//	IsLockFile("test.lock.yml")                                 // returns true
//	IsLockFile(".github/workflows/workflow.lock.yml")           // returns true
//	IsLockFile("test.md")                                       // returns false
func IsLockFile(path string) bool {
	return strings.HasSuffix(path, ".lock.yml")
}
