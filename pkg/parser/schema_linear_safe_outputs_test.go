//go:build !integration

package parser

import "testing"

func TestMainWorkflowSchemaLinearSafeOutputs(t *testing.T) {
	t.Parallel()

	valid := map[string]any{
		"on":     "push",
		"engine": "copilot",
		"safe-outputs": map[string]any{
			"linear-create-issue": map[string]any{
				"team-id":    "9cfb482a-81e3-4154-b5b9-2c805e70a02d",
				"project-id": "810f57a7e383",
				"max":        1,
			},
			"linear-add-comment": map[string]any{
				"target": "ENG-123",
			},
			"linear-update-issue": map[string]any{
				"target": "9cfb482a-81e3-4154-b5b9-2c805e70a02d",
				"title":  true,
				"body":   true,
			},
		},
	}

	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(valid, "/tmp/linear-valid.md"); err != nil {
		t.Fatalf("expected valid Linear safe outputs configuration: %v", err)
	}

	validExpression := map[string]any{
		"on":     "push",
		"engine": "copilot",
		"safe-outputs": map[string]any{
			"linear-create-issue": map[string]any{
				"team-id": "${{ vars.LINEAR_TEAM_ID }}",
			},
		},
	}
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(validExpression, "/tmp/linear-expression.md"); err != nil {
		t.Fatalf("expected expression-valued Linear team ID to be valid: %v", err)
	}

	globalFallback := map[string]any{
		"on":     "push",
		"engine": "copilot",
		"safe-outputs": map[string]any{
			"linear-create-issue": map[string]any{},
		},
	}
	if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(globalFallback, "/tmp/linear-global-fallback.md"); err != nil {
		t.Fatalf("expected global Linear ID fallback configuration to be valid: %v", err)
	}

	tests := []struct {
		name        string
		safeOutputs map[string]any
	}{
		{
			name: "malformed team ID",
			safeOutputs: map[string]any{
				"linear-create-issue": map[string]any{
					"team-id": "not-a-team",
				},
			},
		},
		{
			name: "malformed project ID",
			safeOutputs: map[string]any{
				"linear-token": "${{ secrets.LINEAR_API_KEY }}",
				"linear-create-issue": map[string]any{
					"team-id":    "9cfb482a-81e3-4154-b5b9-2c805e70a02d",
					"project-id": "not-a-project",
				},
			},
		},
		{
			name: "malformed target",
			safeOutputs: map[string]any{
				"linear-token":       "${{ secrets.LINEAR_API_KEY }}",
				"linear-add-comment": map[string]any{"target": "https://api.linear.app"},
			},
		},
		{
			name: "literal token",
			safeOutputs: map[string]any{
				"linear-token":       "lin_api_secret",
				"linear-add-comment": map[string]any{"target": "ENG-123"},
			},
		},
		{
			name: "no update fields",
			safeOutputs: map[string]any{
				"linear-token":        "${{ secrets.LINEAR_API_KEY }}",
				"linear-update-issue": map[string]any{"target": "ENG-123"},
			},
		},
		{
			name: "unknown field",
			safeOutputs: map[string]any{
				"linear-token":       "${{ secrets.LINEAR_API_KEY }}",
				"linear-add-comment": map[string]any{"target": "ENG-123", "endpoint": "https://example.com"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frontmatter := map[string]any{"on": "push", "engine": "copilot", "safe-outputs": tt.safeOutputs}
			if err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "/tmp/linear-invalid.md"); err == nil {
				t.Fatal("expected schema validation to reject malformed Linear safe outputs configuration")
			}
		})
	}
}
