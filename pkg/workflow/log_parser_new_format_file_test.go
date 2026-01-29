//go:build !integration

package workflow

import (
	"testing"
)

// TestParseClaudeLogNewFormatFile tests log parser functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestParseClaudeLogNewFormatFile(t *testing.T) {
	t.Skip("Log parser tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestParseClaudeLogNewFormatJSScriptFromFile tests log parser functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestParseClaudeLogNewFormatJSScriptFromFile(t *testing.T) {
	t.Skip("Log parser tests skipped - scripts now use require() pattern to load external files at runtime")
}
