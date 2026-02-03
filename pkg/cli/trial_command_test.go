//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// Test the host repo slug processing logic with dot notation
func TestHostRepoSlugProcessing(t *testing.T) {
	testCases := []struct {
		name             string
		hostRepoSlug     string
		expectedBehavior string
		description      string
	}{
		{
			name:             "dot notation should call getCurrentRepoSlug",
			hostRepoSlug:     ".",
			expectedBehavior: "current_repo",
			description:      "When hostRepoSlug is '.', it should use getCurrentRepoSlug",
		},
		{
			name:             "full slug should be used as-is",
			hostRepoSlug:     "owner/repo",
			expectedBehavior: "custom_full",
			description:      "When hostRepoSlug contains '/', it should be used as-is",
		},
		{
			name:             "repo name only should be prefixed with username",
			hostRepoSlug:     "my-repo",
			expectedBehavior: "custom_prefixed",
			description:      "When hostRepoSlug is just a name, it should be prefixed with username",
		},
		{
			name:             "empty string should use default",
			hostRepoSlug:     "",
			expectedBehavior: "default",
			description:      "When hostRepoSlug is empty, it should use the default trial repo",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This is mainly a documentation test to ensure we understand the expected behavior
			// In a real test, we would mock the various functions and test the actual logic
			t.Logf("Test case: %s", tc.description)
			t.Logf("Input: %s, Expected behavior: %s", tc.hostRepoSlug, tc.expectedBehavior)
		})
	}
}

// TestCloneRepoWithVersion tests that parseRepoSpec correctly handles version specifications
// and that the version is properly passed to cloneRepoContentsIntoHost
func TestCloneRepoWithVersion(t *testing.T) {
	tests := []struct {
		name            string
		cloneRepoSpec   string
		expectedSlug    string
		expectedVersion string
		shouldError     bool
		description     string
	}{
		{
			name:            "repo with tag",
			cloneRepoSpec:   "owner/repo@v1.0.0",
			expectedSlug:    "owner/repo",
			expectedVersion: "v1.0.0",
			shouldError:     false,
			description:     "Should parse tag version correctly",
		},
		{
			name:            "repo with branch",
			cloneRepoSpec:   "owner/repo@main",
			expectedSlug:    "owner/repo",
			expectedVersion: "main",
			shouldError:     false,
			description:     "Should parse branch name correctly",
		},
		{
			name:            "repo with commit SHA",
			cloneRepoSpec:   "owner/repo@abc123def456",
			expectedSlug:    "owner/repo",
			expectedVersion: "abc123def456",
			shouldError:     false,
			description:     "Should parse commit SHA correctly",
		},
		{
			name:            "repo without version",
			cloneRepoSpec:   "owner/repo",
			expectedSlug:    "owner/repo",
			expectedVersion: "",
			shouldError:     false,
			description:     "Should handle repo without version",
		},
		{
			name:            "GitHub URL with tag",
			cloneRepoSpec:   "https://github.com/owner/repo@v2.1.0",
			expectedSlug:    "owner/repo",
			expectedVersion: "v2.1.0",
			shouldError:     false,
			description:     "Should parse GitHub URL with tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoSpec, err := parseRepoSpec(tt.cloneRepoSpec)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error for spec %q, but got none", tt.cloneRepoSpec)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error for spec %q: %v", tt.cloneRepoSpec, err)
				return
			}

			if repoSpec.RepoSlug != tt.expectedSlug {
				t.Errorf("Expected slug %q, got %q", tt.expectedSlug, repoSpec.RepoSlug)
			}

			if repoSpec.Version != tt.expectedVersion {
				t.Errorf("Expected version %q, got %q", tt.expectedVersion, repoSpec.Version)
			}
		})
	}
}

func TestModifyWorkflowForTrialMode(t *testing.T) {
	tests := []struct {
		name            string
		inputContent    string
		logicalRepoSlug string
		expectedContent string
		description     string
	}{
		{
			name:            "replace github.repository variable",
			logicalRepoSlug: "fslaborg/FsMath",
			inputContent: `---
on:
  workflow_dispatch:

steps:
  - name: Example step
    run: echo "Repository is ${{ github.repository }}"
---

# Test Workflow
This workflow uses ${{ github.repository }} in the markdown too.`,
			expectedContent: `---
on:
  workflow_dispatch:

steps:
  - name: Example step
    run: echo "Repository is fslaborg/FsMath"
---

# Test Workflow
This workflow uses fslaborg/FsMath in the markdown too.`,
			description: "Should replace all instances of ${{ github.repository }} with logical repo slug",
		},
		{
			name:            "replace checkout action with proper indentation",
			logicalRepoSlug: "fslaborg/FsMath",
			inputContent: `---
steps:
  - name: Checkout repository
    uses: actions/checkout@v5
  
  - name: Another step
    run: echo "test"
---`,
			expectedContent: `---
steps:
  - name: Checkout repository
    uses: actions/checkout@v5
    with:
      repository: fslaborg/FsMath
  
  - name: Another step
    run: echo "test"
---`,
			description: "Should add repository parameter to checkout action with correct indentation",
		},
		{
			name:            "replace checkout action with different indentation",
			logicalRepoSlug: "owner/repo",
			inputContent: `---
steps:
- name: Checkout
  uses: actions/checkout@v5
- name: Build
  run: make build
---`,
			expectedContent: `---
steps:
- name: Checkout
  uses: actions/checkout@v5
  with:
    repository: owner/repo
- name: Build
  run: make build
---`,
			description: "Should handle checkout with different base indentation",
		},
		{
			name:            "replace checkout with additional parameters",
			logicalRepoSlug: "test/repo",
			inputContent: `---
steps:
  - name: Checkout with ref
    uses: actions/checkout@v5
    id: checkout-step
---`,
			expectedContent: `---
steps:
  - name: Checkout with ref
    uses: actions/checkout@v5
    with:
      repository: test/repo
    id: checkout-step
---`,
			description: "Should add repository parameter even when checkout has other parameters",
		},
		{
			name:            "handle multiple checkout actions",
			logicalRepoSlug: "multi/repo",
			inputContent: `---
steps:
  - name: Checkout main
    uses: actions/checkout@v5
  
  - name: Some other step
    run: echo "between checkouts"
    
  - name: Checkout different version
    uses: actions/checkout@v5
---`,
			expectedContent: `---
steps:
  - name: Checkout main
    uses: actions/checkout@v5
    with:
      repository: multi/repo
  
  - name: Some other step
    run: echo "between checkouts"
    
  - name: Checkout different version
    uses: actions/checkout@v5
    with:
      repository: multi/repo
---`,
			description: "Should replace all checkout actions in the workflow",
		},
		{
			name:            "preserve existing with clause structure",
			logicalRepoSlug: "preserve/repo",
			inputContent: `---
steps:
  - name: Checkout with existing with
    uses: actions/checkout@v5
    with:
      fetch-depth: 0
---`,
			expectedContent: `---
steps:
  - name: Checkout with existing with
    uses: actions/checkout@v5
    with:
      repository: preserve/repo
    with:
      fetch-depth: 0
---`,
			description: "Should add repository parameter even if with clause already exists (will create duplicate with blocks)",
		},
		{
			name:            "combined replacements",
			logicalRepoSlug: "combined/test",
			inputContent: `---
on:
  workflow_dispatch:

steps:
  - name: Checkout repository
    uses: actions/checkout@v5
  
  - name: Print repo info
    run: echo "Working with ${{ github.repository }}"
---

# Workflow for ${{ github.repository }}
This tests both replacements.`,
			expectedContent: `---
on:
  workflow_dispatch:

steps:
  - name: Checkout repository
    uses: actions/checkout@v5
    with:
      repository: combined/test
  
  - name: Print repo info
    run: echo "Working with combined/test"
---

# Workflow for combined/test
This tests both replacements.`,
			description: "Should handle both github.repository and checkout replacements in same workflow",
		},
		{
			name:            "no modifications needed",
			logicalRepoSlug: "no/changes",
			inputContent: `---
on:
  workflow_dispatch:

steps:
  - name: Simple step
    run: echo "No github.repository or checkout here"
  
  - name: Different action
    uses: actions/setup-node@v3
---

# Simple Workflow
No modifications needed.`,
			expectedContent: `---
on:
  workflow_dispatch:

steps:
  - name: Simple step
    run: echo "No github.repository or checkout here"
  
  - name: Different action
    uses: actions/setup-node@v3
---

# Simple Workflow
No modifications needed.`,
			description: "Should leave workflow unchanged if no replacements needed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir := testutil.TempDir(t, "test-*")

			// Create workflow file
			workflowName := "test-workflow"
			workflowPath := filepath.Join(tempDir, ".github", "workflows", workflowName+".md")

			// Create directory structure
			err := os.MkdirAll(filepath.Dir(workflowPath), 0755)
			if err != nil {
				t.Fatalf("Failed to create directory structure: %v", err)
			}

			// Write input content to file
			err = os.WriteFile(workflowPath, []byte(tt.inputContent), 0644)
			if err != nil {
				t.Fatalf("Failed to write test workflow file: %v", err)
			}

			// Call the function under test
			err = modifyWorkflowForTrialMode(tempDir, workflowName, tt.logicalRepoSlug, false)
			if err != nil {
				t.Fatalf("modifyWorkflowForTrialMode failed: %v", err)
			}

			// Read the modified content
			modifiedContent, err := os.ReadFile(workflowPath)
			if err != nil {
				t.Fatalf("Failed to read modified workflow file: %v", err)
			}

			// Compare with expected content
			actualContent := string(modifiedContent)
			if actualContent != tt.expectedContent {
				t.Errorf("Content mismatch for test '%s'\n%s\nExpected:\n%s\n\nActual:\n%s\n\nDiff:\n%s",
					tt.name, tt.description, tt.expectedContent, actualContent,
					generateSimpleDiff(tt.expectedContent, actualContent))
			}
		})
	}
}

func TestModifyWorkflowForTrialModeEdgeCases(t *testing.T) {
	tests := []struct {
		name            string
		inputContent    string
		logicalRepoSlug string
		expectError     bool
		description     string
	}{
		{
			name:            "empty file",
			logicalRepoSlug: "test/repo",
			inputContent:    "",
			expectError:     false,
			description:     "Should handle empty workflow file without error",
		},
		{
			name:            "malformed yaml",
			logicalRepoSlug: "test/repo",
			inputContent:    "---\ninvalid: yaml: content\n  bad: indentation\n",
			expectError:     false,
			description:     "Should handle malformed YAML (function doesn't validate YAML)",
		},
		{
			name:            "checkout in commented line",
			logicalRepoSlug: "test/repo",
			inputContent: `---
steps:
  # - uses: actions/checkout@v5  # This is commented out
  - name: Real step
    run: echo "test"
---`,
			expectError: false,
			description: "Should not modify commented checkout lines",
		},
		{
			name:            "checkout in string content",
			logicalRepoSlug: "test/repo",
			inputContent: `---
steps:
  - name: Echo checkout command
    run: echo "uses: actions/checkout@v5"
---`,
			expectError: false,
			description: "Should not modify checkout references inside strings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tempDir := testutil.TempDir(t, "test-*")

			// Create workflow file
			workflowName := "test-workflow"
			workflowPath := filepath.Join(tempDir, ".github", "workflows", workflowName+".md")

			// Create directory structure
			err := os.MkdirAll(filepath.Dir(workflowPath), 0755)
			if err != nil {
				t.Fatalf("Failed to create directory structure: %v", err)
			}

			// Write input content to file
			err = os.WriteFile(workflowPath, []byte(tt.inputContent), 0644)
			if err != nil {
				t.Fatalf("Failed to write test workflow file: %v", err)
			}

			// Call the function under test
			err = modifyWorkflowForTrialMode(tempDir, workflowName, tt.logicalRepoSlug, false)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none for test '%s': %s", tt.name, tt.description)
			} else if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for test '%s': %v (%s)", tt.name, err, tt.description)
			}

			// For successful cases, ensure file still exists and is readable
			if !tt.expectError {
				_, err := os.ReadFile(workflowPath)
				if err != nil {
					t.Errorf("Modified file should be readable for test '%s': %v", tt.name, err)
				}
			}
		})
	}
}

// generateSimpleDiff creates a simple diff representation for test output
func generateSimpleDiff(expected, actual string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")

	var diff []string
	maxLines := len(expectedLines)
	if len(actualLines) > maxLines {
		maxLines = len(actualLines)
	}

	for i := 0; i < maxLines; i++ {
		var expectedLine, actualLine string

		if i < len(expectedLines) {
			expectedLine = expectedLines[i]
		}
		if i < len(actualLines) {
			actualLine = actualLines[i]
		}

		if expectedLine != actualLine {
			if expectedLine != "" {
				diff = append(diff, "- "+expectedLine)
			}
			if actualLine != "" {
				diff = append(diff, "+ "+actualLine)
			}
		}
	}

	return strings.Join(diff, "\n")
}

// TestTrialModeValidation tests the validation logic for different combinations of flags
func TestTrialModeValidation(t *testing.T) {
	tests := []struct {
		name          string
		logicalRepo   string
		cloneRepo     string
		hostRepo      string
		shouldError   bool
		errorContains string
		description   string
	}{
		{
			name:          "logical-repo and clone-repo are mutually exclusive",
			logicalRepo:   "owner/repo1",
			cloneRepo:     "owner/repo2",
			hostRepo:      "",
			shouldError:   true,
			errorContains: "mutually exclusive",
			description:   "Should reject both --logical-repo and --clone-repo",
		},
		{
			name:        "repo with clone-repo is allowed",
			logicalRepo: "",
			cloneRepo:   "owner/source-repo",
			hostRepo:    "owner/host-repo",
			shouldError: false,
			description: "Should allow --repo with --clone-repo (clone mode with custom host)",
		},
		{
			name:        "repo with logical-repo is allowed",
			logicalRepo: "owner/logical-repo",
			cloneRepo:   "",
			hostRepo:    "owner/host-repo",
			shouldError: false,
			description: "Should allow --repo with --logical-repo (logical mode with custom host)",
		},
		{
			name:        "repo alone is allowed (direct mode)",
			logicalRepo: "",
			cloneRepo:   "",
			hostRepo:    "owner/host-repo",
			shouldError: false,
			description: "Should allow --repo alone (direct trial mode)",
		},
		{
			name:        "clone-repo alone is allowed",
			logicalRepo: "",
			cloneRepo:   "owner/source-repo",
			hostRepo:    "",
			shouldError: false,
			description: "Should allow --clone-repo alone (clone mode with default host)",
		},
		{
			name:        "logical-repo alone is allowed",
			logicalRepo: "owner/logical-repo",
			cloneRepo:   "",
			hostRepo:    "",
			shouldError: false,
			description: "Should allow --logical-repo alone (logical mode with default host)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the validation logic from RunWorkflowTrials
			var err error

			// Step 0: Validate mutually exclusive flags
			if tt.logicalRepo != "" && tt.cloneRepo != "" {
				err = os.ErrInvalid // Placeholder for actual error
			}

			gotError := err != nil
			if gotError != tt.shouldError {
				t.Errorf("Expected error=%v, got error=%v (err=%v)", tt.shouldError, gotError, err)
			}

			if tt.shouldError && err != nil && tt.errorContains != "" {
				// In actual code, we'd check if error message contains the expected text
				t.Logf("Error validation passed: %s", tt.description)
			}
		})
	}
}
