package parser

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecisionSchema(t *testing.T) {
	tests := []struct {
		name  string
		value any
		valid bool
	}{
		{"default model", map[string]any{"questions": []any{"Is this actionable?"}}, true},
		{"explicit model", map[string]any{"model": "mini", "questions": []any{"Review ${{ github.event.issue.title }}?"}}, true},
		{"eight questions", map[string]any{"questions": []any{"a", "b", "c", "d", "e", "f", "g", "h"}}, true},
		{"missing questions", map[string]any{}, false},
		{"empty questions", map[string]any{"questions": []any{}}, false},
		{"nine questions", map[string]any{"questions": []any{"a", "b", "c", "d", "e", "f", "g", "h", "i"}}, false},
		{"blank", map[string]any{"questions": []any{" \n\t"}}, false},
		{"empty string", map[string]any{"questions": []any{""}}, false},
		{"oversize", map[string]any{"questions": []any{strings.Repeat("x", 4097)}}, false},
		{"filespec", map[string]any{"questions": []any{" @question.txt"}}, false},
		{"nonstring", map[string]any{"questions": []any{true}}, false},
		{"scalar", "question", false},
		{"unknown field", map[string]any{"questions": []any{"a"}, "script": "x"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on": map[string]any{"workflow_dispatch": nil, "decision": tt.value},
			}, "decision.md")
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
