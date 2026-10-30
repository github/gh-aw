//go:build !integration

package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatWorkflowRunError_DispatchGuidance(t *testing.T) {
	t.Setenv("CODESPACES", "false")
	tests := []struct {
		name     string
		message  string
		wantHint string
	}{
		{
			name:     "unregistered branch workflow",
			message:  "HTTP 404: workflow test.lock.yml not found on the default branch",
			wantHint: workflowDispatchRegistrationHint,
		},
		{
			name:     "case insensitive registration error",
			message:  "workflow test.lock.yml NOT FOUND ON THE DEFAULT BRANCH",
			wantHint: workflowDispatchRegistrationHint,
		},
		{
			name:     "selected ref lacks manual trigger",
			message:  "HTTP 422: Workflow does not have 'workflow_dispatch' trigger",
			wantHint: "The workflow at the selected ref must declare on.workflow_dispatch",
		},
		{name: "missing ref", message: "HTTP 422: No ref found for: feature/missing"},
		{name: "repository not found", message: "HTTP 404: Not Found"},
		{name: "permission denied", message: "HTTP 403: Resource not accessible by integration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := &exec.ExitError{Stderr: []byte(tt.message)}
			err := formatWorkflowRunError(original)
			require.ErrorIs(t, err, original)
			if tt.wantHint != "" {
				assert.Contains(t, err.Error(), tt.wantHint)
				assert.Contains(t, err.Error(), "https://github.github.com/gh-aw/setup/cli/#run-from-a-feature-branch")
			} else {
				assert.NotContains(t, err.Error(), "default branch")
				assert.NotContains(t, err.Error(), "on.workflow_dispatch")
			}
		})
	}
	t.Run("non-exit error", func(t *testing.T) {
		original := errors.New("gh executable not found")
		require.ErrorIs(t, formatWorkflowRunError(original), original)
	})
}

func TestRunWorkflowOnGitHub_RemoteFeatureBranch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake gh is not supported on Windows")
	}
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.log")
	t.Setenv("RUN_ARGS_PATH", argsPath)
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  exit 0
fi
printf '%s\n' "$@" >> "$RUN_ARGS_PATH"
case "$1" in
  api)
    printf '%s\n' '[{"workflows":[{"id":1,"path":".github/workflows/test.lock.yml","state":"active"}]}]'
    ;;
  workflow)
    printf '%s\n' 'https://github.com/owner/repo/actions/runs/12345678'
    ;;
  *) exit 1 ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	err := RunWorkflowOnGitHub(context.Background(), "test", RunOptions{
		WorkflowRunFlags: WorkflowRunFlags{
			RepoOverride: "owner/repo",
			RefOverride:  "feature/my-change",
		},
		Inputs: []string{"topic=branch test"},
	})
	require.NoError(t, err)
	args, err := os.ReadFile(argsPath)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"api", "--paginate", "--slurp", "repos/owner/repo/actions/workflows?per_page=100",
		"workflow", "run", "test.lock.yml", "--repo", "owner/repo",
		"--ref", "feature/my-change", "-f", "topic=branch test",
	}, strings.Split(strings.TrimSpace(string(args)), "\n"))
}

// TestRunWorkflowOnGitHub_InputValidation tests input validation in RunWorkflowOnGitHub
func TestRunWorkflowOnGitHub_InputValidation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		workflowName  string
		inputs        []string
		expectError   bool
		errorContains string
	}{
		{
			name:          "empty workflow name",
			workflowName:  "",
			inputs:        []string{},
			expectError:   true,
			errorContains: "workflow name or ID is missing",
		},
		{
			name:          "invalid input format - no equals sign",
			workflowName:  "test-workflow",
			inputs:        []string{"invalidinput"},
			expectError:   true,
			errorContains: "not in key=value format",
		},
		{
			name:          "invalid input format - empty key",
			workflowName:  "test-workflow",
			inputs:        []string{"=value"},
			expectError:   true,
			errorContains: "empty key before '='",
		},
		{
			name:          "valid input format - workflow resolution fails",
			workflowName:  "test-workflow",
			inputs:        []string{"key=value"},
			expectError:   true,       // Will error on workflow resolution
			errorContains: "workflow", // Generic check - could be "not found" or "GitHub CLI"
		},
		{
			name:          "multiple valid inputs - workflow resolution fails",
			workflowName:  "test-workflow",
			inputs:        []string{"key1=value1", "key2=value2"},
			expectError:   true,
			errorContains: "workflow",
		},
		{
			name:          "empty value is allowed - workflow resolution fails",
			workflowName:  "test-workflow",
			inputs:        []string{"key="},
			expectError:   true,
			errorContains: "workflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call RunWorkflowOnGitHub with test parameters
			err := RunWorkflowOnGitHub(
				ctx,
				tt.workflowName,
				RunOptions{
					Inputs: tt.inputs,
				},
			)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain '%s', but got: %s", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestRunWorkflowOnGitHub_ContextCancellation tests context cancellation handling
func TestRunWorkflowOnGitHub_ContextCancellation(t *testing.T) {
	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := RunWorkflowOnGitHub(
		ctx,
		"test-workflow",
		RunOptions{},
	)

	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}

	// Check that it's a context cancellation error
	if !strings.Contains(err.Error(), "context canceled") && !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context cancellation error, got: %v", err)
	}
}

// TestRunWorkflowsOnGitHub_InputValidation tests input validation in RunWorkflowsOnGitHub
func TestRunWorkflowsOnGitHub_InputValidation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		workflowNames []string
		expectError   bool
		errorContains string
	}{
		{
			name:          "empty workflow list",
			workflowNames: []string{},
			expectError:   true,
			errorContains: "workflow list is empty",
		},
		{
			name:          "single workflow - resolution fails",
			workflowNames: []string{"test-workflow"},
			expectError:   true, // Will fail on workflow resolution
			errorContains: "workflow",
		},
		{
			name:          "multiple workflows - resolution fails",
			workflowNames: []string{"workflow1", "workflow2"},
			expectError:   true,
			errorContains: "workflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RunWorkflowsOnGitHub(
				ctx,
				tt.workflowNames,
				RunOptions{},
			)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain '%s', but got: %s", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestRunWorkflowsOnGitHub_ContextCancellation tests context cancellation handling
func TestRunWorkflowsOnGitHub_ContextCancellation(t *testing.T) {
	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := RunWorkflowsOnGitHub(
		ctx,
		[]string{"test-workflow"},
		RunOptions{},
	)

	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}

	// Check that it's a context cancellation error
	if !strings.Contains(err.Error(), "context canceled") && !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context cancellation error, got: %v", err)
	}
}

func TestValidateLocalWorkflowForRun_PropagatesVerbose(t *testing.T) {
	workflowPath, err := filepath.Abs(filepath.Join("..", "..", ".github", "workflows", "smoke-call-workflow.md"))
	require.NoError(t, err, "should resolve workflow path")

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err, "should create stderr pipe")
	os.Stderr = w

	runErr := validateLocalWorkflowForRun(workflowPath, nil, true)

	require.NoError(t, w.Close(), "should close write end of stderr pipe")
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err, "should read captured stderr")

	require.NoError(t, runErr, "local workflow validation should succeed for a runnable direct path")
	assert.Contains(t, buf.String(), "Found workflow file at path",
		"verbose workflow validation should preserve resolveWorkflowFile diagnostics")
}

// TestRunWorkflowOnGitHub_FlagCombinations tests various flag combinations
func TestRunWorkflowOnGitHub_FlagCombinations(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		push          bool
		repoOverride  string
		expectError   bool
		errorContains []string // Multiple acceptable error messages
	}{
		{
			name:         "push flag with remote repo",
			push:         true,
			repoOverride: "owner/repo",
			expectError:  true,
			// Accept either the expected validation error, GH_TOKEN error in CI, HTTP 404 for non-existent repo, HTTP 403 for auth issues, or network/TLS failures in sandbox environments
			errorContains: []string{
				"--push flag is only supported for local workflows",
				"GH_TOKEN environment variable",
				"HTTP 404",
				"HTTP 403",
				"TLS handshake timeout",
				"failed to list workflows",
				"failed to validate remote workflow",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RunWorkflowOnGitHub(
				ctx,
				"test-workflow",
				RunOptions{
					WorkflowRunFlags: WorkflowRunFlags{
						RepoOverride: tt.repoOverride,
						Push:         tt.push,
					},
				},
			)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if len(tt.errorContains) > 0 {
					// Check if error contains at least one of the acceptable messages
					found := false
					for _, msg := range tt.errorContains {
						if strings.Contains(err.Error(), msg) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected error to contain one of %v, but got: %s", tt.errorContains, err.Error())
					}
				}
			}
		})
	}
}

// Note: Full integration testing of RunWorkflowOnGitHub and RunWorkflowsOnGitHub
// requires GitHub CLI, git repository setup, and network access. These tests
// focus on input validation and early error conditions that can be tested
// without those dependencies. Full end-to-end tests should be in integration
// test files (run_command_test.go with //go:build integration tag).

// TestParseRunInfoFromOutput tests extracting run info from gh workflow run output
func TestParseRunInfoFromOutput(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		expectNil   bool
		expectedID  int64
		expectedURL string
	}{
		{
			name: "gh v2.87+ output with run URL",
			output: "Created workflow_dispatch event for test.lock.yml at refs/heads/main\n" +
				"To see the workflow run, visit:\n" +
				"  https://github.com/owner/repo/actions/runs/12345678\n" +
				"Use `gh run view 12345678` to see the run logs",
			expectNil:   false,
			expectedID:  12345678,
			expectedURL: "https://github.com/owner/repo/actions/runs/12345678",
		},
		{
			name:      "old gh output without run URL",
			output:    "Created workflow_dispatch event for test.lock.yml at refs/heads/main",
			expectNil: true,
		},
		{
			name:      "empty output",
			output:    "",
			expectNil: true,
		},
		{
			name:        "URL with org and repo containing hyphens",
			output:      "https://github.com/my-org/my-repo/actions/runs/9876543210",
			expectNil:   false,
			expectedID:  9876543210,
			expectedURL: "https://github.com/my-org/my-repo/actions/runs/9876543210",
		},
		{
			name:        "GitHub Enterprise Server URL",
			output:      "https://github.mycompany.com/owner/repo/actions/runs/55554444",
			expectNil:   false,
			expectedID:  55554444,
			expectedURL: "https://github.mycompany.com/owner/repo/actions/runs/55554444",
		},
		{
			name: "GHES URL in multi-line output",
			output: "Created workflow_dispatch event for test.lock.yml at refs/heads/main\n" +
				"To see the workflow run, visit:\n" +
				"  https://ghe.example.com/myorg/myrepo/actions/runs/99887766\n",
			expectNil:   false,
			expectedID:  99887766,
			expectedURL: "https://ghe.example.com/myorg/myrepo/actions/runs/99887766",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseRunInfoFromOutput(tt.output)
			if tt.expectNil {
				if result != nil {
					t.Errorf("Expected nil result but got: %+v", result)
				}
				return
			}
			if result == nil {
				t.Fatalf("Expected non-nil result but got nil")
			}
			if result.DatabaseID != tt.expectedID {
				t.Errorf("Expected DatabaseID %d, got %d", tt.expectedID, result.DatabaseID)
			}
			if result.URL != tt.expectedURL {
				t.Errorf("Expected URL %q, got %q", tt.expectedURL, result.URL)
			}
		})
	}
}
