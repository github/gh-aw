//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestInvalidReactionValue(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "invalid-reaction-test")

	// Test invalid reaction value
	testContent := `---
on:
  issues:
    types: [opened]
  reaction: invalid_emoji
permissions:
  contents: read
  issues: write
  pull-requests: read
strict: false
tools:
  github:
    allowed: [issue_read]
---

# Invalid Reaction Test

Test workflow with invalid reaction value.
`

	testFile := filepath.Join(tmpDir, "test-invalid-reaction.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow - should fail with validation error
	var err error
	_, err = compiler.ParseWorkflowFile(testFile)
	if err == nil {
		t.Fatal("Expected error for invalid reaction value, but got none")
	}

	// Verify error message mentions the invalid value and valid options
	// The error can come from either schema validation or custom validation
	errMsg := err.Error()
	hasInvalidValue := strings.Contains(errMsg, "invalid_emoji") || strings.Contains(errMsg, "reaction")
	hasValidOptions := strings.Contains(errMsg, "must be one of") || strings.Contains(errMsg, "+1") || strings.Contains(errMsg, "eyes")

	if !hasInvalidValue {
		t.Errorf("Error message should mention the invalid reaction value, got: %v", err)
	}
	if !hasValidOptions {
		t.Errorf("Error message should mention valid reaction options, got: %v", err)
	}
}

// TestNumericReactionParsing tests that +1 and -1 reactions without quotes are parsed correctly
// YAML parses +1 as integer 1 and -1 as integer -1 when unquoted
func TestNumericReactionParsing(t *testing.T) {
	testCases := []struct {
		name             string
		reactionInYAML   string // How it appears in YAML
		expectedReaction string // Expected AIReaction value
	}{
		{
			name:             "plus one without quotes becomes +1",
			reactionInYAML:   "+1", // YAML parses unquoted +1 as int 1
			expectedReaction: "+1",
		},
		{
			name:             "minus one without quotes becomes -1",
			reactionInYAML:   "-1", // YAML parses unquoted -1 as int -1
			expectedReaction: "-1",
		},
		{
			name:             "plus one with quotes stays +1",
			reactionInYAML:   `"+1"`, // YAML parses quoted "+1" as string "+1"
			expectedReaction: "+1",
		},
		{
			name:             "minus one with quotes stays -1",
			reactionInYAML:   `"-1"`, // YAML parses quoted "-1" as string "-1"
			expectedReaction: "-1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "numeric-reaction-test")

			testContent := fmt.Sprintf(`---
on:
  issues:
    types: [opened]
  reaction: %s
permissions:
  contents: read
  issues: write
strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---

# Numeric Reaction Test

Test workflow with numeric reaction value.
`, tc.reactionInYAML)

			testFile := filepath.Join(tmpDir, "test-numeric-reaction.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()

			workflowData, err := compiler.ParseWorkflowFile(testFile)
			if err != nil {
				t.Fatalf("Failed to parse workflow with reaction %s: %v", tc.reactionInYAML, err)
			}

			if workflowData.AIReaction != tc.expectedReaction {
				t.Errorf("Expected AIReaction to be %q, got %q", tc.expectedReaction, workflowData.AIReaction)
			}
		})
	}
}

// TestInvalidNumericReaction tests that invalid numeric reactions are rejected
func TestInvalidNumericReaction(t *testing.T) {
	tmpDir := testutil.TempDir(t, "invalid-numeric-reaction-test")

	// Use integer 2 which is not a valid reaction
	testContent := `---
on:
  issues:
    types: [opened]
  reaction: 2
permissions:
  contents: read
  issues: write
strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---

# Invalid Numeric Reaction Test

Test workflow with invalid numeric reaction value.
`

	testFile := filepath.Join(tmpDir, "test-invalid-numeric-reaction.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow - should fail with validation error
	_, err := compiler.ParseWorkflowFile(testFile)
	if err == nil {
		t.Fatal("Expected error for invalid numeric reaction value, but got none")
	}

	// Verify error message mentions the invalid value
	errMsg := err.Error()
	if !strings.Contains(errMsg, "2") && !strings.Contains(errMsg, "reaction") {
		t.Errorf("Error message should mention the invalid reaction value, got: %v", err)
	}
}

// TestPullRequestDraftFilter tests the pull_request draft: false filter functionality
