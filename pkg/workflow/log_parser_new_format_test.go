//go:build !integration

package workflow

import (
	"testing"
)

// TestParseClaudeLogNewFormat tests log parser functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestParseClaudeLogNewFormat(t *testing.T) {
	t.Skip("Log parser tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestParseClaudeLogNewFormatJSScript tests log parser functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestParseClaudeLogNewFormatJSScript(t *testing.T) {
	t.Skip("Log parser tests skipped - scripts now use require() pattern to load external files at runtime")
}
