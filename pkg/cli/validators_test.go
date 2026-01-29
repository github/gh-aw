//go:build !integration

package cli

import (
	"strings"
	"testing"
)

func TestValidateWorkflowName(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid simple name",
			input:       "my-workflow",
			expectError: false,
		},
		{
			name:        "valid with underscores",
			input:       "my_workflow",
			expectError: false,
		},
		{
			name:        "valid alphanumeric",
			input:       "workflow123",
			expectError: false,
		},
		{
			name:        "valid mixed",
			input:       "my-workflow_v2",
			expectError: false,
		},
		{
			name:        "valid uppercase",
			input:       "MyWorkflow",
			expectError: false,
		},
		{
			name:        "valid all hyphens and underscores",
			input:       "my-workflow_test-123",
			expectError: false,
		},
		{
			name:        "empty string",
			input:       "",
			expectError: true,
			errorMsg:    "workflow name cannot be empty",
		},
		{
			name:        "invalid with spaces",
			input:       "my workflow",
			expectError: true,
			errorMsg:    "workflow name must contain only alphanumeric characters, hyphens, and underscores",
		},
		{
			name:        "invalid with special chars",
			input:       "my@workflow!",
			expectError: true,
			errorMsg:    "workflow name must contain only alphanumeric characters, hyphens, and underscores",
		},
		{
			name:        "invalid with dots",
			input:       "my.workflow",
			expectError: true,
			errorMsg:    "workflow name must contain only alphanumeric characters, hyphens, and underscores",
		},
		{
			name:        "invalid with slashes",
			input:       "my/workflow",
			expectError: true,
			errorMsg:    "workflow name must contain only alphanumeric characters, hyphens, and underscores",
		},
		{
			name:        "invalid with parentheses",
			input:       "my(workflow)",
			expectError: true,
			errorMsg:    "workflow name must contain only alphanumeric characters, hyphens, and underscores",
		},
		{
			name:        "invalid with brackets",
			input:       "my[workflow]",
			expectError: true,
			errorMsg:    "workflow name must contain only alphanumeric characters, hyphens, and underscores",
		},
		{
			name:        "invalid with dollar sign",
			input:       "my$workflow",
			expectError: true,
			errorMsg:    "workflow name must contain only alphanumeric characters, hyphens, and underscores",
		},
		{
			name:        "invalid with percent sign",
			input:       "my%workflow",
			expectError: true,
			errorMsg:    "workflow name must contain only alphanumeric characters, hyphens, and underscores",
		},
		{
			name:        "invalid with hash",
			input:       "my#workflow",
			expectError: true,
			errorMsg:    "workflow name must contain only alphanumeric characters, hyphens, and underscores",
		},
		{
			name:        "invalid with asterisk",
			input:       "my*workflow",
			expectError: true,
			errorMsg:    "workflow name must contain only alphanumeric characters, hyphens, and underscores",
		},
		{
			name:        "invalid with ampersand",
			input:       "my&workflow",
			expectError: true,
			errorMsg:    "workflow name must contain only alphanumeric characters, hyphens, and underscores",
		},
		{
			name:        "invalid with plus",
			input:       "my+workflow",
			expectError: true,
			errorMsg:    "workflow name must contain only alphanumeric characters, hyphens, and underscores",
		},
		{
			name:        "invalid with equals",
			input:       "my=workflow",
			expectError: true,
			errorMsg:    "workflow name must contain only alphanumeric characters, hyphens, and underscores",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkflowName(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("ValidateWorkflowName(%q) expected error but got nil", tt.input)
					return
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("ValidateWorkflowName(%q) error = %q, want error containing %q", tt.input, err.Error(), tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateWorkflowName(%q) unexpected error: %v", tt.input, err)
				}
			}
		})
	}
}

func TestValidateWorkflowName_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
	}{
		{
			name:        "single character",
			input:       "a",
			expectError: false,
		},
		{
			name:        "single number",
			input:       "1",
			expectError: false,
		},
		{
			name:        "single hyphen",
			input:       "-",
			expectError: false,
		},
		{
			name:        "single underscore",
			input:       "_",
			expectError: false,
		},
		{
			name:        "very long valid name",
			input:       strings.Repeat("a", 100),
			expectError: false,
		},
		{
			name:        "starts with hyphen",
			input:       "-workflow",
			expectError: false,
		},
		{
			name:        "ends with hyphen",
			input:       "workflow-",
			expectError: false,
		},
		{
			name:        "starts with underscore",
			input:       "_workflow",
			expectError: false,
		},
		{
			name:        "ends with underscore",
			input:       "workflow_",
			expectError: false,
		},
		{
			name:        "starts with number",
			input:       "123workflow",
			expectError: false,
		},
		{
			name:        "multiple consecutive hyphens",
			input:       "my--workflow",
			expectError: false,
		},
		{
			name:        "multiple consecutive underscores",
			input:       "my__workflow",
			expectError: false,
		},
		{
			name:        "tab character",
			input:       "my\tworkflow",
			expectError: true,
		},
		{
			name:        "newline character",
			input:       "my\nworkflow",
			expectError: true,
		},
		{
			name:        "carriage return",
			input:       "my\rworkflow",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkflowName(tt.input)

			if tt.expectError && err == nil {
				t.Errorf("ValidateWorkflowName(%q) expected error but got nil", tt.input)
			}
			if !tt.expectError && err != nil {
				t.Errorf("ValidateWorkflowName(%q) unexpected error: %v", tt.input, err)
			}
		})
	}
}

func TestValidateWorkflowIntent(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid minimal intent",
			input:       "Create a workflow to triage issues",
			expectError: false,
		},
		{
			name:        "valid long intent",
			input:       "Create a comprehensive workflow that automatically triages GitHub issues by analyzing their content, assigning appropriate labels, and notifying relevant team members",
			expectError: false,
		},
		{
			name:        "valid exactly 20 characters",
			input:       "12345678901234567890",
			expectError: false,
		},
		{
			name:        "valid with newlines",
			input:       "Create a workflow to\ntriage issues automatically",
			expectError: false,
		},
		{
			name:        "valid with leading/trailing whitespace",
			input:       "  Create a workflow to triage issues  ",
			expectError: false,
		},
		{
			name:        "empty string",
			input:       "",
			expectError: true,
			errorMsg:    "workflow instructions cannot be empty",
		},
		{
			name:        "only whitespace",
			input:       "   \t\n  ",
			expectError: true,
			errorMsg:    "workflow instructions cannot be empty",
		},
		{
			name:        "only spaces",
			input:       "     ",
			expectError: true,
			errorMsg:    "workflow instructions cannot be empty",
		},
		{
			name:        "only tabs",
			input:       "\t\t\t",
			expectError: true,
			errorMsg:    "workflow instructions cannot be empty",
		},
		{
			name:        "only newlines",
			input:       "\n\n\n",
			expectError: true,
			errorMsg:    "workflow instructions cannot be empty",
		},
		{
			name:        "too short - single character",
			input:       "a",
			expectError: true,
			errorMsg:    "please provide at least 20 characters of instructions",
		},
		{
			name:        "too short - 5 characters",
			input:       "hello",
			expectError: true,
			errorMsg:    "please provide at least 20 characters of instructions",
		},
		{
			name:        "too short - 19 characters",
			input:       "1234567890123456789",
			expectError: true,
			errorMsg:    "please provide at least 20 characters of instructions",
		},
		{
			name:        "too short with whitespace padding",
			input:       "   test   ",
			expectError: true,
			errorMsg:    "please provide at least 20 characters of instructions",
		},
		{
			name:        "19 chars after trim",
			input:       "   1234567890123456789   ",
			expectError: true,
			errorMsg:    "please provide at least 20 characters of instructions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkflowIntent(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("ValidateWorkflowIntent(%q) expected error but got nil", tt.input)
					return
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("ValidateWorkflowIntent(%q) error = %q, want error containing %q", tt.input, err.Error(), tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateWorkflowIntent(%q) unexpected error: %v", tt.input, err)
				}
			}
		})
	}
}

func TestValidateWorkflowIntent_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
	}{
		{
			name:        "exactly 20 characters with spaces",
			input:       "a b c d e f g h i j k",
			expectError: false,
		},
		{
			name:        "unicode characters",
			input:       "Create workflow with émojis 🎉🎊",
			expectError: false,
		},
		{
			name:        "special characters",
			input:       "Create workflow: @user, #123, $var!",
			expectError: false,
		},
		{
			name:        "very long intent",
			input:       strings.Repeat("a", 1000),
			expectError: false,
		},
		{
			name:        "mixed whitespace",
			input:       "  \t\n  Create workflow for automation  \t\n  ",
			expectError: false,
		},
		{
			name:        "only punctuation but >= 20 chars",
			input:       "!@#$%^&*()_+-={}[]|;",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkflowIntent(tt.input)

			if tt.expectError && err == nil {
				t.Errorf("ValidateWorkflowIntent(%q) expected error but got nil", tt.input)
			}
			if !tt.expectError && err != nil {
				t.Errorf("ValidateWorkflowIntent(%q) unexpected error: %v", tt.input, err)
			}
		})
	}
}
