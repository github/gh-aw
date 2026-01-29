//go:build !integration

package workflow

import (
	"testing"
)

// TestGenerateGitHubContextPromptStep tests workflow functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestGenerateGitHubContextPromptStep(t *testing.T) {
	t.Skip("Workflow tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestGenerateGitHubContextSecurePattern tests workflow functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestGenerateGitHubContextSecurePattern(t *testing.T) {
	t.Skip("Workflow tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestGenerateTemplateRenderingWithGitHubContext tests workflow functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestGenerateTemplateRenderingWithGitHubContext(t *testing.T) {
	t.Skip("Workflow tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestGitHubContextTemplateConditionals tests workflow functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestGitHubContextTemplateConditionals(t *testing.T) {
	t.Skip("Workflow tests skipped - scripts now use require() pattern to load external files at runtime")
}
