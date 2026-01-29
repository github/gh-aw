//go:build integration

package workflow

import (
	"strings"
	"testing"
)

func TestLabelTriggerIntegrationSimple(t *testing.T) {
	frontmatter := map[string]any{
		"on": "issue labeled bug enhancement",
	}

	compiler := NewCompiler()
	err := compiler.preprocessScheduleFields(frontmatter, "", "")
	if err != nil {
		t.Fatalf("preprocessScheduleFields() error = %v", err)
	}

	// Check that the on section was expanded
	on, ok := frontmatter["on"].(map[string]any)
	if !ok {
		t.Fatalf("on section is not a map")
	}

	// Check issues trigger
	issues, ok := on["issues"].(map[string]any)
	if !ok {
		t.Fatalf("issues trigger not found")
	}

	// Check types
	types, ok := issues["types"].([]any)
	if !ok {
		t.Fatalf("issues.types is not an array")
	}
	if len(types) != 1 || types[0] != "labeled" {
		t.Errorf("issues.types = %v, want [labeled]", types)
	}

	// Check names
	names, ok := issues["names"].([]string)
	if !ok {
		t.Fatalf("issues.names is not a string array")
	}
	if len(names) != 2 || names[0] != "bug" || names[1] != "enhancement" {
		t.Errorf("issues.names = %v, want [bug enhancement]", names)
	}

	// Check that the native filter marker is present
	nativeFilter, ok := issues["__gh_aw_native_label_filter__"].(bool)
	if !ok || !nativeFilter {
		t.Errorf("__gh_aw_native_label_filter__ = %v, want true", nativeFilter)
	}

	// Check workflow_dispatch exists
	dispatch, ok := on["workflow_dispatch"].(map[string]any)
	if !ok {
		t.Fatalf("workflow_dispatch not found")
	}

	// Check inputs
	inputs, ok := dispatch["inputs"].(map[string]any)
	if !ok {
		t.Fatalf("workflow_dispatch.inputs is not a map")
	}

	// Check item_number input
	itemNumber, ok := inputs["item_number"].(map[string]any)
	if !ok {
		t.Fatalf("item_number input not found")
	}

	description, ok := itemNumber["description"].(string)
	if !ok {
		t.Fatalf("item_number.description is not a string")
	}
	if !strings.Contains(description, "issue") {
		t.Errorf("item_number.description = %q, want to contain 'issue'", description)
	}

	required, ok := itemNumber["required"].(bool)
	if !ok || !required {
		t.Errorf("item_number.required = %v, want true", required)
	}
}

func TestLabelTriggerIntegrationIssue(t *testing.T) {
	frontmatter := map[string]any{
		"on": "issue labeled bug",
	}

	compiler := NewCompiler()
	err := compiler.preprocessScheduleFields(frontmatter, "", "")
	if err != nil {
		t.Fatalf("preprocessScheduleFields() error = %v", err)
	}

	// Check that the on section was expanded
	on, ok := frontmatter["on"].(map[string]any)
	if !ok {
		t.Fatalf("on section is not a map")
	}

	// Check issues trigger exists
	issues, ok := on["issues"].(map[string]any)
	if !ok {
		t.Fatalf("issues trigger not found")
	}

	// Verify it's a label trigger
	types, ok := issues["types"].([]any)
	if !ok || len(types) != 1 || types[0] != "labeled" {
		t.Errorf("issues.types = %v, want [labeled]", types)
	}
}

func TestLabelTriggerIntegrationPullRequest(t *testing.T) {
	frontmatter := map[string]any{
		"on": "pull_request labeled needs-review approved",
	}

	compiler := NewCompiler()
	err := compiler.preprocessScheduleFields(frontmatter, "", "")
	if err != nil {
		t.Fatalf("preprocessScheduleFields() error = %v", err)
	}

	// Check that the on section was expanded
	on, ok := frontmatter["on"].(map[string]any)
	if !ok {
		t.Fatalf("on section is not a map")
	}

	// Check pull_request trigger
	pr, ok := on["pull_request"].(map[string]any)
	if !ok {
		t.Fatalf("pull_request trigger not found")
	}

	// Check types
	types, ok := pr["types"].([]any)
	if !ok {
		t.Fatalf("pull_request.types is not an array")
	}
	if len(types) != 1 || types[0] != "labeled" {
		t.Errorf("pull_request.types = %v, want [labeled]", types)
	}

	// Check names
	names, ok := pr["names"].([]string)
	if !ok {
		t.Fatalf("pull_request.names is not a string array")
	}
	if len(names) != 2 || names[0] != "needs-review" || names[1] != "approved" {
		t.Errorf("pull_request.names = %v, want [needs-review approved]", names)
	}

	// Check workflow_dispatch exists with correct description
	dispatch, ok := on["workflow_dispatch"].(map[string]any)
	if !ok {
		t.Fatalf("workflow_dispatch not found")
	}

	inputs, ok := dispatch["inputs"].(map[string]any)
	if !ok {
		t.Fatalf("workflow_dispatch.inputs is not a map")
	}

	itemNumber, ok := inputs["item_number"].(map[string]any)
	if !ok {
		t.Fatalf("item_number input not found")
	}

	description, ok := itemNumber["description"].(string)
	if !ok {
		t.Fatalf("item_number.description is not a string")
	}
	if !strings.Contains(description, "pull request") {
		t.Errorf("item_number.description = %q, want to contain 'pull request'", description)
	}
}

func TestLabelTriggerIntegrationDiscussion(t *testing.T) {
	frontmatter := map[string]any{
		"on": "discussion labeled question announcement",
	}

	compiler := NewCompiler()
	err := compiler.preprocessScheduleFields(frontmatter, "", "")
	if err != nil {
		t.Fatalf("preprocessScheduleFields() error = %v", err)
	}

	// Check that the on section was expanded
	on, ok := frontmatter["on"].(map[string]any)
	if !ok {
		t.Fatalf("on section is not a map")
	}

	// Check discussion trigger
	discussion, ok := on["discussion"].(map[string]any)
	if !ok {
		t.Fatalf("discussion trigger not found")
	}

	// Check types
	types, ok := discussion["types"].([]any)
	if !ok {
		t.Fatalf("discussion.types is not an array")
	}
	if len(types) != 1 || types[0] != "labeled" {
		t.Errorf("discussion.types = %v, want [labeled]", types)
	}

	// Note: GitHub Actions doesn't support 'names' field for discussion events
	// So we don't check for names or the native filter marker for discussions

	// Check workflow_dispatch exists
	dispatch, ok := on["workflow_dispatch"].(map[string]any)
	if !ok {
		t.Fatalf("workflow_dispatch not found")
	}

	// Check inputs
	inputs, ok := dispatch["inputs"].(map[string]any)
	if !ok {
		t.Fatalf("workflow_dispatch.inputs is not a map")
	}

	// Check item_number input
	itemNumber, ok := inputs["item_number"].(map[string]any)
	if !ok {
		t.Fatalf("item_number input not found")
	}

	description, ok := itemNumber["description"].(string)
	if !ok {
		t.Fatalf("item_number.description is not a string")
	}
	if !strings.Contains(description, "discussion") {
		t.Errorf("item_number.description = %q, want to contain 'discussion'", description)
	}
}

func TestLabelTriggerIntegrationError(t *testing.T) {
	// Test that implicit "labeled" syntax is not recognized
	frontmatter := map[string]any{
		"on": "labeled bug",
	}

	compiler := NewCompiler()
	err := compiler.preprocessScheduleFields(frontmatter, "", "")
	if err != nil {
		t.Fatalf("preprocessScheduleFields() unexpected error = %v", err)
	}

	// The shorthand should NOT have been expanded (because implicit labeled is not supported)
	// So "on" should still be a string
	onValue := frontmatter["on"]
	if onStr, ok := onValue.(string); !ok || onStr != "labeled bug" {
		t.Errorf("'labeled bug' should not be expanded (implicit syntax removed), got: %v", onValue)
	}
}
