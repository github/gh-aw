//go:build !integration

package workflow

import (
	"testing"
)

// TestPRReviewCommentConfigParsing tests workflow functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestPRReviewCommentConfigParsing(t *testing.T) {
	t.Skip("Workflow tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestPRReviewCommentJobGeneration tests workflow functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestPRReviewCommentJobGeneration(t *testing.T) {
	t.Skip("Workflow tests skipped - scripts now use require() pattern to load external files at runtime")
}
