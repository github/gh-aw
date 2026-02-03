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

func TestNetworkMergeWithImports(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared file with network configuration
	sharedNetworkPath := filepath.Join(tempDir, "shared-network.md")
	sharedNetworkContent := `---
network:
  allowed:
    - example.com
    - api.example.com
---

# Shared Network Configuration

This file provides network access to example.com domains.
`
	if err := os.WriteFile(sharedNetworkPath, []byte(sharedNetworkContent), 0644); err != nil {
		t.Fatalf("Failed to write shared network file: %v", err)
	}

	// Create a workflow file that imports the shared network and has its own network config
	// With firewall enabled to trigger AWF integration
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
network:
  allowed:
    - defaults
    - github.com
  firewall: true
imports:
  - shared-network.md
---

# Test Workflow

This workflow should have merged network domains.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	workflowData := string(lockFileContent)

	// The compiled workflow should contain all network domains from both files
	expectedDomains := []string{
		"example.com",
		"api.example.com",
		"github.com",
	}

	for _, domain := range expectedDomains {
		if !strings.Contains(workflowData, domain) {
			t.Errorf("Expected compiled workflow to contain domain %s, but it was not found", domain)
		}
	}

	// Should use AWF with --allow-domains (Claude uses AWF for network restriction)
	if !strings.Contains(workflowData, "--allow-domains") {
		t.Error("Expected compiled workflow to contain --allow-domains configuration (AWF)")
	}
}
