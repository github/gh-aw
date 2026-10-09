//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRemoteWorkflowPaginated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake gh is not supported on Windows")
	}

	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.log")
	responsePath := filepath.Join(dir, "response.json")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"" + argsPath + "\"\ncat \"" + responsePath + "\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	tests := []struct {
		name      string
		repo      string
		response  string
		wantError string
		wantHost  string
	}{
		{
			name:     "workflow on later page",
			repo:     "owner/repo",
			response: `[{"workflows":[{"id":1,"path":".github/workflows/first.lock.yml","state":"active"}]},{"workflows":[{"id":2,"name":"ESLint Factory Dispatcher","path":".github/workflows/eslint-factory-dispatcher.lock.yml","state":"active"}]}]`,
		},
		{
			name:     "disabled workflow is discoverable",
			repo:     "owner/repo",
			response: `[{"workflows":[{"id":2,"path":".github/workflows/eslint-factory-dispatcher.lock.yml","state":"disabled_manually"}]}]`,
		},
		{
			name:     "enterprise host",
			repo:     "github.example.com/owner/repo",
			response: `[{"workflows":[{"id":2,"path":".github/workflows/eslint-factory-dispatcher.lock.yml","state":"active"}]}]`,
			wantHost: "github.example.com",
		},
		{
			name:      "similarly suffixed workflow is not a match",
			repo:      "owner/repo",
			response:  `[{"workflows":[{"id":2,"path":".github/workflows/not-eslint-factory-dispatcher.lock.yml","state":"active"}]}]`,
			wantError: "not found in repository",
		},
		{
			name:      "empty inventory",
			repo:      "owner/repo",
			response:  `[{"workflows":[]}]`,
			wantError: "not found in repository",
		},
		{
			name:      "malformed response",
			repo:      "owner/repo",
			response:  `invalid-json`,
			wantError: "invalid JSON",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(responsePath, []byte(tt.response), 0o600))
			err := validateRemoteWorkflow(".github/workflows/eslint-factory-dispatcher.md", tt.repo, true)
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantError)
			}
			args, err := os.ReadFile(argsPath)
			require.NoError(t, err)
			assert.Contains(t, string(args), "api --paginate --slurp repos/owner/repo/actions/workflows?per_page=100")
			if tt.wantHost != "" {
				assert.Contains(t, string(args), "--hostname "+tt.wantHost)
			}
		})
	}
}

func TestGetLockFilePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		markdownPath string
		expected     string
	}{
		{
			name:         "regular workflow",
			markdownPath: "/path/to/workflow.md",
			expected:     "/path/to/workflow.lock.yml",
		},
		{
			name:         "workflow in nested directory",
			markdownPath: "/path/to/workflows/nested/workflow.md",
			expected:     "/path/to/workflows/nested/workflow.lock.yml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := getLockFilePath(tt.markdownPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsRunnable_WithLockFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		markdownFile   string
		lockFileYAML   string
		expectRunnable bool
		expectError    bool
		errorContains  string
	}{
		{
			name:         "workflow with workflow_dispatch trigger",
			markdownFile: "test-workflow.md",
			lockFileYAML: `name: "Test Workflow"
on:
  workflow_dispatch:
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expectRunnable: true,
			expectError:    false,
		},
		{
			name:         "workflow with schedule and workflow_dispatch",
			markdownFile: "daily-workflow.md",
			lockFileYAML: `name: "Daily Workflow"
on:
  schedule:
    - cron: "0 0 * * *"
  workflow_dispatch:
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expectRunnable: true,
			expectError:    false,
		},
		{
			name:         "workflow without workflow_dispatch",
			markdownFile: "pr-workflow.md",
			lockFileYAML: `name: "PR Workflow"
on:
  pull_request:
    types: [opened]
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expectRunnable: false,
			expectError:    false,
		},
		{
			name:         "workflow with no triggers",
			markdownFile: "manual-workflow.md",
			lockFileYAML: `name: "Manual Workflow"
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expectRunnable: false,
			expectError:    false,
		},
		{
			name:           "workflow without lock file",
			markdownFile:   "uncompiled-workflow.md",
			lockFileYAML:   "", // No lock file
			expectRunnable: false,
			expectError:    true,
			errorContains:  "has not been compiled yet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory
			tmpDir := t.TempDir()
			markdownPath := filepath.Join(tmpDir, tt.markdownFile)
			lockPath := getLockFilePath(markdownPath)

			// Create markdown file (content doesn't matter for this test)
			err := os.WriteFile(markdownPath, []byte("# Test"), 0644)
			require.NoError(t, err)

			// Create lock file if provided
			if tt.lockFileYAML != "" {
				err = os.WriteFile(lockPath, []byte(tt.lockFileYAML), 0644)
				require.NoError(t, err)
			}

			// Test IsRunnable
			runnable, err := IsRunnable(markdownPath)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					require.ErrorContains(t, err, tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectRunnable, runnable)
			}
		})
	}
}

func TestGetWorkflowInputs_WithLockFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		markdownFile  string
		lockFileYAML  string
		expectedCount int
		expectedReq   map[string]bool // map of input name to required status
		expectError   bool
		errorContains string
	}{
		{
			name:         "workflow with required and optional inputs",
			markdownFile: "test-workflow.md",
			lockFileYAML: `name: "Test Workflow"
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
permissions:
  contents: read
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
			expectError: false,
		},
		{
			name:         "workflow with aw_context input is filtered out",
			markdownFile: "compiled-workflow.md",
			lockFileYAML: `name: "Compiled Workflow"
on:
  workflow_dispatch:
    inputs:
      aw_context:
        default: ""
        description: Agent caller context (used internally by Agentic Workflows).
        required: false
        type: string
      task:
        description: 'Task description'
        required: true
        type: string
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expectedCount: 1, // aw_context should be filtered out, only 'task' remains
			expectedReq: map[string]bool{
				"task": true,
			},
			expectError: false,
		},
		{
			name:         "workflow with only aw_context input returns empty",
			markdownFile: "aw-context-only.md",
			lockFileYAML: `name: "AW Context Only"
on:
  workflow_dispatch:
    inputs:
      aw_context:
        default: ""
        description: Agent caller context (used internally by Agentic Workflows).
        required: false
        type: string
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expectedCount: 0, // aw_context is the only input and should be filtered out
			expectError:   false,
		},
		{
			name:         "workflow with no inputs",
			markdownFile: "simple-workflow.md",
			lockFileYAML: `name: "Simple Workflow"
on:
  workflow_dispatch:
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expectedCount: 0,
			expectError:   false,
		},
		{
			name:         "workflow without workflow_dispatch",
			markdownFile: "pr-workflow.md",
			lockFileYAML: `name: "PR Workflow"
on:
  pull_request:
    types: [opened]
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`,
			expectedCount: 0,
			expectError:   false,
		},
		{
			name:          "workflow without lock file",
			markdownFile:  "uncompiled-workflow.md",
			lockFileYAML:  "", // No lock file
			expectError:   true,
			errorContains: "has not been compiled yet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory
			tmpDir := t.TempDir()
			markdownPath := filepath.Join(tmpDir, tt.markdownFile)
			lockPath := getLockFilePath(markdownPath)

			// Create markdown file (content doesn't matter for this test)
			err := os.WriteFile(markdownPath, []byte("# Test"), 0644)
			require.NoError(t, err)

			// Create lock file if provided
			if tt.lockFileYAML != "" {
				err = os.WriteFile(lockPath, []byte(tt.lockFileYAML), 0644)
				require.NoError(t, err)
			}

			// Extract inputs
			inputs, err := getWorkflowInputs(markdownPath)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					require.ErrorContains(t, err, tt.errorContains)
				}
			} else {
				require.NoError(t, err)

				// Check count
				assert.Len(t, inputs, tt.expectedCount)

				// Check required status
				for name, expectedReq := range tt.expectedReq {
					input, exists := inputs[name]
					require.True(t, exists, "Expected input '%s' not found", name)
					assert.Equal(t, expectedReq, input.Required, "Input '%s': expected required=%v, got %v", name, expectedReq, input.Required)
				}
			}
		})
	}
}

func TestIsRunnable_RealWorldScenario(t *testing.T) {
	// This test simulates the real-world scenario from the issue:
	// A workflow with "on: daily" in the .md file gets compiled to
	// include workflow_dispatch in the .lock.yml file
	tmpDir := t.TempDir()
	markdownPath := filepath.Join(tmpDir, "daily-issues-report.md")
	lockPath := getLockFilePath(markdownPath)

	// Create markdown file with shorthand trigger
	markdownContent := `---
description: Daily report
on: daily
permissions:
  contents: read
engine: codex
---

# Daily Issues Report
`
	err := os.WriteFile(markdownPath, []byte(markdownContent), 0644)
	require.NoError(t, err)

	// Create lock file with expanded triggers (as the compiler would do)
	lockContent := `name: "Daily Issues Report Generator"
on:
  schedule:
  - cron: "10 16 * * *"
  workflow_dispatch:

permissions:
  contents: read

jobs:
  agent:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`
	err = os.WriteFile(lockPath, []byte(lockContent), 0644)
	require.NoError(t, err)

	// Test that the workflow is runnable
	runnable, err := IsRunnable(markdownPath)
	require.NoError(t, err, "Should not error when checking compiled workflow")
	assert.True(t, runnable, "Workflow should be runnable because lock file has workflow_dispatch")
}

func TestValidateWorkflowInputs_WithLockFile(t *testing.T) {
	// This test ensures validateWorkflowInputs still works with lock files
	tmpDir := t.TempDir()
	markdownPath := filepath.Join(tmpDir, "test-workflow.md")
	lockPath := getLockFilePath(markdownPath)

	// Create markdown file
	err := os.WriteFile(markdownPath, []byte("# Test"), 0644)
	require.NoError(t, err)

	// Create lock file with inputs
	lockContent := `name: "Test Workflow"
on:
  workflow_dispatch:
    inputs:
      issue_url:
        description: 'Issue URL'
        required: true
        type: string

permissions:
  contents: read

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "test"
`
	err = os.WriteFile(lockPath, []byte(lockContent), 0644)
	require.NoError(t, err)

	// Test with missing required input
	err = validateWorkflowInputs(markdownPath, []string{})
	require.Error(t, err)
	require.ErrorContains(t, err, "Missing required input(s)")
	require.ErrorContains(t, err, "issue_url")
	var validationErr *workflow.WorkflowValidationError
	require.ErrorAs(t, err, &validationErr, "expected WorkflowValidationError for invalid inputs")
	assert.Contains(t, validationErr.Suggestion, "on:")
	assert.Contains(t, validationErr.Suggestion, "workflow_dispatch:")
	assert.Contains(t, validationErr.Suggestion, "inputs:")

	// Test with provided required input
	err = validateWorkflowInputs(markdownPath, []string{"issue_url=https://example.com"})
	require.NoError(t, err)

	// Test with typo in input name
	err = validateWorkflowInputs(markdownPath, []string{"issue_ur=https://example.com"})
	require.Error(t, err)
	require.ErrorContains(t, err, "Invalid input name")
	require.ErrorContains(t, err, "issue_ur", "Error should include invalid input")
	require.ErrorContains(t, err, "issue_url", "Error should suggest correct input name")
}
