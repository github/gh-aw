//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestStagedFlag(t *testing.T) {
	// Create a compiler instance
	c := NewCompiler()

	// Test frontmatter with staged: true
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"create-issue": nil,
			"staged":       true,
		},
	}

	// Extract the safe outputs config
	config := c.extractSafeOutputsConfig(frontmatter)
	if config == nil {
		t.Fatal("Expected config to be parsed")
	}

	if !config.Staged {
		t.Fatal("Expected staged flag to be true")
	}

	// Test that CreateIssues config is also present
	if config.CreateIssues == nil {
		t.Fatal("Expected CreateIssues config to be present")
	}
}

func TestStagedFlagDefault(t *testing.T) {
	// Create a compiler instance
	c := NewCompiler()

	// Test frontmatter without staged flag
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"create-issue": nil,
		},
	}

	// Extract the safe outputs config
	config := c.extractSafeOutputsConfig(frontmatter)
	if config == nil {
		t.Fatal("Expected config to be parsed")
	}

	// Verify staged flag is false
	if config.Staged {
		t.Fatal("Expected staged flag to be false when not specified")
	}
}

func TestStagedFlagFalse(t *testing.T) {
	// Create a compiler instance
	c := NewCompiler()

	// Test frontmatter with staged: false
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"create-issue": nil,
			"staged":       false,
		},
	}

	// Extract the safe outputs config
	config := c.extractSafeOutputsConfig(frontmatter)
	if config == nil {
		t.Fatal("Expected config to be parsed")
	}

	if config.Staged {
		t.Fatal("Expected staged flag to be false")
	}
}

func TestClaudeEngineWithStagedFlag(t *testing.T) {
	engine := NewClaudeEngine()

	// Test with staged flag true
	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1}},
			Staged:       true, // pointer to true
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "test-log")
	if len(steps) == 0 {
		t.Fatalf("Expected at least one step, got none")
	}

	// Convert first step to YAML string for testing
	stepContent := strings.Join([]string(steps[0]), "\n")

	// Check that GH_AW_SAFE_OUTPUTS_STAGED is included
	if !strings.Contains(stepContent, "GH_AW_SAFE_OUTPUTS_STAGED: true") && !strings.Contains(stepContent, "GH_AW_SAFE_OUTPUTS_STAGED: \"true\"") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_STAGED environment variable to be set to true")
	}

	// Test with staged flag false
	workflowData.SafeOutputs.Staged = false // pointer to false

	steps = engine.GetExecutionSteps(workflowData, "test-log")
	stepContent = strings.Join([]string(steps[0]), "\n")

	// Check that GH_AW_SAFE_OUTPUTS_STAGED is not included when false
	if strings.Contains(stepContent, "GH_AW_SAFE_OUTPUTS_STAGED") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_STAGED environment variable not to be set when staged is false")
	}

}

func TestCodexEngineWithStagedFlag(t *testing.T) {
	engine := NewCodexEngine()

	// Test with staged flag true
	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1}},
			Staged:       true, // pointer to true
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "test-log")
	if len(steps) == 0 {
		t.Fatalf("Expected at least one step, got none")
	}

	// Convert first step to YAML string for testing
	stepContent := strings.Join([]string(steps[0]), "\n")

	// Check that GH_AW_SAFE_OUTPUTS_STAGED is included in the env section
	if !strings.Contains(stepContent, "GH_AW_SAFE_OUTPUTS_STAGED: true") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_STAGED environment variable to be set to true in Codex engine")
	}
}
