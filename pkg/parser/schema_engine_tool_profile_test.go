//go:build !integration

package parser

import "testing"

func TestToolsProfileSchema(t *testing.T) {
	for _, test := range []struct {
		name    string
		profile any
		wantErr bool
	}{
		{name: "go", profile: "go"},
		{name: "go array", profile: []any{"go"}},
		{name: "unknown profile", profile: "repository", wantErr: true},
		{name: "empty profile", profile: "", wantErr: true},
		{name: "boolean", profile: true, wantErr: true},
		{name: "empty array", profile: []any{}, wantErr: true},
		{name: "duplicate array", profile: []any{"go", "go"}, wantErr: true},
		{name: "object", profile: map[string]any{"id": "go"}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on":     "workflow_dispatch",
				"engine": map[string]any{"id": "copilot", "copilot-sdk": true},
				"tools":  map[string]any{"profile": test.profile},
			}, "profile.md")
			if (err != nil) != test.wantErr {
				t.Fatalf("schema error = %v, want error %t", err, test.wantErr)
			}
		})
	}
}

func TestToolsProfileSchemaOptional(t *testing.T) {
	for _, engine := range []any{"copilot", map[string]any{"id": "copilot", "copilot-sdk": true}} {
		if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "workflow_dispatch", "engine": engine,
		}, "legacy.md"); err != nil {
			t.Fatalf("omitted profile must remain valid: %v", err)
		}
	}
}

func TestEngineToolProfileSchemaRemoved(t *testing.T) {
	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
		"on": "workflow_dispatch",
		"engine": map[string]any{
			"id": "copilot", "copilot-sdk": true, "tool-profile": "go-repository",
		},
	}, "legacy-profile.md")
	if err == nil {
		t.Fatal("engine.tool-profile must be rejected")
	}
}
