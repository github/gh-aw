//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestReplyToPullRequestReviewCommentRequiredLabels(t *testing.T) {
	// Test that required-labels is parsed correctly for reply-to-pull-request-review-comment
	tmpDir := testutil.TempDir(t, "output-reply-review-comment-required-labels-test")

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
  reply-to-pull-request-review-comment:
    target: "*"
    required-labels: [automation, my-workflow]
---

# Test Reply to PR Review Comment Required Labels

This workflow tests the reply-to-pull-request-review-comment required-labels configuration.
`

	testFile := filepath.Join(tmpDir, "test-reply-review-comment-required-labels.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with required-labels: %v", err)
	}

	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.ReplyToPullRequestReviewComment == nil {
		t.Fatal("Expected reply-to-pull-request-review-comment configuration to be parsed")
	}

	requiredLabels := workflowData.SafeOutputs.ReplyToPullRequestReviewComment.RequiredLabels
	if len(requiredLabels) != 2 {
		t.Fatalf("Expected 2 required-labels, got %d", len(requiredLabels))
	}

	if requiredLabels[0] != "automation" || requiredLabels[1] != "my-workflow" {
		t.Fatalf("Expected required labels [automation, my-workflow], got %v", requiredLabels)
	}
}

func TestResolvePullRequestReviewThreadRequiredLabels(t *testing.T) {
	// Test that required-labels is parsed correctly for resolve-pull-request-review-thread
	tmpDir := testutil.TempDir(t, "output-resolve-review-thread-required-labels-test")

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
  resolve-pull-request-review-thread:
    target: "*"
    required-labels: [automation, my-workflow]
---

# Test Resolve PR Review Thread Required Labels

This workflow tests the resolve-pull-request-review-thread required-labels configuration.
`

	testFile := filepath.Join(tmpDir, "test-resolve-review-thread-required-labels.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with required-labels: %v", err)
	}

	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.ResolvePullRequestReviewThread == nil {
		t.Fatal("Expected resolve-pull-request-review-thread configuration to be parsed")
	}

	requiredLabels := workflowData.SafeOutputs.ResolvePullRequestReviewThread.RequiredLabels
	if len(requiredLabels) != 2 {
		t.Fatalf("Expected 2 required-labels, got %d", len(requiredLabels))
	}

	if requiredLabels[0] != "automation" || requiredLabels[1] != "my-workflow" {
		t.Fatalf("Expected required labels [automation, my-workflow], got %v", requiredLabels)
	}
}
