//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
)

func TestPostStepsGeneration(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "post-steps-test")

	// Test case with both steps and post-steps
	testContent := `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
steps:
  - name: Pre AI Step
    run: echo "This runs before AI"
post-steps:
  - name: Post AI Step
    run: echo "This runs after AI"
  - name: Another Post Step
    uses: actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f
    with:
      name: test-artifact
      path: test-file.txt
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Test Post Steps Workflow

This workflow tests the post-steps functionality.
`

	testFile := filepath.Join(tmpDir, "test-post-steps.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with post-steps: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-post-steps.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify pre-steps appear before AI execution
	if !strings.Contains(lockContent, "- name: Pre AI Step") {
		t.Error("Expected pre-step 'Pre AI Step' to be in generated workflow")
	}

	// Verify post-steps appear after AI execution
	if !strings.Contains(lockContent, "- name: Post AI Step") {
		t.Error("Expected post-step 'Post AI Step' to be in generated workflow")
	}

	if !strings.Contains(lockContent, "- name: Another Post Step") {
		t.Error("Expected post-step 'Another Post Step' to be in generated workflow")
	}

	// Verify the order: pre-steps should come before AI execution, post-steps after
	// Use indices that exclude comment lines (frontmatter is embedded as comments)
	preStepIndex := indexInNonCommentLines(lockContent, "- name: Pre AI Step")
	aiStepIndex := indexInNonCommentLines(lockContent, "- name: Execute Claude Code CLI")
	postStepIndex := indexInNonCommentLines(lockContent, "- name: Post AI Step")

	if preStepIndex == -1 || aiStepIndex == -1 || postStepIndex == -1 {
		t.Fatal("Could not find expected steps in generated workflow")
	}

	if preStepIndex >= aiStepIndex {
		t.Error("Pre-step should appear before AI execution step")
	}

	if postStepIndex <= aiStepIndex {
		t.Error("Post-step should appear after AI execution step")
	}

	t.Logf("Step order verified: Pre-step (%d) < AI execution (%d) < Post-step (%d)",
		preStepIndex, aiStepIndex, postStepIndex)
}

func TestPostStepsOnly(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "post-steps-only-test")

	// Test case with only post-steps (no pre-steps)
	testContent := `---
on: issues
permissions:
  contents: read
  issues: write
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
post-steps:
  - name: Only Post Step
    run: echo "This runs after AI only"
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Test Post Steps Only Workflow

This workflow tests post-steps without pre-steps.
`

	testFile := filepath.Join(tmpDir, "test-post-steps-only.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow with post-steps only: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-post-steps-only.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify post-step appears after AI execution
	if !strings.Contains(lockContent, "- name: Only Post Step") {
		t.Error("Expected post-step 'Only Post Step' to be in generated workflow")
	}

	// Verify default checkout step is used (since no custom steps defined)
	if !strings.Contains(lockContent, "- name: Checkout repository") {
		t.Error("Expected default checkout step when no custom steps defined")
	}

	// Verify the order: AI execution should come before post-steps
	// Use indices that exclude comment lines (frontmatter is embedded as comments)
	aiStepIndex := indexInNonCommentLines(lockContent, "- name: Execute Claude Code CLI")
	postStepIndex := indexInNonCommentLines(lockContent, "- name: Only Post Step")

	if aiStepIndex == -1 || postStepIndex == -1 {
		t.Fatal("Could not find expected steps in generated workflow")
	}

	if postStepIndex <= aiStepIndex {
		t.Error("Post-step should appear after AI execution step")
	}
}

func TestStopAfterCompiledAway(t *testing.T) {
	// Test that stop-after is properly compiled away and doesn't appear in final YAML
	tmpDir := testutil.TempDir(t, "stop-after-test")

	compiler := NewCompiler()

	tests := []struct {
		name             string
		frontmatter      string
		shouldNotContain []string // Strings that should NOT appear in the lock file
		shouldContain    []string // Strings that should appear in the lock file
		description      string
	}{
		{
			name: "stop-after with workflow_dispatch",
			frontmatter: `---
on:
  workflow_dispatch:
  schedule:
    - cron: "0 2 * * 1-5"
  stop-after: "+48h"
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
---`,
			shouldNotContain: []string{
				"stop-after:",
				"stop-after: +48h",
				"stop-after: \"+48h\"",
			},
			shouldContain: []string{
				"workflow_dispatch:",
				"- cron: \"0 2 * * 1-5\"",
			},
			description: "stop-after should be compiled away when used with workflow_dispatch and schedule",
		},
		{
			name: "stop-after with command trigger",
			frontmatter: `---
on:
  command:
    name: test-bot
  workflow_dispatch:
  stop-after: "2024-12-31T23:59:59Z"
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
---`,
			shouldNotContain: []string{
				"stop-after:",
				"stop-after: 2024-12-31T23:59:59Z",
				"stop-after: \"2024-12-31T23:59:59Z\"",
			},
			shouldContain: []string{
				"workflow_dispatch:",
				"issue_comment:",
				"issues:",
				"pull_request:",
			},
			description: "stop-after should be compiled away when used with alias triggers",
		},
		{
			name: "stop-after with reaction",
			frontmatter: `---
on:
  issues:
    types: [opened]
  reaction: eyes
  stop-after: "+24h"
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
---`,
			shouldNotContain: []string{
				"stop-after:",
				"stop-after: +24h",
				"stop-after: \"+24h\"",
			},
			shouldContain: []string{
				"issues:",
				"types:",
				"- opened",
			},
			description: "stop-after should be compiled away when used with reaction",
		},
		{
			name: "stop-after only with schedule",
			frontmatter: `---
on:
  schedule:
    - cron: "0 9 * * 1"
  stop-after: "+72h"
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
---`,
			shouldNotContain: []string{
				"stop-after:",
				"stop-after: +72h",
				"stop-after: \"+72h\"",
			},
			shouldContain: []string{
				"schedule:",
				"- cron: \"0 9 * * 1\"",
			},
			description: "stop-after should be compiled away when used only with schedule",
		},
		{
			name: "stop-after with both command and reaction",
			frontmatter: `---
on:
  command:
    name: test-bot
  reaction: heart
  workflow_dispatch:
  stop-after: "+36h"
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
---`,
			shouldNotContain: []string{
				"stop-after:",
				"stop-after: +36h",
				"stop-after: \"+36h\"",
			},
			shouldContain: []string{
				"workflow_dispatch:",
				"issue_comment:",
				"issues:",
				"pull_request:",
			},
			description: "stop-after should be compiled away when used with both alias and reaction",
		},
		{
			name: "stop-after with reaction and schedule",
			frontmatter: `---
on:
  issues:
    types: [opened, edited]
  reaction: rocket
  schedule:
    - cron: "0 8 * * *"
  stop-after: "+12h"
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
---`,
			shouldNotContain: []string{
				"stop-after:",
				"stop-after: +12h",
				"stop-after: \"+12h\"",
			},
			shouldContain: []string{
				"issues:",
				"types:",
				"- opened",
				"- edited",
				"schedule:",
				"- cron: \"0 8 * * *\"",
			},
			description: "stop-after should be compiled away when used with reaction and schedule",
		},
		{
			name: "stop-after with command and schedule",
			frontmatter: `---
on:
  command:
    name: scheduler-bot
  schedule:
    - cron: "0 12 * * *"
  workflow_dispatch:
  stop-after: "+96h"
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
---`,
			shouldNotContain: []string{
				"stop-after:",
				"stop-after: +96h",
				"stop-after: \"+96h\"",
			},
			shouldContain: []string{
				"workflow_dispatch:",
				"schedule:",
				"- cron: \"0 12 * * *\"",
				"issue_comment:",
				"issues:",
				"pull_request:",
			},
			description: "stop-after should be compiled away when used with alias and schedule",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test Stop-After Compilation

This workflow tests that stop-after is properly compiled away.
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

			// Check that strings that should NOT appear are indeed absent from non-comment lines
			// (frontmatter is embedded as comments, so we exclude comment lines)
			for _, shouldNotContain := range tt.shouldNotContain {
				if containsInNonCommentLines(lockContent, shouldNotContain) {
					t.Errorf("%s: Lock file should NOT contain '%s' in non-comment lines but it did.\nLock file content:\n%s", tt.description, shouldNotContain, lockContent)
				}
			}

			// Check that expected strings are present
			for _, shouldContain := range tt.shouldContain {
				if !strings.Contains(lockContent, shouldContain) {
					t.Errorf("%s: Expected lock file to contain '%s' but it didn't.\nLock file content:\n%s", tt.description, shouldContain, lockContent)
				}
			}

			// Verify the lock file is valid YAML
			var yamlData map[string]any
			if err := yaml.Unmarshal(content, &yamlData); err != nil {
				t.Errorf("%s: Generated YAML is invalid: %v\nContent:\n%s", tt.description, err, lockContent)
			}
		})
	}
}
