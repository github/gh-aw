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

// TestPullRequestCommentEvent tests the new pull_request_comment event identifier
func TestPullRequestCommentEvent(t *testing.T) {
	// Test that pull_request_comment is recognized
	mapping := GetCommentEventByIdentifier("pull_request_comment")
	if mapping == nil {
		t.Fatal("pull_request_comment should be a valid event identifier")
	}
	if mapping.EventName != "pull_request_comment" {
		t.Errorf("Expected EventName to be 'pull_request_comment', got '%s'", mapping.EventName)
	}
	if !mapping.IsPRComment {
		t.Error("Expected IsPRComment to be true for pull_request_comment")
	}
}

// TestIssueCommentRestriction tests that issue_comment is restricted to issues only
func TestIssueCommentRestriction(t *testing.T) {
	mapping := GetCommentEventByIdentifier("issue_comment")
	if mapping == nil {
		t.Fatal("issue_comment should be a valid event identifier")
	}
	if !mapping.IsIssueComment {
		t.Error("Expected IsIssueComment to be true for issue_comment")
	}
	if mapping.IsPRComment {
		t.Error("Expected IsPRComment to be false for issue_comment")
	}
}

// TestMergeEventsForYAML tests the event merging logic for YAML generation
func TestMergeEventsForYAML(t *testing.T) {
	tests := []struct {
		name     string
		input    []CommentEventMapping
		expected int
		hasEvent string
	}{
		{
			name: "pull_request_comment only",
			input: []CommentEventMapping{
				{EventName: "pull_request_comment", Types: []string{"created", "edited"}, IsPRComment: true},
			},
			expected: 1,
			hasEvent: "issue_comment",
		},
		{
			name: "issue_comment only",
			input: []CommentEventMapping{
				{EventName: "issue_comment", Types: []string{"created", "edited"}, IsIssueComment: true},
			},
			expected: 1,
			hasEvent: "issue_comment",
		},
		{
			name: "both issue_comment and pull_request_comment",
			input: []CommentEventMapping{
				{EventName: "issue_comment", Types: []string{"created", "edited"}, IsIssueComment: true},
				{EventName: "pull_request_comment", Types: []string{"created", "edited"}, IsPRComment: true},
			},
			expected: 1,
			hasEvent: "issue_comment",
		},
		{
			name: "mixed with other events",
			input: []CommentEventMapping{
				{EventName: "issues", Types: []string{"opened", "edited"}},
				{EventName: "pull_request_comment", Types: []string{"created", "edited"}, IsPRComment: true},
			},
			expected: 2,
			hasEvent: "issue_comment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeEventsForYAML(tt.input)
			if len(result) != tt.expected {
				t.Errorf("Expected %d events, got %d", tt.expected, len(result))
			}
			found := false
			for _, event := range result {
				if event.EventName == tt.hasEvent {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected to find event '%s' in merged results", tt.hasEvent)
			}
		})
	}
}

// TestEventAwareCommandConditions tests that command conditions are properly applied only to comment-related events
func TestEventAwareCommandConditions(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "workflow-event-aware-command-test")

	compiler := NewCompiler()

	tests := []struct {
		name                    string
		frontmatter             string
		filename                string
		expectedSimpleCondition bool // true if should use simple condition (command only)
		expectedEventAware      bool // true if should use event-aware condition (command + other events)
	}{
		{
			name: "command only should use simple condition",
			frontmatter: `---
on:
  command:
    name: simple-bot
tools:
  github:
    allowed: [list_issues]
---`,
			filename:                "simple-command.md",
			expectedSimpleCondition: true,
			expectedEventAware:      false,
		},
		{
			name: "command with push should use event-aware condition",
			frontmatter: `---
on:
  command:
    name: push-bot
  push:
    branches: [main]
tools:
  github:
    allowed: [list_issues]
---`,
			filename:                "command-with-push.md",
			expectedSimpleCondition: false,
			expectedEventAware:      true,
		},
		{
			name: "command with schedule should use event-aware condition",
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
			filename:                "command-with-schedule.md",
			expectedSimpleCondition: false,
			expectedEventAware:      true,
		},
		{
			name: "command with pull_request_comment only should check PR comment filter",
			frontmatter: `---
on:
  command:
    name: pr-bot
    events: [pull_request_comment]
tools:
  github:
    allowed: [list_issues]
---`,
			filename:                "command-with-pr-comment.md",
			expectedSimpleCondition: false, // Should contain PR filter logic
			expectedEventAware:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test Event-Aware Command Conditions

This test validates that command conditions are applied correctly based on event types.
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

			// Read the compiled workflow to check the if condition
			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockContentStr := string(lockContent)

			if tt.expectedSimpleCondition {
				// Should contain simple command condition (no complex event_name logic in main job)
				// Check for strict matching patterns: startsWith or exact equality
				startsWithPattern := "startsWith(github.event.issue.body, '/"
				exactMatchPattern := "github.event.issue.body == '/"

				hasStartsWith := strings.Contains(lockContentStr, startsWithPattern)
				hasExactMatch := strings.Contains(lockContentStr, exactMatchPattern)

				if !hasStartsWith && !hasExactMatch {
					t.Errorf("Expected simple command condition with either startsWith or exact match, but found neither")
				}

				// For simple command workflows, the main job condition should not contain github.event_name logic
				// We can check this by looking for conditions that use "startsWith(" or equality checks without "github.event_name"
				// Handle both single-line and multi-line YAML conditions
				lines := strings.Split(lockContentStr, "\n")
				foundSimpleCommandCondition := false

				for i, line := range lines {
					// Check for single-line if condition
					if strings.Contains(line, "if:") && (strings.Contains(line, "startsWith(") || strings.Contains(line, ".body == '/")) && !strings.Contains(line, "github.event_name") {
						foundSimpleCommandCondition = true
						break
					}
					// Check for multi-line if condition (if: > or if: | format)
					if strings.Contains(line, "if:") && (strings.Contains(line, ">") || strings.Contains(line, "|")) {
						// Check the following lines for startsWith() or exact match without github.event_name
						for j := i + 1; j < len(lines) && strings.TrimSpace(lines[j]) != ""; j++ {
							nextLine := lines[j]
							if (strings.Contains(nextLine, "startsWith(") || strings.Contains(nextLine, ".body == '/")) && !strings.Contains(nextLine, "github.event_name") {
								foundSimpleCommandCondition = true
								break
							}
							// Stop if we hit the next YAML key (starts without indentation)
							if len(nextLine) > 0 && nextLine[0] != ' ' && nextLine[0] != '\t' {
								break
							}
						}
						if foundSimpleCommandCondition {
							break
						}
					}
				}
				if !foundSimpleCommandCondition {
					t.Errorf("Expected to find simple command condition (startsWith or exact match without github.event_name) but not found")
				}
			}

			if tt.expectedEventAware {
				// Should contain event-aware condition with event_name checks (but not just in add_reaction job)
				expectedPattern := "github.event_name == 'issues'"
				if !strings.Contains(lockContentStr, expectedPattern) {
					t.Errorf("Expected event-aware condition containing '%s' but not found", expectedPattern)
				}

				// Should contain the complex condition with AND/OR logic
				expectedComplexPattern := "((github.event_name == 'issues'"
				if !strings.Contains(lockContentStr, expectedComplexPattern) {
					t.Errorf("Expected complex event-aware condition containing '%s' but not found", expectedComplexPattern)
				}

				// Should contain the OR for non-comment events
				expectedOrPattern := "!(github.event_name == 'issues'"
				if !strings.Contains(lockContentStr, expectedOrPattern) {
					t.Errorf("Expected event-aware condition with non-comment event clause containing '%s' but not found", expectedOrPattern)
				}
			}
		})
	}
}

// TestCommandEventsFiltering tests that the events field filters which events the command is active on
func TestCommandEventsFiltering(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "workflow-command-events-filtering-test")

	compiler := NewCompiler()

	tests := []struct {
		name                 string
		frontmatter          string
		filename             string
		expectedEvents       []string // Events that should be in the generated workflow
		unexpectedEvents     []string // Events that should NOT be in the generated workflow
		expectedBodyChecks   []string // Body properties that should be checked
		unexpectedBodyChecks []string // Body properties that should NOT be checked
	}{
		{
			name: "command with events: [issues]",
			frontmatter: `---
on:
  command:
    name: issue-bot
    events: [issues]
tools:
  github:
    allowed: [list_issues]
---`,
			filename:             "command-issue-only.md",
			expectedEvents:       []string{"issues:"},
			unexpectedEvents:     []string{"issue_comment:", "pull_request:", "pull_request_review_comment:"},
			expectedBodyChecks:   []string{"github.event.issue.body"},
			unexpectedBodyChecks: []string{"github.event.comment.body", "github.event.pull_request.body"},
		},
		{
			name: "command with events: [issues, issue_comment]",
			frontmatter: `---
on:
  command:
    name: dual-bot
    events: [issues, issue_comment]
tools:
  github:
    allowed: [list_issues]
---`,
			filename:             "command-issue-comment.md",
			expectedEvents:       []string{"issues:", "issue_comment:"},
			unexpectedEvents:     []string{"pull_request:", "pull_request_review_comment:"},
			expectedBodyChecks:   []string{"github.event.issue.body", "github.event.comment.body"},
			unexpectedBodyChecks: []string{"github.event.pull_request.body"},
		},
		{
			name: "command with events: '*' (all events)",
			frontmatter: `---
on:
  command:
    name: all-bot
    events: "*"
tools:
  github:
    allowed: [list_issues]
---`,
			filename:       "command-all-events.md",
			expectedEvents: []string{"issues:", "issue_comment:", "pull_request:", "pull_request_review_comment:"},
			expectedBodyChecks: []string{"github.event.issue.body", "github.event.comment.body",
				"github.event.pull_request.body"},
		},
		{
			name: "command with events: [pull_request]",
			frontmatter: `---
on:
  command:
    name: pr-bot
    events: [pull_request]
tools:
  github:
    allowed: [list_pull_requests]
---`,
			filename:             "command-pr-only.md",
			expectedEvents:       []string{"pull_request:"},
			unexpectedEvents:     []string{"issues:", "issue_comment:", "pull_request_review_comment:"},
			expectedBodyChecks:   []string{"github.event.pull_request.body"},
			unexpectedBodyChecks: []string{"github.event.issue.body", "github.event.comment.body"},
		},
		{
			name: "command with events: [discussion]",
			frontmatter: `---
on:
  command:
    name: discussion-bot
    events: [discussion]
tools:
  github:
    allowed: [list_issues]
---`,
			filename:             "command-discussion-only.md",
			expectedEvents:       []string{"discussion:"},
			unexpectedEvents:     []string{"issues:", "issue_comment:", "pull_request:", "discussion_comment:"},
			expectedBodyChecks:   []string{"github.event.discussion.body"},
			unexpectedBodyChecks: []string{"github.event.issue.body", "github.event.comment.body", "github.event.pull_request.body"},
		},
		{
			name: "command with events: [discussion_comment]",
			frontmatter: `---
on:
  command:
    name: discussion-comment-bot
    events: [discussion_comment]
tools:
  github:
    allowed: [list_issues]
---`,
			filename:             "command-discussion-comment-only.md",
			expectedEvents:       []string{"discussion_comment:"},
			unexpectedEvents:     []string{"issues:", "issue_comment:", "pull_request:", "discussion:"},
			expectedBodyChecks:   []string{"github.event.comment.body"},
			unexpectedBodyChecks: []string{"github.event.issue.body", "github.event.pull_request.body", "github.event.discussion.body"},
		},
		{
			name: "command with events: [discussion, discussion_comment]",
			frontmatter: `---
on:
  command:
    name: both-discussion-bot
    events: [discussion, discussion_comment]
tools:
  github:
    allowed: [list_issues]
---`,
			filename:             "command-both-discussion.md",
			expectedEvents:       []string{"discussion:", "discussion_comment:"},
			unexpectedEvents:     []string{"issues:", "issue_comment:", "pull_request:"},
			expectedBodyChecks:   []string{"github.event.discussion.body", "github.event.comment.body"},
			unexpectedBodyChecks: []string{"github.event.issue.body", "github.event.pull_request.body"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test Command Events Filtering

This test validates that command events filtering works correctly.
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

			// Extract the "on:" section to check for events (not permissions)
			onSectionStart := strings.Index(lockContentStr, "on:")
			onSectionEnd := strings.Index(lockContentStr[onSectionStart:], "\npermissions:")
			if onSectionEnd == -1 {
				onSectionEnd = strings.Index(lockContentStr[onSectionStart:], "\nconcurrency:")
			}
			if onSectionEnd == -1 {
				onSectionEnd = strings.Index(lockContentStr[onSectionStart:], "\njobs:")
			}
			onSection := ""
			if onSectionEnd > 0 {
				onSection = lockContentStr[onSectionStart : onSectionStart+onSectionEnd]
			} else {
				onSection = lockContentStr[onSectionStart:]
			}

			// Check for expected events in the "on:" section only
			for _, expectedEvent := range tt.expectedEvents {
				if !strings.Contains(onSection, expectedEvent) {
					t.Errorf("Expected to find event '%s' in 'on:' section, but not found.\nOn section:\n%s", expectedEvent, onSection)
				}
			}

			// Check for unexpected events in the "on:" section only
			for _, unexpectedEvent := range tt.unexpectedEvents {
				if strings.Contains(onSection, unexpectedEvent) {
					t.Errorf("Did not expect to find event '%s' in 'on:' section, but found it.\nOn section:\n%s", unexpectedEvent, onSection)
				}
			}

			// Check for expected body checks in the if condition
			for _, expectedCheck := range tt.expectedBodyChecks {
				if !strings.Contains(lockContentStr, expectedCheck) {
					t.Errorf("Expected to find body check '%s' in generated workflow", expectedCheck)
				}
			}

			// Check for unexpected body checks
			for _, unexpectedCheck := range tt.unexpectedBodyChecks {
				if strings.Contains(lockContentStr, unexpectedCheck) {
					t.Errorf("Did not expect to find body check '%s' in generated workflow", unexpectedCheck)
				}
			}
		})
	}
}
