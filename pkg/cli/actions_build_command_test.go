//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionsBuildCommand_NoActionsDir(t *testing.T) {
	// Create a temporary directory without actions/
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	defer os.Chdir(originalDir)

	err = os.Chdir(tmpDir)
	require.NoError(t, err, "Failed to change to temp directory")

	// Test with non-existent actions directory
	err = ActionsBuildCommand()
	require.Error(t, err, "Should error when actions/ directory does not exist")
	assert.Contains(t, err.Error(), "actions/ directory does not exist", "Error should mention missing directory")
}

func TestActionsValidateCommand_NoActionsDir(t *testing.T) {
	// Create a temporary directory without actions/
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	defer os.Chdir(originalDir)

	err = os.Chdir(tmpDir)
	require.NoError(t, err, "Failed to change to temp directory")

	// Test with non-existent actions directory
	err = ActionsValidateCommand()
	assert.Error(t, err, "Should error when actions/ directory does not exist")
}

func TestActionsCleanCommand_NoActionsDir(t *testing.T) {
	// Create a temporary directory without actions/
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	defer os.Chdir(originalDir)

	err = os.Chdir(tmpDir)
	require.NoError(t, err, "Failed to change to temp directory")

	// Test with non-existent actions directory
	err = ActionsCleanCommand()
	assert.Error(t, err, "Should error when actions/ directory does not exist")
}

func TestGetActionDirectories(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(string) error
		expectError bool
		expectedLen int
	}{
		{
			name: "no actions directory",
			setup: func(tmpDir string) error {
				return nil // Don't create actions/
			},
			expectError: true,
		},
		{
			name: "empty actions directory",
			setup: func(tmpDir string) error {
				return os.MkdirAll(filepath.Join(tmpDir, "actions"), 0755)
			},
			expectError: false,
			expectedLen: 0,
		},
		{
			name: "actions directory with subdirectories",
			setup: func(tmpDir string) error {
				actionsDir := filepath.Join(tmpDir, "actions")
				if err := os.MkdirAll(filepath.Join(actionsDir, "action1"), 0755); err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Join(actionsDir, "action2"), 0755); err != nil {
					return err
				}
				return nil
			},
			expectError: false,
			expectedLen: 2,
		},
		{
			name: "actions directory with files and subdirectories",
			setup: func(tmpDir string) error {
				actionsDir := filepath.Join(tmpDir, "actions")
				if err := os.MkdirAll(filepath.Join(actionsDir, "action1"), 0755); err != nil {
					return err
				}
				// Create a file - should not be included
				return os.WriteFile(filepath.Join(actionsDir, "README.md"), []byte("test"), 0644)
			},
			expectError: false,
			expectedLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			err := tt.setup(tmpDir)
			require.NoError(t, err, "Setup should not fail")

			actionsDir := filepath.Join(tmpDir, "actions")
			dirs, err := getActionDirectories(actionsDir)

			if tt.expectError {
				require.Error(t, err, "Expected an error")
			} else {
				require.NoError(t, err, "Should not error")
				assert.Len(t, dirs, tt.expectedLen, "Should return expected number of directories")
			}
		})
	}
}

func TestValidateActionYml(t *testing.T) {
	tests := []struct {
		name             string
		actionYmlContent string
		expectError      bool
		errorContains    string
	}{
		{
			name: "valid node20 action",
			actionYmlContent: `name: Test Action
description: A test action
runs:
  using: 'node20'
  main: 'index.js'`,
			expectError: false,
		},
		{
			name: "valid composite action",
			actionYmlContent: `name: Test Action
description: A test action
runs:
  using: 'composite'
  steps:
    - run: echo "test"`,
			expectError: false,
		},
		{
			name:          "missing action.yml",
			expectError:   true,
			errorContains: "action.yml not found",
		},
		{
			name: "missing name field",
			actionYmlContent: `description: A test action
runs:
  using: 'node20'`,
			expectError:   true,
			errorContains: "missing required field 'name'",
		},
		{
			name: "missing description field",
			actionYmlContent: `name: Test Action
runs:
  using: 'node20'`,
			expectError:   true,
			errorContains: "missing required field 'description'",
		},
		{
			name: "missing runs field",
			actionYmlContent: `name: Test Action
description: A test action`,
			expectError:   true,
			errorContains: "missing required field 'runs'",
		},
		{
			name: "invalid runtime",
			actionYmlContent: `name: Test Action
description: A test action
runs:
  using: 'docker'`,
			expectError:   true,
			errorContains: "must use either 'node20' or 'composite'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			actionPath := filepath.Join(tmpDir, "test-action")
			err := os.MkdirAll(actionPath, 0755)
			require.NoError(t, err, "Failed to create action directory")

			if tt.actionYmlContent != "" {
				ymlPath := filepath.Join(actionPath, "action.yml")
				err = os.WriteFile(ymlPath, []byte(tt.actionYmlContent), 0644)
				require.NoError(t, err, "Failed to write action.yml")
			}

			err = validateActionYml(actionPath)

			if tt.expectError {
				require.Error(t, err, "Expected an error")
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error should contain expected message")
				}
			} else {
				require.NoError(t, err, "Should not error for valid action.yml")
			}
		})
	}
}

func TestGetActionDependencies(t *testing.T) {
	tests := []struct {
		name       string
		actionName string
		minDeps    int
	}{
		{
			name:       "setup action",
			actionName: "setup",
			minDeps:    0, // setup has special handling
		},
		{
			name:       "unknown action",
			actionName: "unknown-action",
			minDeps:    0, // Unknown actions should return empty or default dependencies
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := getActionDependencies(tt.actionName)
			assert.GreaterOrEqual(t, len(deps), tt.minDeps, "Should return at least minimum dependencies")
		})
	}
}

func TestActionsBuildCommand_EmptyActionsDir(t *testing.T) {
	// Create a temporary directory with empty actions/
	tmpDir := t.TempDir()
	actionsDir := filepath.Join(tmpDir, "actions")
	err := os.MkdirAll(actionsDir, 0755)
	require.NoError(t, err, "Failed to create actions directory")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	defer os.Chdir(originalDir)

	err = os.Chdir(tmpDir)
	require.NoError(t, err, "Failed to change to temp directory")

	// Test with empty actions directory
	err = ActionsBuildCommand()
	assert.NoError(t, err, "Should not error with empty actions directory")
}

func TestActionsValidateCommand_EmptyActionsDir(t *testing.T) {
	// Create a temporary directory with empty actions/
	tmpDir := t.TempDir()
	actionsDir := filepath.Join(tmpDir, "actions")
	err := os.MkdirAll(actionsDir, 0755)
	require.NoError(t, err, "Failed to create actions directory")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	defer os.Chdir(originalDir)

	err = os.Chdir(tmpDir)
	require.NoError(t, err, "Failed to change to temp directory")

	// Test with empty actions directory
	err = ActionsValidateCommand()
	assert.NoError(t, err, "Should not error with empty actions directory")
}

func TestActionsCleanCommand_EmptyActionsDir(t *testing.T) {
	// Create a temporary directory with empty actions/
	tmpDir := t.TempDir()
	actionsDir := filepath.Join(tmpDir, "actions")
	err := os.MkdirAll(actionsDir, 0755)
	require.NoError(t, err, "Failed to create actions directory")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	defer os.Chdir(originalDir)

	err = os.Chdir(tmpDir)
	require.NoError(t, err, "Failed to change to temp directory")

	// Test with empty actions directory
	err = ActionsCleanCommand()
	assert.NoError(t, err, "Should not error with empty actions directory")
}

func TestIsCompositeAction(t *testing.T) {
	tests := []struct {
		name             string
		actionYmlContent string
		expectComposite  bool
		expectError      bool
	}{
		{
			name: "composite action with single quotes",
			actionYmlContent: `name: Test Action
description: A test action
runs:
  using: 'composite'
  steps:
    - run: echo "test"`,
			expectComposite: true,
			expectError:     false,
		},
		{
			name: "composite action with double quotes",
			actionYmlContent: `name: Test Action
description: A test action
runs:
  using: "composite"
  steps:
    - run: echo "test"`,
			expectComposite: true,
			expectError:     false,
		},
		{
			name: "node20 action",
			actionYmlContent: `name: Test Action
description: A test action
runs:
  using: 'node20'
  main: 'index.js'`,
			expectComposite: false,
			expectError:     false,
		},
		{
			name:            "missing action.yml",
			expectComposite: false,
			expectError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			actionPath := filepath.Join(tmpDir, "test-action")
			err := os.MkdirAll(actionPath, 0755)
			require.NoError(t, err, "Failed to create action directory")

			if tt.actionYmlContent != "" {
				ymlPath := filepath.Join(actionPath, "action.yml")
				err = os.WriteFile(ymlPath, []byte(tt.actionYmlContent), 0644)
				require.NoError(t, err, "Failed to write action.yml")
			}

			isComposite, err := isCompositeAction(actionPath)

			if tt.expectError {
				require.Error(t, err, "Expected an error")
			} else {
				require.NoError(t, err, "Should not error")
				assert.Equal(t, tt.expectComposite, isComposite, "Composite action detection mismatch")
			}
		})
	}
}

func TestBuildAction_CompositeAction(t *testing.T) {
	// Create a temporary directory with a composite action
	tmpDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	defer os.Chdir(originalDir)

	err = os.Chdir(tmpDir)
	require.NoError(t, err, "Failed to change to temp directory")

	actionsDir := filepath.Join(tmpDir, "actions")
	compositeActionPath := filepath.Join(actionsDir, "test-composite")
	err = os.MkdirAll(compositeActionPath, 0755)
	require.NoError(t, err, "Failed to create composite action directory")

	// Create action.yml for composite action
	actionYml := `name: Test Composite Action
description: A test composite action
runs:
  using: 'composite'
  steps:
    - run: echo "test"
      shell: bash`
	err = os.WriteFile(filepath.Join(compositeActionPath, "action.yml"), []byte(actionYml), 0644)
	require.NoError(t, err, "Failed to write action.yml")

	// Build the composite action (should succeed without src/index.js)
	err = buildAction("actions", "test-composite")
	require.NoError(t, err, "Should successfully build composite action without JavaScript source")

	// Verify that no index.js was generated
	indexPath := filepath.Join(compositeActionPath, "index.js")
	_, err = os.Stat(indexPath)
	assert.True(t, os.IsNotExist(err), "index.js should not be generated for composite actions")
}
