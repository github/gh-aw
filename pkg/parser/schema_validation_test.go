//go:build !integration

package parser

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestForbiddenFieldsInSharedWorkflows verifies each forbidden field is properly rejected
func TestForbiddenFieldsInSharedWorkflows(t *testing.T) {
	// Use the SharedWorkflowForbiddenFields constant from constants package
	forbiddenFields := constants.SharedWorkflowForbiddenFields

	for _, field := range forbiddenFields {
		t.Run("reject_"+field, func(t *testing.T) {
			frontmatter := map[string]any{
				field:   "test-value",
				"tools": map[string]any{"bash": true},
			}

			err := ValidateIncludedFileFrontmatterWithSchema(frontmatter)
			if err == nil {
				t.Errorf("Expected error for forbidden field '%s', got nil", field)
			}

			if err != nil && !strings.Contains(err.Error(), "cannot be used in shared workflows") {
				t.Errorf("Error message should mention shared workflows, got: %v", err)
			}
		})
	}
}

// TestAllowedFieldsInSharedWorkflows verifies allowed fields work correctly
func TestAllowedFieldsInSharedWorkflows(t *testing.T) {
	allowedFields := map[string]any{
		"tools":          map[string]any{"bash": true},
		"engine":         "copilot",
		"network":        map[string]any{"allowed": []string{"defaults"}},
		"mcp-servers":    map[string]any{},
		"permissions":    "read-all",
		"runtimes":       map[string]any{"node": map[string]any{"version": "20"}},
		"safe-outputs":   map[string]any{},
		"safe-inputs":    map[string]any{},
		"services":       map[string]any{},
		"steps":          []any{},
		"secret-masking": true,
		"jobs":           map[string]any{"test": map[string]any{"runs-on": "ubuntu-latest", "steps": []any{map[string]any{"run": "echo test"}}}},
		"description":    "test",
		"metadata":       map[string]any{},
		"inputs":         map[string]any{},
		"bots":           []string{"copilot"},
		"post-steps":     []any{map[string]any{"run": "echo cleanup"}},
		"labels":         []string{"automation", "testing"},
		"imports":        []string{"./shared.md"},
		"cache":          map[string]any{"key": "test-key", "path": "node_modules"},
		"source":         "githubnext/agentics/workflows/ci-doctor.md@v1.0.0",
	}

	for field, value := range allowedFields {
		t.Run("allow_"+field, func(t *testing.T) {
			frontmatter := map[string]any{
				field: value,
			}

			err := ValidateIncludedFileFrontmatterWithSchema(frontmatter)
			if err != nil && strings.Contains(err.Error(), "cannot be used in shared workflows") {
				t.Errorf("Field '%s' should be allowed in shared workflows, got error: %v", field, err)
			}
		})
	}
}
