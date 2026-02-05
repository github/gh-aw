//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
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

	t.Run("workflow with ssl-bump and allow-urls compiles correctly", func(t *testing.T) {
		// Create temporary directory for test
		tmpDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
		err := os.MkdirAll(workflowsDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create workflows directory: %v", err)
		}

		// Create test workflow with ssl-bump and allow-urls
		workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
network:
  allowed:
    - "github.com"
    - "api.github.com"
  firewall:
    ssl-bump: true
    allow-urls:
      - "https://github.com/githubnext/*"
      - "https://api.github.com/repos/*"
    log-level: debug
---

# Test SSL Bump Workflow

Test workflow with SSL bump and allow-urls configuration.
`

		workflowPath := filepath.Join(workflowsDir, "test-ssl-bump.md")
		err = os.WriteFile(workflowPath, []byte(workflowContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompilerWithVersion("test-ssl-bump")
		compiler.SetSkipValidation(true)

		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatalf("Failed to compile workflow: %v", err)
		}

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-ssl-bump.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}

		lockYAML := string(lockContent)

		// Verify ssl-bump flag is present
		if !strings.Contains(lockYAML, "--ssl-bump") {
			t.Error("Compiled workflow should contain '--ssl-bump' flag")
		}

		// Verify allow-urls flag is present
		if !strings.Contains(lockYAML, "--allow-urls") {
			t.Error("Compiled workflow should contain '--allow-urls' flag")
		}

		// Verify the URL patterns are present
		if !strings.Contains(lockYAML, "https://github.com/githubnext/*") {
			t.Error("Compiled workflow should contain URL pattern 'https://github.com/githubnext/*'")
		}

		if !strings.Contains(lockYAML, "https://api.github.com/repos/*") {
			t.Error("Compiled workflow should contain URL pattern 'https://api.github.com/repos/*'")
		}

		// Verify standard AWF flags are still present
		if !strings.Contains(lockYAML, "--env-all") {
			t.Error("Compiled workflow should still contain '--env-all' flag")
		}

		if !strings.Contains(lockYAML, "--log-level debug") {
			t.Error("Compiled workflow should contain '--log-level debug'")
		}
	})
}
