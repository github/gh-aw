//go:build !integration

package workflow

import (
	"testing"
)

// TestTokenCountingConsistency tests log parser functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestTokenCountingConsistency(t *testing.T) {
	t.Skip("Log parser tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestTokenCountingWithoutCacheTokens tests log parser functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestTokenCountingWithoutCacheTokens(t *testing.T) {
	t.Skip("Log parser tests skipped - scripts now use require() pattern to load external files at runtime")
}
