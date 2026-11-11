//go:build !integration

package parser

import (
	"fmt"
	"testing"
)

func TestMainWorkflowSchema_CheckoutDataOnly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		valid bool
	}{
		{"enabled", true, true},
		{"disabled", false, true},
		{"string", "true", false},
		{"expression", "${{ inputs.data_only }}", false},
		{"number", 1, false},
		{"null", nil, false},
	} {
		for _, list := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/list=%t", tc.name, list), func(t *testing.T) {
				var checkout any = map[string]any{"data-only": tc.value}
				if list {
					checkout = []any{checkout}
				}
				frontmatter := map[string]any{"on": "push", "engine": "copilot", "checkout": checkout}
				err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/gh-aw/data-only.md")
				if (err == nil) != tc.valid {
					t.Fatalf("schema validity = %v, want %v: %v", err == nil, tc.valid, err)
				}
			})
		}
	}
}
