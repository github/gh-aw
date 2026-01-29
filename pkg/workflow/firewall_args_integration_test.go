//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/githubnext/gh-aw/pkg/testutil"
)

// TestFirewallArgsIntegration tests that custom AWF args appear in compiled workflows
func TestFirewallArgsIntegration(t *testing.T) {
	t.Run("workflow with custom firewall args compiles correctly", func(t *testing.T) {
		// Create temporary directory for test
		tmpDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
		err := os.MkdirAll(workflowsDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create workflows directory: %v", err)
		}

		// Create test workflow with custom firewall args
		workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
network:
  firewall:
    args: ["--custom-flag", "custom-value", "--another-arg"]
---

# Test Workflow

Test workflow with custom AWF arguments.
`

		workflowPath := filepath.Join(workflowsDir, "test-firewall-args.md")
		err = os.WriteFile(workflowPath, []byte(workflowContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompilerWithVersion("test-firewall-args")
		compiler.SetSkipValidation(true)

		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatalf("Failed to compile workflow: %v", err)
		}

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-firewall-args.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}

		lockYAML := string(lockContent)

		// Verify custom args are present in the AWF command
		if !strings.Contains(lockYAML, "--custom-flag") {
			t.Error("Compiled workflow should contain custom flag '--custom-flag'")
		}

		if !strings.Contains(lockYAML, "custom-value") {
			t.Error("Compiled workflow should contain custom value 'custom-value'")
		}

		if !strings.Contains(lockYAML, "--another-arg") {
			t.Error("Compiled workflow should contain custom arg '--another-arg'")
		}

		// Verify standard AWF flags are still present
		if !strings.Contains(lockYAML, "--env-all") {
			t.Error("Compiled workflow should still contain '--env-all' flag")
		}

		if !strings.Contains(lockYAML, "--allow-domains") {
			t.Error("Compiled workflow should still contain '--allow-domains' flag")
		}

		if !strings.Contains(lockYAML, "--log-level") {
			t.Error("Compiled workflow should still contain '--log-level' flag")
		}
	})

	t.Run("workflow without custom args uses only default flags", func(t *testing.T) {
		// Create temporary directory for test
		tmpDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
		err := os.MkdirAll(workflowsDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create workflows directory: %v", err)
		}

		// Create test workflow without custom firewall args
		workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
network:
  firewall: true
---

# Test Workflow

Test workflow without custom AWF arguments.
`

		workflowPath := filepath.Join(workflowsDir, "test-no-custom-args.md")
		err = os.WriteFile(workflowPath, []byte(workflowContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompilerWithVersion("test-no-custom-args")
		compiler.SetSkipValidation(true)

		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatalf("Failed to compile workflow: %v", err)
		}

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-no-custom-args.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}

		lockYAML := string(lockContent)

		// Verify standard AWF flags are present
		if !strings.Contains(lockYAML, "--env-all") {
			t.Error("Compiled workflow should contain '--env-all' flag")
		}

		if !strings.Contains(lockYAML, "--allow-domains") {
			t.Error("Compiled workflow should contain '--allow-domains' flag")
		}

		if !strings.Contains(lockYAML, "--log-level") {
			t.Error("Compiled workflow should contain '--log-level' flag")
		}
	})
}
