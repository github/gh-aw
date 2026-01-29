//go:build !integration

package workflow

import (
	"testing"
)

// TestSafeOutputsMCPServerUsesCjsExtension tests integration functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestSafeOutputsMCPServerUsesCjsExtension(t *testing.T) {
	t.Skip("Integration tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestSafeInputsMCPServerUsesCjsExtension tests integration functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestSafeInputsMCPServerUsesCjsExtension(t *testing.T) {
	t.Skip("Integration tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestSafeInputsToolsConfigUsesCjsExtension tests integration functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestSafeInputsToolsConfigUsesCjsExtension(t *testing.T) {
	t.Skip("Integration tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestJavaScriptSourcesUseCjsExtension tests integration functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestJavaScriptSourcesUseCjsExtension(t *testing.T) {
	t.Skip("Integration tests skipped - scripts now use require() pattern to load external files at runtime")
}
