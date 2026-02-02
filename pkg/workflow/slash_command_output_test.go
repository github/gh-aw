//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSlashCommandOutputReferencesPreActivation ensures that the slash_command output
// in the activation job references needs.pre_activation.outputs.matched_command
// instead of steps.check_command_position.outputs.matched_command
func TestSlashCommandOutputReferencesPreActivation(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := t.TempDir()

	// Create a test workflow with slash_command trigger
	workflowContent := `---
name: Test Slash Command
on:
  slash_command:
    name: test
permissions:
  contents: read
engine: copilot
---

Test workflow content
`

	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	require.NoError(t, err)

	// Compile the workflow
	compiler := NewCompiler()
	err = compiler.CompileWorkflow(workflowPath)
	require.NoError(t, err, "Failed to compile workflow")

	// Get the lock file path
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)

	// Read the compiled workflow
	lockContent, err := os.ReadFile(lockFilePath)
	require.NoError(t, err)

	// Parse the YAML
	var workflow map[string]any
	err = yaml.Unmarshal(lockContent, &workflow)
	require.NoError(t, err)

	// Get the jobs
	jobs, ok := workflow["jobs"].(map[string]any)
	require.True(t, ok, "Expected jobs to be a map")

	// Check pre_activation job exists and has matched_command output
	preActivation, ok := jobs["pre_activation"].(map[string]any)
	require.True(t, ok, "Expected pre_activation job to exist")

	preActivationOutputs, ok := preActivation["outputs"].(map[string]any)
	require.True(t, ok, "Expected pre_activation job to have outputs")

	matchedCommand, ok := preActivationOutputs["matched_command"]
	require.True(t, ok, "Expected pre_activation job to have matched_command output")
	require.Contains(t, matchedCommand, "steps.check_command_position.outputs.matched_command",
		"Expected matched_command to reference check_command_position step output")

	// Check activation job exists and has slash_command output
	activation, ok := jobs["activation"].(map[string]any)
	require.True(t, ok, "Expected activation job to exist")

	activationOutputs, ok := activation["outputs"].(map[string]any)
	require.True(t, ok, "Expected activation job to have outputs")

	slashCommand, ok := activationOutputs["slash_command"]
	require.True(t, ok, "Expected activation job to have slash_command output")

	// Verify it references needs.pre_activation.outputs.matched_command
	assert.Contains(t, slashCommand, "needs.pre_activation.outputs.matched_command",
		"Expected slash_command to reference needs.pre_activation.outputs.matched_command")

	// Verify it does NOT reference steps.check_command_position
	assert.NotContains(t, slashCommand, "steps.check_command_position",
		"Expected slash_command to NOT reference steps.check_command_position directly")
}
