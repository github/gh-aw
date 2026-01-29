//go:build !integration

package workflow

import (
	"testing"
)

// TestParseClaudeLogDockerPullFormat tests log parser functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestParseClaudeLogDockerPullFormat(t *testing.T) {
	t.Skip("Log parser tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestParseClaudeLogDockerPullFormatJS tests log parser functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestParseClaudeLogDockerPullFormatJS(t *testing.T) {
	t.Skip("Log parser tests skipped - scripts now use require() pattern to load external files at runtime")
}
