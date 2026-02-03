//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestBlockedDomainsIntegration tests that blocked domains are properly compiled into workflows
func TestBlockedDomainsIntegration(t *testing.T) {
	t.Run("workflow with blocked domains compiles correctly", func(t *testing.T) {
		// Create temporary directory for test
		tmpDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
		err := os.MkdirAll(workflowsDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create workflows directory: %v", err)
		}

		// Create test workflow with blocked domains
		workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
network:
  firewall: true
  allowed:
    - defaults
    - github
  blocked:
    - tracker.example.com
    - analytics.example.com
---

# Test Workflow

Test workflow with blocked domains.
`

		workflowPath := filepath.Join(workflowsDir, "test-blocked-domains.md")
		err = os.WriteFile(workflowPath, []byte(workflowContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompilerWithVersion("test-blocked-domains")
		compiler.SetSkipValidation(true)

		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatalf("Failed to compile workflow: %v", err)
		}

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-blocked-domains.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}

		lockYAML := string(lockContent)

		// Verify --block-domains flag is present
		if !strings.Contains(lockYAML, "--block-domains") {
			t.Error("Compiled workflow should contain '--block-domains' flag")
		}

		// Verify blocked domains are in the command
		if !strings.Contains(lockYAML, "analytics.example.com") {
			t.Error("Compiled workflow should contain blocked domain 'analytics.example.com'")
		}

		if !strings.Contains(lockYAML, "tracker.example.com") {
			t.Error("Compiled workflow should contain blocked domain 'tracker.example.com'")
		}

		// Verify standard AWF flags are still present
		if !strings.Contains(lockYAML, "--allow-domains") {
			t.Error("Compiled workflow should still contain '--allow-domains' flag")
		}

		if !strings.Contains(lockYAML, "--log-level") {
			t.Error("Compiled workflow should still contain '--log-level' flag")
		}
	})

	t.Run("workflow with blocked ecosystem identifiers compiles correctly", func(t *testing.T) {
		// Create temporary directory for test
		tmpDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
		err := os.MkdirAll(workflowsDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create workflows directory: %v", err)
		}

		// Create test workflow with blocked ecosystem
		workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
network:
  firewall: true
  allowed:
    - defaults
    - github
  blocked:
    - python
---

# Test Workflow

Test workflow with blocked ecosystem.
`

		workflowPath := filepath.Join(workflowsDir, "test-blocked-ecosystem.md")
		err = os.WriteFile(workflowPath, []byte(workflowContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompilerWithVersion("test-blocked-ecosystem")
		compiler.SetSkipValidation(true)

		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatalf("Failed to compile workflow: %v", err)
		}

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-blocked-ecosystem.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}

		lockYAML := string(lockContent)

		// Verify --block-domains flag is present
		if !strings.Contains(lockYAML, "--block-domains") {
			t.Error("Compiled workflow should contain '--block-domains' flag")
		}

		// Verify at least one Python ecosystem domain is blocked
		pythonDomains := []string{"pypi.org", "files.pythonhosted.org"}
		foundPythonDomain := false
		for _, domain := range pythonDomains {
			if strings.Contains(lockYAML, domain) {
				foundPythonDomain = true
				break
			}
		}
		if !foundPythonDomain {
			t.Error("Compiled workflow should contain at least one Python ecosystem domain in blocked list")
		}
	})

	t.Run("workflow without blocked domains does not have block-domains flag", func(t *testing.T) {
		// Create temporary directory for test
		tmpDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
		err := os.MkdirAll(workflowsDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create workflows directory: %v", err)
		}

		// Create test workflow without blocked domains
		workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
network:
  firewall: true
  allowed:
    - defaults
    - github
---

# Test Workflow

Test workflow without blocked domains.
`

		workflowPath := filepath.Join(workflowsDir, "test-no-blocked.md")
		err = os.WriteFile(workflowPath, []byte(workflowContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompilerWithVersion("test-no-blocked")
		compiler.SetSkipValidation(true)

		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatalf("Failed to compile workflow: %v", err)
		}

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-no-blocked.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}

		lockYAML := string(lockContent)

		// Verify --block-domains flag is NOT present
		if strings.Contains(lockYAML, "--block-domains") {
			t.Error("Compiled workflow should NOT contain '--block-domains' flag when no domains are blocked")
		}

		// Verify --allow-domains is still present
		if !strings.Contains(lockYAML, "--allow-domains") {
			t.Error("Compiled workflow should still contain '--allow-domains' flag")
		}
	})

	t.Run("claude workflow with blocked domains compiles correctly", func(t *testing.T) {
		// Create temporary directory for test
		tmpDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
		err := os.MkdirAll(workflowsDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create workflows directory: %v", err)
		}

		// Create test workflow with blocked domains for Claude
		workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: claude
network:
  firewall: true
  allowed:
    - defaults
  blocked:
    - tracker.example.com
---

# Test Workflow

Test Claude workflow with blocked domains.
`

		workflowPath := filepath.Join(workflowsDir, "test-claude-blocked.md")
		err = os.WriteFile(workflowPath, []byte(workflowContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompilerWithVersion("test-claude-blocked")
		compiler.SetSkipValidation(true)

		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatalf("Failed to compile workflow: %v", err)
		}

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-claude-blocked.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}

		lockYAML := string(lockContent)

		// Verify --block-domains flag is present
		if !strings.Contains(lockYAML, "--block-domains") {
			t.Error("Compiled Claude workflow should contain '--block-domains' flag")
		}

		// Verify blocked domain is in the command
		if !strings.Contains(lockYAML, "tracker.example.com") {
			t.Error("Compiled Claude workflow should contain blocked domain 'tracker.example.com'")
		}
	})

	t.Run("codex workflow with blocked domains compiles correctly", func(t *testing.T) {
		// Create temporary directory for test
		tmpDir := testutil.TempDir(t, "test-*")
		workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
		err := os.MkdirAll(workflowsDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create workflows directory: %v", err)
		}

		// Create test workflow with blocked domains for Codex
		workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: codex
network:
  firewall: true
  allowed:
    - defaults
  blocked:
    - tracker.example.com
---

# Test Workflow

Test Codex workflow with blocked domains.
`

		workflowPath := filepath.Join(workflowsDir, "test-codex-blocked.md")
		err = os.WriteFile(workflowPath, []byte(workflowContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompilerWithVersion("test-codex-blocked")
		compiler.SetSkipValidation(true)

		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatalf("Failed to compile workflow: %v", err)
		}

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-codex-blocked.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}

		lockYAML := string(lockContent)

		// Verify --block-domains flag is present
		if !strings.Contains(lockYAML, "--block-domains") {
			t.Error("Compiled Codex workflow should contain '--block-domains' flag")
		}

		// Verify blocked domain is in the command
		if !strings.Contains(lockYAML, "tracker.example.com") {
			t.Error("Compiled Codex workflow should contain blocked domain 'tracker.example.com'")
		}
	})
}
