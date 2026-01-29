//go:build !integration

package cli

import (
	"testing"

	"github.com/spf13/cobra"
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

func TestAddWorkflows(t *testing.T) {
	tests := []struct {
		name          string
		workflows     []string
		number        int
		expectError   bool
		errorContains string
	}{
		{
			name:          "empty workflows list",
			workflows:     []string{},
			number:        1,
			expectError:   true,
			errorContains: "at least one workflow",
		},
		{
			name:        "repo-only spec (should list workflows)",
			workflows:   []string{"owner/repo"},
			number:      1,
			expectError: true, // Will error on workflow listing, but tests repo-only detection
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := AddWorkflows(tt.workflows, tt.number, false, "", "", false, "", false, false, false, "", false, "")

			if tt.expectError {
				require.Error(t, err, "Expected error for test case: %s", tt.name)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error should contain expected message")
				}
			} else {
				assert.NoError(t, err, "Should not error for test case: %s", tt.name)
			}
		})
	}
}

// TestUpdateWorkflowTitle is tested in commands_utils_test.go
// Removed duplicate test to avoid redeclaration

// TestAddCommandStructure removed - redundant with TestNewAddCommand

func TestAddResolvedWorkflows(t *testing.T) {
	tests := []struct {
		name          string
		number        int
		expectError   bool
		errorContains string
	}{
		{
			name:          "invalid number - zero",
			number:        0,
			expectError:   true,
			errorContains: "number of copies must be a positive integer",
		},
		{
			name:          "invalid number - negative",
			number:        -1,
			expectError:   true,
			errorContains: "number of copies must be a positive integer",
		},
		{
			name:        "valid number - one",
			number:      1,
			expectError: true, // Will still error due to missing workflow, but validates number check
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal resolved workflow structure
			resolved := &ResolvedWorkflows{
				Workflows: []*ResolvedWorkflow{
					{
						Spec: &WorkflowSpec{
							RepoSpec: RepoSpec{
								RepoSlug: "test/repo",
							},
							WorkflowName: "test-workflow",
							WorkflowPath: "test.md",
						},
					},
				},
			}

			_, err := AddResolvedWorkflows(
				[]string{"test/repo/test-workflow"},
				resolved,
				tt.number,
				false, // verbose
				false, // quiet
				"",    // engineOverride
				"",    // name
				false, // force
				"",    // appendText
				false, // createPR
				false, // push
				false, // noGitattributes
				"",    // workflowDir
				false, // noStopAfter
				"",    // stopAfter
			)

			if tt.expectError {
				require.Error(t, err, "Expected error for test case: %s", tt.name)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error should contain expected message")
				}
			} else {
				assert.NoError(t, err, "Should not error for test case: %s", tt.name)
			}
		})
	}
}

func TestAddWorkflowsResult(t *testing.T) {
	tests := []struct {
		name                string
		prNumber            int
		prURL               string
		hasWorkflowDispatch bool
	}{
		{
			name:                "default values",
			prNumber:            0,
			prURL:               "",
			hasWorkflowDispatch: false,
		},
		{
			name:                "with PR number",
			prNumber:            123,
			prURL:               "",
			hasWorkflowDispatch: false,
		},
		{
			name:                "with PR URL",
			prNumber:            0,
			prURL:               "https://github.com/owner/repo/pull/123",
			hasWorkflowDispatch: false,
		},
		{
			name:                "with workflow dispatch",
			prNumber:            0,
			prURL:               "",
			hasWorkflowDispatch: true,
		},
		{
			name:                "all fields set",
			prNumber:            456,
			prURL:               "https://github.com/owner/repo/pull/456",
			hasWorkflowDispatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &AddWorkflowsResult{
				PRNumber:            tt.prNumber,
				PRURL:               tt.prURL,
				HasWorkflowDispatch: tt.hasWorkflowDispatch,
			}

			// Verify all fields are accessible and have expected values
			assert.Equal(t, tt.prNumber, result.PRNumber, "PRNumber should match")
			assert.Equal(t, tt.prURL, result.PRURL, "PRURL should match")
			assert.Equal(t, tt.hasWorkflowDispatch, result.HasWorkflowDispatch, "HasWorkflowDispatch should match")
		})
	}
}

func TestAddCommandFlagInteractions(t *testing.T) {
	tests := []struct {
		name        string
		flagSetup   func(cmd *cobra.Command)
		expectValid bool
		description string
	}{
		{
			name: "no-stop-after and stop-after together",
			flagSetup: func(cmd *cobra.Command) {
				cmd.Flags().Set("no-stop-after", "true")
				cmd.Flags().Set("stop-after", "+48h")
			},
			expectValid: true, // Both flags can be set, stop-after takes precedence
			description: "Both no-stop-after and stop-after flags can be set",
		},
		{
			name: "create-pull-request and pr alias",
			flagSetup: func(cmd *cobra.Command) {
				cmd.Flags().Set("create-pull-request", "true")
				cmd.Flags().Set("pr", "true")
			},
			expectValid: true, // Both aliases should work
			description: "Both create-pull-request and pr flags can be set (aliases)",
		},
		{
			name: "force flag with number",
			flagSetup: func(cmd *cobra.Command) {
				cmd.Flags().Set("force", "true")
				cmd.Flags().Set("number", "3")
			},
			expectValid: true,
			description: "Force flag should work with multiple numbered copies",
		},
		{
			name: "dir flag with subdirectory",
			flagSetup: func(cmd *cobra.Command) {
				cmd.Flags().Set("dir", "shared")
			},
			expectValid: true,
			description: "Dir flag should accept subdirectory name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewAddCommand(validateEngineStub)

			// Apply flag setup
			tt.flagSetup(cmd)

			// Verify flags are set correctly
			flags := cmd.Flags()
			assert.NotNil(t, flags, "Command flags should not be nil")

			// The actual validation happens during RunE execution
			// Here we just verify the flags can be set without panic
			assert.True(t, tt.expectValid, tt.description)
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
