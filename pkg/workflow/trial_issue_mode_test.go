//go:build integration

package workflow

import (
	"os"
	"strings"
	"testing"
)

func TestTrialModeIssueDetection(t *testing.T) {
	compiler := NewCompiler()

	testCases := []struct {
		name      string
		onSection string
		expected  bool
	}{
		{
			name: "Has issues trigger",
			onSection: `on:
  issues:
    types: [opened, edited]`,
			expected: true,
		},
		{
			name: "Has issue trigger (singular)",
			onSection: `on:
  issue:
    types: [opened]`,
			expected: true,
		},
		{
			name: "Has issue_comment trigger",
			onSection: `on:
  issue_comment:
    types: [created]`,
			expected: true,
		},
		{
			name: "No issue triggers",
			onSection: `on:
  push:
    branches: [main]
  pull_request:`,
			expected: false,
		},
		{
			name: "Mixed triggers with issues",
			onSection: `on:
  push:
    branches: [main]
  issues:
    types: [opened]
  workflow_dispatch:`,
			expected: true,
		},
		{
			name:      "Empty on section",
			onSection: "",
			expected:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := compiler.hasIssueTrigger(tc.onSection)
			if result != tc.expected {
				t.Errorf("hasIssueTrigger() = %v, expected %v for %s", result, tc.expected, tc.onSection)
			}
		})
	}
}

func TestWorkflowDispatchInjection(t *testing.T) {
	compiler := NewCompiler()

	testCases := []struct {
		name      string
		onSection string
		expected  string
	}{
		{
			name: "Simple issues trigger",
			onSection: `on:
  issues:
    types: [opened, edited]`,
			expected: "workflow_dispatch:",
		},
		{
			name: "Complex triggers with issues",
			onSection: `on:
  push:
    branches: [main]
  issues:
    types: [opened]
  pull_request:`,
			expected: "workflow_dispatch:",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := compiler.injectWorkflowDispatchForIssue(tc.onSection)
			if !strings.Contains(result, tc.expected) {
				t.Errorf("injectWorkflowDispatchForIssue() result does not contain %s\nGot: %s", tc.expected, result)
			}
			// Check that issue_number input is added
			if !strings.Contains(result, "issue_number") {
				t.Errorf("injectWorkflowDispatchForIssue() result does not contain issue_number input")
			}
		})
	}
}

func TestIssueNumberReplacement(t *testing.T) {
	compiler := NewCompiler()

	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "Replace issue number in steps",
			input: `jobs:
  main:
    runs-on: ubuntu-latest
    steps:
      - name: Get issue
        run: echo "Issue number is ${{ github.event.issue.number }}"`,
			expected: `jobs:
  main:
    runs-on: ubuntu-latest
    steps:
      - name: Get issue
        run: echo "Issue number is ${{ inputs.issue_number }}"`,
		},
		{
			name: "Multiple replacements",
			input: `jobs:
  main:
    runs-on: ubuntu-latest
    steps:
      - name: Step 1
        run: echo "${{ github.event.issue.number }}"
      - name: Step 2
        env:
          ISSUE: ${{ github.event.issue.number }}
        run: echo "Processing issue ${{ github.event.issue.number }}"`,
			expected: `jobs:
  main:
    runs-on: ubuntu-latest
    steps:
      - name: Step 1
        run: echo "${{ inputs.issue_number }}"
      - name: Step 2
        env:
          ISSUE: ${{ inputs.issue_number }}
        run: echo "Processing issue ${{ inputs.issue_number }}"`,
		},
		{
			name: "No issue number references",
			input: `jobs:
  main:
    runs-on: ubuntu-latest
    steps:
      - name: Regular step
        run: echo "No issue references here"`,
			expected: `jobs:
  main:
    runs-on: ubuntu-latest
    steps:
      - name: Regular step
        run: echo "No issue references here"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := compiler.replaceIssueNumberReferences(tc.input)
			if result != tc.expected {
				t.Errorf("replaceIssueNumberReferences() mismatch\nGot:\n%s\nExpected:\n%s", result, tc.expected)
			}
		})
	}
}

func TestTrialModeWithIssueWorkflow(t *testing.T) {
	// Create a test workflow with issue triggers
	workflowContent := `---
on:
  issues:
    types: [opened, edited, closed]
  issue_comment:
    types: [created, edited]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
---

# Issue Handler Workflow

This workflow responds to issue events and uses the issue number.

## Instructions

Process the issue ${{ github.event.issue.number }} and create a response.
`

	// Create temporary file
	tmpFile, err := os.CreateTemp("", "trial-issue-workflow-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write content to file
	if _, err := tmpFile.WriteString(workflowContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Test trial mode compilation with issue triggers
	t.Run("Trial Mode with Issue Triggers", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetTrialMode(true)      // Trial mode
		compiler.SetSkipValidation(true) // Skip validation for test

		// Parse the workflow file to get WorkflowData
		workflowData, err := compiler.ParseWorkflowFile(tmpFile.Name())
		if err != nil {
			t.Fatalf("Failed to parse workflow file in trial mode: %v", err)
		}

		// Generate YAML content
		lockContent, err := compiler.generateYAML(workflowData, tmpFile.Name())
		if err != nil {
			t.Fatalf("Failed to generate YAML in trial mode: %v", err)
		}

		// Should inject workflow_dispatch trigger
		if !strings.Contains(lockContent, "workflow_dispatch") {
			t.Error("Expected workflow_dispatch trigger to be injected in trial mode with issue triggers")
		}

		// Should have issue_number input
		if !strings.Contains(lockContent, "issue_number") {
			t.Error("Expected issue_number input to be added for workflow_dispatch trigger")
		}

		// Should replace github.event.issue.number with inputs.issue_number
		if strings.Contains(lockContent, "github.event.issue.number") {
			t.Error("Expected github.event.issue.number to be replaced with inputs.issue_number")
		}

		if !strings.Contains(lockContent, "inputs.issue_number") {
			t.Error("Expected inputs.issue_number to be present after replacement")
		}

		// Should still contain original issue triggers
		if !strings.Contains(lockContent, "issues:") {
			t.Error("Expected original issue triggers to be preserved")
		}
	})

	// Test normal mode (no injection)
	t.Run("Normal Mode with Issue Triggers", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetTrialMode(false)     // Normal mode
		compiler.SetSkipValidation(true) // Skip validation for test

		// Parse the workflow file to get WorkflowData
		workflowData, err := compiler.ParseWorkflowFile(tmpFile.Name())
		if err != nil {
			t.Fatalf("Failed to parse workflow file in normal mode: %v", err)
		}

		// Generate YAML content
		lockContent, err := compiler.generateYAML(workflowData, tmpFile.Name())
		if err != nil {
			t.Fatalf("Failed to generate YAML in normal mode: %v", err)
		}

		// Should NOT inject workflow_dispatch trigger in normal mode
		if strings.Contains(lockContent, "workflow_dispatch:") {
			t.Error("Did not expect workflow_dispatch trigger to be injected in normal mode")
		}

		// Should NOT replace github.event.issue.number in normal mode
		if !strings.Contains(lockContent, "github.event.issue.number") {
			t.Error("Expected github.event.issue.number to be preserved in normal mode")
		}

		if strings.Contains(lockContent, "inputs.issue_number") {
			t.Error("Did not expect inputs.issue_number in normal mode")
		}
	})
}
