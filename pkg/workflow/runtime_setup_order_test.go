//go:build !integration

package workflow

import (
	"testing"
)

// TestGenerateSerenaLanguageServiceStepsDeterministicOrder verifies that
// GenerateSerenaLanguageServiceSteps returns no steps since Serena now runs in a container
func TestGenerateSerenaLanguageServiceStepsDeterministicOrder(t *testing.T) {
	// Create a Serena configuration with multiple languages
	tools := NewTools(map[string]any{
		"serena": map[string]any{
			"languages": map[string]any{
				"typescript": map[string]any{},
				"go":         map[string]any{},
				"python":     map[string]any{},
				"rust":       map[string]any{},
			},
		},
	})

	// Serena now runs in a container, so no language service steps should be generated
	steps := GenerateSerenaLanguageServiceSteps(tools)

	if len(steps) != 0 {
		t.Errorf("Expected 0 steps since Serena runs in a container, got %d steps", len(steps))
	}
}
