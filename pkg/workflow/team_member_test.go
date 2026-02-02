//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/constants"
)

// TestTeamMemberCheckForCommandWorkflows tests that team member checks are only added to command workflows
func TestTeamMemberCheckForCommandWorkflows(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "workflow-team-member-test")

	compiler := NewCompiler()

	tests := []struct {
		name                  string
		frontmatter           string
		filename              string
		expectTeamMemberCheck bool
	}{
		{
			name: "command workflow should include team member check",
			frontmatter: `---
on:
  command:
    name: test-bot
tools:
  github:
    allowed: [list_issues]
---

# Test Bot
Test workflow content.`,
			filename:              "command-workflow.md",
			expectTeamMemberCheck: true,
		},
		{
			name: "schedule workflow should not include team member check",
			frontmatter: `---
on:
  schedule:
    - cron: "0 9 * * 1"
tools:
  github:
    allowed: [list_issues]
---

# Schedule Workflow
Test workflow content.`,
			filename:              "schedule-workflow.md",
			expectTeamMemberCheck: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tmpDir, tt.filename)
			err := os.WriteFile(testFile, []byte(tt.frontmatter), 0644)
			if err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			lockContentStr := string(lockContent)

			// Check for team member check (now in pre_activation job)
			hasTeamMemberCheck := strings.Contains(lockContentStr, "Check team membership for command workflow") ||
				strings.Contains(lockContentStr, "Check team membership for workflow") ||
				strings.Contains(lockContentStr, string(constants.PreActivationJobName)+":")

			if tt.expectTeamMemberCheck {
				if !hasTeamMemberCheck {
					t.Errorf("Expected team member check in command workflow but not found")
				}
				// Note: The specific failure message "Access denied: User" is now in an external script
				// loaded at runtime, so we can't check for it in the compiled workflow YAML.
				// The team member check functionality is still present via the pre_activation job.

				// Note: As per comment feedback, the conditional if statement has been removed
				// since the JavaScript already tests membership and command filter is applied at job level
				// Verify that team member check no longer has unnecessary conditional logic
				if strings.Contains(lockContentStr, "if: contains(github.event.issue.body") {
					t.Errorf("Team member check should not have conditional if statement (per comment feedback)")
				}
				// Find the team member check section and ensure it doesn't have github.event_name logic
				teamMemberCheckStart := strings.Index(lockContentStr, "Check team membership for command workflow")
				if teamMemberCheckStart == -1 {
					teamMemberCheckStart = strings.Index(lockContentStr, "Check team membership for workflow")
				}
				if teamMemberCheckStart == -1 {
					// Look for the new pre_activation job structure
					teamMemberCheckStart = strings.Index(lockContentStr, string(constants.PreActivationJobName)+":")
				}
				// Find the next job after the team member check to extract the section
				var teamMemberCheckEnd int
				if strings.Contains(lockContentStr, "activation:") {
					activationStart := strings.Index(lockContentStr, "\n  activation:")
					if activationStart != -1 && activationStart > teamMemberCheckStart {
						teamMemberCheckEnd = activationStart - teamMemberCheckStart
					}
				}
				if teamMemberCheckEnd == 0 && strings.Contains(lockContentStr, "agent:") {
					agentStart := strings.Index(lockContentStr, "\n  agent:")
					if agentStart != -1 && agentStart > teamMemberCheckStart {
						teamMemberCheckEnd = agentStart - teamMemberCheckStart
					}
				}
				if teamMemberCheckStart != -1 && teamMemberCheckEnd > 0 {
					teamMemberSection := lockContentStr[teamMemberCheckStart : teamMemberCheckStart+teamMemberCheckEnd]
					if strings.Contains(teamMemberSection, "github.event_name") {
						t.Errorf("Team member check section should not contain github.event_name logic")
					}
				}
			} else {
				if hasTeamMemberCheck {
					t.Errorf("Did not expect team member check in non-command workflow but found it")
				}
			}
		})
	}
}
