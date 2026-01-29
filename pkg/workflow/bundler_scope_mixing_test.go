//go:build !integration

package workflow

import (
	"testing"
)

// TestBundleJavaScriptWithMixedScopeRequires tests bundler functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestBundleJavaScriptWithMixedScopeRequires(t *testing.T) {
	t.Skip("Bundler tests skipped - scripts now use require() pattern to load external files at runtime")
}
