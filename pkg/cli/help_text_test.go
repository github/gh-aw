//go:build !integration

package cli

import (
	"strings"
	"testing"
)

func TestWorkflowIDExplanation(t *testing.T) {
	// Test that the constant is not empty
	if WorkflowIDExplanation == "" {
		t.Error("WorkflowIDExplanation should not be empty")
	}

	// Test that it contains expected key phrases
	expectedPhrases := []string{
		"workflow-id",
		"basename",
		"Markdown",
		".md extension",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(WorkflowIDExplanation, phrase) {
			t.Errorf("WorkflowIDExplanation should contain '%s', but it doesn't", phrase)
		}
	}

	// Test that it uses proper capitalization (Markdown, not markdown)
	if strings.Contains(WorkflowIDExplanation, "markdown file") {
		t.Error("WorkflowIDExplanation should use 'Markdown' (capitalized), not 'markdown'")
	}

	// Test that it's properly formatted (multi-line)
	lines := strings.Split(WorkflowIDExplanation, "\n")
	if len(lines) < 2 {
		t.Error("WorkflowIDExplanation should be multi-line")
	}
}
