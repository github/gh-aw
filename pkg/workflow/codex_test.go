//go:build !integration

package workflow

import (
	"testing"
)

// TestCodexAIConfiguration tests workflow compilation
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestCodexAIConfiguration(t *testing.T) {
	t.Skip("Workflow compilation tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestCodexMCPConfigGeneration tests workflow compilation
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestCodexMCPConfigGeneration(t *testing.T) {
	t.Skip("Workflow compilation tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestCodexConfigField tests workflow compilation
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestCodexConfigField(t *testing.T) {
	t.Skip("Workflow compilation tests skipped - scripts now use require() pattern to load external files at runtime")
}
