//go:build !integration

package parser

import "testing"

func TestToolsProfileSchema(t *testing.T) {
	for _, test := range []struct {
		name    string
		profile any
		wantErr bool
	}{
		{name: "string", profile: "go"},
		{name: "array", profile: []any{"go", "future"}},
		{name: "empty string", profile: "", wantErr: true},
		{name: "blank string", profile: " ", wantErr: true},
		{name: "boolean", profile: true, wantErr: true},
		{name: "empty array", profile: []any{}, wantErr: true},
		{name: "duplicate array", profile: []any{"go", "go"}, wantErr: true},
		{name: "object", profile: map[string]any{"id": "go"}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on":    "workflow_dispatch",
				"tools": map[string]any{"profile": test.profile},
			}, "profile.md")
			if (err != nil) != test.wantErr {
				t.Fatalf("schema error = %v, want error %t", err, test.wantErr)
			}
		})
	}
}
