//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestEngineConcurrencyIntegration(t *testing.T) {
	tests := []struct {
		name             string
		markdown         string
		expectedInJob    string
		notExpectedInJob string
		description      string
	}{
		{
			name: "Copilot with push does NOT have default concurrency",
			markdown: `---
on: push
engine:
  id: copilot
tools:
  github:
    allowed: [list_issues]
---

# Test workflow
Test content`,
			notExpectedInJob: `concurrency:`,
			description:      "Copilot with push trigger should NOT have default concurrency (special case)",
		},
		{
			name: "Copilot with workflow_dispatch HAS default concurrency",
			markdown: `---
on: workflow_dispatch
engine:
  id: copilot
tools:
  github:
    allowed: [list_issues]
---

# Test workflow
Test content`,
			expectedInJob: `concurrency:
      group: "gh-aw-copilot-${{ github.workflow }}"`,
			description: "Copilot with workflow_dispatch should have default concurrency",
		},
		{
			name: "Claude with issues does NOT have default concurrency",
			markdown: `---
on:
  issues:
    types: [opened]
engine:
  id: claude
tools:
  github:
    allowed: [list_issues]
---

# Test workflow
Test content`,
			notExpectedInJob: `concurrency:`,
			description:      "Claude with issues trigger should NOT have default concurrency (special case)",
		},
		{
			name: "Claude with workflow_dispatch HAS default concurrency",
			markdown: `---
on: workflow_dispatch
engine:
  id: claude
tools:
  github:
    allowed: [list_issues]
---

# Test workflow
Test content`,
			expectedInJob: `concurrency:
      group: "gh-aw-claude-${{ github.workflow }}"`,
			description: "Claude with workflow_dispatch should have default concurrency",
		},
		{
			name: "Custom concurrency with string format",
			markdown: `---
on: push
engine:
  id: claude
  concurrency: "custom-${{ github.ref }}"
tools:
  github:
    allowed: [list_issues]
---

# Test workflow
Test content`,
			expectedInJob: `concurrency:
      group: "custom-${{ github.ref }}"`,
			description: "Should use custom concurrency group from string format",
		},
		{
			name: "Custom concurrency with object format",
			markdown: `---
on: push
engine:
  id: claude
  concurrency:
    group: "my-group-${{ github.workflow }}"
    cancel-in-progress: true
tools:
  github:
    allowed: [list_issues]
---

# Test workflow
Test content`,
			expectedInJob: `concurrency:
      group: "my-group-${{ github.workflow }}"
      cancel-in-progress: true`,
			description: "Should use custom concurrency with cancel-in-progress",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory and file
			tmpDir := testutil.TempDir(t, "test-*")
			workflowPath := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(workflowPath, []byte(tt.markdown), 0644); err != nil {
				t.Fatalf("Failed to write test workflow: %v", err)
			}

			// Compile workflow
			compiler := NewCompiler()
			err := compiler.CompileWorkflow(workflowPath)
			if err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read generated lock file: %v", err)
			}

			// Check if expected concurrency is in the job section
			if tt.expectedInJob != "" && !strings.Contains(string(lockContent), tt.expectedInJob) {
				t.Errorf("Compiled workflow doesn't contain expected concurrency\nExpected to find:\n%s\n\nFull output:\n%s",
					tt.expectedInJob, string(lockContent))
			}

			// Check that notExpectedInJob is NOT in the agent job section
			if tt.notExpectedInJob != "" {
				// Extract agent job section
				agentJobStart := strings.Index(string(lockContent), "agent:")
				if agentJobStart == -1 {
					t.Fatalf("Could not find agent job in compiled workflow")
				}
				// Find the next job (or end of file)
				nextJobStart := strings.Index(string(lockContent)[agentJobStart+10:], "\n  ")
				agentJobSection := ""
				if nextJobStart == -1 {
					agentJobSection = string(lockContent)[agentJobStart:]
				} else {
					agentJobSection = string(lockContent)[agentJobStart : agentJobStart+10+nextJobStart]
				}

				if strings.Contains(agentJobSection, tt.notExpectedInJob) {
					t.Errorf("Compiled workflow contains unexpected content in agent job\nDid not expect to find:\n%s\n\nAgent job section:\n%s",
						tt.notExpectedInJob, agentJobSection)
				}
			}
		})
	}
}
