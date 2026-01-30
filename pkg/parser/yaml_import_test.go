//go:build !integration

package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsYAMLWorkflowFile(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "yml file",
			filePath: "workflow.yml",
			expected: true,
		},
		{
			name:     "yaml file",
			filePath: "workflow.yaml",
			expected: true,
		},
		{
			name:     "lock yml file - should be rejected",
			filePath: "workflow.lock.yml",
			expected: false,
		},
		{
			name:     "markdown file",
			filePath: "workflow.md",
			expected: false,
		},
		{
			name:     "uppercase YML",
			filePath: "workflow.YML",
			expected: true,
		},
		{
			name:     "uppercase LOCK.YML",
			filePath: "workflow.LOCK.YML",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isYAMLWorkflowFile(tt.filePath)
			assert.Equal(t, tt.expected, result, "File: %s", tt.filePath)
		})
	}
}

func TestIsActionDefinitionFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		content  string
		expected bool
	}{
		{
			name:     "action.yml by name",
			filename: "action.yml",
			content: `name: Test Action
runs:
  using: node20
  main: index.js`,
			expected: true,
		},
		{
			name:     "action.yaml by name",
			filename: "action.yaml",
			content: `name: Test Action
runs:
  using: node20
  main: index.js`,
			expected: true,
		},
		{
			name:     "workflow with jobs",
			filename: "workflow.yml",
			content: `name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest`,
			expected: false,
		},
		{
			name:     "action by structure - has runs, no jobs",
			filename: "my-action.yml",
			content: `name: My Action
runs:
  using: composite
  steps:
    - run: echo "test"`,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := isActionDefinitionFile(tt.filename, []byte(tt.content))
			require.NoError(t, err, "Should not error on valid YAML")
			assert.Equal(t, tt.expected, result, "Filename: %s", tt.filename)
		})
	}
}

func TestProcessYAMLWorkflowImport(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("simple workflow with jobs", func(t *testing.T) {
		workflowContent := `name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - run: echo "test"
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "build"`

		workflowFile := filepath.Join(tmpDir, "test-workflow.yml")
		err := os.WriteFile(workflowFile, []byte(workflowContent), 0644)
		require.NoError(t, err, "Should write test workflow file")

		jobs, services, err := processYAMLWorkflowImport(workflowFile)
		require.NoError(t, err, "Should process YAML workflow")
		assert.NotEmpty(t, jobs, "Should extract jobs")
		assert.Contains(t, jobs, "test", "Should contain test job")
		assert.Contains(t, jobs, "build", "Should contain build job")
		assert.Empty(t, services, "Should not have services")
	})

	t.Run("workflow with services", func(t *testing.T) {
		workflowContent := `name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:13
        env:
          POSTGRES_PASSWORD: password
    steps:
      - run: echo "test"`

		workflowFile := filepath.Join(tmpDir, "test-services.yml")
		err := os.WriteFile(workflowFile, []byte(workflowContent), 0644)
		require.NoError(t, err, "Should write test workflow file")

		jobs, services, err := processYAMLWorkflowImport(workflowFile)
		require.NoError(t, err, "Should process YAML workflow")
		assert.NotEmpty(t, jobs, "Should extract jobs")
		assert.NotEmpty(t, services, "Should extract services")
		assert.Contains(t, services, "test_postgres", "Should contain prefixed service name")
	})

	t.Run("reject action definition", func(t *testing.T) {
		actionContent := `name: Test Action
runs:
  using: node20
  main: index.js`

		actionFile := filepath.Join(tmpDir, "action.yml")
		err := os.WriteFile(actionFile, []byte(actionContent), 0644)
		require.NoError(t, err, "Should write test action file")

		_, _, err = processYAMLWorkflowImport(actionFile)
		require.Error(t, err, "Should reject action definition")
		assert.Contains(t, err.Error(), "cannot import action definition", "Error should mention action definition")
	})

	t.Run("reject invalid workflow", func(t *testing.T) {
		invalidContent := `name: Not a Workflow
description: This is not a valid workflow`

		invalidFile := filepath.Join(tmpDir, "invalid.yml")
		err := os.WriteFile(invalidFile, []byte(invalidContent), 0644)
		require.NoError(t, err, "Should write test invalid file")

		_, _, err = processYAMLWorkflowImport(invalidFile)
		require.Error(t, err, "Should reject invalid workflow")
		assert.Contains(t, err.Error(), "not a valid GitHub Actions workflow", "Error should mention invalid workflow")
	})
}

func TestImportYAMLWorkflow(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a simple YAML workflow
	yamlWorkflow := `name: CI Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - run: npm test`

	yamlFile := filepath.Join(tmpDir, "ci.yml")
	err := os.WriteFile(yamlFile, []byte(yamlWorkflow), 0644)
	require.NoError(t, err, "Should create YAML workflow file")

	// Create a markdown workflow that imports the YAML workflow
	mdWorkflow := `---
name: Main Workflow
on: issue_comment
imports:
  - ci.yml
---

# Main Workflow
This imports a YAML workflow.`

	mdFile := filepath.Join(tmpDir, "main.md")
	err = os.WriteFile(mdFile, []byte(mdWorkflow), 0644)
	require.NoError(t, err, "Should create markdown workflow file")

	// Process imports
	result, err := ExtractFrontmatterFromContent(mdWorkflow)
	require.NoError(t, err, "Should extract frontmatter")

	importsResult, err := ProcessImportsFromFrontmatterWithManifest(result.Frontmatter, tmpDir, nil)
	require.NoError(t, err, "Should process imports")

	// Verify jobs were imported
	assert.NotEmpty(t, importsResult.MergedJobs, "Should have merged jobs from YAML workflow")
	assert.Contains(t, importsResult.MergedJobs, "test", "Should contain test job from YAML workflow")
}

func TestRejectLockYMLImport(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a lock file
	lockContent := `# This is a compiled lock file
name: Compiled Workflow
jobs:
  test:
    runs-on: ubuntu-latest`

	lockFile := filepath.Join(tmpDir, "workflow.lock.yml")
	err := os.WriteFile(lockFile, []byte(lockContent), 0644)
	require.NoError(t, err, "Should create lock file")

	// Create a markdown workflow that tries to import the lock file
	mdWorkflow := `---
name: Main Workflow
on: push
imports:
  - workflow.lock.yml
---

# Main Workflow`

	mdFile := filepath.Join(tmpDir, "main.md")
	err = os.WriteFile(mdFile, []byte(mdWorkflow), 0644)
	require.NoError(t, err, "Should create markdown workflow file")

	// Process imports - should fail
	result, err := ExtractFrontmatterFromContent(mdWorkflow)
	require.NoError(t, err, "Should extract frontmatter")

	_, err = ProcessImportsFromFrontmatterWithManifest(result.Frontmatter, tmpDir, nil)
	require.Error(t, err, "Should reject .lock.yml import")
	assert.Contains(t, err.Error(), "cannot import .lock.yml files", "Error should mention .lock.yml rejection")
	assert.Contains(t, err.Error(), "Import the source .md file instead", "Error should suggest importing .md file")
}
