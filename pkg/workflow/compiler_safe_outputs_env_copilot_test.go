//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCopilotAssignmentEnvVarIsSet verifies that GH_AW_ASSIGN_COPILOT is set
// when copilot is in the assignees list
func TestCopilotAssignmentEnvVarIsSet(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				Assignees:            []string{"copilot"},
			},
		},
	}

	var steps []string
	compiler.addAllSafeOutputConfigEnvVars(&steps, data)

	// Join steps to search for the env var
	stepsStr := strings.Join(steps, "")

	assert.Contains(t, stepsStr, "GH_AW_ASSIGN_COPILOT", "Expected GH_AW_ASSIGN_COPILOT to be set when copilot is in assignees")
	assert.Contains(t, stepsStr, `GH_AW_ASSIGN_COPILOT: "true"`, "Expected GH_AW_ASSIGN_COPILOT to be set to 'true'")
}

// TestCopilotAssignmentEnvVarNotSetWithoutCopilot verifies that GH_AW_ASSIGN_COPILOT
// is not set when copilot is not in the assignees list
func TestCopilotAssignmentEnvVarNotSetWithoutCopilot(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				Assignees:            []string{"user1"},
			},
		},
	}

	var steps []string
	compiler.addAllSafeOutputConfigEnvVars(&steps, data)

	// Join steps to search for the env var
	stepsStr := strings.Join(steps, "")

	assert.NotContains(t, stepsStr, "GH_AW_ASSIGN_COPILOT", "Expected GH_AW_ASSIGN_COPILOT not to be set when copilot is not in assignees")
}

// TestCopilotAssignmentEnvVarWithMixedAssignees verifies that GH_AW_ASSIGN_COPILOT is set
// when copilot is in the assignees list along with other users
func TestCopilotAssignmentEnvVarWithMixedAssignees(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				Assignees:            []string{"user1", "copilot", "user2"},
			},
		},
	}

	var steps []string
	compiler.addAllSafeOutputConfigEnvVars(&steps, data)

	// Join steps to search for the env var
	stepsStr := strings.Join(steps, "")

	assert.Contains(t, stepsStr, "GH_AW_ASSIGN_COPILOT", "Expected GH_AW_ASSIGN_COPILOT to be set when copilot is among multiple assignees")
	assert.Contains(t, stepsStr, `GH_AW_ASSIGN_COPILOT: "true"`, "Expected GH_AW_ASSIGN_COPILOT to be set to 'true'")
}

// TestCopilotAssignmentEnvVarWithNilAssignees verifies that GH_AW_ASSIGN_COPILOT
// is not set when assignees field is nil
func TestCopilotAssignmentEnvVarWithNilAssignees(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				Assignees:            nil,
			},
		},
	}

	var steps []string
	compiler.addAllSafeOutputConfigEnvVars(&steps, data)

	// Join steps to search for the env var
	stepsStr := strings.Join(steps, "")

	assert.NotContains(t, stepsStr, "GH_AW_ASSIGN_COPILOT", "Expected GH_AW_ASSIGN_COPILOT not to be set when assignees is nil")
}

// TestCopilotAssignmentEnvVarWithEmptyAssignees verifies that GH_AW_ASSIGN_COPILOT
// is not set when assignees array is empty
func TestCopilotAssignmentEnvVarWithEmptyAssignees(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		Name: "Test",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				Assignees:            []string{},
			},
		},
	}

	var steps []string
	compiler.addAllSafeOutputConfigEnvVars(&steps, data)

	// Join steps to search for the env var
	stepsStr := strings.Join(steps, "")

	assert.NotContains(t, stepsStr, "GH_AW_ASSIGN_COPILOT", "Expected GH_AW_ASSIGN_COPILOT not to be set when assignees is empty")
}
