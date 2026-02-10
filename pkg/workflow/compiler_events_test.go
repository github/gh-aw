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

func TestOnSection(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "workflow-on-test")

	compiler := NewCompiler()

	tests := []struct {
		name        string
		frontmatter string
		expectedOn  string
	}{
		{
			name: "default on section",
			frontmatter: `---
on: push
tools:
  github:
    allowed: [list_issues]
---`,
			expectedOn: `"on": push`,
		},
		{
			name: "custom on workflow_dispatch",
			frontmatter: `---
on:
  workflow_dispatch:
tools:
  github:
    allowed: [list_issues]
---`,
			expectedOn: `"on":
  workflow_dispatch:`,
		},
		{
			name: "custom on with push",
			frontmatter: `---
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
tools:
  github:
    allowed: [list_issues]
---`,
			expectedOn: `"on":
  pull_request:
    branches:
    - main
  push:
    branches:
    - main`,
		},
		{
			name: "custom on with multiple events",
			frontmatter: `---
on:
  workflow_dispatch:
  issues:
    types: [opened, closed]  
  schedule:
    - cron: "0 8 * * *"
tools:
  github:
    allowed: [list_issues]
---`,
			expectedOn: `"on":
  issues:
    types:
    - opened
    - closed
  schedule:
  - cron: "0 8 * * *"
  workflow_dispatch:`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test Workflow

This is a test workflow.
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

			// Check that the expected on section is present
			if !strings.Contains(lockContent, tt.expectedOn) {
				t.Errorf("Expected lock file to contain '%s' but it didn't.\nContent:\n%s", tt.expectedOn, lockContent)
			}
		})
	}
}

func TestCommandSection(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "workflow-command-test")

	compiler := NewCompiler()

	tests := []struct {
		name            string
		frontmatter     string
		filename        string
		expectedOn      string
		expectedIf      string
		expectedCommand string
	}{
		{
			name: "command trigger",
			frontmatter: `---
on:
  command:
    name: test-bot
tools:
  github:
    allowed: [list_issues]
---`,
			filename:        "test-bot.md",
			expectedOn:      "pull_request_review_comment:\n    types:\n    - created\n    - edited",
			expectedIf:      "startsWith(github.event.issue.body, '/test-bot ')",
			expectedCommand: "test-bot",
		},
		{
			name: "new format command trigger",
			frontmatter: `---
on:
  command:
    name: new-bot
tools:
  github:
    allowed: [list_issues]
---`,
			filename:        "test-new-format.md",
			expectedOn:      "pull_request_review_comment:\n    types:\n    - created\n    - edited",
			expectedIf:      "startsWith(github.event.issue.body, '/new-bot ')",
			expectedCommand: "new-bot",
		},
		{
			name: "new format command trigger no name defaults to filename",
			frontmatter: `---
on:
  command: {}
tools:
  github:
    allowed: [list_issues]
---`,
			filename:        "default-name-bot.md",
			expectedOn:      "pull_request_review_comment:\n    types:\n    - created\n    - edited",
			expectedIf:      "startsWith(github.event.issue.body, '/default-name-bot ')",
			expectedCommand: "default-name-bot",
		},
		{
			name: "string format command trigger",
			frontmatter: `---
on:
  command: "customname"
tools:
  github:
    allowed: [list_issues]
---`,
			filename:        "test-string-format.md",
			expectedOn:      "pull_request_review_comment:\n    types:\n    - created\n    - edited",
			expectedIf:      "startsWith(github.event.issue.body, '/customname ')",
			expectedCommand: "customname",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test Command Workflow

This is a test workflow for command triggering.
`

			testFile := filepath.Join(tmpDir, tt.filename)
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

			// Check that the expected on section is present
			if !strings.Contains(lockContent, tt.expectedOn) {
				t.Errorf("Expected lock file to contain '%s' but it didn't.\nContent:\n%s", tt.expectedOn, lockContent)
			}

			// Check that the expected if condition is present (normalize for multiline comparison)
			normalizedLockContent := NormalizeExpressionForComparison(lockContent)
			normalizedExpectedIf := NormalizeExpressionForComparison(tt.expectedIf)
			if !strings.Contains(normalizedLockContent, normalizedExpectedIf) {
				t.Errorf("Expected lock file to contain '%s' but it didn't.\nExpected (normalized): %s\nActual (normalized): %s\nOriginal Content:\n%s",
					tt.expectedIf, normalizedExpectedIf, normalizedLockContent, lockContent)
			}

			// The command is validated during compilation and should be present in the if condition
		})
	}
}

func TestCommandWithOtherEvents(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "workflow-command-merge-test")

	compiler := NewCompiler()

	tests := []struct {
		name             string
		frontmatter      string
		filename         string
		expectedOn       string
		expectedIf       string
		expectedCommand  string
		shouldError      bool
		expectedErrorMsg string
	}{
		{
			name: "command with workflow_dispatch",
			frontmatter: `---
on:
  command:
    name: test-bot
  workflow_dispatch:
tools:
  github:
    allowed: [list_issues]
---`,
			filename:        "command-with-dispatch.md",
			expectedOn:      "\"on\":\n  discussion:\n    types:\n    - created\n    - edited\n  discussion_comment:\n    types:\n    - created\n    - edited\n  issue_comment:\n    types:\n    - created\n    - edited\n  issues:\n    types:\n    - opened\n    - edited\n    - reopened\n  pull_request:\n    types:\n    - opened\n    - edited\n    - reopened\n  pull_request_review_comment:\n    types:\n    - created\n    - edited\n  workflow_dispatch:",
			expectedIf:      "github.event_name == 'issues'",
			expectedCommand: "test-bot",
			shouldError:     false,
		},
		{
			name: "command with schedule",
			frontmatter: `---
on:
  command:
    name: schedule-bot
  schedule:
    - cron: "0 9 * * 1"
tools:
  github:
    allowed: [list_issues]
---`,
			filename:        "command-with-schedule.md",
			expectedOn:      "\"on\":\n  discussion:\n    types:\n    - created\n    - edited\n  discussion_comment:\n    types:\n    - created\n    - edited\n  issue_comment:\n    types:\n    - created\n    - edited\n  issues:\n    types:\n    - opened\n    - edited\n    - reopened\n  pull_request:\n    types:\n    - opened\n    - edited\n    - reopened\n  pull_request_review_comment:\n    types:\n    - created\n    - edited\n  schedule:\n  - cron: \"0 9 * * 1\"",
			expectedIf:      "github.event_name == 'issues'",
			expectedCommand: "schedule-bot",
			shouldError:     false,
		},
		{
			name: "command with multiple compatible events",
			frontmatter: `---
on:
  command:
    name: multi-bot
  workflow_dispatch:
  push:
    branches: [main]
tools:
  github:
    allowed: [list_issues]
---`,
			filename:        "command-with-multiple.md",
			expectedOn:      "\"on\":\n  discussion:\n    types:\n    - created\n    - edited\n  discussion_comment:\n    types:\n    - created\n    - edited\n  issue_comment:\n    types:\n    - created\n    - edited\n  issues:\n    types:\n    - opened\n    - edited\n    - reopened\n  pull_request:\n    types:\n    - opened\n    - edited\n    - reopened\n  pull_request_review_comment:\n    types:\n    - created\n    - edited\n  push:\n    branches:\n    - main\n  workflow_dispatch:",
			expectedIf:      "github.event_name == 'issues'",
			expectedCommand: "multi-bot",
			shouldError:     false,
		},
		{
			name: "command with conflicting issues event - should error",
			frontmatter: `---
on:
  command:
    name: conflict-bot
  issues:
    types: [closed]
tools:
  github:
    allowed: [list_issues]
---`,
			filename:         "command-with-issues.md",
			shouldError:      true,
			expectedErrorMsg: "command trigger cannot be used with 'issues' event",
		},
		{
			name: "command with conflicting issue_comment event - should error",
			frontmatter: `---
on:
  command:
    name: conflict-bot
  issue_comment:
    types: [deleted]
tools:
  github:
    allowed: [list_issues]
---`,
			filename:         "command-with-issue-comment.md",
			shouldError:      true,
			expectedErrorMsg: "command trigger cannot be used with 'issue_comment' event",
		},
		{
			name: "command with conflicting pull_request event - should error",
			frontmatter: `---
on:
  command:
    name: conflict-bot
  pull_request:
    types: [closed]
tools:
  github:
    allowed: [list_issues]
---`,
			filename:         "command-with-pull-request.md",
			shouldError:      true,
			expectedErrorMsg: "command trigger cannot be used with 'pull_request' event",
		},
		{
			name: "command with conflicting pull_request_review_comment event - should error",
			frontmatter: `---
on:
  command:
    name: conflict-bot
  pull_request_review_comment:
    types: [created]
tools:
  github:
    allowed: [list_issues]
---`,
			filename:         "command-with-pull-request-review-comment.md",
			shouldError:      true,
			expectedErrorMsg: "command trigger cannot be used with 'pull_request_review_comment' event",
		},
		{
			name: "command with label-only issues event - should succeed",
			frontmatter: `---
on:
  command:
    name: label-bot
  issues:
    types: [labeled]
    names: [label-bot]
tools:
  github:
    allowed: [list_issues]
---`,
			filename:        "command-with-labeled-issues.md",
			shouldError:     false,
			expectedCommand: "label-bot",
		},
		{
			name: "command with labeled and unlabeled - should succeed",
			frontmatter: `---
on:
  command:
    name: label-bot
  issues:
    types: [labeled, unlabeled]
    names: [bot-label]
tools:
  github:
    allowed: [list_issues]
---`,
			filename:        "command-with-labeled-unlabeled.md",
			shouldError:     false,
			expectedCommand: "label-bot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test Command with Other Events Workflow

This is a test workflow for command merging with other events.
`

			testFile := filepath.Join(tmpDir, tt.filename)
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			err := compiler.CompileWorkflow(testFile)

			if tt.shouldError {
				if err == nil {
					t.Fatalf("Expected error but compilation succeeded")
				}
				if !strings.Contains(err.Error(), tt.expectedErrorMsg) {
					t.Errorf("Expected error message to contain '%s' but got '%s'", tt.expectedErrorMsg, err.Error())
				}
				return
			}

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

			// Check that the expected on section is present
			if !strings.Contains(lockContent, tt.expectedOn) {
				t.Errorf("Expected lock file to contain '%s' but it didn't.\nContent:\n%s", tt.expectedOn, lockContent)
			}

			// Check that the expected if condition is present (normalize for multiline comparison)
			normalizedLockContent := NormalizeExpressionForComparison(lockContent)
			normalizedExpectedIf := NormalizeExpressionForComparison(tt.expectedIf)
			if !strings.Contains(normalizedLockContent, normalizedExpectedIf) {
				t.Errorf("Expected lock file to contain '%s' but it didn't.\nExpected (normalized): %s\nActual (normalized): %s\nOriginal Content:\n%s",
					tt.expectedIf, normalizedExpectedIf, normalizedLockContent, lockContent)
			}

			// The alias is validated during compilation and should be correctly applied
		})
	}
}
