//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestConcurrencyRules(t *testing.T) {
	// Test the new concurrency rules for pull_request and alias workflows
	tmpDir := testutil.TempDir(t, "concurrency-test")

	compiler := NewCompiler()

	tests := []struct {
		name                string
		frontmatter         string
		filename            string
		expectedConcurrency string
		shouldHaveCancel    bool
		description         string
	}{
		{
			name: "PR workflow should have dynamic concurrency with cancel",
			frontmatter: `---
on:
  pull_request:
    types: [opened, edited]
tools:
  github:
    allowed: [list_issues]
---`,
			filename: "pr-workflow.md",
			expectedConcurrency: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}"
  cancel-in-progress: true`,
			shouldHaveCancel: true,
			description:      "PR workflows should use dynamic concurrency with PR number and cancellation",
		},
		{
			name: "command workflow should have dynamic concurrency without cancel",
			frontmatter: `---
on:
  command:
    name: test-bot
tools:
  github:
    allowed: [list_issues]
---`,
			filename: "command-workflow.md",
			expectedConcurrency: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.event.pull_request.number }}"`,
			shouldHaveCancel: false,
			description:      "Alias workflows should use dynamic concurrency with ref but without cancellation",
		},
		{
			name: "regular workflow should use static concurrency without cancel",
			frontmatter: `---
on:
  schedule:
    - cron: "0 9 * * 1"
tools:
  github:
    allowed: [list_issues]
---`,
			filename: "regular-workflow.md",
			expectedConcurrency: `concurrency:
  group: "gh-aw-${{ github.workflow }}"`,
			shouldHaveCancel: false,
			description:      "Regular workflows should use static concurrency without cancellation",
		},
		{
			name: "push workflow should use dynamic concurrency with ref",
			frontmatter: `---
on:
  push:
    branches: [main]
tools:
  github:
    allowed: [list_issues]
---`,
			filename: "push-workflow.md",
			expectedConcurrency: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.ref }}"`,
			shouldHaveCancel: false,
			description:      "Push workflows should use dynamic concurrency with github.ref",
		},
		{
			name: "issue workflow should have dynamic concurrency with issue number",
			frontmatter: `---
on:
  issues:
    types: [opened, edited]
tools:
  github:
    allowed: [list_issues]
---`,
			filename: "issue-workflow.md",
			expectedConcurrency: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number }}"`,
			shouldHaveCancel: false,
			description:      "Issue workflows use global concurrency with engine ID and slot",
		},
		{
			name: "slash_command workflow should have dynamic concurrency with issue/PR number",
			frontmatter: `---
on:
  slash_command:
    name: test-bot
tools:
  github:
    allowed: [list_issues]
---`,
			filename: "slash-command-workflow.md",
			expectedConcurrency: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.event.pull_request.number }}"`,
			shouldHaveCancel: false,
			description:      "slash_command workflows should use dynamic concurrency with issue/PR number without cancellation",
		},
		{
			name: "slash_command shorthand workflow should have dynamic concurrency with issue/PR number",
			frontmatter: `---
on: /test-bot
tools:
  github:
    allowed: [list_issues]
---`,
			filename: "slash-command-shorthand-workflow.md",
			expectedConcurrency: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.event.pull_request.number }}"`,
			shouldHaveCancel: false,
			description:      "slash_command shorthand workflows should use dynamic concurrency with issue/PR number without cancellation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test Concurrency Workflow

This is a test workflow for concurrency behavior.
`

			testFile := filepath.Join(tmpDir, tt.filename)
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Parse the workflow to get its data
			workflowData, err := compiler.ParseWorkflowFile(testFile)
			if err != nil {
				t.Errorf("Failed to parse workflow: %v", err)
				return
			}

			t.Logf("Workflow: %s", tt.description)
			t.Logf("  On: %s", workflowData.On)
			t.Logf("  Concurrency: %s", workflowData.Concurrency)

			// Check that the concurrency field matches expected pattern
			if !strings.Contains(workflowData.Concurrency, "gh-aw-${{ github.workflow }}") {
				t.Errorf("Expected concurrency to use gh-aw-${{ github.workflow }}, got: %s", workflowData.Concurrency)
			}

			// Check for cancel-in-progress based on workflow type
			hasCancel := strings.Contains(workflowData.Concurrency, "cancel-in-progress: true")
			if tt.shouldHaveCancel && !hasCancel {
				t.Errorf("Expected cancel-in-progress: true for %s workflow, but not found in: %s", tt.name, workflowData.Concurrency)
			} else if !tt.shouldHaveCancel && hasCancel {
				t.Errorf("Did not expect cancel-in-progress: true for %s workflow, but found in: %s", tt.name, workflowData.Concurrency)
			}
		})
	}
}

func TestGenerateConcurrencyConfig(t *testing.T) {
	tests := []struct {
		name           string
		workflowData   *WorkflowData
		isAliasTrigger bool
		expected       string
		description    string
	}{
		{
			name: "PR workflow should have dynamic concurrency with cancel and PR number",
			workflowData: &WorkflowData{
				On: `on:
  pull_request:
    types: [opened, synchronize]`,
				Concurrency: "", // Empty, should be generated
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.pull_request.number || github.ref || github.run_id }}"
  cancel-in-progress: true`,
			description: "PR workflows should use PR number or ref with cancellation",
		},
		{
			name: "Alias workflow should have dynamic concurrency without cancel",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [opened, edited, reopened]`,
				Concurrency: "", // Empty, should be generated
			},
			isAliasTrigger: true,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.event.pull_request.number || github.run_id }}"
  queue: max`,
			description: "Alias workflows should use dynamic concurrency with ref but without cancellation",
		},
		{
			name: "Mixed slash_command and PR workflow should cancel stale runs",
			workflowData: &WorkflowData{
				On: `on:
  slash_command: test-bot
  pull_request:
    types: [opened, synchronize]
  workflow_dispatch:`,
			},
			isAliasTrigger: true,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.event.pull_request.number || github.run_id }}"
  cancel-in-progress: true`,
			description: "Mixed slash_command and pull_request workflows should cancel stale PR runs",
		},
		{
			name: "Push workflow should have dynamic concurrency with ref",
			workflowData: &WorkflowData{
				On: `on:
  push:
    branches: [main]`,
				Concurrency: "", // Empty, should be generated
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.ref || github.run_id }}"
  queue: max`,
			description: "Push workflows should use github.ref without cancellation",
		},
		{
			name: "Regular workflow should use static concurrency without cancel",
			workflowData: &WorkflowData{
				On: `on:
  schedule:
    - cron: "0 9 * * 1"`,
				Concurrency: "", // Empty, should be generated
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}"
  queue: max`,
			description: "Regular workflows should use static concurrency without cancellation",
		},
		{
			name: "Issue workflow should have dynamic concurrency with issue number",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [opened, edited]`,
				Concurrency: "", // Empty, should be generated
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.run_id }}"
  queue: max`,
			description: "Issue workflows should use issue number without cancellation",
		},
		{
			name: "Issue comment workflow should have dynamic concurrency with issue number",
			workflowData: &WorkflowData{
				On: `on:
  issue_comment:
    types: [created, edited]`,
				Concurrency: "", // Empty, should be generated
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.run_id }}"
  queue: max`,
			description: "Issue comment workflows should use issue number without cancellation",
		},
		{
			name: "Mixed issue and PR workflow should have dynamic concurrency with issue/PR number",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [opened, edited]
  pull_request:
    types: [opened, synchronize]`,
				Concurrency: "", // Empty, should be generated
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.event.pull_request.number || github.run_id }}"
  cancel-in-progress: true`,
			description: "Mixed workflows should use issue/PR number with cancellation enabled",
		},
		{
			name: "Discussion workflow should have dynamic concurrency with discussion number",
			workflowData: &WorkflowData{
				On: `on:
  discussion:
    types: [created, edited]`,
				Concurrency: "", // Empty, should be generated
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.discussion.number || github.run_id }}"
  queue: max`,
			description: "Discussion workflows should use discussion number without cancellation",
		},
		{
			name: "Mixed issue and discussion workflow should have dynamic concurrency with issue/discussion number",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [opened, edited]
  discussion:
    types: [created, edited]`,
				Concurrency: "", // Empty, should be generated
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.event.discussion.number || github.run_id }}"
  queue: max`,
			description: "Mixed issue and discussion workflows should use issue/discussion number without cancellation",
		},
		{
			name: "Existing concurrency should not be overridden",
			workflowData: &WorkflowData{
				On: `on:
  pull_request:
    types: [opened, synchronize]`,
				Concurrency: `concurrency:
  group: "custom-group"`,
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "custom-group"`,
			description: "Existing concurrency configuration should be preserved",
		},
		{
			name: "slash_command input YAML should have dynamic concurrency with issue/PR number",
			workflowData: &WorkflowData{
				On: `on:
  slash_command: test-bot
  workflow_dispatch:`,
				Concurrency: "",
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.event.pull_request.number || github.run_id }}"
  queue: max`,
			description: "slash_command (input-level YAML) should use issue/PR number, same as command trigger",
		},
		{
			name: "slash_command rendered YAML (issue_comment + workflow_dispatch) should have dynamic concurrency",
			workflowData: &WorkflowData{
				On: `on:
  issue_comment:
    types: [created]
  workflow_dispatch:`,
				Concurrency: "",
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.run_id }}"
  queue: max`,
			description: "Rendered slash_command YAML (issue_comment + workflow_dispatch) uses issue number via isIssueWorkflow",
		},
		{
			name: "workflow_call worker uses hard-baked workflow ID to avoid caller-name deadlock",
			workflowData: &WorkflowData{
				On: `on:
  workflow_call:
    inputs:
      sentinel:
        required: true
        type: string`,
				WorkflowID:  "call-test-copilot-call-worker",
				Concurrency: "",
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-call-test-copilot-call-worker-${{ github.run_id }}"
  queue: max`,
			description: "workflow_call workers must use compile-time workflow ID and run_id to avoid caller-name collisions and fan-out queue contention",
		},
		{
			name: "workflow_call without WorkflowID falls back to github.workflow plus run_id expression",
			workflowData: &WorkflowData{
				On: `on:
  workflow_call:`,
				WorkflowID:  "",
				Concurrency: "",
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.run_id }}"
  queue: max`,
			description: "workflow_call without WorkflowID still appends run_id so worker runs remain collision-free",
		},
		{
			name: "workflow_call mixed with workflow_dispatch still uses hard-baked ID and run_id",
			workflowData: &WorkflowData{
				On: `on:
  workflow_call:
  workflow_dispatch:`,
				WorkflowID:  "mixed-call-worker",
				Concurrency: "",
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-mixed-call-worker-${{ github.run_id }}"
  queue: max`,
			description: "workflow_call mixed with workflow_dispatch should still use compile-time ID and run_id",
		},
		{
			name: "top-level concurrency can disable queue max feature",
			workflowData: &WorkflowData{
				On: "on:\n  schedule:\n    - cron: '0 0 * * *'",
				Features: map[string]any{
					"group-concurrency-queue": false,
				},
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}"`,
			description: "feature flag can disable queue:max emission for the top-level workflow concurrency group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateConcurrencyConfig(tt.workflowData, tt.isAliasTrigger)

			if result != tt.expected {
				t.Errorf("GenerateConcurrencyConfig() failed for %s\nExpected:\n%s\nGot:\n%s", tt.description, tt.expected, result)
			}
		})
	}
}

// TestGenerateJobConcurrencyConfig tests the job-level concurrency configuration for agentic workflow runs
func TestGenerateJobConcurrencyConfig(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expected     string
		description  string
	}{
		{
			name: "No default concurrency for workflow_dispatch-only with copilot engine",
			workflowData: &WorkflowData{
				On:           "on:\n  workflow_dispatch:",
				EngineConfig: &EngineConfig{ID: "copilot"},
			},
			expected:    "",
			description: "Copilot with workflow_dispatch-only should NOT get default concurrency (user intent, top-level group is sufficient)",
		},
		{
			name: "No default concurrency for workflow_dispatch-only with claude engine",
			workflowData: &WorkflowData{
				On:           "on:\n  workflow_dispatch:",
				EngineConfig: &EngineConfig{ID: "claude"},
			},
			expected:    "",
			description: "Claude with workflow_dispatch-only should NOT get default concurrency (user intent, top-level group is sufficient)",
		},
		{
			name: "No default concurrency for push workflows",
			workflowData: &WorkflowData{
				On:           "on:\n  push:\n    branches: [main]",
				EngineConfig: &EngineConfig{ID: "copilot"},
			},
			expected:    "",
			description: "Push workflows should NOT get default concurrency (special case)",
		},
		{
			name: "No default concurrency for issue workflows",
			workflowData: &WorkflowData{
				On:           "on:\n  issues:\n    types: [opened]",
				EngineConfig: &EngineConfig{ID: "claude"},
			},
			expected:    "",
			description: "Issue workflows should NOT get default concurrency (special case)",
		},
		{
			name: "Custom concurrency string (simple group)",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "claude",
					Concurrency: `concurrency:
  group: "custom-group-${{ github.ref }}"`,
				},
			},
			expected: `concurrency:
  group: "custom-group-${{ github.ref }}"`,
			description: "Should use custom concurrency when specified",
		},
		{
			name: "Custom concurrency with cancel-in-progress",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Concurrency: `concurrency:
  group: "custom-group"
  cancel-in-progress: true`,
				},
			},
			expected: `concurrency:
  group: "custom-group"
  cancel-in-progress: true`,
			description: "Should preserve cancel-in-progress when specified",
		},
		{
			name: "Default concurrency for schedule with codex engine",
			workflowData: &WorkflowData{
				On:           "on:\n  schedule:\n    - cron: '0 0 * * *'",
				EngineConfig: &EngineConfig{ID: "codex"},
			},
			expected: `concurrency:
  group: "gh-aw-codex-${{ github.workflow }}"
  queue: max`,
			description: "Codex with schedule should get default concurrency",
		},
		{
			name: "Default concurrency for workflow_dispatch combined with schedule",
			workflowData: &WorkflowData{
				On:           "on:\n  workflow_dispatch:\n  schedule:\n    - cron: '0 0 * * *'",
				EngineConfig: &EngineConfig{ID: "copilot"},
			},
			expected: `concurrency:
  group: "gh-aw-copilot-${{ github.workflow }}"
  queue: max`,
			description: "workflow_dispatch combined with schedule should still get default concurrency (not workflow_dispatch-only)",
		},
		{
			name: "No default concurrency for slash_command input YAML (pre-rendered)",
			workflowData: &WorkflowData{
				On:           "on:\n  slash_command: test-bot\n  workflow_dispatch:",
				EngineConfig: &EngineConfig{ID: "copilot"},
			},
			expected:    "",
			description: "slash_command in input YAML should NOT get default concurrency (isSlashCommandWorkflow detects the synthetic event)",
		},
		{
			name: "No default concurrency for slash_command rendered YAML (issue_comment + workflow_dispatch)",
			workflowData: &WorkflowData{
				On:           "on:\n  issue_comment:\n    types: [created]\n  workflow_dispatch:",
				EngineConfig: &EngineConfig{ID: "copilot"},
			},
			expected:    "",
			description: "Rendered slash_command YAML (issue_comment + workflow_dispatch) should NOT get default concurrency (isIssueWorkflow detects it)",
		},
		{
			name: "Job discriminator appended to default group for schedule workflow",
			workflowData: &WorkflowData{
				On:                          "on:\n  schedule:\n    - cron: '0 0 * * *'",
				EngineConfig:                &EngineConfig{ID: "copilot"},
				ConcurrencyJobDiscriminator: "${{ inputs.finding_id }}",
			},
			expected: `concurrency:
  group: "gh-aw-copilot-${{ github.workflow }}-${{ inputs.finding_id }}"
  queue: max`,
			description: "job-discriminator should be appended to the default group to prevent fan-out cancellations",
		},
		{
			name: "Job discriminator appended when workflow_dispatch combined with schedule",
			workflowData: &WorkflowData{
				On:                          "on:\n  workflow_dispatch:\n  schedule:\n    - cron: '0 0 * * *'",
				EngineConfig:                &EngineConfig{ID: "copilot"},
				ConcurrencyJobDiscriminator: "${{ inputs.item_id }}",
			},
			expected: `concurrency:
  group: "gh-aw-copilot-${{ github.workflow }}-${{ inputs.item_id }}"
  queue: max`,
			description: "job-discriminator should be appended for mixed workflow_dispatch + schedule workflows",
		},
		{
			name: "Job discriminator ignored when workflow has special triggers (no default group generated)",
			workflowData: &WorkflowData{
				On:                          "on:\n  workflow_dispatch:",
				EngineConfig:                &EngineConfig{ID: "copilot"},
				ConcurrencyJobDiscriminator: "${{ inputs.finding_id }}",
			},
			expected:    "",
			description: "job-discriminator has no effect when no default job concurrency is generated (workflow_dispatch-only)",
		},
		{
			name: "Job discriminator ignored when engine provides explicit concurrency",
			workflowData: &WorkflowData{
				On: "on:\n  schedule:\n    - cron: '0 0 * * *'",
				EngineConfig: &EngineConfig{
					ID:          "copilot",
					Concurrency: "concurrency:\n  group: \"engine-custom-group\"",
				},
				ConcurrencyJobDiscriminator: "${{ inputs.finding_id }}",
			},
			expected: `concurrency:
  group: "engine-custom-group"`,
			description: "job-discriminator does not modify explicitly set engine concurrency",
		},
		{
			name: "Job discriminator using github.run_id for universal uniqueness (schedule)",
			workflowData: &WorkflowData{
				On:                          "on:\n  schedule:\n    - cron: '0 0 * * *'",
				EngineConfig:                &EngineConfig{ID: "copilot"},
				ConcurrencyJobDiscriminator: "${{ github.run_id }}",
			},
			expected: `concurrency:
  group: "gh-aw-copilot-${{ github.workflow }}-${{ github.run_id }}"
  queue: max`,
			description: "github.run_id makes each run unique — useful when fan-out workflows all share the same schedule trigger",
		},
		{
			name: "Job discriminator with claude engine and schedule trigger",
			workflowData: &WorkflowData{
				On:                          "on:\n  schedule:\n    - cron: '0 9 1 * *'",
				EngineConfig:                &EngineConfig{ID: "claude"},
				ConcurrencyJobDiscriminator: "${{ inputs.organization || github.run_id }}",
			},
			expected: `concurrency:
  group: "gh-aw-claude-${{ github.workflow }}-${{ inputs.organization || github.run_id }}"
  queue: max`,
			description: "job-discriminator works with any engine; fallback to run_id handles scheduled (no-input) runs",
		},
		{
			name: "workflow_call default group uses workflow id namespace",
			workflowData: &WorkflowData{
				On:         "on:\n  workflow_call:\n    inputs:\n      item:\n        required: false\n        type: string\n  schedule:\n    - cron: '0 0 * * *'",
				WorkflowID: "reviewer-worker",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
			},
			expected: `concurrency:
  group: "gh-aw-copilot-reviewer-worker"
  queue: max`,
			description: "workflow_call workers should use workflow-id-based concurrency to avoid caller-name collisions",
		},
		{
			name: "generated job concurrency can disable queue max feature",
			workflowData: &WorkflowData{
				On:           "on:\n  schedule:\n    - cron: '0 0 * * *'",
				EngineConfig: &EngineConfig{ID: "copilot"},
				Features: map[string]any{
					"group-concurrency-queue": false,
				},
			},
			expected: `concurrency:
  group: "gh-aw-copilot-${{ github.workflow }}"`,
			description: "feature flag can disable queue:max emission for generated job concurrency",
		},
		{
			name: "Job discriminator ignored when push trigger (special trigger, no default group)",
			workflowData: &WorkflowData{
				On:                          "on:\n  push:\n    branches: [main]",
				EngineConfig:                &EngineConfig{ID: "copilot"},
				ConcurrencyJobDiscriminator: "${{ github.run_id }}",
			},
			expected:    "",
			description: "push is a special trigger — no default job concurrency is generated, so job-discriminator has no effect",
		},
		{
			name: "Job discriminator with pull_request trigger (special trigger, no default group)",
			workflowData: &WorkflowData{
				On:                          "on:\n  pull_request:\n    types: [opened, synchronize]",
				EngineConfig:                &EngineConfig{ID: "codex"},
				ConcurrencyJobDiscriminator: "${{ github.run_id }}",
			},
			expected:    "",
			description: "pull_request is a special trigger — job-discriminator has no effect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateJobConcurrencyConfig(tt.workflowData)

			if result != tt.expected {
				t.Errorf("GenerateJobConcurrencyConfig() failed for %s\nExpected:\n%s\nGot:\n%s", tt.description, tt.expected, result)
			}
		})
	}
}

func TestIsPullRequestWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		on       string
		expected bool
	}{
		{
			name: "Pull request workflow should be identified",
			on: `on:
  pull_request:
    types: [opened, synchronize]`,
			expected: true,
		},
		{
			name: "Pull request review comment workflow should be identified",
			on: `on:
  pull_request_review_comment:
    types: [created]`,
			expected: true,
		},
		{
			name: "Schedule workflow should not be identified as PR workflow",
			on: `on:
  schedule:
    - cron: "0 9 * * 1"`,
			expected: false,
		},
		{
			name: "Issues workflow should not be identified as PR workflow",
			on: `on:
  issues:
    types: [opened, edited]`,
			expected: false,
		},
		{
			name: "Mixed workflow with PR should be identified",
			on: `on:
  issues:
    types: [opened, edited]
  pull_request:
    types: [opened, synchronize]`,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPullRequestWorkflow(tt.on)
			if result != tt.expected {
				t.Errorf("isPullRequestWorkflow() for %s = %v, expected %v", tt.name, result, tt.expected)
			}
		})
	}
}

func TestIsIssueWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		on       string
		expected bool
	}{
		{
			name: "Issues workflow should be identified",
			on: `on:
  issues:
    types: [opened, edited]`,
			expected: true,
		},
		{
			name: "Issue comment workflow should be identified",
			on: `on:
  issue_comment:
    types: [created]`,
			expected: true,
		},
		{
			name: "Pull request workflow should not be identified as issue workflow",
			on: `on:
  pull_request:
    types: [opened, synchronize]`,
			expected: false,
		},
		{
			name: "Schedule workflow should not be identified as issue workflow",
			on: `on:
  schedule:
    - cron: "0 9 * * 1"`,
			expected: false,
		},
		{
			name: "Mixed workflow with issues should be identified",
			on: `on:
  issues:
    types: [opened, edited]
  push:
    branches: [main]`,
			expected: true,
		},
		{
			name: "Mixed workflow with issue_comment should be identified",
			on: `on:
  issue_comment:
    types: [created]
  schedule:
    - cron: "0 9 * * 1"`,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isIssueWorkflow(tt.on)
			if result != tt.expected {
				t.Errorf("isIssueWorkflow() for %s = %v, expected %v", tt.name, result, tt.expected)
			}
		})
	}
}

func TestIsPushWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		on       string
		expected bool
	}{
		{
			name: "Push workflow should be identified",
			on: `on:
  push:
    branches: [main]`,
			expected: true,
		},
		{
			name: "Pull request workflow should not be identified as push workflow",
			on: `on:
  pull_request:
    types: [opened, synchronize]`,
			expected: false,
		},
		{
			name: "Schedule workflow should not be identified as push workflow",
			on: `on:
  schedule:
    - cron: "0 9 * * 1"`,
			expected: false,
		},
		{
			name: "Mixed workflow with push should be identified",
			on: `on:
  push:
    branches: [main]
  pull_request:
    types: [opened, synchronize]`,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPushWorkflow(tt.on)
			if result != tt.expected {
				t.Errorf("isPushWorkflow() for %s = %v, expected %v", tt.name, result, tt.expected)
			}
		})
	}
}

func TestIsDiscussionWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		on       string
		expected bool
	}{
		{
			name: "Discussion workflow should be identified",
			on: `on:
  discussion:
    types: [created, edited]`,
			expected: true,
		},
		{
			name: "Discussion comment workflow should be identified",
			on: `on:
  discussion_comment:
    types: [created]`,
			expected: true,
		},
		{
			name: "Issues workflow should not be identified as discussion workflow",
			on: `on:
  issues:
    types: [opened, edited]`,
			expected: false,
		},
		{
			name: "Mixed workflow with discussion should be identified",
			on: `on:
  discussion:
    types: [created, edited]
  push:
    branches: [main]`,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDiscussionWorkflow(tt.on)
			if result != tt.expected {
				t.Errorf("isDiscussionWorkflow() for %s = %v, expected %v", tt.name, result, tt.expected)
			}
		})
	}
}

func TestBuildConcurrencyGroupKeys(t *testing.T) {
	tests := []struct {
		name           string
		workflowData   *WorkflowData
		isAliasTrigger bool
		expected       []string
		description    string
	}{
		{
			name: "Alias workflow should include issue/PR number",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [opened, edited]`,
			},
			isAliasTrigger: true,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.event.issue.number || github.event.pull_request.number || github.run_id }}"},
			description:    "Alias workflows should use issue/PR number",
		},
		{
			name: "Pure PR workflow should include PR number",
			workflowData: &WorkflowData{
				On: `on:
  pull_request:
    types: [opened, synchronize]`,
			},
			isAliasTrigger: false,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.event.pull_request.number || github.ref || github.run_id }}"},
			description:    "Pure PR workflows should use PR number",
		},
		{
			name: "Pure issue workflow should include issue number",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [opened, edited]`,
			},
			isAliasTrigger: false,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.event.issue.number || github.run_id }}"},
			description:    "Pure issue workflows should use issue number",
		},
		{
			name: "Mixed issue and PR workflow should include issue/PR number",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [opened, edited]
  pull_request:
    types: [opened, synchronize]`,
			},
			isAliasTrigger: false,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.event.issue.number || github.event.pull_request.number || github.run_id }}"},
			description:    "Mixed workflows should use issue/PR number",
		},
		{
			name: "Pure discussion workflow should include discussion number",
			workflowData: &WorkflowData{
				On: `on:
  discussion:
    types: [created, edited]`,
			},
			isAliasTrigger: false,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.event.discussion.number || github.run_id }}"},
			description:    "Pure discussion workflows should use discussion number",
		},
		{
			name: "Mixed issue and discussion workflow should include issue/discussion number",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [opened, edited]
  discussion:
    types: [created, edited]`,
			},
			isAliasTrigger: false,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.event.issue.number || github.event.discussion.number || github.run_id }}"},
			description:    "Mixed issue and discussion workflows should use issue/discussion number",
		},
		{
			name: "Push workflow should include github.ref",
			workflowData: &WorkflowData{
				On: `on:
  push:
    branches: [main]`,
			},
			isAliasTrigger: false,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.ref || github.run_id }}"},
			description:    "Push workflows should use github.ref",
		},
		{
			name: "Mixed push and PR workflow should use PR logic (PR takes priority)",
			workflowData: &WorkflowData{
				On: `on:
  push:
    branches: [main]
  pull_request:
    types: [opened, synchronize]`,
			},
			isAliasTrigger: false,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.event.pull_request.number || github.ref || github.run_id }}"},
			description:    "Mixed push+PR workflows should use PR logic since PR is checked first",
		},
		{
			name: "Other workflow should not include additional keys",
			workflowData: &WorkflowData{
				On: `on:
  schedule:
    - cron: "0 9 * * 1"`,
			},
			isAliasTrigger: false,
			expected:       []string{"gh-aw", "${{ github.workflow }}"},
			description:    "Other workflows should use just workflow name",
		},
		{
			name: "slash_command input YAML should include issue/PR number",
			workflowData: &WorkflowData{
				On: `on:
  slash_command: test-bot
  workflow_dispatch:`,
			},
			isAliasTrigger: false,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.event.issue.number || github.event.pull_request.number || github.run_id }}"},
			description:    "slash_command (input-level YAML) should include issue/PR number in concurrency group",
		},
		{
			name: "Mixed issues and workflow_dispatch should fall back to run_id for dispatch runs",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [opened, edited]
  workflow_dispatch:`,
			},
			isAliasTrigger: false,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.event.issue.number || github.run_id }}"},
			description:    "Mixed issues+workflow_dispatch workflows should fall back to run_id when issue number is unavailable",
		},
		{
			name: "Mixed discussion and workflow_dispatch should fall back to run_id for dispatch runs",
			workflowData: &WorkflowData{
				On: `on:
  discussion:
    types: [created, edited]
  workflow_dispatch:`,
			},
			isAliasTrigger: false,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.event.discussion.number || github.run_id }}"},
			description:    "Mixed discussion+workflow_dispatch workflows should fall back to run_id when discussion number is unavailable",
		},
		{
			name: "Label trigger shorthand PR workflow should include inputs.item_number fallback and label name",
			workflowData: &WorkflowData{
				On: `on:
  pull_request:
    types: [labeled]
  workflow_dispatch:
    inputs:
      item_number:
        description: The number of the pull request
        required: true
        type: string`,
				HasDispatchItemNumber: true,
			},
			isAliasTrigger: false,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.event.pull_request.number || inputs.item_number || github.ref || github.run_id }}", "${{ github.event.label.name || github.run_id }}"},
			description:    "Label trigger shorthand PR workflows should include inputs.item_number before ref fallback and github.event.label.name to prevent cross-label cancellation",
		},
		{
			name: "Label trigger shorthand issue workflow should include inputs.item_number fallback and label name",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [labeled]
  workflow_dispatch:
    inputs:
      item_number:
        description: The number of the issue
        required: true
        type: string`,
				HasDispatchItemNumber: true,
			},
			isAliasTrigger: false,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.event.issue.number || inputs.item_number || github.run_id }}", "${{ github.event.label.name || github.run_id }}"},
			description:    "Label trigger shorthand issue workflows should include inputs.item_number fallback and github.event.label.name",
		},
		{
			name: "Label trigger shorthand discussion workflow should include inputs.item_number fallback and label name",
			workflowData: &WorkflowData{
				On: `on:
  discussion:
    types: [labeled]
  workflow_dispatch:
    inputs:
      item_number:
        description: The number of the discussion
        required: true
        type: string`,
				HasDispatchItemNumber: true,
			},
			isAliasTrigger: false,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.event.discussion.number || inputs.item_number || github.run_id }}", "${{ github.event.label.name || github.run_id }}"},
			description:    "Label trigger shorthand discussion workflows should include inputs.item_number fallback and github.event.label.name",
		},
		{
			name: "Label command workflow should include label name in concurrency group",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [labeled]
  pull_request:
    types: [labeled]
  discussion:
    types: [labeled]
  workflow_dispatch:
    inputs:
      item_number:
        description: The number of the issue, pull request, or discussion
        required: false
        type: string`,
				HasDispatchItemNumber: true,
			},
			isAliasTrigger: true,
			expected:       []string{"gh-aw", "${{ github.workflow }}", "${{ github.event.issue.number || github.event.pull_request.number || github.run_id }}", "${{ github.event.label.name || github.run_id }}"},
			description:    "Label command (command-trigger) workflows should include github.event.label.name to prevent different-label runs from sharing a concurrency group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildConcurrencyGroupKeys(tt.workflowData, tt.isAliasTrigger)

			if len(result) != len(tt.expected) {
				t.Errorf("buildConcurrencyGroupKeys() for %s returned %d keys, expected %d", tt.description, len(result), len(tt.expected))
				return
			}

			for i, key := range result {
				if key != tt.expected[i] {
					t.Errorf("buildConcurrencyGroupKeys() for %s key[%d] = %s, expected %s", tt.description, i, key, tt.expected[i])
				}
			}
		})
	}
}

func TestShouldEnableCancelInProgress(t *testing.T) {
	tests := []struct {
		name           string
		workflowData   *WorkflowData
		isAliasTrigger bool
		expected       bool
		description    string
	}{
		{
			name: "Alias workflow with pull request trigger should enable cancellation",
			workflowData: &WorkflowData{
				On: `on:
  pull_request:
    types: [opened, synchronize]`,
			},
			isAliasTrigger: true,
			expected:       true,
			description:    "Alias workflows with pull_request triggers should enable cancellation",
		},
		{
			name: "Command-only workflow should not enable cancellation",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [opened, edited]`,
			},
			isAliasTrigger: true,
			expected:       false,
			description:    "Command-only workflows should remain queued",
		},
		{
			name: "PR workflow should enable cancellation",
			workflowData: &WorkflowData{
				On: `on:
  pull_request:
    types: [opened, synchronize]`,
			},
			isAliasTrigger: false,
			expected:       true,
			description:    "PR workflows should enable cancellation",
		},
		{
			name: "Issue workflow should not enable cancellation",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [opened, edited]`,
			},
			isAliasTrigger: false,
			expected:       false,
			description:    "Issue workflows should not enable cancellation",
		},
		{
			name: "Mixed issue and PR workflow should enable cancellation",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [opened, edited]
  pull_request:
    types: [opened, synchronize]`,
			},
			isAliasTrigger: false,
			expected:       true,
			description:    "Mixed workflows with PR should enable cancellation",
		},
		{
			name: "Other workflow should not enable cancellation",
			workflowData: &WorkflowData{
				On: `on:
  schedule:
    - cron: "0 9 * * 1"`,
			},
			isAliasTrigger: false,
			expected:       false,
			description:    "Other workflows should not enable cancellation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldEnableCancelInProgress(tt.workflowData, tt.isAliasTrigger)
			if result != tt.expected {
				t.Errorf("shouldEnableCancelInProgress() for %s = %v, expected %v", tt.description, result, tt.expected)
			}
		})
	}
}

func TestIsWorkflowDispatchOnly(t *testing.T) {
	tests := []struct {
		name     string
		on       string
		expected bool
		desc     string
	}{
		{
			name:     "Pure workflow_dispatch should be identified as dispatch-only",
			on:       "on:\n  workflow_dispatch:",
			expected: true,
			desc:     "A workflow with only workflow_dispatch is dispatch-only",
		},
		{
			name: "workflow_dispatch with inputs should be identified as dispatch-only",
			on: `on:
  workflow_dispatch:
    inputs:
      environment:
        description: "Environment"`,
			expected: true,
			desc:     "workflow_dispatch with inputs is still dispatch-only",
		},
		{
			name: "No workflow_dispatch should not be identified as dispatch-only",
			on: `on:
  schedule:
    - cron: "0 9 * * 1"`,
			expected: false,
			desc:     "A workflow without workflow_dispatch is not dispatch-only",
		},
		{
			name: "workflow_dispatch combined with schedule should not be dispatch-only",
			on: `on:
  workflow_dispatch:
  schedule:
    - cron: "0 9 * * 1"`,
			expected: false,
			desc:     "schedule is a real trigger so the workflow is not dispatch-only",
		},
		{
			name: "workflow_dispatch combined with push should not be dispatch-only",
			on: `on:
  workflow_dispatch:
  push:
    branches: [main]`,
			expected: false,
			desc:     "push makes the workflow not dispatch-only",
		},
		{
			name: "workflow_dispatch combined with pull_request should not be dispatch-only",
			on: `on:
  workflow_dispatch:
  pull_request:
    types: [opened]`,
			expected: false,
			desc:     "pull_request makes the workflow not dispatch-only",
		},
		{
			name: "workflow_dispatch combined with issues should not be dispatch-only",
			on: `on:
  workflow_dispatch:
  issues:
    types: [opened]`,
			expected: false,
			desc:     "issues makes the workflow not dispatch-only",
		},
		{
			name: "slash_command with workflow_dispatch should not be dispatch-only",
			on: `on:
  slash_command: test-bot
  workflow_dispatch:`,
			expected: false,
			desc:     "slash_command is a synthetic event that expands to issue_comment; its presence means the workflow is not dispatch-only",
		},
		{
			name: "slash_command map format with workflow_dispatch should not be dispatch-only",
			on: `on:
  slash_command:
    name: test-bot
  workflow_dispatch:`,
			expected: false,
			desc:     "slash_command in map format is still a synthetic event that makes the workflow not dispatch-only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isWorkflowDispatchOnly(tt.on)
			if result != tt.expected {
				t.Errorf("isWorkflowDispatchOnly() for %q = %v, want %v: %s", tt.name, result, tt.expected, tt.desc)
			}
		})
	}
}

func TestHasSpecialTriggers(t *testing.T) {
	tests := []struct {
		name     string
		on       string
		expected bool
		desc     string
	}{
		{
			name: "Issue workflow is a special trigger",
			on: `on:
  issues:
    types: [opened]`,
			expected: true,
			desc:     "issues trigger should be detected as special",
		},
		{
			name: "PR workflow is a special trigger",
			on: `on:
  pull_request:
    types: [opened]`,
			expected: true,
			desc:     "pull_request trigger should be detected as special",
		},
		{
			name: "Push workflow is a special trigger",
			on: `on:
  push:
    branches: [main]`,
			expected: true,
			desc:     "push trigger should be detected as special",
		},
		{
			name: "Discussion workflow is a special trigger",
			on: `on:
  discussion:
    types: [created]`,
			expected: true,
			desc:     "discussion trigger should be detected as special",
		},
		{
			name:     "workflow_dispatch-only is a special trigger",
			on:       "on:\n  workflow_dispatch:",
			expected: true,
			desc:     "workflow_dispatch-only is treated as special (explicit user intent)",
		},
		{
			name: "slash_command input YAML is a special trigger",
			on: `on:
  slash_command: test-bot
  workflow_dispatch:`,
			expected: true,
			desc:     "slash_command is a synthetic event that should be detected as special",
		},
		{
			name: "slash_command map format is a special trigger",
			on: `on:
  slash_command:
    name: test-bot
  workflow_dispatch:`,
			expected: true,
			desc:     "slash_command in map format should also be detected as special",
		},
		{
			name: "schedule-only is NOT a special trigger",
			on: `on:
  schedule:
    - cron: "0 9 * * 1"`,
			expected: false,
			desc:     "schedule alone is not a special trigger and should receive default job concurrency",
		},
		{
			name: "schedule + workflow_dispatch is NOT a special trigger",
			on: `on:
  schedule:
    - cron: "0 9 * * 1"
  workflow_dispatch:`,
			expected: false,
			desc:     "schedule + workflow_dispatch is not a special trigger and should receive default job concurrency",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wd := &WorkflowData{On: tt.on}
			result := hasSpecialTriggers(wd)
			if result != tt.expected {
				t.Errorf("hasSpecialTriggers() for %q = %v, want %v: %s", tt.name, result, tt.expected, tt.desc)
			}
		})
	}
}

// TestHasBotSelfCancelRisk tests detection of the dangerous combination that can
// cause passive GitHub App bot-authored comment events to self-cancel a workflow run.
func TestHasBotSelfCancelRisk(t *testing.T) {
	appConfig := &GitHubAppConfig{
		AppID:      "${{ vars.APP_ID }}",
		PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
	}

	tests := []struct {
		name         string
		workflowData *WorkflowData
		expected     bool
		description  string
	}{
		{
			name: "slash_command with github-app is at risk",
			workflowData: &WorkflowData{
				On: `on:
  slash_command:
    name: test`,
				SafeOutputs: &SafeOutputsConfig{GitHubApp: appConfig},
			},
			expected:    true,
			description: "slash_command workflows with GitHub App safe-outputs can self-cancel",
		},
		{
			name: "issue_comment with github-app is at risk",
			workflowData: &WorkflowData{
				On: `on:
  issue_comment:
    types: [created]`,
				SafeOutputs: &SafeOutputsConfig{GitHubApp: appConfig},
			},
			expected:    true,
			description: "issue_comment workflows with GitHub App safe-outputs can self-cancel",
		},
		{
			name: "command trigger with github-app is at risk",
			workflowData: &WorkflowData{
				On:          `on:`,
				Command:     []string{"test-bot"},
				SafeOutputs: &SafeOutputsConfig{GitHubApp: appConfig},
			},
			expected:    true,
			description: "command trigger workflows with GitHub App safe-outputs can self-cancel",
		},
		{
			name: "slash_command without github-app is NOT at risk",
			workflowData: &WorkflowData{
				On: `on:
  slash_command:
    name: test`,
				SafeOutputs: &SafeOutputsConfig{},
			},
			expected:    false,
			description: "slash_command workflows without GitHub App safe-outputs are not at risk",
		},
		{
			name: "issue-only trigger with github-app is NOT at risk",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [opened]`,
				SafeOutputs: &SafeOutputsConfig{GitHubApp: appConfig},
			},
			expected:    false,
			description: "issues: trigger without issue_comment does not re-trigger on app-posted comments",
		},
		{
			name: "PR trigger with github-app is NOT at risk",
			workflowData: &WorkflowData{
				On: `on:
  pull_request:
    types: [opened]`,
				SafeOutputs: &SafeOutputsConfig{GitHubApp: appConfig},
			},
			expected:    false,
			description: "PR-only workflows without issue_comment do not re-trigger on app-posted comments",
		},
		{
			name: "no safe-outputs is NOT at risk",
			workflowData: &WorkflowData{
				On: `on:
  issue_comment:
    types: [created]`,
				SafeOutputs: nil,
			},
			expected:    false,
			description: "workflows without safe-outputs do not post App-authored comments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasBotSelfCancelRisk(tt.workflowData)
			if result != tt.expected {
				t.Errorf("hasBotSelfCancelRisk() for %q = %v, want %v: %s", tt.name, result, tt.expected, tt.description)
			}
		})
	}
}

// TestBotActorIsolationInConcurrencyKeys tests that the bot-actor isolation prefix
// is correctly applied to concurrency group keys when the dangerous combination
// (issue_comment triggers + GitHub App safe-outputs) is detected.
func TestBotActorIsolationInConcurrencyKeys(t *testing.T) {
	appConfig := &GitHubAppConfig{
		AppID:      "${{ vars.APP_ID }}",
		PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
	}
	const botPrefix = "contains(github.actor, '[bot]') && github.run_id"

	tests := []struct {
		name           string
		workflowData   *WorkflowData
		isAliasTrigger bool
		expectBotKey   bool
		description    string
	}{
		{
			name: "slash_command with github-app gets bot-actor isolation",
			workflowData: &WorkflowData{
				On: `on:
  slash_command:
    name: test`,
				SafeOutputs: &SafeOutputsConfig{GitHubApp: appConfig},
			},
			isAliasTrigger: false,
			expectBotKey:   true,
			description:    "slash_command + github-app should include bot-actor isolation",
		},
		{
			name: "issue_comment with github-app gets bot-actor isolation",
			workflowData: &WorkflowData{
				On: `on:
  issue_comment:
    types: [created]`,
				SafeOutputs: &SafeOutputsConfig{GitHubApp: appConfig},
			},
			isAliasTrigger: false,
			expectBotKey:   true,
			description:    "issue_comment + github-app should include bot-actor isolation",
		},
		{
			name: "command trigger with github-app gets bot-actor isolation",
			workflowData: &WorkflowData{
				On:          `on:`,
				Command:     []string{"test-bot"},
				SafeOutputs: &SafeOutputsConfig{GitHubApp: appConfig},
			},
			isAliasTrigger: true,
			expectBotKey:   true,
			description:    "command trigger + github-app should include bot-actor isolation",
		},
		{
			name: "slash_command without github-app does NOT get bot-actor isolation",
			workflowData: &WorkflowData{
				On: `on:
  slash_command:
    name: test`,
				SafeOutputs: &SafeOutputsConfig{},
			},
			isAliasTrigger: false,
			expectBotKey:   false,
			description:    "slash_command without github-app should not include bot-actor isolation",
		},
		{
			name: "issue_comment without safe-outputs does NOT get bot-actor isolation",
			workflowData: &WorkflowData{
				On: `on:
  issue_comment:
    types: [created]`,
				SafeOutputs: nil,
			},
			isAliasTrigger: false,
			expectBotKey:   false,
			description:    "issue_comment without safe-outputs should not include bot-actor isolation",
		},
		{
			name: "issues-only with github-app does NOT get bot-actor isolation",
			workflowData: &WorkflowData{
				On: `on:
  issues:
    types: [opened]`,
				SafeOutputs: &SafeOutputsConfig{GitHubApp: appConfig},
			},
			isAliasTrigger: false,
			expectBotKey:   false,
			description:    "issues-only trigger + github-app does not re-trigger on app-posted comments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := buildConcurrencyGroupKeys(tt.workflowData, tt.isAliasTrigger)

			hasBotKey := false
			for _, key := range keys {
				if strings.Contains(key, botPrefix) {
					hasBotKey = true
					break
				}
			}

			if tt.expectBotKey && !hasBotKey {
				t.Errorf("buildConcurrencyGroupKeys() for %q: expected bot-actor isolation prefix %q in keys %v", tt.name, botPrefix, keys)
			}
			if !tt.expectBotKey && hasBotKey {
				t.Errorf("buildConcurrencyGroupKeys() for %q: unexpected bot-actor isolation prefix %q in keys %v", tt.name, botPrefix, keys)
			}
		})
	}
}

// TestGenerateConcurrencyConfigWithBotIsolation tests that the full concurrency
// config string is correctly generated with bot-actor isolation when applicable.
func TestGenerateConcurrencyConfigWithBotIsolation(t *testing.T) {
	appConfig := &GitHubAppConfig{
		AppID:      "${{ vars.APP_ID }}",
		PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
	}

	tests := []struct {
		name           string
		workflowData   *WorkflowData
		isAliasTrigger bool
		expected       string
		description    string
	}{
		{
			name: "slash_command with github-app uses bot-actor isolation",
			workflowData: &WorkflowData{
				On: `on:
  slash_command:
    name: test`,
				SafeOutputs: &SafeOutputsConfig{GitHubApp: appConfig},
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ contains(github.actor, '[bot]') && github.run_id || github.event.issue.number || github.event.pull_request.number || github.run_id }}"
  queue: max`,
			description: "slash_command + github-app generates bot-isolated concurrency group",
		},
		{
			name: "issue_comment with github-app uses bot-actor isolation",
			workflowData: &WorkflowData{
				On: `on:
  issue_comment:
    types: [created]
  workflow_dispatch:`,
				SafeOutputs: &SafeOutputsConfig{GitHubApp: appConfig},
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ contains(github.actor, '[bot]') && github.run_id || github.event.issue.number || github.run_id }}"
  queue: max`,
			description: "issue_comment + github-app generates bot-isolated concurrency group",
		},
		{
			name: "slash_command without github-app uses standard concurrency key",
			workflowData: &WorkflowData{
				On: `on:
  slash_command:
    name: test`,
				SafeOutputs: &SafeOutputsConfig{},
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.event.pull_request.number || github.run_id }}"
  queue: max`,
			description: "slash_command without github-app uses standard concurrency (backward compat)",
		},
		{
			name: "Custom concurrency is preserved unchanged even with bot risk",
			workflowData: &WorkflowData{
				On: `on:
  slash_command:
    name: test`,
				Concurrency: `concurrency:
  group: "custom-group-${{ github.event.issue.number }}"
  cancel-in-progress: true`,
				SafeOutputs: &SafeOutputsConfig{GitHubApp: appConfig},
			},
			isAliasTrigger: false,
			expected: `concurrency:
  group: "custom-group-${{ github.event.issue.number }}"
  cancel-in-progress: true`,
			description: "Custom concurrency is always preserved as-is (user must add bot isolation themselves)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateConcurrencyConfig(tt.workflowData, tt.isAliasTrigger)
			if result != tt.expected {
				t.Errorf("GenerateConcurrencyConfig() for %q\nExpected:\n%s\nGot:\n%s",
					tt.description, tt.expected, result)
			}
		})
	}
}
