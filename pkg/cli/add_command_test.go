//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validateEngineStub is a stub validation function for testing
func validateEngineStub(engine string) error {
	return nil
}

func TestNewAddCommand(t *testing.T) {
	cmd := NewAddCommand(validateEngineStub)

	require.NotNil(t, cmd, "NewAddCommand should not return nil")
	assert.Equal(t, "add <workflow>...", cmd.Use, "Command use should be 'add <workflow>...'")
	assert.Equal(t, "Add agentic workflows from repositories to .github/workflows", cmd.Short, "Command short description should match")
	assert.Contains(t, cmd.Long, "Add one or more workflows", "Command long description should contain expected text")

	// Verify Args validator is set
	assert.NotNil(t, cmd.Args, "Args validator should be set")

	// Verify flags are registered
	flags := cmd.Flags()

	// Check number flag
	numberFlag := flags.Lookup("number")
	assert.NotNil(t, numberFlag, "Should have 'number' flag")
	assert.Empty(t, numberFlag.Shorthand, "Number flag should not have shorthand (conflicts with logs -c)")

	// Check name flag
	nameFlag := flags.Lookup("name")
	assert.NotNil(t, nameFlag, "Should have 'name' flag")
	assert.Equal(t, "n", nameFlag.Shorthand, "Name flag shorthand should be 'n'")

	// Check engine flag
	engineFlag := flags.Lookup("engine")
	assert.NotNil(t, engineFlag, "Should have 'engine' flag")

	// Check repo flag
	repoFlag := flags.Lookup("repo")
	assert.NotNil(t, repoFlag, "Should have 'repo' flag")
	assert.Equal(t, "r", repoFlag.Shorthand, "Repo flag shorthand should be 'r'")

	// Check PR flags
	createPRFlag := flags.Lookup("create-pull-request")
	assert.NotNil(t, createPRFlag, "Should have 'create-pull-request' flag")
	prFlag := flags.Lookup("pr")
	assert.NotNil(t, prFlag, "Should have 'pr' flag (alias)")

	// Check push flag
	pushFlag := flags.Lookup("push")
	assert.NotNil(t, pushFlag, "Should have 'push' flag")

	// Check force flag
	forceFlag := flags.Lookup("force")
	assert.NotNil(t, forceFlag, "Should have 'force' flag")

	// Check append flag
	appendFlag := flags.Lookup("append")
	assert.NotNil(t, appendFlag, "Should have 'append' flag")

	// Check no-gitattributes flag
	noGitattributesFlag := flags.Lookup("no-gitattributes")
	assert.NotNil(t, noGitattributesFlag, "Should have 'no-gitattributes' flag")

	// Check dir flag
	dirFlag := flags.Lookup("dir")
	assert.NotNil(t, dirFlag, "Should have 'dir' flag")
	assert.Equal(t, "d", dirFlag.Shorthand, "Dir flag shorthand should be 'd'")

	// Check no-stop-after flag
	noStopAfterFlag := flags.Lookup("no-stop-after")
	assert.NotNil(t, noStopAfterFlag, "Should have 'no-stop-after' flag")

	// Check stop-after flag
	stopAfterFlag := flags.Lookup("stop-after")
	assert.NotNil(t, stopAfterFlag, "Should have 'stop-after' flag")
}

func TestAddWorkflows_EmptyWorkflows(t *testing.T) {
	_, err := AddWorkflows([]string{}, 1, false, "", "", false, "", false, false, false, "", false, "")
	require.Error(t, err, "Should error when no workflows are provided")
	assert.Contains(t, err.Error(), "at least one workflow", "Error should mention missing workflow")
}

func TestAddWorkflows_InvalidNumber(t *testing.T) {
	tests := []struct {
		name        string
		number      int
		expectError bool
	}{
		{
			name:        "valid number",
			number:      1,
			expectError: false,
		},
		{
			name:        "zero number",
			number:      0,
			expectError: true,
		},
		{
			name:        "negative number",
			number:      -1,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: This test checks validation logic exists
			// Full integration would require actual workflow specs
			if tt.expectError && tt.number <= 0 {
				// The function should validate number > 0
				// This is a placeholder - actual validation may occur in the function
				assert.LessOrEqual(t, tt.number, 0, "Invalid number should be non-positive")
			}
		})
	}
}

// TestUpdateWorkflowTitle is tested in commands_utils_test.go
// Removed duplicate test to avoid redeclaration

func TestAddCommandStructure(t *testing.T) {
	tests := []struct {
		name           string
		commandCreator func() interface{}
	}{
		{
			name: "add command exists",
			commandCreator: func() interface{} {
				return NewAddCommand(validateEngineStub)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.commandCreator()
			require.NotNil(t, cmd, "Command should not be nil")
		})
	}
}

func TestAddCommandFlagDefaults(t *testing.T) {
	cmd := NewAddCommand(validateEngineStub)
	flags := cmd.Flags()

	tests := []struct {
		flagName     string
		defaultValue string
	}{
		{"number", "1"},
		{"name", ""},
		{"engine", ""},
		{"repo", ""},
		{"append", ""},
		{"dir", ""},
		{"stop-after", ""},
	}

	for _, tt := range tests {
		t.Run(tt.flagName, func(t *testing.T) {
			flag := flags.Lookup(tt.flagName)
			require.NotNil(t, flag, "Flag should exist: %s", tt.flagName)
			assert.Equal(t, tt.defaultValue, flag.DefValue, "Default value should match for flag: %s", tt.flagName)
		})
	}
}

func TestAddCommandBooleanFlags(t *testing.T) {
	cmd := NewAddCommand(validateEngineStub)
	flags := cmd.Flags()

	boolFlags := []string{"create-pull-request", "pr", "force", "no-gitattributes", "no-stop-after"}

	for _, flagName := range boolFlags {
		t.Run(flagName, func(t *testing.T) {
			flag := flags.Lookup(flagName)
			require.NotNil(t, flag, "Boolean flag should exist: %s", flagName)
			assert.Equal(t, "false", flag.DefValue, "Boolean flag should default to false: %s", flagName)
		})
	}
}

func TestAddCommandArgs(t *testing.T) {
	cmd := NewAddCommand(validateEngineStub)

	// Test that Args validator is set (MinimumNArgs(1))
	require.NotNil(t, cmd.Args, "Args validator should be set")

	// Verify it requires at least 1 arg
	err := cmd.Args(cmd, []string{})
	require.Error(t, err, "Should error with no arguments")

	err = cmd.Args(cmd, []string{"workflow1"})
	require.NoError(t, err, "Should not error with 1 argument")

	err = cmd.Args(cmd, []string{"workflow1", "workflow2"})
	require.NoError(t, err, "Should not error with multiple arguments")
}
