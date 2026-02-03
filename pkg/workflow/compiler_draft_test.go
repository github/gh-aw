//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestPullRequestDraftFilter(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "draft-filter-test")

	compiler := NewCompiler()

	tests := []struct {
		name         string
		frontmatter  string
		expectedIf   string // Expected if condition in the generated lock file
		shouldHaveIf bool   // Whether an if condition should be present
	}{
		{
			name: "pull_request with draft: false",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]
    draft: false

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedIf:   "(github.event_name != 'pull_request') || (github.event.pull_request.draft == false)",
			shouldHaveIf: true,
		},
		{
			name: "pull_request with draft: true (include only drafts)",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]
    draft: true

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedIf:   "(github.event_name != 'pull_request') || (github.event.pull_request.draft == true)",
			shouldHaveIf: true,
		},
		{
			name: "pull_request without draft field (no filter)",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			shouldHaveIf: false,
		},
		{
			name: "pull_request with draft: false and existing if condition",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]
    draft: false

if: github.actor != 'dependabot[bot]'

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedIf:   "(github.actor != 'dependabot[bot]') && ((github.event_name != 'pull_request') || (github.event.pull_request.draft == false))",
			shouldHaveIf: true,
		},
		{
			name: "pull_request with draft: true and existing if condition",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]
    draft: true

if: github.actor != 'dependabot[bot]'

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			expectedIf:   "(github.actor != 'dependabot[bot]') && ((github.event_name != 'pull_request') || (github.event.pull_request.draft == true))",
			shouldHaveIf: true,
		},
		{
			name: "non-pull_request trigger (no filter applied)",
			frontmatter: `---
on:
  issues:
    types: [opened]

permissions:
  contents: read
  issues: write
  pull-requests: read

strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [issue_read]
---`,
			shouldHaveIf: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test Draft Filter Workflow

This is a test workflow for draft filtering.
`

			testFile := filepath.Join(tmpDir, tt.name+"-workflow.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			err := compiler.CompileWorkflow(testFile)
			if err != nil {
				t.Fatalf("Unexpected error compiling workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			content, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockContent := string(content)

			if tt.shouldHaveIf {
				// Check that the expected if condition is present (normalize for multiline comparison)
				normalizedLockContent := NormalizeExpressionForComparison(lockContent)
				normalizedExpectedIf := NormalizeExpressionForComparison(tt.expectedIf)
				if !strings.Contains(normalizedLockContent, normalizedExpectedIf) {
					t.Errorf("Expected lock file to contain '%s' but it didn't.\nExpected (normalized): %s\nActual (normalized): %s\nOriginal Content:\n%s",
						tt.expectedIf, normalizedExpectedIf, normalizedLockContent, lockContent)
				}
			} else {
				// Check that no draft-related if condition is present in the main job
				if strings.Contains(lockContent, "github.event.pull_request.draft == false") {
					t.Errorf("Expected no draft filter condition but found one in lock file.\nContent:\n%s", lockContent)
				}
			}
		})
	}
}

// TestDraftFieldCommentingInOnSection specifically tests that the draft field is commented out in the on section
func TestCommentOutProcessedFieldsInOnSection(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name        string
		input       string
		expected    string
		description string
	}{
		{
			name: "pull_request with draft and paths",
			input: `on:
    pull_request:
        draft: false
        paths:
            - go.mod
            - go.sum
    workflow_dispatch:`,
			expected: `on:
    pull_request:
        # draft: false # Draft filtering applied via job conditions
        paths:
            - go.mod
            - go.sum
    workflow_dispatch:`,
			description: "Should comment out draft but keep paths",
		},
		{
			name: "pull_request with draft and types",
			input: `on:
    pull_request:
        draft: true
        types:
            - opened
            - edited`,
			expected: `on:
    pull_request:
        # draft: true # Draft filtering applied via job conditions
        types:
            - opened
            - edited`,
			description: "Should comment out draft but keep types",
		},
		{
			name: "pull_request with only draft field",
			input: `on:
    pull_request:
        draft: false
    workflow_dispatch:`,
			expected: `on:
    pull_request:
        # draft: false # Draft filtering applied via job conditions
    workflow_dispatch:`,
			description: "Should comment out draft even when it's the only field",
		},
		{
			name: "multiple pull_request sections",
			input: `on:
    pull_request:
        draft: false
        paths:
            - "*.go"
    schedule:
        - cron: "0 9 * * 1"`,
			expected: `on:
    pull_request:
        # draft: false # Draft filtering applied via job conditions
        paths:
            - "*.go"
    schedule:
        - cron: "0 9 * * 1"`,
			description: "Should comment out draft in pull_request while leaving other sections unchanged",
		},
		{
			name: "no pull_request section",
			input: `on:
    workflow_dispatch:
    push:
        branches:
            - main`,
			expected: `on:
    workflow_dispatch:
    push:
        branches:
            - main`,
			description: "Should leave unchanged when no pull_request section",
		},
		{
			name: "pull_request without draft field",
			input: `on:
    pull_request:
        types:
            - opened`,
			expected: `on:
    pull_request:
        types:
            - opened`,
			description: "Should leave unchanged when no draft field in pull_request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.commentOutProcessedFieldsInOnSection(tt.input, map[string]any{})

			if result != tt.expected {
				t.Errorf("%s\nExpected:\n%s\nGot:\n%s", tt.description, tt.expected, result)
			}
		})
	}
}

// containsInNonCommentLines checks if a string appears in any non-comment lines
// A comment line is one that starts with '#' (after trimming leading whitespace)
