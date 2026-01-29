//go:build !integration

package workflow

import (
	"testing"
)

// TestNoOpScriptBundling tests script functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestNoOpScriptBundling(t *testing.T) {
	t.Skip("Script tests skipped - scripts now use require() pattern to load external files at runtime")
}
