//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestGenerateCreateAwInfoWithStaged(t *testing.T) {
	// Create a compiler instance
	c := NewCompiler()

	// Test with staged: true
	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1}},
			Staged:       true, // pointer to true
		},
	}

	// Create a test engine
	engine := NewClaudeEngine()

	var yaml strings.Builder
	c.generateCreateAwInfo(&yaml, workflowData, engine)

	result := yaml.String()

	// Check that staged: true is included in the aw_info.json
	if !strings.Contains(result, "staged: true") {
		t.Error("Expected 'staged: true' to be included in aw_info.json when staged is true")
	}

	// Test with staged: false
	workflowData.SafeOutputs.Staged = false

	yaml.Reset()
	c.generateCreateAwInfo(&yaml, workflowData, engine)

	result = yaml.String()

	// Check that staged: false is included in the aw_info.json
	if !strings.Contains(result, "staged: false") {
		t.Error("Expected 'staged: false' to be included in aw_info.json when staged is false")
	}

	// Test with no SafeOutputs config
	workflowData.SafeOutputs = nil

	yaml.Reset()
	c.generateCreateAwInfo(&yaml, workflowData, engine)

	result = yaml.String()

	// Check that staged: false is included in the aw_info.json when SafeOutputs is nil
	if !strings.Contains(result, "staged: false") {
		t.Error("Expected 'staged: false' to be included in aw_info.json when SafeOutputs is nil")
	}
}
