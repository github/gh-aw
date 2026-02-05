package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var githubToolsetValidationLog = logger.New("workflow:github_toolset_validation_error")

// GitHubToolsetValidationError represents an error when GitHub tools are specified
// but their required toolsets are not enabled
type GitHubToolsetValidationError struct {
	// MissingToolsets maps toolset name to the list of tools that require it
	MissingToolsets map[string][]string
}

// NewGitHubToolsetValidationError creates a new validation error
func NewGitHubToolsetValidationError(missingToolsets map[string][]string) *GitHubToolsetValidationError {
	githubToolsetValidationLog.Printf("Creating toolset validation error for %d missing toolsets", len(missingToolsets))
	return &GitHubToolsetValidationError{
		MissingToolsets: missingToolsets,
	}
}

// Error implements the error interface
func (e *GitHubToolsetValidationError) Error() string {
	githubToolsetValidationLog.Printf("Formatting error message for %d missing toolsets", len(e.MissingToolsets))

	var lines []string
	lines = append(lines, "ERROR: GitHub tools specified in 'allowed' field require toolsets that are not enabled:")
	lines = append(lines, "")

	// Sort toolsets for consistent output
	var toolsets []string
	for toolset := range e.MissingToolsets {
		toolsets = append(toolsets, toolset)
	}
	sort.Strings(toolsets)

	// List each missing toolset and the tools that need it
	for _, toolset := range toolsets {
		tools := e.MissingToolsets[toolset]
		sort.Strings(tools)
		lines = append(lines, fmt.Sprintf("  Toolset '%s' is required by:", toolset))
		for _, tool := range tools {
			lines = append(lines, fmt.Sprintf("    - %s", tool))
		}
		lines = append(lines, "")
	}

	// Provide fix suggestion
	lines = append(lines, "Suggested fix: Add the missing toolsets to your GitHub tool configuration:")
	lines = append(lines, "")
	lines = append(lines, "tools:")
	lines = append(lines, "  github:")
	lines = append(lines, "    toolsets:")

	// Build the toolsets list
	var allToolsets []string
	allToolsets = append(allToolsets, "default") // Start with default
	allToolsets = append(allToolsets, toolsets...)

	for _, toolset := range allToolsets {
		lines = append(lines, fmt.Sprintf("      - %s", toolset))
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("See: %s", constants.DocsGitHubToolsURL))

	return strings.Join(lines, "\n")
}
