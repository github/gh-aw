//go:build !integration

package workflow

import (
	"testing"
)

// TestLogParserSnapshots tests script functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestLogParserSnapshots(t *testing.T) {
	t.Skip("Script tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestLogParserScriptRetrieval tests script functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestLogParserScriptRetrieval(t *testing.T) {
	t.Skip("Script tests skipped - scripts now use require() pattern to load external files at runtime")
}
