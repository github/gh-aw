//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestUpdatePullRequestRequiredFilters(t *testing.T) {
	// Test that required-author, required-labels, and required-title-prefix are parsed correctly
	tmpDir := testutil.TempDir(t, "output-update-pr-required-filters-test")

	testContent := `---
on:
  pull_request:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
strict: false
safe-outputs:
  update-pull-request:
    required-title-prefix: "[ci] "
    required-labels: [automation, bot]
    required-author: app/workflow-bot
    body: true
    sync-stack: false
---

# Test Update Pull Request Required Filters

This workflow tests the update-pull-request required-labels and required-title-prefix configuration.
`

	testFile := filepath.Join(tmpDir, "test-update-pr-required-filters.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with required filters: %v", err)
	}

	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdatePullRequests == nil {
		t.Fatal("Expected update-pull-request configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdatePullRequests.RequiredTitlePrefix != "[ci] " {
		t.Fatalf("Expected required-title-prefix to be '[ci] ', got '%s'", workflowData.SafeOutputs.UpdatePullRequests.RequiredTitlePrefix)
	}

	if len(workflowData.SafeOutputs.UpdatePullRequests.RequiredLabels) != 2 {
		t.Fatalf("Expected 2 required-labels, got %d", len(workflowData.SafeOutputs.UpdatePullRequests.RequiredLabels))
	}
	if workflowData.SafeOutputs.UpdatePullRequests.RequiredAuthor != "app/workflow-bot" {
		t.Fatalf("Expected required-author to be 'app/workflow-bot', got '%s'", workflowData.SafeOutputs.UpdatePullRequests.RequiredAuthor)
	}
	handlerConfig := handlerRegistry["update_pull_request"](workflowData.SafeOutputs)
	if handlerConfig["required_author"] != "app/workflow-bot" {
		t.Fatalf("Expected runtime required_author to be 'app/workflow-bot', got '%v'", handlerConfig["required_author"])
	}

	if workflowData.SafeOutputs.UpdatePullRequests.RequiredLabels[0] != "automation" {
		t.Fatalf("Expected first required label to be 'automation', got '%s'", workflowData.SafeOutputs.UpdatePullRequests.RequiredLabels[0])
	}

	if workflowData.SafeOutputs.UpdatePullRequests.RequiredLabels[1] != "bot" {
		t.Fatalf("Expected second required label to be 'bot', got '%s'", workflowData.SafeOutputs.UpdatePullRequests.RequiredLabels[1])
	}

	if workflowData.SafeOutputs.UpdatePullRequests.UpdateBranchStacks == nil {
		t.Fatal("Expected sync-stack to be parsed")
	}

	if *workflowData.SafeOutputs.UpdatePullRequests.UpdateBranchStacks {
		t.Fatal("Expected sync-stack to be false")
	}
}
