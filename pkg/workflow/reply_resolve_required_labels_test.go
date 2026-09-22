//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
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

func TestReplyToPullRequestReviewCommentRequiredLabelsCompiledOutput(t *testing.T) {
	// Verify that required-labels reaches the generated handler config JSON in the compiled lock file
	tmpDir := testutil.TempDir(t, "output-reply-review-comment-required-labels-compiled-test")

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
    required-title-prefix: "[bot] "
---

# Test Reply to PR Review Comment Required Labels Compiled Output

This workflow tests that the reply-to-pull-request-review-comment required-labels
configuration is enforced in the generated handler configuration.
`

	testFile := filepath.Join(tmpDir, "test-reply-review-comment-required-labels-compiled.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	if !strings.Contains(lockContentStr, `reply_to_pull_request_review_comment`) {
		t.Fatal("Expected generated workflow to contain reply_to_pull_request_review_comment handler config")
	}
	if !strings.Contains(lockContentStr, `required_labels`) || !strings.Contains(lockContentStr, `automation`) || !strings.Contains(lockContentStr, `my-workflow`) {
		t.Errorf("Generated workflow should contain required_labels [automation, my-workflow] in handler config JSON")
	}
	if !strings.Contains(lockContentStr, `required_title_prefix`) || !strings.Contains(lockContentStr, `[bot]`) {
		t.Errorf("Generated workflow should contain required_title_prefix [bot] in handler config JSON")
	}
}

func TestResolvePullRequestReviewThreadRequiredLabelsCompiledOutput(t *testing.T) {
	// Verify that required-labels reaches the generated handler config JSON in the compiled lock file
	tmpDir := testutil.TempDir(t, "output-resolve-review-thread-required-labels-compiled-test")

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
    required-title-prefix: "[bot] "
---

# Test Resolve PR Review Thread Required Labels Compiled Output

This workflow tests that the resolve-pull-request-review-thread required-labels
configuration is enforced in the generated handler configuration.
`

	testFile := filepath.Join(tmpDir, "test-resolve-review-thread-required-labels-compiled.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContentStr := string(lockContent)

	if !strings.Contains(lockContentStr, `resolve_pull_request_review_thread`) {
		t.Fatal("Expected generated workflow to contain resolve_pull_request_review_thread handler config")
	}
	if !strings.Contains(lockContentStr, `required_labels`) || !strings.Contains(lockContentStr, `automation`) || !strings.Contains(lockContentStr, `my-workflow`) {
		t.Errorf("Generated workflow should contain required_labels [automation, my-workflow] in handler config JSON")
	}
	if !strings.Contains(lockContentStr, `required_title_prefix`) || !strings.Contains(lockContentStr, `[bot]`) {
		t.Errorf("Generated workflow should contain required_title_prefix [bot] in handler config JSON")
	}
}
