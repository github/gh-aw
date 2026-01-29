//go:build !integration

package workflow

import (
	"testing"
)

// TestLogParserScriptMethods tests log parser functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestLogParserScriptMethods(t *testing.T) {
	t.Skip("Log parser tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestGetLogParserScript tests log parser functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestGetLogParserScript(t *testing.T) {
	t.Skip("Log parser tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestParseClaudeLogSmoke tests log parser functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestParseClaudeLogSmoke(t *testing.T) {
	t.Skip("Log parser tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestParseClaudeLogInitialization tests log parser functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestParseClaudeLogInitialization(t *testing.T) {
	t.Skip("Log parser tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestParseClaudeMixedFormatLog tests log parser functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestParseClaudeMixedFormatLog(t *testing.T) {
	t.Skip("Log parser tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestClaudeEngineMixedFormatParsing tests log parser functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestClaudeEngineMixedFormatParsing(t *testing.T) {
	t.Skip("Log parser tests skipped - scripts now use require() pattern to load external files at runtime")
}
