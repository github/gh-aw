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

// TestCommandConditionPrecision tests that command conditions check the correct body field for each event type
func TestCommandConditionPrecision(t *testing.T) {
	tmpDir := testutil.TempDir(t, "workflow-command-precision-test")

	compiler := NewCompiler()

	tests := []struct {
		name             string
		frontmatter      string
		filename         string
		shouldContain    []string // Conditions that MUST be present
		shouldNotContain []string // Conditions that must NOT be present
	}{
		{
			name: "issues event should only check issue.body when event is issues",
			frontmatter: `---
on:
  command:
    name: test-bot
    events: [issues]
tools:
  github:
    allowed: [list_issues]
---`,
			filename: "command-issues-precision.md",
			shouldContain: []string{
				"(github.event_name == 'issues') && ((startsWith(github.event.issue.body, '/test-bot ')",
				"github.event.issue.body == '/test-bot'",
			},
			shouldNotContain: []string{
				"github.event.comment.body",
				"github.event.pull_request.body",
			},
		},
		{
			name: "issue_comment event should only check comment.body when event is issue_comment",
			frontmatter: `---
on:
  command:
    name: test-bot
    events: [issue_comment]
tools:
  github:
    allowed: [list_issues]
---`,
			filename: "command-issue-comment-precision.md",
			shouldContain: []string{
				"(github.event_name == 'issue_comment')",
				"startsWith(github.event.comment.body, '/test-bot ')",
				"github.event.comment.body == '/test-bot'",
				"github.event.issue.pull_request == null",
			},
			shouldNotContain: []string{
				"github.event.issue.body",
				"github.event.pull_request.body",
			},
		},
		{
			name: "pull_request event should only check pull_request.body when event is pull_request",
			frontmatter: `---
on:
  command:
    name: test-bot
    events: [pull_request]
tools:
  github:
    allowed: [list_pull_requests]
---`,
			filename: "command-pr-precision.md",
			shouldContain: []string{
				"(github.event_name == 'pull_request') && ((startsWith(github.event.pull_request.body, '/test-bot ')",
				"github.event.pull_request.body == '/test-bot'",
			},
			shouldNotContain: []string{
				"github.event.issue.body",
				"github.event.comment.body",
			},
		},
		{
			name: "pull_request_comment event should only check comment.body when event is issue_comment on PR",
			frontmatter: `---
on:
  command:
    name: test-bot
    events: [pull_request_comment]
tools:
  github:
    allowed: [list_pull_requests]
---`,
			filename: "command-pr-comment-precision.md",
			shouldContain: []string{
				"(github.event_name == 'issue_comment')",
				"startsWith(github.event.comment.body, '/test-bot ')",
				"github.event.comment.body == '/test-bot'",
				"github.event.issue.pull_request != null",
			},
			shouldNotContain: []string{
				"github.event.issue.body",
				"github.event.pull_request.body",
			},
		},
		{
			name: "pull_request_review_comment event should only check comment.body when event is pull_request_review_comment",
			frontmatter: `---
on:
  command:
    name: test-bot
    events: [pull_request_review_comment]
tools:
  github:
    allowed: [list_pull_requests]
---`,
			filename: "command-pr-review-comment-precision.md",
			shouldContain: []string{
				"(github.event_name == 'pull_request_review_comment') && ((startsWith(github.event.comment.body, '/test-bot ')",
				"github.event.comment.body == '/test-bot'",
			},
			shouldNotContain: []string{
				"github.event.issue.body",
				"github.event.pull_request.body",
			},
		},
		{
			name: "multiple events should check the correct body field for each event type",
			frontmatter: `---
on:
  command:
    name: test-bot
    events: [issues, issue_comment, pull_request]
tools:
  github:
    allowed: [list_issues, list_pull_requests]
---`,
			filename: "command-multiple-precision.md",
			shouldContain: []string{
				"(github.event_name == 'issues') && ((startsWith(github.event.issue.body, '/test-bot ')",
				"github.event.issue.body == '/test-bot'",
				"(github.event_name == 'issue_comment')",
				"startsWith(github.event.comment.body, '/test-bot ')",
				"github.event.comment.body == '/test-bot'",
				"(github.event_name == 'pull_request') && ((startsWith(github.event.pull_request.body, '/test-bot ')",
				"github.event.pull_request.body == '/test-bot'",
			},
		},
		{
			name: "command with push should have precise event checks",
			frontmatter: `---
on:
  command:
    name: test-bot
    events: [issues, issue_comment]
  push:
    branches: [main]
tools:
  github:
    allowed: [list_issues]
---`,
			filename: "command-with-push-precision.md",
			shouldContain: []string{
				"(github.event_name == 'issues') && ((startsWith(github.event.issue.body, '/test-bot ')",
				"github.event.issue.body == '/test-bot'",
				"(github.event_name == 'issue_comment')",
				"startsWith(github.event.comment.body, '/test-bot ')",
				"github.event.comment.body == '/test-bot'",
			},
			shouldNotContain: []string{
				// Should not check issue.body when event is issue_comment
				// This is implicit - the event type gates the check
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test Command Precision

This test validates that command conditions check the correct body field for each event type.
`

			testFile := filepath.Join(tmpDir, tt.filename)
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			err := compiler.CompileWorkflow(testFile)
			if err != nil {
				t.Fatalf("Compilation failed: %v", err)
			}

			// Read the compiled workflow
			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockContentStr := string(lockContent)

			// Check for expected patterns in the entire workflow
			for _, expectedPattern := range tt.shouldContain {
				if !strings.Contains(lockContentStr, expectedPattern) {
					t.Errorf("Expected to find pattern '%s' in generated workflow", expectedPattern)
				}
			}

			// Check for unexpected patterns
			for _, unexpectedPattern := range tt.shouldNotContain {
				if strings.Contains(lockContentStr, unexpectedPattern) {
					t.Errorf("Did not expect to find pattern '%s' in generated workflow", unexpectedPattern)
				}
			}
		})
	}
}
