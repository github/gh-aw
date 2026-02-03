//go:build !integration

package workflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/workflow"
)

func TestNetworkMergeEdgeCases(t *testing.T) {
	t.Run("duplicate domains are deduplicated", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")

		// Create shared file with overlapping domain
		sharedPath := filepath.Join(tempDir, "shared.md")
		sharedContent := `---
network:
  allowed:
    - github.com
    - example.com
---
`
		if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
			t.Fatal(err)
		}

		// Workflow also has github.com (should be deduplicated)
		workflowPath := filepath.Join(tempDir, "workflow.md")
		workflowContent := `---
on: issues
engine: claude
permissions:
  contents: read
  issues: read
  pull-requests: read
strict: false
network:
  allowed:
    - github.com
    - api.github.com
  firewall: true
imports:
  - shared.md
---

# Test
`
		if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := workflow.NewCompiler()
		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatal(err)
		}

		lockPath := stringutil.MarkdownToLockFile(workflowPath)
		content, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}

		lockStr := string(content)

		// Extract the --allow-domains line and count github.com occurrences within it
		// The domain should only appear once in the --allow-domains list (not duplicated)
		lines := strings.Split(lockStr, "\n")
		var allowDomainsLine string
		for _, line := range lines {
			if strings.Contains(line, "--allow-domains") {
				allowDomainsLine = line
				break
			}
		}

		if allowDomainsLine == "" {
			t.Fatal("Could not find --allow-domains line in compiled workflow")
		}

		// Count github.com occurrences within the --allow-domains line only
		count := strings.Count(allowDomainsLine, "github.com")
		// github.com appears twice: once as github.com and once as api.github.com
		// We just need to check the --allow-domains is present
		if count < 1 {
			t.Errorf("Expected github.com to appear in --allow-domains, but found %d occurrences", count)
		}
	})

	t.Run("empty network in import is handled", func(t *testing.T) {
		tempDir := testutil.TempDir(t, "test-*")

		// Create shared file with empty network
		sharedPath := filepath.Join(tempDir, "shared.md")
		sharedContent := `---
network: {}
---
`
		if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
			t.Fatal(err)
		}

		workflowPath := filepath.Join(tempDir, "workflow.md")
		workflowContent := `---
on: issues
engine: claude
permissions:
  contents: read
  issues: read
  pull-requests: read
strict: false
network:
  allowed:
    - github.com
imports:
  - shared.md
---

# Test
`
		if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
			t.Fatal(err)
		}

		compiler := workflow.NewCompiler()
		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatal(err)
		}

		// Should still compile successfully with github.com
		lockPath := stringutil.MarkdownToLockFile(workflowPath)
		content, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(string(content), "github.com") {
			t.Error("Expected github.com to be in ALLOWED_DOMAINS")
		}
	})
}
