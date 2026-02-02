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

// TestPatchArtifactDownloadInConsolidatedSafeOutputs verifies that the patch artifact
// download step is correctly generated in the consolidated safe_outputs job when
// create-pull-request is used.
//
// This is a verification test that ensures the patch download happens in the correct order:
// 1. Download agent output artifact
// 2. Setup agent output environment variable
// 3. Download patch artifact (aw.patch) ✓ THIS IS WHAT WE'RE VERIFYING
// 4. Setup JavaScript files
// 5. Checkout repository
// 6. Configure Git credentials
// 7. Create Pull Request (which needs the patch file)
func TestPatchArtifactDownloadInConsolidatedSafeOutputs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	testMarkdown := `---
on:
  pull_request:
    types: [opened]
safe-outputs:
  create-pull-request:
    title-prefix: "[bot] "
---

# Test Patch Artifact Download in Consolidated Job

This test verifies that the patch artifact download step is generated correctly
in the consolidated safe_outputs job when create-pull-request is enabled.
`

	mdFile := filepath.Join(tmpDir, "test-patch-download.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// 1. Verify that safe_outputs job exists
	if !strings.Contains(lockContentStr, "safe_outputs:") {
		t.Fatal("Expected consolidated safe_outputs job to be generated")
	}

	// 2. Verify that patch download step exists
	if !strings.Contains(lockContentStr, "- name: Download patch artifact") {
		t.Fatal("Expected 'Download patch artifact' step in safe_outputs job")
	}

	// 3. Verify artifact is downloaded from unified agent-artifacts
	if !strings.Contains(lockContentStr, "name: agent-artifacts") {
		t.Error("Expected patch to be downloaded from 'agent-artifacts' unified artifact")
	}

	// 4. Verify correct download path
	if !strings.Contains(lockContentStr, "path: /tmp/gh-aw/") {
		t.Error("Expected patch artifact to be downloaded to '/tmp/gh-aw/'")
	}

	// 5. Verify the step has continue-on-error set (since patch might not exist)
	if !strings.Contains(lockContentStr, "continue-on-error: true") {
		t.Error("Expected patch download to have continue-on-error: true")
	}

	// 6. Verify the handler manager step exists (which processes create_pull_request)
	if !strings.Contains(lockContentStr, "- name: Process Safe Outputs") {
		t.Error("Expected 'Process Safe Outputs' (handler manager) step in safe_outputs job")
	}

	// 7. Verify the checkout step exists (needed for applying the patch)
	if !strings.Contains(lockContentStr, "- name: Checkout repository") {
		t.Error("Expected 'Checkout repository' step in safe_outputs job")
	}

	// 8. Verify patch download comes BEFORE Process Safe Outputs step
	patchDownloadPos := strings.Index(lockContentStr, "- name: Download patch artifact")
	processSafeOutputsPos := strings.Index(lockContentStr, "- name: Process Safe Outputs")
	if patchDownloadPos == -1 || processSafeOutputsPos == -1 {
		t.Fatal("Both patch download and Process Safe Outputs steps should exist")
	}
	if patchDownloadPos > processSafeOutputsPos {
		t.Error("Patch download step should come BEFORE Process Safe Outputs step")
	}

	// 9. Verify patch download comes AFTER agent output download
	agentOutputPos := strings.Index(lockContentStr, "- name: Download agent output artifact")
	if agentOutputPos == -1 {
		t.Fatal("Agent output download step should exist")
	}
	if patchDownloadPos < agentOutputPos {
		t.Error("Patch download should come AFTER agent output download")
	}
}

// TestPatchArtifactDownloadWithPushToPullRequestBranch verifies that the patch artifact
// download step is also generated when push-to-pull-request-branch is used instead of
// create-pull-request.
func TestPatchArtifactDownloadWithPushToPullRequestBranch(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	testMarkdown := `---
on:
  pull_request:
    types: [opened]
safe-outputs:
  push-to-pull-request-branch:
    target: triggering
---

# Test Patch Artifact Download with Push To PR Branch

This test verifies that the patch artifact download step is generated when
push-to-pull-request-branch is enabled.
`

	mdFile := filepath.Join(tmpDir, "test-push-patch-download.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify patch download step exists
	if !strings.Contains(lockContentStr, "- name: Download patch artifact") {
		t.Fatal("Expected 'Download patch artifact' step when push-to-pull-request-branch is enabled")
	}

	// Verify artifact is downloaded from unified agent-artifacts
	if !strings.Contains(lockContentStr, "name: agent-artifacts") {
		t.Error("Expected patch to be downloaded from 'agent-artifacts' unified artifact")
	}
	if !strings.Contains(lockContentStr, "path: /tmp/gh-aw/") {
		t.Error("Expected patch artifact to be downloaded to '/tmp/gh-aw/'")
	}
}

// TestPatchArtifactDownloadNotGeneratedWithoutPullRequestOperations verifies that
// the patch artifact download step is NOT generated when neither create-pull-request
// nor push-to-pull-request-branch is enabled.
func TestPatchArtifactDownloadNotGeneratedWithoutPullRequestOperations(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	testMarkdown := `---
on:
  pull_request:
    types: [opened]
safe-outputs:
  create-issue:
    title-prefix: "[bot] "
---

# Test Patch Download Not Generated Without PR Operations

This test verifies that the patch artifact download is NOT generated when
only non-PR safe outputs are enabled.
`

	mdFile := filepath.Join(tmpDir, "test-no-patch.md")
	if err := os.WriteFile(mdFile, []byte(testMarkdown), 0644); err != nil {
		t.Fatalf("Failed to write test markdown file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(mdFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// The safe_outputs job should still exist
	if !strings.Contains(lockContentStr, "safe_outputs:") {
		t.Fatal("Expected safe_outputs job to exist")
	}

	// But the patch download step should NOT be present in safe_outputs job
	// Note: patch download might still be in threat_detection job if that's enabled,
	// but we're checking specifically for the one in safe_outputs job
	safeOutputsJobStart := strings.Index(lockContentStr, "safe_outputs:")
	if safeOutputsJobStart == -1 {
		t.Fatal("Could not find safe_outputs job")
	}

	// Find the next job after safe_outputs (or end of file)
	nextJobStart := strings.Index(lockContentStr[safeOutputsJobStart+1:], "\n  ")
	var safeOutputsJobContent string
	if nextJobStart == -1 {
		safeOutputsJobContent = lockContentStr[safeOutputsJobStart:]
	} else {
		safeOutputsJobContent = lockContentStr[safeOutputsJobStart : safeOutputsJobStart+nextJobStart]
	}

	// The patch download step should not be in the safe_outputs job
	// (it might be in threat_detection, but that's a different job)
	if strings.Contains(safeOutputsJobContent, "- name: Download patch artifact") {
		t.Error("Expected NO 'Download patch artifact' step in safe_outputs job when only create-issue is enabled")
	}
}
