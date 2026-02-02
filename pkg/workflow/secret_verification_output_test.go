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

// TestSecretVerificationOutput tests that the agent job outputs include secret_verification_result
func TestSecretVerificationOutput(t *testing.T) {
	testDir := testutil.TempDir(t, "test-secret-verification-output-*")
	workflowFile := filepath.Join(testDir, "test-workflow.md")

	workflow := `---
on: workflow_dispatch
engine: copilot
---

Test workflow`

	if err := os.WriteFile(workflowFile, []byte(workflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Check that agent job has secret_verification_result output
	if !strings.Contains(lockStr, "secret_verification_result: ${{ steps.validate-secret.outputs.verification_result }}") {
		t.Error("Expected agent job to have secret_verification_result output")
	}

	// Check that validate-secret step has an id
	if !strings.Contains(lockStr, "id: validate-secret") {
		t.Error("Expected validate-secret step to have an id")
	}
}

// TestSecretVerificationOutputInConclusionJob tests that the conclusion job receives the secret verification result
func TestSecretVerificationOutputInConclusionJob(t *testing.T) {
	testDir := testutil.TempDir(t, "test-secret-verification-conclusion-*")
	workflowFile := filepath.Join(testDir, "test-workflow.md")

	workflow := `---
on: workflow_dispatch
engine: copilot
safe-outputs:
  add-comment:
    max: 5
---

Test workflow`

	if err := os.WriteFile(workflowFile, []byte(workflow), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)

	// Check that conclusion job receives secret verification result
	if !strings.Contains(lockStr, "GH_AW_SECRET_VERIFICATION_RESULT: ${{ needs.agent.outputs.secret_verification_result }}") {
		t.Error("Expected conclusion job to receive secret_verification_result from agent job")
	}
}
