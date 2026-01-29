//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetWorkflowInputs(t *testing.T) {
	tests := []struct {
		name          string
		lockContent   string
		expectedCount int
		expectedReq   map[string]bool // map of input name to required status
	}{
		{
			name: "workflow with required and optional inputs",
			lockContent: `name: "Test Workflow"
on:
  workflow_dispatch:
    inputs:
      issue_url:
        description: 'Issue URL'
        required: true
        type: string
      debug_mode:
        description: 'Enable debug mode'
        required: false
        type: boolean
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expectedCount: 2,
			expectedReq: map[string]bool{
				"issue_url":  true,
				"debug_mode": false,
			},
		},
		{
			name: "workflow with no inputs",
			lockContent: `name: "Test Workflow"
on:
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expectedCount: 0,
		},
		{
			name: "workflow without workflow_dispatch",
			lockContent: `name: "Test Workflow"
on:
  issues:
    types: [opened]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "test-workflow.md")
			lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")

			// Create markdown file (content doesn't matter)
			if err := os.WriteFile(tmpFile, []byte("# Test"), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Create lock file with the actual workflow content
			if err := os.WriteFile(lockFile, []byte(tt.lockContent), 0644); err != nil {
				t.Fatalf("Failed to write lock file: %v", err)
			}

			// Extract inputs
			inputs, err := getWorkflowInputs(tmpFile)
			if err != nil {
				t.Fatalf("getWorkflowInputs() error = %v", err)
			}

			// Check count
			if len(inputs) != tt.expectedCount {
				t.Errorf("Expected %d inputs, got %d", tt.expectedCount, len(inputs))
			}

			// Check required status
			for name, expectedReq := range tt.expectedReq {
				input, exists := inputs[name]
				if !exists {
					t.Errorf("Expected input '%s' not found", name)
					continue
				}
				if input.Required != expectedReq {
					t.Errorf("Input '%s': expected required=%v, got %v", name, expectedReq, input.Required)
				}
			}
		})
	}
}

func TestValidateWorkflowInputs(t *testing.T) {
	tests := []struct {
		name           string
		lockContent    string
		providedInputs []string
		expectError    bool
		errorContains  []string // strings that should be in the error message
	}{
		{
			name: "all required inputs provided",
			lockContent: `name: "Test Workflow"
on:
  workflow_dispatch:
    inputs:
      issue_url:
        description: 'Issue URL'
        required: true
        type: string
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			providedInputs: []string{"issue_url=https://github.com/owner/repo/issues/123"},
			expectError:    false,
		},
		{
			name: "missing required input",
			lockContent: `name: "Test Workflow"
on:
  workflow_dispatch:
    inputs:
      issue_url:
        description: 'Issue URL'
        required: true
        type: string
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			providedInputs: []string{},
			expectError:    true,
			errorContains:  []string{"Missing required input(s)", "issue_url"},
		},
		{
			name: "typo in input name",
			lockContent: `name: "Test Workflow"
on:
  workflow_dispatch:
    inputs:
      issue_url:
        description: 'Issue URL'
        required: true
        type: string
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			providedInputs: []string{"issue_ur=https://github.com/owner/repo/issues/123"},
			expectError:    true,
			errorContains:  []string{"Invalid input name", "issue_ur", "issue_url"},
		},
		{
			name: "multiple errors: missing required and typo",
			lockContent: `name: "Test Workflow"
on:
  workflow_dispatch:
    inputs:
      issue_url:
        description: 'Issue URL'
        required: true
        type: string
      debug_mode:
        description: 'Debug mode'
        required: true
        type: boolean
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			providedInputs: []string{"debugmode=true"},
			expectError:    true,
			errorContains:  []string{"Missing required input(s)", "issue_url", "Invalid input name", "debugmode"},
		},
		{
			name: "no inputs defined",
			lockContent: `name: "Test Workflow"
on:
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			providedInputs: []string{"any_input=value"},
			expectError:    false, // No inputs defined, so no validation
		},
		{
			name: "optional input not provided - should not error",
			lockContent: `name: "Test Workflow"
on:
  workflow_dispatch:
    inputs:
      issue_url:
        description: 'Issue URL'
        required: false
        type: string
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			providedInputs: []string{},
			expectError:    false,
		},
		{
			name: "unknown input with no close matches",
			lockContent: `name: "Test Workflow"
on:
  workflow_dispatch:
    inputs:
      config_file:
        description: 'Config file path'
        required: false
        type: string
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			providedInputs: []string{"xyz=value"},
			expectError:    true,
			errorContains:  []string{"Invalid input name", "xyz", "not a valid input name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "test-workflow.md")
			lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")

			// Create markdown file (content doesn't matter)
			if err := os.WriteFile(tmpFile, []byte("# Test"), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Create lock file with the actual workflow content
			if err := os.WriteFile(lockFile, []byte(tt.lockContent), 0644); err != nil {
				t.Fatalf("Failed to write lock file: %v", err)
			}

			// Validate inputs
			err := validateWorkflowInputs(tmpFile, tt.providedInputs)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else {
					errStr := err.Error()
					for _, expected := range tt.errorContains {
						if !strings.Contains(errStr, expected) {
							t.Errorf("Expected error to contain '%s', but got: %s", expected, errStr)
						}
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}
