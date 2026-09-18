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
				"github.event_name == 'issues'",
				"startsWith(github.event.issue.body, '/test-bot ')",
				"startsWith(github.event.issue.body, '/test-bot\\n')",
				"startsWith(github.event.issue.body, '/test-bot\\r')",
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
				"github.event_name == 'issue_comment'",
				"startsWith(github.event.comment.body, '/test-bot ')",
				"startsWith(github.event.comment.body, '/test-bot\\n')",
				"startsWith(github.event.comment.body, '/test-bot\\r')",
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
				"github.event_name == 'pull_request'",
				"startsWith(github.event.pull_request.body, '/test-bot ')",
				"startsWith(github.event.pull_request.body, '/test-bot\\n')",
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
				"github.event_name == 'issue_comment'",
				"startsWith(github.event.comment.body, '/test-bot ')",
				"startsWith(github.event.comment.body, '/test-bot\\n')",
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
				"github.event_name == 'pull_request_review_comment'",
				"startsWith(github.event.comment.body, '/test-bot ')",
				"startsWith(github.event.comment.body, '/test-bot\\n')",
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
				"github.event_name == 'issues'",
				"startsWith(github.event.issue.body, '/test-bot ')",
				"startsWith(github.event.issue.body, '/test-bot\\n')",
				"github.event.issue.body == '/test-bot'",
				"github.event_name == 'issue_comment'",
				"startsWith(github.event.comment.body, '/test-bot ')",
				"startsWith(github.event.comment.body, '/test-bot\\n')",
				"github.event.comment.body == '/test-bot'",
				"github.event_name == 'pull_request'",
				"startsWith(github.event.pull_request.body, '/test-bot ')",
				"startsWith(github.event.pull_request.body, '/test-bot\\n')",
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
				"github.event_name == 'issues'",
				"startsWith(github.event.issue.body, '/test-bot ')",
				"startsWith(github.event.issue.body, '/test-bot\\n')",
				"github.event.issue.body == '/test-bot'",
				"github.event_name == 'issue_comment'",
				"startsWith(github.event.comment.body, '/test-bot ')",
				"startsWith(github.event.comment.body, '/test-bot\\n')",
				"github.event.comment.body == '/test-bot'",
			},
			shouldNotContain: []string{
				// Should not check issue.body when event is issue_comment
				// This is implicit - the event type gates the check
			},
		},
		{
			name: "slash_command should match bot comments with newline-separated metadata",
			frontmatter: `---
on:
  slash_command:
    name: test-bot
    events: [issue_comment]
tools:
  github:
    allowed: [list_issues]
---`,
			filename: "command-bot-newline-precision.md",
			shouldContain: []string{
				// Must match a bot comment body like "/test-bot\n> Generated by ..."
				// The YAML double-quoted string encodes \n so the YAML parser yields a real newline.
				"startsWith(github.event.comment.body, '/test-bot\\n')",
				// Must also still match the normal space-separated form
				"startsWith(github.event.comment.body, '/test-bot ')",
				// And the exact-match form
				"github.event.comment.body == '/test-bot'",
			},
		},
		{
			name: "slash_command should not match leading whitespace before command",
			frontmatter: `---
on:
  slash_command:
    name: test-bot
    events: [issue_comment]
tools:
  github:
    allowed: [list_issues]
---`,
			filename: "command-no-leading-whitespace.md",
			shouldContain: []string{
				// Generated conditions must use raw body accessors (no trim), so they match
				// only when the command appears at position zero. This aligns with the runtime
				// check_command_position.cjs which also requires the command at position zero.
				"startsWith(github.event.comment.body, '/test-bot ')",
				"startsWith(github.event.comment.body, '/test-bot\\n')",
				"github.event.comment.body == '/test-bot'",
			},
			shouldNotContain: []string{
				// Must not wrap the body in a trim or format call; that would diverge from
				// the character-zero policy enforced by the runtime helper.
				"trim(",
				"format('{0}",
			},
		},
		{
			frontmatter: `---
on:
  slash_command:
    name: smoke*
    events: [issue_comment]
tools:
  github:
    allowed: [list_issues]
---`,
			filename: "command-wildcard-prefix.md",
			shouldContain: []string{
				"startsWith(github.event.comment.body, '/smoke')",
				"github.event_name == 'issue_comment'",
			},
			shouldNotContain: []string{
				"github.event.comment.body == '/smoke*'",
				"startsWith(github.event.comment.body, '/smoke* ')",
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
