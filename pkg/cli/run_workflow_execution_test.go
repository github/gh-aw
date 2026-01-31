//go:build !integration

package cli

import (
	"context"
	"strings"
	"testing"
)

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
			errorContains: "workflow name or ID is required",
		},
		{
			name:          "invalid input format - no equals sign",
			workflowName:  "test-workflow",
			inputs:        []string{"invalidinput"},
			expectError:   true,
			errorContains: "invalid input format",
		},
		{
			name:          "invalid input format - empty key",
			workflowName:  "test-workflow",
			inputs:        []string{"=value"},
			expectError:   true,
			errorContains: "key cannot be empty",
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
				false, // enable
				"",    // engineOverride
				"",    // repoOverride
				"",    // refOverride
				false, // autoMergePRs
				false, // pushSecrets
				false, // push
				false, // waitForCompletion
				tt.inputs,
				false, // verbose
				false, // dryRun
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
		false, // enable
		"",    // engineOverride
		"",    // repoOverride
		"",    // refOverride
		false, // autoMergePRs
		false, // pushSecrets
		false, // push
		false, // waitForCompletion
		[]string{},
		false, // verbose
		false, // dryRun
	)

	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}

	// Check that it's a context cancellation error
	if !strings.Contains(err.Error(), "context canceled") && err != context.Canceled {
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
			errorContains: "at least one workflow name or ID is required",
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
				0,     // repeatCount
				false, // enable
				"",    // engineOverride
				"",    // repoOverride
				"",    // refOverride
				false, // autoMergePRs
				false, // pushSecrets
				false, // push
				[]string{},
				false, // verbose
				false, // dryRun
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
		0,     // repeatCount
		false, // enable
		"",    // engineOverride
		"",    // repoOverride
		"",    // refOverride
		false, // autoMergePRs
		false, // pushSecrets
		false, // push
		[]string{},
		false, // verbose
		false, // dryRun
	)

	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}

	// Check that it's a context cancellation error
	if !strings.Contains(err.Error(), "context canceled") && err != context.Canceled {
		t.Errorf("Expected context cancellation error, got: %v", err)
	}
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
			// Accept either the expected validation error, GH_TOKEN error in CI, or HTTP 404 for non-existent repo
			errorContains: []string{
				"--push flag is only supported for local workflows",
				"GH_TOKEN environment variable",
				"HTTP 404",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RunWorkflowOnGitHub(
				ctx,
				"test-workflow",
				false, // enable
				"",    // engineOverride
				tt.repoOverride,
				"",    // refOverride
				false, // autoMergePRs
				false, // pushSecrets
				tt.push,
				false, // waitForCompletion
				[]string{},
				false, // verbose
				false, // dryRun
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
