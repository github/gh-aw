//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSafeOutputsImport tests that safe-output types can be imported from shared workflows
func TestSafeOutputsImport(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with create-issue configuration
	sharedWorkflow := `---
safe-outputs:
  create-issue:
    title-prefix: "[shared] "
    labels:
      - imported
      - automation
---

# Shared Create Issue Configuration

This shared workflow provides create-issue configuration.
`

	sharedFile := filepath.Join(workflowsDir, "shared-create-issue.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that imports the create-issue configuration
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-create-issue.md
---

# Main Workflow

This workflow uses the imported create-issue configuration.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.CreateIssues, "CreateIssues configuration should be imported")

	// Verify create-issue configuration was imported correctly
	assert.Equal(t, "[shared] ", workflowData.SafeOutputs.CreateIssues.TitlePrefix)
	assert.Equal(t, []string{"imported", "automation"}, workflowData.SafeOutputs.CreateIssues.Labels)
}

// TestSafeOutputsImportMultipleTypes tests importing multiple safe-output types from a shared workflow
func TestSafeOutputsImportMultipleTypes(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with multiple safe-output types
	sharedWorkflow := `---
safe-outputs:
  create-issue:
    title-prefix: "[bug] "
    labels:
      - bug
  add-comment:
    max: 3
---

# Shared Safe Outputs

This shared workflow provides multiple safe-output types.
`

	sharedFile := filepath.Join(workflowsDir, "shared-outputs.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that imports the safe-outputs
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-outputs.md
---

# Main Workflow
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify both types were imported
	require.NotNil(t, workflowData.SafeOutputs.CreateIssues, "CreateIssues should be imported")
	assert.Equal(t, "[bug] ", workflowData.SafeOutputs.CreateIssues.TitlePrefix)
	assert.Equal(t, []string{"bug"}, workflowData.SafeOutputs.CreateIssues.Labels)

	require.NotNil(t, workflowData.SafeOutputs.AddComments, "AddComments should be imported")
	assert.Equal(t, strPtr("3"), workflowData.SafeOutputs.AddComments.Max)
}

// TestSafeOutputsImportOverride tests that when the same safe-output type is defined in both main and imported workflow, the main workflow's definition takes precedence
func TestSafeOutputsImportOverride(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with create-issue configuration
	sharedWorkflow := `---
safe-outputs:
  create-issue:
    title-prefix: "[shared] "
---

# Shared Create Issue Configuration
`

	sharedFile := filepath.Join(workflowsDir, "shared-create-issue.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that also defines create-issue (main overrides imported)
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-create-issue.md
safe-outputs:
  create-issue:
    title-prefix: "[main] "
---

# Main Workflow with Override
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow - should succeed with main's definition taking precedence
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Should not return error - main workflow overrides imported")

	// Verify the main workflow's configuration took precedence
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should be present")
	require.NotNil(t, workflowData.SafeOutputs.CreateIssues, "CreateIssues should be present")
	assert.Equal(t, "[main] ", workflowData.SafeOutputs.CreateIssues.TitlePrefix, "Main workflow's title-prefix should override imported")
}

// TestSafeOutputsImportConflictBetweenImports tests that a conflict error is returned when the same safe-output type is defined in multiple imported workflows
func TestSafeOutputsImportConflictBetweenImports(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create first shared workflow with create-issue
	sharedWorkflow1 := `---
safe-outputs:
  create-issue:
    title-prefix: "[shared1] "
---

# Shared Create Issue 1
`

	sharedFile1 := filepath.Join(workflowsDir, "shared-create-issue1.md")
	err = os.WriteFile(sharedFile1, []byte(sharedWorkflow1), 0644)
	require.NoError(t, err, "Failed to write shared file 1")

	// Create second shared workflow with create-issue (conflict)
	sharedWorkflow2 := `---
safe-outputs:
  create-issue:
    title-prefix: "[shared2] "
---

# Shared Create Issue 2
`

	sharedFile2 := filepath.Join(workflowsDir, "shared-create-issue2.md")
	err = os.WriteFile(sharedFile2, []byte(sharedWorkflow2), 0644)
	require.NoError(t, err, "Failed to write shared file 2")

	// Create main workflow that imports both (conflict between imports)
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-create-issue1.md
  - ./shared-create-issue2.md
---

# Main Workflow with Import Conflict
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow - should fail with conflict error
	_, err = compiler.ParseWorkflowFile("main.md")
	require.Error(t, err, "Expected conflict error")
	require.ErrorContains(t, err, "safe-outputs conflict")
	require.ErrorContains(t, err, "create-issue")
}

// TestSafeOutputsImportNoConflictDifferentTypes tests that importing different safe-output types does not cause a conflict
func TestSafeOutputsImportNoConflictDifferentTypes(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with create-discussion configuration
	sharedWorkflow := `---
safe-outputs:
  create-discussion:
    title-prefix: "[shared] "
    category: "general"
---

# Shared Create Discussion Configuration
`

	sharedFile := filepath.Join(workflowsDir, "shared-create-discussion.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow with create-issue (different type, no conflict)
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-create-discussion.md
safe-outputs:
  create-issue:
    title-prefix: "[main] "
---

# Main Workflow with Different Types
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow - should succeed
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify both types are present
	require.NotNil(t, workflowData.SafeOutputs.CreateIssues, "CreateIssues should be present from main")
	assert.Equal(t, "[main] ", workflowData.SafeOutputs.CreateIssues.TitlePrefix)

	require.NotNil(t, workflowData.SafeOutputs.CreateDiscussions, "CreateDiscussions should be imported")
	assert.Equal(t, "[shared] ", workflowData.SafeOutputs.CreateDiscussions.TitlePrefix)
	assert.Equal(t, "general", workflowData.SafeOutputs.CreateDiscussions.Category)
}

// TestSafeOutputsImportFromMultipleWorkflows tests importing different safe-output types from multiple workflows
func TestSafeOutputsImportFromMultipleWorkflows(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create first shared workflow with create-issue
	sharedWorkflow1 := `---
safe-outputs:
  create-issue:
    title-prefix: "[issue] "
---

# Shared Create Issue
`

	sharedFile1 := filepath.Join(workflowsDir, "shared-issue.md")
	err = os.WriteFile(sharedFile1, []byte(sharedWorkflow1), 0644)
	require.NoError(t, err, "Failed to write shared file 1")

	// Create second shared workflow with add-comment
	sharedWorkflow2 := `---
safe-outputs:
  add-comment:
    max: 5
---

# Shared Add Comment
`

	sharedFile2 := filepath.Join(workflowsDir, "shared-comment.md")
	err = os.WriteFile(sharedFile2, []byte(sharedWorkflow2), 0644)
	require.NoError(t, err, "Failed to write shared file 2")

	// Create main workflow that imports both
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-issue.md
  - ./shared-comment.md
---

# Main Workflow
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify both types are present
	require.NotNil(t, workflowData.SafeOutputs.CreateIssues, "CreateIssues should be imported from first shared workflow")
	assert.Equal(t, "[issue] ", workflowData.SafeOutputs.CreateIssues.TitlePrefix)

	require.NotNil(t, workflowData.SafeOutputs.AddComments, "AddComments should be imported from second shared workflow")
	assert.Equal(t, strPtr("5"), workflowData.SafeOutputs.AddComments.Max)
}

// TestSafeOutputsImportMergePullRequestType tests importing merge-pull-request from a shared workflow.
func TestSafeOutputsImportMergePullRequestType(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	sharedWorkflow := `---
safe-outputs:
  merge-pull-request:
    required-labels:
      - ready-to-merge
---

# Shared Merge Pull Request Configuration
`

	sharedFile := filepath.Join(workflowsDir, "shared-merge-pr.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	mainWorkflow := `---
on: pull_request
permissions:
  contents: read
imports:
  - ./shared-merge-pr.md
---

# Main Workflow
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.MergePullRequest, "MergePullRequest should be imported")
	assert.Equal(t, []string{"ready-to-merge"}, workflowData.SafeOutputs.MergePullRequest.RequiredLabels)
}

// TestMergeSafeOutputsUnit tests the MergeSafeOutputs function directly
func TestMergeSafeOutputsUnit(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tests := []struct {
		name          string
		topConfig     *SafeOutputsConfig
		importedJSON  []string
		expectError   bool
		errorContains string
		expectedTypes []string // Types that should be present after merge
	}{
		{
			name:          "empty imports",
			topConfig:     nil,
			importedJSON:  []string{},
			expectError:   false,
			expectedTypes: []string{},
		},
		{
			name:      "import create-issue to empty config",
			topConfig: nil,
			importedJSON: []string{
				`{"create-issue":{"title-prefix":"[test] "}}`,
			},
			expectError:   false,
			expectedTypes: []string{"create-issue"},
		},
		{
			name: "override: main workflow overrides imported create-issue",
			topConfig: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{TitlePrefix: "[top] "},
			},
			importedJSON: []string{
				`{"create-issue":{"title-prefix":"[imported] "}}`,
			},
			expectError:   false,
			expectedTypes: []string{"create-issue"},
		},
		{
			name:      "conflict: same type in multiple imports",
			topConfig: nil,
			importedJSON: []string{
				`{"create-issue":{"title-prefix":"[import1] "}}`,
				`{"create-issue":{"title-prefix":"[import2] "}}`,
			},
			expectError:   true,
			errorContains: "safe-outputs conflict",
		},
		{
			name: "no conflict: different types",
			topConfig: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{TitlePrefix: "[top] "},
			},
			importedJSON: []string{
				`{"add-comment":{"max":3}}`,
			},
			expectError:   false,
			expectedTypes: []string{"create-issue", "add-comment"},
		},
		{
			name:      "import multiple types from single config",
			topConfig: nil,
			importedJSON: []string{
				`{"create-issue":{"title-prefix":"[test] "},"add-comment":{"max":5}}`,
			},
			expectError:   false,
			expectedTypes: []string{"create-issue", "add-comment"},
		},
		{
			name:      "import merge-pull-request to empty config",
			topConfig: nil,
			importedJSON: []string{
				`{"merge-pull-request":{"required-labels":["ready-to-merge"]}}`,
			},
			expectError:   false,
			expectedTypes: []string{"merge-pull-request"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compiler.MergeSafeOutputs(tt.topConfig, tt.importedJSON, nil)

			if tt.expectError {
				require.Error(t, err)
				require.ErrorContains(t, err, tt.errorContains)
				return
			}

			require.NoError(t, err)

			// Verify expected types are present
			for _, expectedType := range tt.expectedTypes {
				assert.True(t, hasSafeOutputType(result, expectedType), "Expected %s to be present", expectedType)
			}
		})
	}
}

func TestMergeSafeOutputsDescriptorMergedFieldsUnit(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	t.Run("imports unassign-from-user", func(t *testing.T) {
		result, err := compiler.MergeSafeOutputs(nil, []string{
			`{"unassign-from-user":{"max":1}}`,
		}, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.UnassignFromUser)
		assert.Equal(t, strPtr("1"), result.UnassignFromUser.Max)
	})

	t.Run("imports dispatch_repository", func(t *testing.T) {
		result, err := compiler.MergeSafeOutputs(nil, []string{
			`{"dispatch_repository":{"trigger_ci":{"workflow":"ci.yml","event_type":"ci_trigger","repository":"org/target-repo","max":1}}}`,
		}, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.DispatchRepository)
		require.Len(t, result.DispatchRepository.Tools, 1)
		tool := result.DispatchRepository.Tools["trigger_ci"]
		require.NotNil(t, tool)
		assert.Equal(t, "ci.yml", tool.Workflow)
		assert.Equal(t, "ci_trigger", tool.EventType)
		assert.Equal(t, "org/target-repo", tool.Repository)
		assert.Equal(t, strPtr("1"), tool.Max)
	})
}

func TestMergeSafeOutputsSteer(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	t.Run("imports steer", func(t *testing.T) {
		result, err := compiler.MergeSafeOutputs(nil, []string{`{"steer":true}`}, nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.Steer)
	})

	t.Run("explicit top-level false overrides import", func(t *testing.T) {
		result, err := compiler.MergeSafeOutputs(
			&SafeOutputsConfig{},
			[]string{`{"steer":true}`},
			map[string]any{"steer": false},
		)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.Steer)
	})
}

// TestMergeSafeOutputsMessagesUnit tests the MergeSafeOutputs function for messages field
func TestMergeSafeOutputsMessagesUnit(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tests := []struct {
		name             string
		topConfig        *SafeOutputsConfig
		importedJSON     []string
		expectError      bool
		expectedMessages *SafeOutputMessagesConfig
	}{
		{
			name:      "import messages to empty config",
			topConfig: nil,
			importedJSON: []string{
				`{"messages":{"footer":"> Imported footer","run-success":"Imported success"}}`,
			},
			expectError: false,
			expectedMessages: &SafeOutputMessagesConfig{
				Footer:     "> Imported footer",
				RunSuccess: "Imported success",
			},
		},
		{
			name: "import messages to config with nil messages",
			topConfig: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{TitlePrefix: "[test] "},
			},
			importedJSON: []string{
				`{"messages":{"footer":"> Imported footer"}}`,
			},
			expectError: false,
			expectedMessages: &SafeOutputMessagesConfig{
				Footer: "> Imported footer",
			},
		},
		{
			name: "main messages take precedence over imported",
			topConfig: &SafeOutputsConfig{
				Messages: &SafeOutputMessagesConfig{
					Footer: "> Main footer",
				},
			},
			importedJSON: []string{
				`{"messages":{"footer":"> Imported footer","run-success":"Imported success"}}`,
			},
			expectError: false,
			expectedMessages: &SafeOutputMessagesConfig{
				Footer:     "> Main footer",
				RunSuccess: "Imported success",
			},
		},
		{
			name: "field-level merge: main overrides specific fields",
			topConfig: &SafeOutputsConfig{
				Messages: &SafeOutputMessagesConfig{
					Footer:     "> Main footer",
					RunSuccess: "Main success",
				},
			},
			importedJSON: []string{
				`{"messages":{"footer":"> Imported footer","footer-install":"> Imported install","run-success":"Imported success","run-failure":"Imported failure"}}`,
			},
			expectError: false,
			expectedMessages: &SafeOutputMessagesConfig{
				Footer:        "> Main footer",
				FooterInstall: "> Imported install",
				RunSuccess:    "Main success",
				RunFailure:    "Imported failure",
			},
		},
		{
			name: "merge from multiple imports",
			topConfig: &SafeOutputsConfig{
				Messages: &SafeOutputMessagesConfig{
					Footer: "> Main footer",
				},
			},
			importedJSON: []string{
				`{"messages":{"footer-install":"> Import1 install"}}`,
				`{"messages":{"run-success":"Import2 success"}}`,
			},
			expectError: false,
			expectedMessages: &SafeOutputMessagesConfig{
				Footer:        "> Main footer",
				FooterInstall: "> Import1 install",
				RunSuccess:    "Import2 success",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compiler.MergeSafeOutputs(tt.topConfig, tt.importedJSON, nil)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tt.expectedMessages != nil {
				require.NotNil(t, result.Messages, "Messages should not be nil")
				assert.Equal(t, tt.expectedMessages.Footer, result.Messages.Footer, "Footer mismatch")
				assert.Equal(t, tt.expectedMessages.FooterInstall, result.Messages.FooterInstall, "FooterInstall mismatch")
				assert.Equal(t, tt.expectedMessages.StagedTitle, result.Messages.StagedTitle, "StagedTitle mismatch")
				assert.Equal(t, tt.expectedMessages.StagedDescription, result.Messages.StagedDescription, "StagedDescription mismatch")
				assert.Equal(t, tt.expectedMessages.RunStarted, result.Messages.RunStarted, "RunStarted mismatch")
				assert.Equal(t, tt.expectedMessages.RunSuccess, result.Messages.RunSuccess, "RunSuccess mismatch")
				assert.Equal(t, tt.expectedMessages.RunFailure, result.Messages.RunFailure, "RunFailure mismatch")
			}
		})
	}
}

// TestSafeOutputsImportMetaFields tests that safe-output meta fields can be imported from shared workflows
func TestSafeOutputsImportMetaFields(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with meta fields
	sharedWorkflow := `---
safe-outputs:
  allowed-domains:
    - "example.com"
    - "api.example.com"
  report-failure-as-issue: ${{ inputs.report-failure-as-issue }}
  staged: true
  env:
    TEST_VAR: "test_value"
  github-token: "${{ secrets.CUSTOM_TOKEN }}"
  max-patch-size: 2048
  runs-on: "ubuntu-latest"
---

# Shared Meta Fields Configuration

This shared workflow provides meta configuration fields.
`

	sharedFile := filepath.Join(workflowsDir, "shared-meta.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that imports the meta configuration
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-meta.md
safe-outputs:
  create-issue:
    title-prefix: "[test] "
---

# Main Workflow

This workflow uses the imported meta configuration.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify create-issue from main workflow
	require.NotNil(t, workflowData.SafeOutputs.CreateIssues, "CreateIssues should be present from main")
	assert.Equal(t, "[test] ", workflowData.SafeOutputs.CreateIssues.TitlePrefix)

	// Verify imported meta fields
	assert.Equal(t, []string{"example.com", "api.example.com"}, workflowData.SafeOutputs.AllowedDomains, "AllowedDomains should be imported")
	assert.True(t, templatableBoolIsTrue(workflowData.SafeOutputs.Staged), "Staged should be imported and set to true")
	assert.Equal(t, map[string]string{"TEST_VAR": "test_value"}, workflowData.SafeOutputs.Env, "Env should be imported")
	assert.Equal(t, "${{ secrets.CUSTOM_TOKEN }}", workflowData.SafeOutputs.GitHubToken, "GitHubToken should be imported")
	require.NotNil(t, workflowData.SafeOutputs.ReportFailureAsIssue, "ReportFailureAsIssue should be imported")
	assert.Equal(t, "${{ inputs.report-failure-as-issue }}", workflowData.SafeOutputs.ReportFailureAsIssue.String(), "ReportFailureAsIssue should be imported as templatable bool")
	// Note: When main workflow has safe-outputs section, extractSafeOutputsConfig sets MaximumPatchSize default (4096)
	// before merge happens, so imported value is not used. User should specify max-patch-size in main workflow.
	assert.Equal(t, 4096, workflowData.SafeOutputs.MaximumPatchSize, "MaximumPatchSize defaults to 4096 when main has safe-outputs")
	assert.Equal(t, "runs-on: ubuntu-latest", workflowData.SafeOutputs.RunsOn, "RunsOn should be imported")
}

// TestSafeOutputsImportMetaFieldsMainTakesPrecedence tests that main workflow meta fields take precedence over imports
func TestSafeOutputsImportMetaFieldsMainTakesPrecedence(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with meta fields
	sharedWorkflow := `---
safe-outputs:
  allowed-domains:
    - "shared.example.com"
  report-failure-as-issue: false
  github-token: "${{ secrets.SHARED_TOKEN }}"
  max-patch-size: 1024
---

# Shared Meta Fields Configuration
`

	sharedFile := filepath.Join(workflowsDir, "shared-meta.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that has its own meta fields
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-meta.md
safe-outputs:
  allowed-domains:
    - "main.example.com"
  report-failure-as-issue: ${{ inputs.report-failure-as-issue }}
  github-token: "${{ secrets.MAIN_TOKEN }}"
  max-patch-size: 2048
  create-issue:
    title-prefix: "[test] "
---

# Main Workflow

This workflow has its own meta configuration that should take precedence.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify main workflow meta fields take precedence
	assert.Equal(t, []string{"main.example.com"}, workflowData.SafeOutputs.AllowedDomains, "AllowedDomains from main should take precedence")
	require.NotNil(t, workflowData.SafeOutputs.ReportFailureAsIssue, "ReportFailureAsIssue from main should be set")
	assert.Equal(t, "${{ inputs.report-failure-as-issue }}", workflowData.SafeOutputs.ReportFailureAsIssue.String(), "ReportFailureAsIssue from main should take precedence")
	assert.Equal(t, "${{ secrets.MAIN_TOKEN }}", workflowData.SafeOutputs.GitHubToken, "GitHubToken from main should take precedence")
	assert.Equal(t, 2048, workflowData.SafeOutputs.MaximumPatchSize, "MaximumPatchSize from main should take precedence")
}

// TestSafeOutputsImportMetaFieldsFromOnlyImport tests that meta fields are correctly imported when main has no safe-outputs section
func TestSafeOutputsImportMetaFieldsFromOnlyImport(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with meta fields and create-issue
	sharedWorkflow := `---
safe-outputs:
  create-issue:
    title-prefix: "[imported] "
  allowed-domains:
    - "import.example.com"
  github-token: "${{ secrets.IMPORT_TOKEN }}"
  max-patch-size: 4096
  staged: true
  runs-on: "ubuntu-22.04"
---

# Shared Safe Outputs Configuration
`

	sharedFile := filepath.Join(workflowsDir, "shared-full.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that has NO safe-outputs section (only imports)
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-full.md
---

# Main Workflow

This workflow uses only imported safe-outputs configuration.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify safe output type from import
	require.NotNil(t, workflowData.SafeOutputs.CreateIssues, "CreateIssues should be imported")
	assert.Equal(t, "[imported] ", workflowData.SafeOutputs.CreateIssues.TitlePrefix)

	// Verify all meta fields from import (no defaults from main since main has no safe-outputs)
	assert.Equal(t, []string{"import.example.com"}, workflowData.SafeOutputs.AllowedDomains, "AllowedDomains should be imported")
	assert.Equal(t, "${{ secrets.IMPORT_TOKEN }}", workflowData.SafeOutputs.GitHubToken, "GitHubToken should be imported")
	assert.Equal(t, 4096, workflowData.SafeOutputs.MaximumPatchSize, "MaximumPatchSize should be imported")
	assert.True(t, templatableBoolIsTrue(workflowData.SafeOutputs.Staged), "Staged should be imported and set to true")
	assert.Equal(t, "runs-on: ubuntu-22.04", workflowData.SafeOutputs.RunsOn, "RunsOn should be imported")
}

// TestSafeOutputsImportJobsFromSharedWorkflow tests that safe-outputs.jobs can be imported from shared workflows
func TestSafeOutputsImportJobsFromSharedWorkflow(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with safe-outputs.jobs configuration
	sharedWorkflow := `---
safe-outputs:
  jobs:
    my-custom-job:
      name: "My Custom Job"
      runs-on: ubuntu-latest
      permissions:
        contents: read
        issues: write
      steps:
        - name: Run custom action
          run: echo "Hello from custom job"
---

# Shared Safe Jobs Configuration

This shared workflow provides custom safe-job definitions.
`

	sharedFile := filepath.Join(workflowsDir, "shared-safe-jobs.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that imports the safe-jobs configuration
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-safe-jobs.md
---

# Main Workflow

This workflow imports safe-jobs from a shared workflow.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify that jobs were imported
	require.NotNil(t, workflowData.SafeOutputs.Jobs, "Jobs should be imported")
	require.Contains(t, workflowData.SafeOutputs.Jobs, "my-custom-job", "my-custom-job should be present")

	// Verify job configuration
	job := workflowData.SafeOutputs.Jobs["my-custom-job"]
	assert.Equal(t, "My Custom Job", job.Name, "Job name should match")
	assert.Equal(t, "runs-on: ubuntu-latest", job.RunsOn, "Job runs-on should match")
	assert.Len(t, job.Steps, 1, "Job should have 1 step")
	assert.Contains(t, job.Permissions, "contents", "Job should have contents permission")
	assert.Contains(t, job.Permissions, "issues", "Job should have issues permission")
}

// TestSafeOutputsImportJobsWithMainWorkflowJobs tests importing jobs when main workflow also has jobs
func TestSafeOutputsImportJobsWithMainWorkflowJobs(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with safe-outputs.jobs configuration
	sharedWorkflow := `---
safe-outputs:
  jobs:
    imported-job:
      name: "Imported Job"
      runs-on: ubuntu-latest
      steps:
        - name: Imported step
          run: echo "Imported"
---

# Shared Safe Jobs Configuration
`

	sharedFile := filepath.Join(workflowsDir, "shared-jobs.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that has its own jobs AND imports jobs
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-jobs.md
safe-outputs:
  jobs:
    main-job:
      name: "Main Job"
      runs-on: ubuntu-latest
      steps:
        - name: Main step
          run: echo "Main"
---

# Main Workflow with Jobs

This workflow has its own jobs and imports more jobs.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify that both main and imported jobs are present
	require.NotNil(t, workflowData.SafeOutputs.Jobs, "Jobs should not be nil")
	require.Contains(t, workflowData.SafeOutputs.Jobs, "main-job", "main-job should be present")
	require.Contains(t, workflowData.SafeOutputs.Jobs, "imported-job", "imported-job should be imported")

	// Verify both job configurations
	mainJob := workflowData.SafeOutputs.Jobs["main-job"]
	assert.Equal(t, "Main Job", mainJob.Name, "Main job name should match")

	importedJob := workflowData.SafeOutputs.Jobs["imported-job"]
	assert.Equal(t, "Imported Job", importedJob.Name, "Imported job name should match")
}

// TestSafeOutputsImportJobsConflict tests that a conflict error is returned when the same job name is defined in both main and imported workflow
func TestSafeOutputsImportJobsConflict(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with safe-outputs.jobs configuration
	sharedWorkflow := `---
safe-outputs:
  jobs:
    duplicate-job:
      name: "Shared Duplicate Job"
      runs-on: ubuntu-latest
      steps:
        - name: Shared step
          run: echo "Shared"
---

# Shared Safe Jobs Configuration with Duplicate
`

	sharedFile := filepath.Join(workflowsDir, "shared-duplicate.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that has the same job name (conflict)
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-duplicate.md
safe-outputs:
  jobs:
    duplicate-job:
      name: "Main Duplicate Job"
      runs-on: ubuntu-latest
      steps:
        - name: Main step
          run: echo "Main"
---

# Main Workflow with Duplicate Job Name
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow - should fail with conflict error
	_, err = compiler.ParseWorkflowFile("main.md")
	require.Error(t, err, "Expected conflict error")
	require.ErrorContains(t, err, "duplicate-job", "Error should mention the conflicting job name")
	require.ErrorContains(t, err, "conflict", "Error should mention conflict")
}

// TestSafeOutputsImportMessagesFromSharedWorkflow tests that safe-outputs.messages can be imported from shared workflows
func TestSafeOutputsImportMessagesFromSharedWorkflow(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with messages configuration
	sharedWorkflow := `---
safe-outputs:
  messages:
    footer: "> Custom footer from [{workflow_name}]({run_url})"
    footer-install: "> Install: ` + "`gh aw add {workflow_source}`" + `"
    staged-title: "## 🔍 Preview: {operation}"
    staged-description: "Preview of {operation}:"
    run-started: "🚀 Workflow started"
    run-success: "✅ Workflow completed successfully"
    run-failure: "❌ Workflow failed"
---

# Shared Messages Configuration

This shared workflow provides custom messages templates.
`

	sharedFile := filepath.Join(workflowsDir, "shared-messages.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that imports the messages configuration
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-messages.md
safe-outputs:
  create-issue:
    title-prefix: "[test] "
---

# Main Workflow

This workflow imports messages from a shared workflow.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify messages were imported
	require.NotNil(t, workflowData.SafeOutputs.Messages, "Messages should be imported")
	assert.Equal(t, "> Custom footer from [{workflow_name}]({run_url})", workflowData.SafeOutputs.Messages.Footer, "Footer should be imported")
	assert.Equal(t, "> Install: `gh aw add {workflow_source}`", workflowData.SafeOutputs.Messages.FooterInstall, "FooterInstall should be imported")
	assert.Equal(t, "## 🔍 Preview: {operation}", workflowData.SafeOutputs.Messages.StagedTitle, "StagedTitle should be imported")
	assert.Equal(t, "Preview of {operation}:", workflowData.SafeOutputs.Messages.StagedDescription, "StagedDescription should be imported")
	assert.Equal(t, "🚀 Workflow started", workflowData.SafeOutputs.Messages.RunStarted, "RunStarted should be imported")
	assert.Equal(t, "✅ Workflow completed successfully", workflowData.SafeOutputs.Messages.RunSuccess, "RunSuccess should be imported")
	assert.Equal(t, "❌ Workflow failed", workflowData.SafeOutputs.Messages.RunFailure, "RunFailure should be imported")
}

// TestSafeOutputsImportMessagesMainOverrides tests that main workflow messages take precedence over imports
func TestSafeOutputsImportMessagesMainOverrides(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with messages configuration
	sharedWorkflow := `---
safe-outputs:
  messages:
    footer: "> Shared footer"
    footer-install: "> Shared install instructions"
    staged-title: "## Shared Preview"
    staged-description: "Shared preview description"
    run-started: "Shared started"
    run-success: "Shared success"
    run-failure: "Shared failure"
---

# Shared Messages Configuration
`

	sharedFile := filepath.Join(workflowsDir, "shared-messages.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow with partial messages configuration (some fields only)
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-messages.md
safe-outputs:
  create-issue:
    title-prefix: "[test] "
  messages:
    footer: "> Main footer (takes precedence)"
    run-success: "Main success (takes precedence)"
---

# Main Workflow with Partial Messages Override

Main workflow defines some messages that should take precedence.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify messages merge: main overrides some, shared provides others
	require.NotNil(t, workflowData.SafeOutputs.Messages, "Messages should not be nil")

	// Main workflow fields take precedence
	assert.Equal(t, "> Main footer (takes precedence)", workflowData.SafeOutputs.Messages.Footer, "Footer from main should take precedence")
	assert.Equal(t, "Main success (takes precedence)", workflowData.SafeOutputs.Messages.RunSuccess, "RunSuccess from main should take precedence")

	// Shared workflow fields fill in the gaps
	assert.Equal(t, "> Shared install instructions", workflowData.SafeOutputs.Messages.FooterInstall, "FooterInstall should come from shared")
	assert.Equal(t, "## Shared Preview", workflowData.SafeOutputs.Messages.StagedTitle, "StagedTitle should come from shared")
	assert.Equal(t, "Shared preview description", workflowData.SafeOutputs.Messages.StagedDescription, "StagedDescription should come from shared")
	assert.Equal(t, "Shared started", workflowData.SafeOutputs.Messages.RunStarted, "RunStarted should come from shared")
	assert.Equal(t, "Shared failure", workflowData.SafeOutputs.Messages.RunFailure, "RunFailure should come from shared")
}

// TestSafeOutputsImportMessagesWithNoMainSafeOutputs tests messages import when main has no safe-outputs section
func TestSafeOutputsImportMessagesWithNoMainSafeOutputs(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with messages and a safe output type
	sharedWorkflow := `---
safe-outputs:
  create-issue:
    title-prefix: "[imported] "
  messages:
    footer: "> Imported footer"
    run-success: "Imported success"
---

# Shared Safe Outputs with Messages
`

	sharedFile := filepath.Join(workflowsDir, "shared-full.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow with NO safe-outputs section
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-full.md
---

# Main Workflow

Uses only imported safe-outputs including messages.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify safe output type from import
	require.NotNil(t, workflowData.SafeOutputs.CreateIssues, "CreateIssues should be imported")
	assert.Equal(t, "[imported] ", workflowData.SafeOutputs.CreateIssues.TitlePrefix)

	// Verify messages from import
	require.NotNil(t, workflowData.SafeOutputs.Messages, "Messages should be imported")
	assert.Equal(t, "> Imported footer", workflowData.SafeOutputs.Messages.Footer, "Footer should be imported")
	assert.Equal(t, "Imported success", workflowData.SafeOutputs.Messages.RunSuccess, "RunSuccess should be imported")
}

// TestMergeSafeOutputsJobsNotMerged tests that Jobs are NOT merged in MergeSafeOutputs
// because they are handled separately in the orchestrator via mergeSafeJobsFromIncludedConfigs
func TestMergeSafeOutputsJobsNotMerged(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a top-level config with a job
	topConfig := &SafeOutputsConfig{
		Jobs: map[string]*SafeJobConfig{
			"existing-job": {
				Name:   "Existing Job",
				RunsOn: "runs-on: ubuntu-latest",
			},
		},
	}

	// Import JSON that contains a job - this should be ignored by MergeSafeOutputs
	importedJSON := []string{
		`{"jobs":{"imported-job":{"name":"Imported Job","runs-on":"ubuntu-latest"}},"create-issue":{"title-prefix":"[test] "}}`,
	}

	result, err := compiler.MergeSafeOutputs(topConfig, importedJSON, nil)
	require.NoError(t, err, "MergeSafeOutputs should not error")

	// Verify that the existing job is preserved (Jobs field untouched)
	require.NotNil(t, result.Jobs, "Jobs should not be nil")
	assert.Contains(t, result.Jobs, "existing-job", "Existing job should be preserved")
	assert.NotContains(t, result.Jobs, "imported-job", "Imported job should NOT be merged here (handled separately in orchestrator)")

	// Verify that other safe-output types ARE merged
	require.NotNil(t, result.CreateIssues, "CreateIssues should be merged")
	assert.Equal(t, "[test] ", result.CreateIssues.TitlePrefix, "CreateIssues config should be imported")
}

// TestMergeSafeOutputsJobsSkippedWhenEmpty tests that Jobs field is not created if not present
func TestMergeSafeOutputsJobsSkippedWhenEmpty(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a top-level config without jobs
	topConfig := &SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{TitlePrefix: "[main] "},
	}

	// Import JSON that contains a job - this should be ignored
	importedJSON := []string{
		`{"jobs":{"imported-job":{"name":"Imported Job"}},"add-comment":{"max":5}}`,
	}

	result, err := compiler.MergeSafeOutputs(topConfig, importedJSON, nil)
	require.NoError(t, err, "MergeSafeOutputs should not error")

	// Jobs should still be nil since we don't merge them in MergeSafeOutputs
	assert.Nil(t, result.Jobs, "Jobs should remain nil (not merged in this function)")

	// Other safe-output types should be merged
	require.NotNil(t, result.CreateIssues, "CreateIssues should be preserved")
	require.NotNil(t, result.AddComments, "AddComments should be merged")
	assert.Equal(t, strPtr("5"), result.AddComments.Max, "AddComments config should be correct")
}

// TestMergeSafeOutputsErrorPropagation tests error propagation from mergeSafeOutputConfig
// This test verifies the error handling infrastructure is in place
func TestMergeSafeOutputsErrorPropagation(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tests := []struct {
		name          string
		importedJSON  []string
		expectError   bool
		errorContains string
	}{
		{
			name: "valid JSON should not error",
			importedJSON: []string{
				`{"create-issue":{"title-prefix":"[test] "}}`,
			},
			expectError: false,
		},
		{
			name: "malformed JSON should be skipped gracefully",
			importedJSON: []string{
				`{"create-issue":{"title-prefix":"[test] "}}`,
				`invalid json{`,
				`{"add-comment":{"max":3}}`,
			},
			expectError: false, // Malformed JSON is skipped, not an error
		},
		{
			name: "conflicting safe-output types should error",
			importedJSON: []string{
				`{"create-issue":{"title-prefix":"[import1] "}}`,
				`{"create-issue":{"title-prefix":"[import2] "}}`,
			},
			expectError:   true,
			errorContains: "safe-outputs conflict",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compiler.MergeSafeOutputs(nil, tt.importedJSON, nil)

			if tt.expectError {
				require.Error(t, err, "Expected error")
				require.ErrorContains(t, err, tt.errorContains, "Error message should contain expected text")
				return
			}

			require.NoError(t, err, "Should not error")
			require.NotNil(t, result, "Result should not be nil")
		})
	}
}

// TestMergeSafeOutputsWithJobsIntegration tests the complete workflow parsing with Jobs
// This verifies that Jobs ARE properly imported when going through ParseWorkflowFile
// (which uses the orchestrator's mergeSafeJobsFromIncludedConfigs)
func TestMergeSafeOutputsWithJobsIntegration(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with both jobs and safe-output types
	sharedWorkflow := `---
safe-outputs:
  jobs:
    notify:
      name: "Notification Job"
      runs-on: ubuntu-latest
      steps:
        - name: Send notification
          run: echo "Notification sent"
  create-issue:
    title-prefix: "[shared] "
  messages:
    footer: "> Shared footer"
---

# Shared Configuration
`

	sharedFile := filepath.Join(workflowsDir, "shared-all.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that imports everything
	mainWorkflow := `---
on: issues
permissions:
  contents: read
  issues: read
imports:
  - ./shared-all.md
---

# Main Workflow

This workflow imports jobs and safe-outputs.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow - this goes through the orchestrator
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify Jobs ARE imported (via orchestrator's mergeSafeJobsFromIncludedConfigs)
	require.NotNil(t, workflowData.SafeOutputs.Jobs, "Jobs should be imported by orchestrator")
	require.Contains(t, workflowData.SafeOutputs.Jobs, "notify", "notify job should be present")
	assert.Equal(t, "Notification Job", workflowData.SafeOutputs.Jobs["notify"].Name, "Job name should match")

	// Verify safe-output types ARE imported (via MergeSafeOutputs)
	require.NotNil(t, workflowData.SafeOutputs.CreateIssues, "CreateIssues should be imported")
	assert.Equal(t, "[shared] ", workflowData.SafeOutputs.CreateIssues.TitlePrefix, "CreateIssues config should match")

	// Verify messages ARE imported (via MergeSafeOutputs)
	require.NotNil(t, workflowData.SafeOutputs.Messages, "Messages should be imported")
	assert.Equal(t, "> Shared footer", workflowData.SafeOutputs.Messages.Footer, "Footer should match")
}

// TestProjectSafeOutputsImport tests that project-related safe-output types can be imported from shared workflows
// This specifically tests the fix for the bug where CreateProjectStatusUpdates was not being merged from imports
func TestProjectSafeOutputsImport(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with all project-related safe-output types
	// This mimics the structure of shared/campaign.md
	sharedWorkflow := `---
safe-outputs:
  update-project:
    max: 100
    github-token: "${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}"
  create-project-status-update:
    max: 1
    github-token: "${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}"
  create-project:
    max: 5
    github-token: "${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}"
---

# Shared Project Safe Outputs

This shared workflow provides project-related safe-output configuration.
`

	sharedFile := filepath.Join(workflowsDir, "shared-project.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that imports the project configuration
	mainWorkflow := `---
on: workflow_dispatch
permissions:
  contents: read
imports:
  - ./shared-project.md
---

# Main Workflow

This workflow uses the imported project safe-output configuration.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify update-project configuration was imported correctly
	require.NotNil(t, workflowData.SafeOutputs.UpdateProjects, "UpdateProjects configuration should be imported")
	assert.Equal(t, strPtr("100"), workflowData.SafeOutputs.UpdateProjects.Max)
	assert.Equal(t, "${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}", workflowData.SafeOutputs.UpdateProjects.GitHubToken)

	// Verify create-project-status-update configuration was imported correctly (the bug fix)
	require.NotNil(t, workflowData.SafeOutputs.CreateProjectStatusUpdates, "CreateProjectStatusUpdates configuration should be imported")
	assert.Equal(t, strPtr("1"), workflowData.SafeOutputs.CreateProjectStatusUpdates.Max)
	assert.Equal(t, "${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}", workflowData.SafeOutputs.CreateProjectStatusUpdates.GitHubToken)

	// Verify create-project configuration was imported correctly
	require.NotNil(t, workflowData.SafeOutputs.CreateProjects, "CreateProjects configuration should be imported")
	assert.Equal(t, strPtr("5"), workflowData.SafeOutputs.CreateProjects.Max)
	assert.Equal(t, "${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}", workflowData.SafeOutputs.CreateProjects.GitHubToken)
}

// TestAllMissingSafeOutputTypesImport tests that all previously missing safe-output types can be imported
// This test ensures that all types in SafeOutputsConfig are properly merged from imports
func TestAllMissingSafeOutputTypesImport(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with all the previously missing safe-output types
	sharedWorkflow := `---
safe-outputs:
  update-discussion:
    max: 10
  link-sub-issue:
    max: 5
  hide-comment:
    max: 20
  dispatch-workflow:
    max: 3
  assign-to-user:
    max: 15
  autofix-code-scanning-alert:
    max: 8
  mark-pull-request-as-ready-for-review:
    max: 12
  missing-data:
    max: 2
---

# Shared Additional Safe Outputs

This shared workflow provides additional safe-output types that were missing from merge logic.
`

	sharedFile := filepath.Join(workflowsDir, "shared-additional.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that imports the configuration
	mainWorkflow := `---
on: workflow_dispatch
permissions:
  contents: read
imports:
  - ./shared-additional.md
---

# Main Workflow

This workflow uses the imported safe-output configuration for previously missing types.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify all previously missing types are now imported correctly
	require.NotNil(t, workflowData.SafeOutputs.UpdateDiscussions, "UpdateDiscussions should be imported")
	assert.Equal(t, strPtr("10"), workflowData.SafeOutputs.UpdateDiscussions.Max)

	require.NotNil(t, workflowData.SafeOutputs.LinkSubIssue, "LinkSubIssue should be imported")
	assert.Equal(t, strPtr("5"), workflowData.SafeOutputs.LinkSubIssue.Max)

	require.NotNil(t, workflowData.SafeOutputs.HideComment, "HideComment should be imported")
	assert.Equal(t, strPtr("20"), workflowData.SafeOutputs.HideComment.Max)

	require.NotNil(t, workflowData.SafeOutputs.DispatchWorkflow, "DispatchWorkflow should be imported")
	assert.Equal(t, strPtr("3"), workflowData.SafeOutputs.DispatchWorkflow.Max)

	require.NotNil(t, workflowData.SafeOutputs.AssignToUser, "AssignToUser should be imported")
	assert.Equal(t, strPtr("15"), workflowData.SafeOutputs.AssignToUser.Max)

	require.NotNil(t, workflowData.SafeOutputs.AutofixCodeScanningAlert, "AutofixCodeScanningAlert should be imported")
	assert.Equal(t, strPtr("8"), workflowData.SafeOutputs.AutofixCodeScanningAlert.Max)

	require.NotNil(t, workflowData.SafeOutputs.MarkPullRequestAsReadyForReview, "MarkPullRequestAsReadyForReview should be imported")
	assert.Equal(t, strPtr("12"), workflowData.SafeOutputs.MarkPullRequestAsReadyForReview.Max)

	require.NotNil(t, workflowData.SafeOutputs.MissingData, "MissingData should be imported")
	assert.Equal(t, strPtr("2"), workflowData.SafeOutputs.MissingData.Max)
}

// TestSafeOutputsImportMessagesAllFields tests that all message fields can be imported correctly
func TestSafeOutputsImportMessagesAllFields(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with ALL message fields defined
	sharedWorkflow := `---
safe-outputs:
  messages:
    append-only-comments: true
    footer: "> Custom footer"
    footer-install: "> Install instructions"
    footer-workflow-recompile: "> Workflow recompile footer"
    footer-workflow-recompile-comment: "> Workflow recompile comment footer"
    staged-title: "## Staged Title"
    staged-description: "Staged description"
    run-started: "Run started"
    run-success: "Run success"
    run-failure: "Run failure"
---

# Shared Messages with All Fields
`

	sharedFile := filepath.Join(workflowsDir, "shared-all-messages.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that imports all message fields
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-all-messages.md
safe-outputs:
  create-issue:
    title-prefix: "[test] "
---

# Main Workflow Importing All Message Fields
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify ALL message fields were imported correctly
	require.NotNil(t, workflowData.SafeOutputs.Messages, "Messages should be imported")
	assert.True(t, workflowData.SafeOutputs.Messages.AppendOnlyComments, "AppendOnlyComments should be imported")
	assert.Equal(t, "> Custom footer", workflowData.SafeOutputs.Messages.Footer, "Footer should be imported")
	assert.Equal(t, "> Install instructions", workflowData.SafeOutputs.Messages.FooterInstall, "FooterInstall should be imported")
	assert.Equal(t, "> Workflow recompile footer", workflowData.SafeOutputs.Messages.FooterWorkflowRecompile, "FooterWorkflowRecompile should be imported")
	assert.Equal(t, "> Workflow recompile comment footer", workflowData.SafeOutputs.Messages.FooterWorkflowRecompileComment, "FooterWorkflowRecompileComment should be imported")
	assert.Equal(t, "## Staged Title", workflowData.SafeOutputs.Messages.StagedTitle, "StagedTitle should be imported")
	assert.Equal(t, "Staged description", workflowData.SafeOutputs.Messages.StagedDescription, "StagedDescription should be imported")
	assert.Equal(t, "Run started", workflowData.SafeOutputs.Messages.RunStarted, "RunStarted should be imported")
	assert.Equal(t, "Run success", workflowData.SafeOutputs.Messages.RunSuccess, "RunSuccess should be imported")
	assert.Equal(t, "Run failure", workflowData.SafeOutputs.Messages.RunFailure, "RunFailure should be imported")
}

// TestSafeOutputsImportMessagesAllFieldsPartialOverride tests that main workflow can selectively override imported message fields
func TestSafeOutputsImportMessagesAllFieldsPartialOverride(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with ALL message fields
	sharedWorkflow := `---
safe-outputs:
  messages:
    append-only-comments: true
    footer: "> Shared footer"
    footer-install: "> Shared install"
    footer-workflow-recompile: "> Shared recompile"
    footer-workflow-recompile-comment: "> Shared recompile comment"
    staged-title: "## Shared Title"
    staged-description: "Shared description"
    run-started: "Shared started"
    run-success: "Shared success"
    run-failure: "Shared failure"
---

# Shared Messages
`

	sharedFile := filepath.Join(workflowsDir, "shared-messages.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that overrides some message fields
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-messages.md
safe-outputs:
  create-issue:
    title-prefix: "[test] "
  messages:
    footer: "> Main footer (override)"
    footer-workflow-recompile: "> Main recompile (override)"
    run-success: "Main success (override)"
---

# Main Workflow with Partial Override
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Verify message field merging: main overrides specific fields, shared provides others
	require.NotNil(t, workflowData.SafeOutputs.Messages, "Messages should not be nil")

	// Main overrides
	assert.Equal(t, "> Main footer (override)", workflowData.SafeOutputs.Messages.Footer, "Footer from main should take precedence")
	assert.Equal(t, "> Main recompile (override)", workflowData.SafeOutputs.Messages.FooterWorkflowRecompile, "FooterWorkflowRecompile from main should take precedence")
	assert.Equal(t, "Main success (override)", workflowData.SafeOutputs.Messages.RunSuccess, "RunSuccess from main should take precedence")

	// Shared values fill the gaps
	assert.True(t, workflowData.SafeOutputs.Messages.AppendOnlyComments, "AppendOnlyComments should come from shared")
	assert.Equal(t, "> Shared install", workflowData.SafeOutputs.Messages.FooterInstall, "FooterInstall should come from shared")
	assert.Equal(t, "> Shared recompile comment", workflowData.SafeOutputs.Messages.FooterWorkflowRecompileComment, "FooterWorkflowRecompileComment should come from shared")
	assert.Equal(t, "## Shared Title", workflowData.SafeOutputs.Messages.StagedTitle, "StagedTitle should come from shared")
	assert.Equal(t, "Shared description", workflowData.SafeOutputs.Messages.StagedDescription, "StagedDescription should come from shared")
	assert.Equal(t, "Shared started", workflowData.SafeOutputs.Messages.RunStarted, "RunStarted should come from shared")
	assert.Equal(t, "Shared failure", workflowData.SafeOutputs.Messages.RunFailure, "RunFailure should come from shared")
}

// TestMergeSafeOutputsThreatDetectionExplicitDisableNotOverridden tests that when the main workflow
// explicitly disables threat-detection, imported fragments with no threat-detection key do not
// re-enable it.
func TestMergeSafeOutputsThreatDetectionExplicitDisableNotOverridden(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Simulate main workflow that explicitly disabled threat-detection:
	// threat-detection: false → parseThreatDetectionConfig returns nil.
	topConfig := &SafeOutputsConfig{
		ThreatDetection: nil,
		AddComments:     &AddCommentsConfig{},
	}

	// Import fragment with safe-outputs but no threat-detection key.
	importedJSON := []string{
		`{"add-comment":{"max":1}}`,
	}

	result, err := compiler.MergeSafeOutputs(topConfig, importedJSON, nil)
	require.NoError(t, err, "MergeSafeOutputs should not error")
	require.NotNil(t, result, "Result should not be nil")

	// The explicit disable must survive the merge: threat detection must remain nil.
	assert.Nil(t, result.ThreatDetection, "ThreatDetection must remain nil when explicitly disabled by main workflow")
}

// TestMergeSafeOutputsThreatDetectionImportedWhenExplicit tests that an import that explicitly
// carries a threat-detection key can set it when the main workflow has not configured it.
func TestMergeSafeOutputsThreatDetectionImportedWhenExplicit(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	// Import fragment that explicitly enables threat-detection.
	importedJSON := []string{
		`{"add-comment":{"max":1},"threat-detection":{"enabled":true}}`,
	}

	result, err := compiler.MergeSafeOutputs(nil, importedJSON, nil)
	require.NoError(t, err, "MergeSafeOutputs should not error")
	require.NotNil(t, result, "Result should not be nil")

	// Import explicitly set threat-detection, so it should be present.
	assert.NotNil(t, result.ThreatDetection, "ThreatDetection should be set when explicitly configured in import")
}

// TestSafeOutputsImportDoesNotReenableThreatDetection is an integration test that reproduces
// the bug where an imported fragment re-enables threat-detection that was explicitly disabled
// in the main workflow. This caused a compilation error when sandbox.agent was also false.
func TestSafeOutputsImportDoesNotReenableThreatDetection(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Fragment with safe-outputs but no threat-detection key (mimics safe-output-add-comment.md)
	sharedWorkflow := `---
safe-outputs:
  add-comment:
    max: 1
---

# Shared Add Comment Fragment
`

	sharedFile := filepath.Join(workflowsDir, "safe-output-add-comment.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Main workflow: sandbox.agent disabled + threat-detection explicitly disabled
	mainWorkflow := `---
on: issues
engine: copilot
strict: false
sandbox:
  agent: false
imports:
  - ./safe-output-add-comment.md
safe-outputs:
  activation-comments: false
  threat-detection: false
---

# Main Workflow
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "ParseWorkflowFile should not error when threat-detection is explicitly disabled")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// The explicit disable must survive the import merge.
	assert.Nil(t, workflowData.SafeOutputs.ThreatDetection, "ThreatDetection must remain nil when explicitly disabled by main workflow")
}

// TestSafeOutputsDifferentTypesFromImportsMerged reproduces the bug reported in
// https://github.com/github/gh-aw/issues/<issue>:
// When the main workflow defines one safe-outputs type (e.g. noop) and an imported
// workflow defines a different type (e.g. threat-detection), the imported type should
// be merged into the compiled output. Previously the auto-default applied by
// extractSafeOutputsConfig (which enabled threat-detection by default whenever any
// safe-outputs were present) caused threat-detection to appear as "already defined" in
// topDefinedTypes, so the import's explicit threat-detection configuration was dropped.
func TestSafeOutputsDifferentTypesFromImportsMerged(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Imported workflow: only defines threat-detection with a custom step
	importedWorkflow := `---
safe-outputs:
  threat-detection:
    steps:
      - name: Print abc
        run: echo "abc"
---
`
	importedFile := filepath.Join(workflowsDir, "abc.md")
	err = os.WriteFile(importedFile, []byte(importedWorkflow), 0644)
	require.NoError(t, err, "Failed to write imported file")

	// Main workflow: only defines noop, does NOT define threat-detection
	mainWorkflow := `---
description: hello world
on:
  workflow_dispatch:
imports:
  - ./abc.md
safe-outputs:
  noop:
    report-as-issue: false
---
Print "hello world!".
`
	mainFile := filepath.Join(workflowsDir, "hello-world.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	workflowData, err := compiler.ParseWorkflowFile("hello-world.md")
	require.NoError(t, err, "ParseWorkflowFile should not error")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// noop was explicitly set in the main workflow
	require.NotNil(t, workflowData.SafeOutputs.NoOp, "NoOp should be set (from main workflow)")

	// threat-detection was explicitly set in the import — it must be merged
	require.NotNil(t, workflowData.SafeOutputs.ThreatDetection,
		"ThreatDetection should be merged from the imported workflow")
	assert.Len(t, workflowData.SafeOutputs.ThreatDetection.Steps, 1,
		"ThreatDetection should have 1 custom step from the import")
}

// TestSafeOutputsAutoDefaultableTypesImportedWhenMainHasNone verifies that every
// auto-defaultable type (noop, missing-tool, missing-data, report-incomplete,
// threat-detection) is properly imported when the main workflow has no safe-outputs.
// Previously, extractSafeOutputsConfig created auto-defaults for these types that
// would silently block import merges.
func TestSafeOutputsAutoDefaultableTypesImportedWhenMainHasNone(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Import defines all five auto-defaultable types with explicit custom values.
	importedWorkflow := `---
safe-outputs:
  noop:
    report-as-issue: false
  missing-tool:
    title-prefix: "[imported missing-tool] "
  missing-data:
    title-prefix: "[imported missing-data] "
  report-incomplete:
    title-prefix: "[imported report-incomplete] "
  threat-detection:
    steps:
      - name: Custom detection step
        run: echo "custom"
---
`
	importedFile := filepath.Join(workflowsDir, "shared.md")
	err = os.WriteFile(importedFile, []byte(importedWorkflow), 0644)
	require.NoError(t, err, "Failed to write imported file")

	// Main workflow has no safe-outputs section at all.
	mainWorkflow := `---
on:
  workflow_dispatch:
imports:
  - ./shared.md
---
Run a task.
`
	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "ParseWorkflowFile should not error")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// noop: report-as-issue was explicitly set to false in the import
	require.NotNil(t, workflowData.SafeOutputs.NoOp, "NoOp should be imported")
	require.NotNil(t, workflowData.SafeOutputs.NoOp.ReportAsIssue, "NoOp.ReportAsIssue should be set")
	assert.Equal(t, "false", *workflowData.SafeOutputs.NoOp.ReportAsIssue,
		"NoOp.ReportAsIssue should be 'false' from the import")

	// missing-tool with custom title-prefix
	require.NotNil(t, workflowData.SafeOutputs.MissingTool, "MissingTool should be imported")
	assert.Equal(t, "[imported missing-tool] ", workflowData.SafeOutputs.MissingTool.TitlePrefix,
		"MissingTool.TitlePrefix should come from the import")

	// missing-data with custom title-prefix
	require.NotNil(t, workflowData.SafeOutputs.MissingData, "MissingData should be imported")
	assert.Equal(t, "[imported missing-data] ", workflowData.SafeOutputs.MissingData.TitlePrefix,
		"MissingData.TitlePrefix should come from the import")

	// report-incomplete with custom title-prefix
	require.NotNil(t, workflowData.SafeOutputs.ReportIncomplete, "ReportIncomplete should be imported")
	assert.Equal(t, "[imported report-incomplete] ", workflowData.SafeOutputs.ReportIncomplete.TitlePrefix,
		"ReportIncomplete.TitlePrefix should come from the import")

	// threat-detection with custom step
	require.NotNil(t, workflowData.SafeOutputs.ThreatDetection, "ThreatDetection should be imported")
	assert.Len(t, workflowData.SafeOutputs.ThreatDetection.Steps, 1,
		"ThreatDetection should have the 1 custom step from the import")
}

// TestSafeOutputsMainExplicitAutoDefaultableTypeOverridesImport verifies that when the main
// workflow explicitly configures an auto-defaultable type (e.g. noop), an import that also
// defines the same type is overridden by the main (main wins / override semantics).
func TestSafeOutputsMainExplicitAutoDefaultableTypeOverridesImport(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	importedWorkflow := `---
safe-outputs:
  noop:
    report-as-issue: true
  missing-tool:
    title-prefix: "[imported] "
---
`
	importedFile := filepath.Join(workflowsDir, "shared.md")
	err = os.WriteFile(importedFile, []byte(importedWorkflow), 0644)
	require.NoError(t, err, "Failed to write imported file")

	// Main explicitly sets noop (report-as-issue: false) — import's noop should be ignored.
	mainWorkflow := `---
on:
  workflow_dispatch:
imports:
  - ./shared.md
safe-outputs:
  noop:
    report-as-issue: false
---
Run a task.
`
	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "ParseWorkflowFile should not error")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// Main's noop (report-as-issue: false) must take precedence over import's (true).
	require.NotNil(t, workflowData.SafeOutputs.NoOp, "NoOp should be present")
	require.NotNil(t, workflowData.SafeOutputs.NoOp.ReportAsIssue, "NoOp.ReportAsIssue should be set")
	assert.Equal(t, "false", *workflowData.SafeOutputs.NoOp.ReportAsIssue,
		"Main's noop (report-as-issue: false) should override import's noop (true)")

	// missing-tool was only in the import — it must still be merged.
	require.NotNil(t, workflowData.SafeOutputs.MissingTool, "MissingTool should be imported")
	assert.Equal(t, "[imported] ", workflowData.SafeOutputs.MissingTool.TitlePrefix,
		"MissingTool.TitlePrefix should come from the import")
}

// TestSafeOutputsMultipleImportsEachContributeAutoDefaultableType verifies that when
// several imports each contribute a different auto-defaultable type, all of them are
// merged and none triggers a conflict error.
func TestSafeOutputsMultipleImportsEachContributeAutoDefaultableType(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// First import: noop
	import1 := `---
safe-outputs:
  noop:
    report-as-issue: false
---
`
	err = os.WriteFile(filepath.Join(workflowsDir, "noop.md"), []byte(import1), 0644)
	require.NoError(t, err, "Failed to write noop.md")

	// Second import: missing-tool
	import2 := `---
safe-outputs:
  missing-tool:
    title-prefix: "[missing-tool import] "
---
`
	err = os.WriteFile(filepath.Join(workflowsDir, "missing-tool.md"), []byte(import2), 0644)
	require.NoError(t, err, "Failed to write missing-tool.md")

	// Third import: report-incomplete
	import3 := `---
safe-outputs:
  report-incomplete:
    title-prefix: "[report-incomplete import] "
---
`
	err = os.WriteFile(filepath.Join(workflowsDir, "report-incomplete.md"), []byte(import3), 0644)
	require.NoError(t, err, "Failed to write report-incomplete.md")

	// Main workflow: only defines create-issue; all three auto-defaultable types come from imports.
	mainWorkflow := `---
on:
  workflow_dispatch:
imports:
  - ./noop.md
  - ./missing-tool.md
  - ./report-incomplete.md
safe-outputs:
  create-issue:
    title-prefix: "[main] "
---
Run a task.
`
	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "ParseWorkflowFile should not error — no conflicts expected")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	// create-issue from main
	require.NotNil(t, workflowData.SafeOutputs.CreateIssues, "CreateIssues should be present from main")
	assert.Equal(t, "[main] ", workflowData.SafeOutputs.CreateIssues.TitlePrefix)

	// noop from first import
	require.NotNil(t, workflowData.SafeOutputs.NoOp, "NoOp should be imported from noop.md")
	require.NotNil(t, workflowData.SafeOutputs.NoOp.ReportAsIssue, "NoOp.ReportAsIssue should be set")
	assert.Equal(t, "false", *workflowData.SafeOutputs.NoOp.ReportAsIssue)

	// missing-tool from second import
	require.NotNil(t, workflowData.SafeOutputs.MissingTool, "MissingTool should be imported from missing-tool.md")
	assert.Equal(t, "[missing-tool import] ", workflowData.SafeOutputs.MissingTool.TitlePrefix)

	// report-incomplete from third import
	require.NotNil(t, workflowData.SafeOutputs.ReportIncomplete, "ReportIncomplete should be imported from report-incomplete.md")
	assert.Equal(t, "[report-incomplete import] ", workflowData.SafeOutputs.ReportIncomplete.TitlePrefix)
}

func TestSafeOutputsNeedsMergedFromImports(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	importedWorkflow := `---
safe-outputs:
  needs: [imported_job, shared_job]
---
`
	importedFile := filepath.Join(workflowsDir, "shared-needs.md")
	err = os.WriteFile(importedFile, []byte(importedWorkflow), 0644)
	require.NoError(t, err, "Failed to write imported file")

	mainWorkflow := `---
on:
  workflow_dispatch:
imports:
  - ./shared-needs.md
safe-outputs:
  needs: [main_job, imported_job]
jobs:
  main_job:
    runs-on: ubuntu-latest
    steps:
      - run: echo "main"
  imported_job:
    runs-on: ubuntu-latest
    steps:
      - run: echo "imported"
  shared_job:
    runs-on: ubuntu-latest
    steps:
      - run: echo "shared"
---
Run a task.
`
	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "ParseWorkflowFile should not error")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	assert.ElementsMatch(t, []string{"main_job", "imported_job", "shared_job"}, workflowData.SafeOutputs.Needs,
		"safe-outputs.needs should merge main and imported dependencies with deduplication")

	err = compiler.CompileWorkflow("main.md")
	require.NoError(t, err, "CompileWorkflow should not error")

	lockPath := filepath.Join(workflowsDir, "main.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err, "compiled lock file should exist")

	var compiled map[string]any
	err = yaml.Unmarshal(lockContent, &compiled)
	require.NoError(t, err, "compiled lock file should be valid YAML")

	jobs, ok := compiled["jobs"].(map[string]any)
	require.True(t, ok, "compiled workflow should include jobs")
	safeOutputsJob, ok := jobs["safe_outputs"].(map[string]any)
	require.True(t, ok, "compiled workflow should include safe_outputs job")
	needsRaw, ok := safeOutputsJob["needs"].([]any)
	require.True(t, ok, "safe_outputs job should include needs array")

	needs := make([]string, 0, len(needsRaw))
	for _, need := range needsRaw {
		require.IsType(t, "", need, "safe_outputs needs entries should be strings")
		needStr := need.(string)
		needs = append(needs, needStr)
	}

	assert.Contains(t, needs, "main_job", "safe_outputs needs should include dependency from main workflow")
	assert.Contains(t, needs, "imported_job", "safe_outputs needs should include deduped dependency from import")
	assert.Contains(t, needs, "shared_job", "safe_outputs needs should include dependency from import")
}

// TestSafeOutputsImportLinearTokenFromOnlyImport ensures that a Linear handler and its
// linear-token supplied purely by an imported shared workflow retain the credential after
// merge, mirroring the equivalent GitHubToken merge behavior.
func TestSafeOutputsImportLinearTokenFromOnlyImport(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	sharedWorkflow := `---
safe-outputs:
  linear-token: "${{ secrets.IMPORT_LINEAR_TOKEN }}"
  linear-add-comment:
    target: "ENG-123"
---

# Shared Linear Configuration
`

	sharedFile := filepath.Join(workflowsDir, "shared-linear.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-linear.md
---

# Main Workflow

This workflow uses only the imported Linear safe-outputs configuration.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	require.NotNil(t, workflowData.SafeOutputs.LinearAddComment, "LinearAddComment should be imported")
	assert.Equal(t, "ENG-123", workflowData.SafeOutputs.LinearAddComment.Target)
	assert.Equal(t, "${{ secrets.IMPORT_LINEAR_TOKEN }}", workflowData.SafeOutputs.LinearToken, "LinearToken should be merged from the import")

	// The Linear-only configuration must not implicitly enable GitHub issue creation
	// for incomplete-run reporting.
	require.NotNil(t, workflowData.SafeOutputs.ReportIncomplete)
	require.NotNil(t, workflowData.SafeOutputs.ReportIncomplete.CreateIssue)
	assert.Equal(t, "false", *workflowData.SafeOutputs.ReportIncomplete.CreateIssue, "ReportIncomplete.CreateIssue should default to false for a Linear-only merged configuration")
}

// TestSafeOutputsImportLinearOnlyDoesNotEnableGitHubIssueReporting is a regression test for
// the implicit report-incomplete default being computed before imported Linear handlers are
// merged in. Previously, a main workflow declaring only meta safe-outputs fields (like
// linear-token) would get an implicit "create-issue: true" default before the import's Linear
// handler was merged in, and that implicit true value would then be preserved by the merge
// because the import did not explicitly set report-incomplete.
func TestSafeOutputsImportLinearOnlyDoesNotEnableGitHubIssueReporting(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	sharedWorkflow := `---
safe-outputs:
  linear-create-issue:
    team-id: "9cfb482a-81e3-4154-b5b9-2c805e70a02d"
---

# Shared Linear Configuration
`

	sharedFile := filepath.Join(workflowsDir, "shared-linear.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Main workflow declares a safe-outputs section (so the implicit default gets
	// computed before merge) but only sets meta fields, not any GitHub-backed handler.
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-linear.md
safe-outputs:
  linear-token: "${{ secrets.LINEAR_API_KEY }}"
---

# Main Workflow
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")

	require.NotNil(t, workflowData.SafeOutputs.LinearCreateIssue, "LinearCreateIssue should be imported")
	require.Nil(t, workflowData.SafeOutputs.CreateIssues, "CreateIssues (GitHub) must not be implicitly enabled")
	require.NotNil(t, workflowData.SafeOutputs.ReportIncomplete)
	require.NotNil(t, workflowData.SafeOutputs.ReportIncomplete.CreateIssue)
	assert.Equal(t, "false", *workflowData.SafeOutputs.ReportIncomplete.CreateIssue, "ReportIncomplete.CreateIssue should be recomputed to false after merging the imported Linear-only handler")
	assert.Empty(t, computePermissionsForSafeOutputs(workflowData.SafeOutputs, false).permissions, "Linear-only merged configuration should not request GitHub write permissions")
}
