//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestReactionConditionIncludesDiscussions verifies that the reaction condition
// includes both discussion and discussion_comment events
func TestReactionConditionIncludesDiscussions(t *testing.T) {
	result := BuildReactionCondition()
	rendered := result.Render()

	// Verify discussion events are included
	if !strings.Contains(rendered, "github.event_name == 'discussion'") {
		t.Error("Expected reaction condition to include 'discussion' event")
	}

	if !strings.Contains(rendered, "github.event_name == 'discussion_comment'") {
		t.Error("Expected reaction condition to include 'discussion_comment' event")
	}

	// Verify the rendered condition is valid
	if rendered == "" {
		t.Error("Rendered condition should not be empty")
	}

	// Log the full condition for manual review
	t.Logf("Full reaction condition: %s", rendered)
}
