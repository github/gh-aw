//go:build !integration

package workflow

import (
	"testing"
)

// TestBundleJavaScriptFsInsideFunctionWithMultilineDestructure tests bundler functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundleJavaScriptFsInsideFunctionWithMultilineDestructure(t *testing.T) {
	t.Skip("Bundler tests skipped - scripts now use require() pattern to load external files at runtime")
}
