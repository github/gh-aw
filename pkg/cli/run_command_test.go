//go:build integration

package cli

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestAuditSuggestionMessage tests that the audit suggestion message
// has the expected format and includes the CLI command prefix
func TestAuditSuggestionMessage(t *testing.T) {
	// Sample run ID
	runID := int64(1234567890)

	// Generate the audit suggestion message
	auditSuggestion := fmt.Sprintf("💡 To analyze this run, use: %s audit %d", string(constants.CLIExtensionPrefix), runID)

	// Verify the message contains the expected elements
	expectedElements := []string{
		"💡", // Lightbulb emoji for friendly suggestion
		"To analyze this run",
		"use:",
		string(constants.CLIExtensionPrefix), // Should be "gh aw"
		"audit",
		fmt.Sprintf("%d", runID),
	}

	for _, element := range expectedElements {
		if !strings.Contains(auditSuggestion, element) {
			t.Errorf("Expected audit suggestion to contain %q, got: %s", element, auditSuggestion)
		}
	}

	// Verify the full command format
	expectedCommand := fmt.Sprintf("%s audit %d", string(constants.CLIExtensionPrefix), runID)
	if !strings.Contains(auditSuggestion, expectedCommand) {
		t.Errorf("Expected audit suggestion to contain full command %q, got: %s", expectedCommand, auditSuggestion)
	}
}

// TestAuditSuggestionMessageFormat tests the exact format of the audit suggestion
func TestAuditSuggestionMessageFormat(t *testing.T) {
	tests := []struct {
		name     string
		runID    int64
		expected string
	}{
		{
			name:     "small run ID",
			runID:    123,
			expected: "💡 To analyze this run, use: gh aw audit 123",
		},
		{
			name:     "large run ID",
			runID:    9876543210,
			expected: "💡 To analyze this run, use: gh aw audit 9876543210",
		},
		{
			name:     "typical run ID",
			runID:    1234567890,
			expected: "💡 To analyze this run, use: gh aw audit 1234567890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate the audit suggestion message
			auditSuggestion := fmt.Sprintf("💡 To analyze this run, use: %s audit %d", string(constants.CLIExtensionPrefix), tt.runID)

			// Verify exact format
			if auditSuggestion != tt.expected {
				t.Errorf("Expected audit suggestion %q, got %q", tt.expected, auditSuggestion)
			}

			// Verify it's agent-friendly (clear, actionable, no ambiguity)
			if !strings.HasPrefix(auditSuggestion, "💡") {
				t.Error("Expected audit suggestion to start with lightbulb emoji for friendliness")
			}

			if !strings.Contains(auditSuggestion, "To analyze this run") {
				t.Error("Expected audit suggestion to clearly state the purpose")
			}

			if !strings.Contains(auditSuggestion, "use:") {
				t.Error("Expected audit suggestion to provide clear action keyword 'use:'")
			}
		})
	}
}

// TestAuditSuggestionAgentFriendliness tests that the message is suitable for AI agents
func TestAuditSuggestionAgentFriendliness(t *testing.T) {
	runID := int64(1234567890)
	auditSuggestion := fmt.Sprintf("💡 To analyze this run, use: %s audit %d", string(constants.CLIExtensionPrefix), runID)

	// Agent-friendly characteristics:
	// 1. Clear action verb ("use")
	if !strings.Contains(auditSuggestion, "use:") {
		t.Error("Expected clear action verb 'use:'")
	}

	// 2. Specific command (not just a hint)
	if !strings.Contains(auditSuggestion, "gh aw audit") {
		t.Error("Expected specific command 'gh aw audit'")
	}

	// 3. Includes the run ID (no need to look it up)
	if !strings.Contains(auditSuggestion, fmt.Sprintf("%d", runID)) {
		t.Error("Expected run ID to be included in the command")
	}

	// 4. Not too wordy (agents prefer concise)
	wordCount := len(strings.Fields(auditSuggestion))
	if wordCount > 15 {
		t.Errorf("Expected concise message (< 15 words), got %d words", wordCount)
	}

	// 5. No ambiguous pronouns or references
	// Note: "this run" is acceptable as it refers to the just-triggered workflow run
	auditSuggestionLower := strings.ToLower(auditSuggestion)
	if strings.Contains(auditSuggestionLower, " it ") ||
		strings.Contains(auditSuggestionLower, "this one") ||
		strings.Contains(auditSuggestionLower, " that ") {
		t.Error("Expected no ambiguous references like 'it', 'this one', 'that'")
	}
}

// TestProgressFlagSignature tests that the progress flag has been removed
func TestProgressFlagSignature(t *testing.T) {
	// Test that functions no longer accept the progress parameter
	// This is a compile-time check more than a runtime check

	// RunWorkflowOnGitHub should NOT accept progress parameter anymore
	_ = RunWorkflowOnGitHub(context.Background(), "test", false, "", "", "", false, false, false, false, []string{}, false, false)

	// RunWorkflowsOnGitHub should NOT accept progress parameter anymore
	_ = RunWorkflowsOnGitHub(context.Background(), []string{"test"}, 0, false, "", "", "", false, false, false, []string{}, false, false)

	// getLatestWorkflowRunWithRetry should NOT accept progress parameter anymore
	_, _ = getLatestWorkflowRunWithRetry("test.lock.yml", "", false)
}

// TestRefFlagSignature tests that the ref flag is supported
func TestRefFlagSignature(t *testing.T) {
	// Test that RunWorkflowOnGitHub accepts refOverride parameter
	// This is a compile-time check that ensures the refOverride parameter exists
	_ = RunWorkflowOnGitHub(context.Background(), "test", false, "", "", "main", false, false, false, false, []string{}, false, false)

	// Test that RunWorkflowsOnGitHub accepts refOverride parameter
	_ = RunWorkflowsOnGitHub(context.Background(), []string{"test"}, 0, false, "", "", "main", false, false, false, []string{}, false, false)
}

// TestRunWorkflowOnGitHubWithRef tests that the ref parameter is handled correctly
func TestRunWorkflowOnGitHubWithRef(t *testing.T) {
	// Test with explicit ref override (should still fail for non-existent workflow, but syntax is valid)
	err := RunWorkflowOnGitHub(context.Background(), "nonexistent-workflow", false, "", "", "main", false, false, false, false, []string{}, false, false)
	if err == nil {
		t.Error("RunWorkflowOnGitHub should return error for non-existent workflow even with ref flag")
	}

	// Test with ref override and repo override
	err = RunWorkflowOnGitHub(context.Background(), "nonexistent-workflow", false, "", "owner/repo", "feature-branch", false, false, false, false, []string{}, false, false)
	if err == nil {
		t.Error("RunWorkflowOnGitHub should return error for non-existent workflow with both ref and repo")
	}
}

// TestInputFlagSignature tests that the inputs parameter is supported
func TestInputFlagSignature(t *testing.T) {
	// Test that RunWorkflowOnGitHub accepts inputs parameter
	// This is a compile-time check that ensures the inputs parameter exists
	_ = RunWorkflowOnGitHub(context.Background(), "test", false, "", "", "", false, false, false, false, []string{"key=value"}, false, false)

	// Test that RunWorkflowsOnGitHub accepts inputs parameter
	_ = RunWorkflowsOnGitHub(context.Background(), []string{"test"}, 0, false, "", "", "", false, false, false, []string{"key=value"}, false, false)
}

// TestInputValidation tests that input validation works correctly
func TestInputValidation(t *testing.T) {
	tests := []struct {
		name        string
		inputs      []string
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "valid single input",
			inputs:      []string{"name=value"},
			shouldError: false,
		},
		{
			name:        "valid multiple inputs",
			inputs:      []string{"name=value", "env=prod"},
			shouldError: false,
		},
		{
			name:        "valid input with special characters",
			inputs:      []string{"message=hello world", "path=/tmp/file.txt"},
			shouldError: false,
		},
		{
			name:        "invalid input without equals",
			inputs:      []string{"namevalue"},
			shouldError: true,
			errorMsg:    "invalid input format",
		},
		{
			name:        "invalid input - empty key",
			inputs:      []string{"=value"},
			shouldError: true,
			errorMsg:    "key cannot be empty",
		},
		{
			name:        "invalid input - empty value",
			inputs:      []string{"name="},
			shouldError: false, // Empty value is valid
		},
		{
			name:        "mixed valid and invalid",
			inputs:      []string{"name=value", "invalid"},
			shouldError: true,
			errorMsg:    "invalid input format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Since we can't actually run workflows in tests, we'll just test the validation
			// by checking if the function would error before attempting to run
			err := RunWorkflowOnGitHub(context.Background(), "nonexistent-workflow", false, "", "owner/repo", "", false, false, false, false, tt.inputs, false, false)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error for inputs %v, got nil", tt.inputs)
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got: %v", tt.errorMsg, err)
				}
			}
			// Note: For non-error cases, we still expect an error because the workflow doesn't exist,
			// but the error should not be about input validation
			if !tt.shouldError && err != nil && strings.Contains(err.Error(), "invalid input format") {
				t.Errorf("Got unexpected input validation error: %v", err)
			}
		})
	}
}
