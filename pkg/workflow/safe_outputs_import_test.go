//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSafeOutputsImport tests that safe-output types can be imported from shared workflows
func TestSafeOutputsImport(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

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
	compiler := NewCompilerWithVersion("1.0.0")

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
	assert.Equal(t, 3, workflowData.SafeOutputs.AddComments.Max)
}

// TestSafeOutputsImportOverride tests that when the same safe-output type is defined in both main and imported workflow, the main workflow's definition takes precedence
func TestSafeOutputsImportOverride(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

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
	compiler := NewCompilerWithVersion("1.0.0")

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
	assert.Contains(t, err.Error(), "safe-outputs conflict")
	assert.Contains(t, err.Error(), "create-issue")
}

// TestSafeOutputsImportNoConflictDifferentTypes tests that importing different safe-output types does not cause a conflict
func TestSafeOutputsImportNoConflictDifferentTypes(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

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
	compiler := NewCompilerWithVersion("1.0.0")

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
	assert.Equal(t, 5, workflowData.SafeOutputs.AddComments.Max)
}

// TestMergeSafeOutputsUnit tests the MergeSafeOutputs function directly
func TestMergeSafeOutputsUnit(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compiler.MergeSafeOutputs(tt.topConfig, tt.importedJSON)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
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

// TestMergeSafeOutputsMessagesUnit tests the MergeSafeOutputs function for messages field
func TestMergeSafeOutputsMessagesUnit(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

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
			result, err := compiler.MergeSafeOutputs(tt.topConfig, tt.importedJSON)

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
	compiler := NewCompilerWithVersion("1.0.0")

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
	assert.True(t, workflowData.SafeOutputs.Staged, "Staged should be imported and set to true")
	assert.Equal(t, map[string]string{"TEST_VAR": "test_value"}, workflowData.SafeOutputs.Env, "Env should be imported")
	assert.Equal(t, "${{ secrets.CUSTOM_TOKEN }}", workflowData.SafeOutputs.GitHubToken, "GitHubToken should be imported")
	// Note: When main workflow has safe-outputs section, extractSafeOutputsConfig sets MaximumPatchSize default (1024)
	// before merge happens, so imported value is not used. User should specify max-patch-size in main workflow.
	assert.Equal(t, 1024, workflowData.SafeOutputs.MaximumPatchSize, "MaximumPatchSize defaults to 1024 when main has safe-outputs")
	assert.Equal(t, "ubuntu-latest", workflowData.SafeOutputs.RunsOn, "RunsOn should be imported")
}

// TestSafeOutputsImportMetaFieldsMainTakesPrecedence tests that main workflow meta fields take precedence over imports
func TestSafeOutputsImportMetaFieldsMainTakesPrecedence(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

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
	assert.Equal(t, "${{ secrets.MAIN_TOKEN }}", workflowData.SafeOutputs.GitHubToken, "GitHubToken from main should take precedence")
	assert.Equal(t, 2048, workflowData.SafeOutputs.MaximumPatchSize, "MaximumPatchSize from main should take precedence")
}

// TestSafeOutputsImportMetaFieldsFromOnlyImport tests that meta fields are correctly imported when main has no safe-outputs section
func TestSafeOutputsImportMetaFieldsFromOnlyImport(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

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
	assert.True(t, workflowData.SafeOutputs.Staged, "Staged should be imported and set to true")
	assert.Equal(t, "ubuntu-22.04", workflowData.SafeOutputs.RunsOn, "RunsOn should be imported")
}

// TestSafeOutputsImportJobsFromSharedWorkflow tests that safe-outputs.jobs can be imported from shared workflows
func TestSafeOutputsImportJobsFromSharedWorkflow(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

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
	assert.Equal(t, "ubuntu-latest", job.RunsOn, "Job runs-on should match")
	assert.Len(t, job.Steps, 1, "Job should have 1 step")
	assert.Contains(t, job.Permissions, "contents", "Job should have contents permission")
	assert.Contains(t, job.Permissions, "issues", "Job should have issues permission")
}

// TestSafeOutputsImportJobsWithMainWorkflowJobs tests importing jobs when main workflow also has jobs
func TestSafeOutputsImportJobsWithMainWorkflowJobs(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

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
	compiler := NewCompilerWithVersion("1.0.0")

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
	assert.Contains(t, err.Error(), "duplicate-job", "Error should mention the conflicting job name")
	assert.Contains(t, err.Error(), "conflict", "Error should mention conflict")
}

// TestSafeOutputsImportMessagesFromSharedWorkflow tests that safe-outputs.messages can be imported from shared workflows
func TestSafeOutputsImportMessagesFromSharedWorkflow(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

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
	compiler := NewCompilerWithVersion("1.0.0")

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
	compiler := NewCompilerWithVersion("1.0.0")

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
	compiler := NewCompilerWithVersion("1.0.0")

	// Create a top-level config with a job
	topConfig := &SafeOutputsConfig{
		Jobs: map[string]*SafeJobConfig{
			"existing-job": {
				Name:   "Existing Job",
				RunsOn: "ubuntu-latest",
			},
		},
	}

	// Import JSON that contains a job - this should be ignored by MergeSafeOutputs
	importedJSON := []string{
		`{"jobs":{"imported-job":{"name":"Imported Job","runs-on":"ubuntu-latest"}},"create-issue":{"title-prefix":"[test] "}}`,
	}

	result, err := compiler.MergeSafeOutputs(topConfig, importedJSON)
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
	compiler := NewCompilerWithVersion("1.0.0")

	// Create a top-level config without jobs
	topConfig := &SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{TitlePrefix: "[main] "},
	}

	// Import JSON that contains a job - this should be ignored
	importedJSON := []string{
		`{"jobs":{"imported-job":{"name":"Imported Job"}},"add-comment":{"max":5}}`,
	}

	result, err := compiler.MergeSafeOutputs(topConfig, importedJSON)
	require.NoError(t, err, "MergeSafeOutputs should not error")

	// Jobs should still be nil since we don't merge them in MergeSafeOutputs
	assert.Nil(t, result.Jobs, "Jobs should remain nil (not merged in this function)")

	// Other safe-output types should be merged
	require.NotNil(t, result.CreateIssues, "CreateIssues should be preserved")
	require.NotNil(t, result.AddComments, "AddComments should be merged")
	assert.Equal(t, 5, result.AddComments.Max, "AddComments config should be correct")
}

// TestMergeSafeOutputsErrorPropagation tests error propagation from mergeSafeOutputConfig
// This test verifies the error handling infrastructure is in place
func TestMergeSafeOutputsErrorPropagation(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

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
			result, err := compiler.MergeSafeOutputs(nil, tt.importedJSON)

			if tt.expectError {
				require.Error(t, err, "Expected error")
				assert.Contains(t, err.Error(), tt.errorContains, "Error message should contain expected text")
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
	compiler := NewCompilerWithVersion("1.0.0")

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
	compiler := NewCompilerWithVersion("1.0.0")

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
	assert.Equal(t, 100, workflowData.SafeOutputs.UpdateProjects.Max)
	assert.Equal(t, "${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}", workflowData.SafeOutputs.UpdateProjects.GitHubToken)

	// Verify create-project-status-update configuration was imported correctly (the bug fix)
	require.NotNil(t, workflowData.SafeOutputs.CreateProjectStatusUpdates, "CreateProjectStatusUpdates configuration should be imported")
	assert.Equal(t, 1, workflowData.SafeOutputs.CreateProjectStatusUpdates.Max)
	assert.Equal(t, "${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}", workflowData.SafeOutputs.CreateProjectStatusUpdates.GitHubToken)

	// Verify create-project configuration was imported correctly
	require.NotNil(t, workflowData.SafeOutputs.CreateProjects, "CreateProjects configuration should be imported")
	assert.Equal(t, 5, workflowData.SafeOutputs.CreateProjects.Max)
	assert.Equal(t, "${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}", workflowData.SafeOutputs.CreateProjects.GitHubToken)
}

// TestAllMissingSafeOutputTypesImport tests that all previously missing safe-output types can be imported
// This test ensures that all types in SafeOutputsConfig are properly merged from imports
func TestAllMissingSafeOutputTypesImport(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

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
	assert.Equal(t, 10, workflowData.SafeOutputs.UpdateDiscussions.Max)

	require.NotNil(t, workflowData.SafeOutputs.LinkSubIssue, "LinkSubIssue should be imported")
	assert.Equal(t, 5, workflowData.SafeOutputs.LinkSubIssue.Max)

	require.NotNil(t, workflowData.SafeOutputs.HideComment, "HideComment should be imported")
	assert.Equal(t, 20, workflowData.SafeOutputs.HideComment.Max)

	require.NotNil(t, workflowData.SafeOutputs.DispatchWorkflow, "DispatchWorkflow should be imported")
	assert.Equal(t, 3, workflowData.SafeOutputs.DispatchWorkflow.Max)

	require.NotNil(t, workflowData.SafeOutputs.AssignToUser, "AssignToUser should be imported")
	assert.Equal(t, 15, workflowData.SafeOutputs.AssignToUser.Max)

	require.NotNil(t, workflowData.SafeOutputs.AutofixCodeScanningAlert, "AutofixCodeScanningAlert should be imported")
	assert.Equal(t, 8, workflowData.SafeOutputs.AutofixCodeScanningAlert.Max)

	require.NotNil(t, workflowData.SafeOutputs.MarkPullRequestAsReadyForReview, "MarkPullRequestAsReadyForReview should be imported")
	assert.Equal(t, 12, workflowData.SafeOutputs.MarkPullRequestAsReadyForReview.Max)

	require.NotNil(t, workflowData.SafeOutputs.MissingData, "MissingData should be imported")
	assert.Equal(t, 2, workflowData.SafeOutputs.MissingData.Max)
}
