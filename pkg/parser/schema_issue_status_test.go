//go:build !integration

package parser

import "testing"

func TestMainWorkflowSchemaIssuePlanningStatus(t *testing.T) {
	tests := []struct {
		name   string
		output map[string]any
		valid  bool
	}{
		{"set-defaults", map[string]any{"set-issue-status": nil}, true},
		{"set", map[string]any{"set-issue-status": map[string]any{"allowed": []any{"Backlog", 12}, "allow-close": true, "allow-reopen": false, "target": "*", "required-labels": []any{"triaged"}}}, true},
		{"set-invalid-boolean", map[string]any{"set-issue-status": map[string]any{"allow-close": "true"}}, false},
		{"set-invalid-id", map[string]any{"set-issue-status": map[string]any{"allowed": []any{0}}}, false},
		{"set-invalid-fraction", map[string]any{"set-issue-status": map[string]any{"allowed": []any{1.5}}}, false},
		{"set-blank-name", map[string]any{"set-issue-status": map[string]any{"allowed": []any{"  "}}}, false},
		{"set-no-suggestions", map[string]any{"set-issue-status": map[string]any{"issue-intent": true}}, false},
		{"create-name", map[string]any{"create-issue": map[string]any{"planning-status": "Backlog", "allowed-statuses": []any{"Backlog", 12}}}, true},
		{"create-id", map[string]any{"create-issue": map[string]any{"planning-status": 12}}, true},
		{"create-invalid-type", map[string]any{"create-issue": map[string]any{"planning-status": true}}, false},
		{"create-invalid-id", map[string]any{"create-issue": map[string]any{"planning-status": -1}}, false},
		{"create-invalid-allowed", map[string]any{"create-issue": map[string]any{"allowed-statuses": []any{false}}}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on": "issues", "engine": "copilot", "safe-outputs": test.output,
			}, "status.md")
			if (err == nil) != test.valid {
				t.Fatalf("schema validity = %v, want %v: %v", err == nil, test.valid, err)
			}
		})
	}
}
