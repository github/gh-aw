//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCustomActionCopilotTokenFallback tests that custom actions use the correct
// Copilot token fallback when no custom token is provided
func TestCustomActionCopilotTokenFallback(t *testing.T) {
	compiler := NewCompiler()

	// Register a test custom action
	testScript := `console.log('test');`
	actionPath := "./actions/test-action"
	err := DefaultScriptRegistry.RegisterWithAction("test_handler", testScript, RuntimeModeGitHubScript, actionPath)
	require.NoError(t, err)

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{},
	}

	// Test with UseCopilotToken=true and no custom token
	config := GitHubScriptStepConfig{
		StepName:        "Test Custom Action",
		StepID:          "test",
		Token:           "", // No custom token
		UseCopilotToken: true,
	}

	steps := compiler.buildCustomActionStep(workflowData, config, "test_handler")
	stepsContent := strings.Join(steps, "")

	t.Logf("Generated steps:\n%s", stepsContent)

	// Should use COPILOT_GITHUB_TOKEN fallback, not COPILOT_TOKEN
	assert.Contains(t, stepsContent, "COPILOT_GITHUB_TOKEN", "Should use COPILOT_GITHUB_TOKEN in fallback")
	assert.NotContains(t, stepsContent, "COPILOT_TOKEN ||", "Should not use deprecated COPILOT_TOKEN")

	// Verify it's using the correct fallback chain
	assert.Contains(t, stepsContent, "secrets.COPILOT_GITHUB_TOKEN || secrets.GH_AW_GITHUB_TOKEN",
		"Should use correct Copilot token fallback chain")
}
