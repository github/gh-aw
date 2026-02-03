//go:build !integration

package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsCopilotSetupStepsFile(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "copilot-setup-steps.yml",
			filePath: ".github/workflows/copilot-setup-steps.yml",
			expected: true,
		},
		{
			name:     "copilot-setup-steps.yml uppercase",
			filePath: ".github/workflows/Copilot-Setup-Steps.yml",
			expected: true,
		},
		{
			name:     "other yml file",
			filePath: ".github/workflows/ci.yml",
			expected: false,
		},
		{
			name:     "copilot-setup-steps.yaml (also supported)",
			filePath: ".github/workflows/copilot-setup-steps.yaml",
			expected: true,
		},
		{
			name:     "just filename",
			filePath: "copilot-setup-steps.yml",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCopilotSetupStepsFile(tt.filePath)
			assert.Equal(t, tt.expected, result, "File: %s", tt.filePath)
		})
	}
}

func TestExtractStepsFromCopilotSetup(t *testing.T) {
	// Create a sample copilot-setup-steps.yml workflow
	workflow := map[string]any{
		"name": "Copilot Setup Steps",
		"on":   "workflow_dispatch",
		"jobs": map[string]any{
			"copilot-setup-steps": map[string]any{
				"runs-on": "ubuntu-latest",
				"permissions": map[string]any{
					"contents": "read",
				},
				"steps": []any{
					map[string]any{
						"name": "Install gh-aw extension",
						"run":  "curl -fsSL https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh | bash",
					},
					map[string]any{
						"name": "Checkout code",
						"uses": "actions/checkout@v4",
					},
					map[string]any{
						"name": "Set up Node.js",
						"uses": "actions/setup-node@v4",
						"with": map[string]any{
							"node-version": "20",
						},
					},
				},
			},
		},
	}

	stepsYAML, err := extractStepsFromCopilotSetup(workflow)
	require.NoError(t, err, "Should extract steps without error")
	require.NotEmpty(t, stepsYAML, "Should return non-empty steps YAML")

	// Verify the YAML contains the expected content (as a YAML array, not with "steps:" wrapper)
	assert.NotContains(t, stepsYAML, "steps:", "Should NOT contain steps field wrapper")
	assert.Contains(t, stepsYAML, "Install gh-aw extension", "Should contain install step")
	assert.Contains(t, stepsYAML, "Checkout code", "Should contain checkout step")
	assert.Contains(t, stepsYAML, "Set up Node.js", "Should contain Node.js setup step")
	assert.Contains(t, stepsYAML, "actions/checkout@v4", "Should contain checkout action")
	assert.Contains(t, stepsYAML, "actions/setup-node@v4", "Should contain Node.js setup action")

	// Verify it's formatted as a YAML array (starts with "- name:")
	assert.True(t, strings.HasPrefix(stepsYAML, "- "), "Should start with YAML array format")

	// Verify it doesn't contain job-level fields
	assert.NotContains(t, stepsYAML, "runs-on:", "Should not contain runs-on")
	assert.NotContains(t, stepsYAML, "permissions:", "Should not contain permissions")
}

func TestExtractStepsFromCopilotSetup_MissingJob(t *testing.T) {
	workflow := map[string]any{
		"name": "Copilot Setup Steps",
		"on":   "workflow_dispatch",
		"jobs": map[string]any{
			"other-job": map[string]any{
				"runs-on": "ubuntu-latest",
				"steps": []any{
					map[string]any{
						"name": "Some step",
						"run":  "echo hello",
					},
				},
			},
		},
	}

	_, err := extractStepsFromCopilotSetup(workflow)
	require.Error(t, err, "Should error when copilot-setup-steps job is missing")
	assert.Contains(t, err.Error(), "copilot-setup-steps job not found", "Error should mention missing job")
}

func TestExtractStepsFromCopilotSetup_NoSteps(t *testing.T) {
	workflow := map[string]any{
		"name": "Copilot Setup Steps",
		"on":   "workflow_dispatch",
		"jobs": map[string]any{
			"copilot-setup-steps": map[string]any{
				"runs-on": "ubuntu-latest",
			},
		},
	}

	_, err := extractStepsFromCopilotSetup(workflow)
	require.Error(t, err, "Should error when steps are missing")
	assert.Contains(t, err.Error(), "no steps found", "Error should mention missing steps")
}

func TestExtractStepsFromCopilotSetup_EnsuresCheckoutFirst(t *testing.T) {
	// Test workflow with checkout step NOT first
	workflow := map[string]any{
		"name": "Copilot Setup Steps",
		"on":   "workflow_dispatch",
		"jobs": map[string]any{
			"copilot-setup-steps": map[string]any{
				"runs-on": "ubuntu-latest",
				"steps": []any{
					map[string]any{
						"name": "Install gh-aw extension",
						"run":  "curl -fsSL https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh | bash",
					},
					map[string]any{
						"name": "Checkout code",
						"uses": "actions/checkout@v4",
					},
					map[string]any{
						"name": "Set up Node.js",
						"uses": "actions/setup-node@v4",
						"with": map[string]any{
							"node-version": "20",
						},
					},
				},
			},
		},
	}

	stepsYAML, err := extractStepsFromCopilotSetup(workflow)
	require.NoError(t, err, "Should extract steps without error")
	require.NotEmpty(t, stepsYAML, "Should return non-empty steps YAML")

	// Verify checkout step is first
	lines := strings.Split(stepsYAML, "\n")
	var firstStepName string
	for _, line := range lines {
		if strings.Contains(line, "name:") {
			firstStepName = line
			break
		}
	}
	assert.Contains(t, firstStepName, "Checkout code", "First step should be the checkout step")

	// Verify all steps are present
	assert.Contains(t, stepsYAML, "Install gh-aw extension", "Should contain install step")
	assert.Contains(t, stepsYAML, "Checkout code", "Should contain checkout step")
	assert.Contains(t, stepsYAML, "Set up Node.js", "Should contain Node.js setup step")

	// Verify checkout comes before install (order should be: checkout, install, node)
	checkoutIndex := strings.Index(stepsYAML, "Checkout code")
	installIndex := strings.Index(stepsYAML, "Install gh-aw extension")
	nodeIndex := strings.Index(stepsYAML, "Set up Node.js")
	assert.Less(t, checkoutIndex, installIndex, "Checkout should come before install")
	assert.Less(t, installIndex, nodeIndex, "Install should come before Node.js setup")
}

func TestExtractStepsFromCopilotSetup_AddsCheckoutIfMissing(t *testing.T) {
	// Test workflow without any checkout step
	workflow := map[string]any{
		"name": "Copilot Setup Steps",
		"on":   "workflow_dispatch",
		"jobs": map[string]any{
			"copilot-setup-steps": map[string]any{
				"runs-on": "ubuntu-latest",
				"steps": []any{
					map[string]any{
						"name": "Install dependencies",
						"run":  "npm install",
					},
					map[string]any{
						"name": "Run linter",
						"run":  "npm run lint",
					},
				},
			},
		},
	}

	stepsYAML, err := extractStepsFromCopilotSetup(workflow)
	require.NoError(t, err, "Should extract steps without error")
	require.NotEmpty(t, stepsYAML, "Should return non-empty steps YAML")

	// Verify checkout step was added
	assert.Contains(t, stepsYAML, "Checkout code", "Should contain added checkout step")
	assert.Contains(t, stepsYAML, "actions/checkout@v4", "Should contain checkout action")

	// Verify checkout step is first
	lines := strings.Split(stepsYAML, "\n")
	var firstStepName string
	for _, line := range lines {
		if strings.Contains(line, "name:") {
			firstStepName = line
			break
		}
	}
	assert.Contains(t, firstStepName, "Checkout code", "First step should be the checkout step")

	// Verify original steps are still present
	assert.Contains(t, stepsYAML, "Install dependencies", "Should contain original install step")
	assert.Contains(t, stepsYAML, "Run linter", "Should contain original linter step")

	// Verify checkout comes before other steps
	checkoutIndex := strings.Index(stepsYAML, "Checkout code")
	installIndex := strings.Index(stepsYAML, "Install dependencies")
	lintIndex := strings.Index(stepsYAML, "Run linter")
	assert.Less(t, checkoutIndex, installIndex, "Checkout should come before install")
	assert.Less(t, checkoutIndex, lintIndex, "Checkout should come before lint")
}

func TestExtractStepsFromCopilotSetup_CheckoutAlreadyFirst(t *testing.T) {
	// Test workflow with checkout step already first
	workflow := map[string]any{
		"name": "Copilot Setup Steps",
		"on":   "workflow_dispatch",
		"jobs": map[string]any{
			"copilot-setup-steps": map[string]any{
				"runs-on": "ubuntu-latest",
				"steps": []any{
					map[string]any{
						"name": "Checkout code",
						"uses": "actions/checkout@v4",
					},
					map[string]any{
						"name": "Install dependencies",
						"run":  "npm install",
					},
					map[string]any{
						"name": "Run tests",
						"run":  "npm test",
					},
				},
			},
		},
	}

	stepsYAML, err := extractStepsFromCopilotSetup(workflow)
	require.NoError(t, err, "Should extract steps without error")
	require.NotEmpty(t, stepsYAML, "Should return non-empty steps YAML")

	// Verify checkout step is first
	lines := strings.Split(stepsYAML, "\n")
	var firstStepName string
	for _, line := range lines {
		if strings.Contains(line, "name:") {
			firstStepName = line
			break
		}
	}
	assert.Contains(t, firstStepName, "Checkout code", "First step should be the checkout step")

	// Verify all steps are present
	assert.Contains(t, stepsYAML, "Checkout code", "Should contain checkout step")
	assert.Contains(t, stepsYAML, "Install dependencies", "Should contain install step")
	assert.Contains(t, stepsYAML, "Run tests", "Should contain test step")

	// Verify order is maintained
	checkoutIndex := strings.Index(stepsYAML, "Checkout code")
	installIndex := strings.Index(stepsYAML, "Install dependencies")
	testIndex := strings.Index(stepsYAML, "Run tests")
	assert.Less(t, checkoutIndex, installIndex, "Checkout should come before install")
	assert.Less(t, installIndex, testIndex, "Install should come before tests")
}

func TestProcessYAMLWorkflowImport_CopilotSetupSteps(t *testing.T) {
	// Create a temporary copilot-setup-steps.yml file
	tmpDir, err := os.MkdirTemp("", "copilot-setup-test*")
	require.NoError(t, err, "Failed to create temp directory")
	defer os.RemoveAll(tmpDir)

	workflowContent := `name: Copilot Setup Steps
on:
  workflow_dispatch:
  push:
    paths:
      - .github/workflows/copilot-setup-steps.yml

jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - name: Install gh-aw extension
        run: curl -fsSL https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh | bash
      - name: Checkout code
        uses: actions/checkout@v4
      - name: Set up Node.js
        uses: actions/setup-node@v4
        with:
          node-version: "20"
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: "1.21"
`

	workflowFile := filepath.Join(tmpDir, "copilot-setup-steps.yml")
	err = os.WriteFile(workflowFile, []byte(workflowContent), 0600)
	require.NoError(t, err, "Failed to write workflow file")

	// Process the file
	stepsYAML, services, err := processYAMLWorkflowImport(workflowFile)
	require.NoError(t, err, "Should process copilot-setup-steps.yml without error")
	assert.Empty(t, services, "Should not extract services from copilot-setup-steps.yml")
	assert.NotEmpty(t, stepsYAML, "Should return steps YAML")

	// Verify the steps YAML contains expected content (as a YAML array)
	assert.NotContains(t, stepsYAML, "steps:", "Should NOT contain steps field wrapper")
	assert.Contains(t, stepsYAML, "Install gh-aw extension", "Should contain install step")
	assert.Contains(t, stepsYAML, "Checkout code", "Should contain checkout step")
	assert.Contains(t, stepsYAML, "Set up Node.js", "Should contain Node.js setup step")
	assert.Contains(t, stepsYAML, "Set up Go", "Should contain Go setup step")

	// Verify it's formatted as a YAML array
	assert.True(t, strings.HasPrefix(stepsYAML, "- "), "Should start with YAML array format")

	// Verify it doesn't contain job-level fields
	assert.NotContains(t, stepsYAML, "runs-on:", "Should not contain runs-on")
	assert.NotContains(t, stepsYAML, "permissions:", "Should not contain permissions")
	assert.NotContains(t, stepsYAML, "jobs:", "Should not contain jobs field")
}

func TestProcessYAMLWorkflowImport_RegularWorkflow(t *testing.T) {
	// Create a temporary regular workflow file
	tmpDir, err := os.MkdirTemp("", "regular-workflow-test*")
	require.NoError(t, err, "Failed to create temp directory")
	defer os.RemoveAll(tmpDir)

	workflowContent := `name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
      - name: Run tests
        run: npm test
`

	workflowFile := filepath.Join(tmpDir, "test.yml")
	err = os.WriteFile(workflowFile, []byte(workflowContent), 0600)
	require.NoError(t, err, "Failed to write workflow file")

	// Process the file
	jobsJSON, services, err := processYAMLWorkflowImport(workflowFile)
	require.NoError(t, err, "Should process regular workflow without error")
	assert.Empty(t, services, "Should not extract services from this workflow")
	assert.NotEmpty(t, jobsJSON, "Should return jobs JSON")

	// Verify the jobs JSON contains the test job
	assert.Contains(t, jobsJSON, "test", "Should contain test job")
	assert.Contains(t, jobsJSON, "ubuntu-latest", "Should contain runs-on")
}

func TestImportCopilotSetupStepsInWorkflow(t *testing.T) {
	// Create a temporary directory structure
	tmpDir, err := os.MkdirTemp("", "import-copilot-setup-test*")
	require.NoError(t, err, "Failed to create temp directory")
	defer os.RemoveAll(tmpDir)

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err = os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create copilot-setup-steps.yml
	copilotSetupContent := `name: Copilot Setup Steps
on: workflow_dispatch
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - name: Install gh-aw extension
        run: curl -fsSL https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh | bash
      - name: Set up Node.js
        uses: actions/setup-node@v4
        with:
          node-version: "20"
`
	copilotSetupFile := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	err = os.WriteFile(copilotSetupFile, []byte(copilotSetupContent), 0600)
	require.NoError(t, err, "Failed to write copilot-setup-steps.yml")

	// Create a workflow that imports copilot-setup-steps.yml
	workflowContent := `---
name: Test Workflow
on: issue_comment
imports:
  - copilot-setup-steps.yml
engine: copilot
---

# Test Workflow

This workflow imports copilot-setup-steps.yml.
`
	workflowFile := filepath.Join(workflowsDir, "test-workflow.md")
	err = os.WriteFile(workflowFile, []byte(workflowContent), 0600)
	require.NoError(t, err, "Failed to write test workflow")

	// Read the workflow content
	content, err := os.ReadFile(workflowFile)
	require.NoError(t, err, "Failed to read workflow file")

	// Extract frontmatter from the workflow
	result, err := ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err, "Failed to extract frontmatter")

	// Process imports
	importsResult, err := ProcessImportsFromFrontmatterWithManifest(result.Frontmatter, workflowsDir, nil)
	require.NoError(t, err, "Failed to process imports")

	// Verify that steps were extracted to CopilotSetupSteps (not MergedSteps or MergedJobs)
	assert.NotEmpty(t, importsResult.CopilotSetupSteps, "Should have copilot-setup steps from copilot-setup-steps.yml")
	assert.Empty(t, importsResult.MergedSteps, "Should not have regular merged steps (copilot-setup goes to separate field)")
	assert.Empty(t, importsResult.MergedJobs, "Should not have merged jobs from copilot-setup-steps.yml")

	// Verify the copilot-setup steps contain expected content
	assert.Contains(t, importsResult.CopilotSetupSteps, "Install gh-aw extension", "Should contain install step")
	assert.Contains(t, importsResult.CopilotSetupSteps, "Set up Node.js", "Should contain Node.js setup step")

	// Verify it doesn't contain job-level fields
	stepsLower := strings.ToLower(importsResult.CopilotSetupSteps)
	assert.NotContains(t, stepsLower, "runs-on", "Should not contain runs-on in steps")
	assert.NotContains(t, stepsLower, "permissions", "Should not contain permissions in steps")
}
