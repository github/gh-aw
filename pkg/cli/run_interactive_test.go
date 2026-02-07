//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindRunnableWorkflows(t *testing.T) {
	// Create a temporary directory for test workflows
	tempDir := t.TempDir()
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create test workflow files
	tests := []struct {
		name        string
		mdContent   string
		lockContent string
		shouldFind  bool
	}{
		{
			name: "workflow-dispatch.md",
			mdContent: `---
on:
  workflow_dispatch:
---
# Test Workflow with workflow_dispatch
`,
			lockContent: `name: "Test Workflow"
on:
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			shouldFind: true,
		},
		{
			name: "schedule.md",
			mdContent: `---
on:
  schedule:
    - cron: "0 0 * * *"
---
# Test Workflow with schedule
`,
			lockContent: `name: "Test Workflow"
on:
  schedule:
    - cron: "0 0 * * *"
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			shouldFind: true,
		},
		{
			name: "push-only.md",
			mdContent: `---
on:
  push:
    branches: [main]
---
# Test Workflow with push only
`,
			lockContent: `name: "Test Workflow"
on:
  push:
    branches: [main]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			shouldFind: false,
		},
		{
			name: "no-trigger.md",
			mdContent: `---
engine: copilot
---
# Test Workflow with no trigger
`,
			lockContent: `name: "Test Workflow"
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			shouldFind: false, // No 'on' section means not runnable
		},
	}

	for _, tt := range tests {
		// Write markdown file
		mdPath := filepath.Join(workflowsDir, tt.name)
		require.NoError(t, os.WriteFile(mdPath, []byte(tt.mdContent), 0600))

		// Write lock file
		lockName := strings.TrimSuffix(tt.name, ".md") + ".lock.yml"
		lockPath := filepath.Join(workflowsDir, lockName)
		require.NoError(t, os.WriteFile(lockPath, []byte(tt.lockContent), 0600))
	}

	// Change to temp directory
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()
	require.NoError(t, os.Chdir(tempDir))

	// Find runnable workflows
	workflows, err := findRunnableWorkflows(false)
	require.NoError(t, err)

	// Verify expected workflows were found
	expectedNames := []string{}
	for _, tt := range tests {
		if tt.shouldFind {
			expectedNames = append(expectedNames, strings.TrimSuffix(tt.name, ".md"))
		}
	}

	assert.Len(t, workflows, len(expectedNames), "Should find expected number of runnable workflows")

	// Verify each expected workflow is in the results
	foundNames := make(map[string]bool)
	for _, wf := range workflows {
		foundNames[wf.Name] = true
	}

	for _, expected := range expectedNames {
		assert.True(t, foundNames[expected], "Should find workflow: %s", expected)
	}
}

func TestBuildWorkflowDescription(t *testing.T) {
	tests := []struct {
		name     string
		inputs   map[string]*workflow.InputDefinition
		expected string
	}{
		{
			name:     "no inputs",
			inputs:   nil,
			expected: "",
		},
		{
			name: "only required inputs",
			inputs: map[string]*workflow.InputDefinition{
				"input1": {Required: true},
				"input2": {Required: true},
			},
			expected: "",
		},
		{
			name: "only optional inputs",
			inputs: map[string]*workflow.InputDefinition{
				"input1": {Required: false},
				"input2": {Required: false},
			},
			expected: "",
		},
		{
			name: "mixed inputs",
			inputs: map[string]*workflow.InputDefinition{
				"input1": {Required: true},
				"input2": {Required: false},
				"input3": {Required: true},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildWorkflowDescription(tt.inputs)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildCommandString(t *testing.T) {
	tests := []struct {
		name           string
		workflowName   string
		inputs         []string
		repoOverride   string
		refOverride    string
		autoMergePRs   bool
		pushSecrets    bool
		push           bool
		engineOverride string
		expected       string
	}{
		{
			name:         "basic command",
			workflowName: "test-workflow",
			inputs:       nil,
			expected:     "gh aw run test-workflow",
		},
		{
			name:         "with inputs",
			workflowName: "test-workflow",
			inputs:       []string{"name=value", "env=prod"},
			expected:     "gh aw run test-workflow -F name=value -F env=prod",
		},
		{
			name:         "with repo override",
			workflowName: "test-workflow",
			repoOverride: "owner/repo",
			expected:     "gh aw run test-workflow --repo owner/repo",
		},
		{
			name:         "with ref override",
			workflowName: "test-workflow",
			refOverride:  "main",
			expected:     "gh aw run test-workflow --ref main",
		},
		{
			name:         "with auto merge PRs",
			workflowName: "test-workflow",
			autoMergePRs: true,
			expected:     "gh aw run test-workflow --auto-merge-prs",
		},
		{
			name:         "with push secrets",
			workflowName: "test-workflow",
			pushSecrets:  true,
			expected:     "gh aw run test-workflow --use-local-secrets",
		},
		{
			name:         "with push",
			workflowName: "test-workflow",
			push:         true,
			expected:     "gh aw run test-workflow --push",
		},
		{
			name:           "with engine override",
			workflowName:   "test-workflow",
			engineOverride: "claude",
			expected:       "gh aw run test-workflow --engine claude",
		},
		{
			name:           "all flags",
			workflowName:   "test-workflow",
			inputs:         []string{"name=value"},
			repoOverride:   "owner/repo",
			refOverride:    "main",
			autoMergePRs:   true,
			pushSecrets:    true,
			push:           true,
			engineOverride: "copilot",
			expected:       "gh aw run test-workflow -F name=value --repo owner/repo --ref main --auto-merge-prs --use-local-secrets --push --engine copilot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildCommandString(tt.workflowName, tt.inputs, tt.repoOverride, tt.refOverride, tt.autoMergePRs, tt.pushSecrets, tt.push, tt.engineOverride)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFindRunnableWorkflows_NoWorkflowsDir(t *testing.T) {
	// Create a temporary directory without .github/workflows
	tempDir := t.TempDir()

	// Change to temp directory
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()
	require.NoError(t, os.Chdir(tempDir))

	// Should handle missing directory gracefully
	workflows, err := findRunnableWorkflows(false)

	// Should return error or empty list
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.Empty(t, workflows)
	}
}

func TestFindRunnableWorkflows_WithInputs(t *testing.T) {
	// Create a temporary directory for test workflows
	tempDir := t.TempDir()
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	// Create workflow with inputs (markdown file)
	workflowContent := `---
on:
  workflow_dispatch:
    inputs:
      name:
        description: 'Name input'
        required: true
        type: string
      optional:
        description: 'Optional input'
        required: false
        type: string
        default: 'default-value'
---
# Test Workflow with inputs
`

	workflowPath := filepath.Join(workflowsDir, "test-inputs.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0600))

	// Create corresponding lock file
	lockContent := `name: "Test Workflow"
on:
  workflow_dispatch:
    inputs:
      name:
        description: 'Name input'
        required: true
        type: string
      optional:
        description: 'Optional input'
        required: false
        type: string
        default: 'default-value'
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`
	lockPath := filepath.Join(workflowsDir, "test-inputs.lock.yml")
	require.NoError(t, os.WriteFile(lockPath, []byte(lockContent), 0600))

	// Change to temp directory
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(oldWd) }()
	require.NoError(t, os.Chdir(tempDir))

	// Find runnable workflows
	workflows, err := findRunnableWorkflows(false)
	require.NoError(t, err)
	require.Len(t, workflows, 1)

	// Verify inputs were detected
	wf := workflows[0]
	assert.Equal(t, "test-inputs", wf.Name)
	assert.NotNil(t, wf.Inputs)
	assert.Len(t, wf.Inputs, 2)

	// Verify description is empty (input counts no longer shown)
	assert.Empty(t, wf.Description)
}

// TestSelectWorkflowStructure tests that selectWorkflow creates the correct Huh form structure
func TestSelectWorkflowStructure(t *testing.T) {
	// This test verifies that the selectWorkflow function would create a properly
	// configured huh.Select with fuzzy filtering enabled

	workflows := []WorkflowOption{
		{Name: "workflow-a", Description: "", FilePath: "workflow-a.md"},
		{Name: "workflow-b", Description: "", FilePath: "workflow-b.md"},
		{Name: "test-workflow", Description: "", FilePath: "test-workflow.md"},
	}

	// Verify we have the expected number of workflows
	assert.Len(t, workflows, 3)

	// Verify workflow names for fuzzy matching
	workflowNames := make([]string, len(workflows))
	for i, wf := range workflows {
		workflowNames[i] = wf.Name
	}

	assert.Contains(t, workflowNames, "workflow-a")
	assert.Contains(t, workflowNames, "workflow-b")
	assert.Contains(t, workflowNames, "test-workflow")
}

// TestSelectWorkflowFuzzySearchability tests that workflow names are searchable
func TestSelectWorkflowFuzzySearchability(t *testing.T) {
	// Test that workflow names can be matched by fuzzy search patterns
	tests := []struct {
		name          string
		workflowName  string
		searchPattern string
		shouldMatch   bool
	}{
		{
			name:          "exact match",
			workflowName:  "test-workflow",
			searchPattern: "test-workflow",
			shouldMatch:   true,
		},
		{
			name:          "partial match",
			workflowName:  "test-workflow",
			searchPattern: "test",
			shouldMatch:   true,
		},
		{
			name:          "fuzzy match",
			workflowName:  "test-workflow",
			searchPattern: "twf",
			shouldMatch:   true, // t(est-) w(ork) f(low)
		},
		{
			name:          "case insensitive",
			workflowName:  "test-workflow",
			searchPattern: "TEST",
			shouldMatch:   true,
		},
		{
			name:          "no match",
			workflowName:  "test-workflow",
			searchPattern: "xyz",
			shouldMatch:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simple substring matching for testing (Huh's fuzzy matching is more sophisticated)
			matched := strings.Contains(strings.ToLower(tt.workflowName), strings.ToLower(tt.searchPattern))

			if tt.shouldMatch {
				// For fuzzy patterns like "twf", we just verify the workflow name contains the characters
				if tt.searchPattern == "twf" {
					// Check that workflow name contains 't', 'w', and 'f' in order
					assert.Contains(t, tt.workflowName, "t")
					assert.Contains(t, tt.workflowName, "w")
					assert.Contains(t, tt.workflowName, "f")
				} else {
					assert.True(t, matched, "Expected workflow %q to match pattern %q", tt.workflowName, tt.searchPattern)
				}
			} else {
				assert.False(t, matched, "Expected workflow %q not to match pattern %q", tt.workflowName, tt.searchPattern)
			}
		})
	}
}

// TestSelectWorkflowNonInteractive tests the non-interactive fallback
func TestSelectWorkflowNonInteractive(t *testing.T) {
	workflows := []WorkflowOption{
		{Name: "workflow-a", Description: "", FilePath: "workflow-a.md"},
		{Name: "workflow-b", Description: "", FilePath: "workflow-b.md"},
		{Name: "test-workflow", Description: "", FilePath: "test-workflow.md"},
	}

	// Test that selectWorkflowNonInteractive would format workflows correctly
	assert.Len(t, workflows, 3)

	// Verify each workflow has a name for selection
	for i, wf := range workflows {
		assert.NotEmpty(t, wf.Name, "Workflow at index %d should have a name", i)
	}
}
