//go:build !integration

package workflow

import (
	"testing"
)

// TestStagedPreviewInlined tests that staged preview functionality is bundled
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestStagedPreviewInlined(t *testing.T) {
	t.Skip("Staged preview bundling tests skipped - scripts now use require() pattern to load external files at runtime")
}
