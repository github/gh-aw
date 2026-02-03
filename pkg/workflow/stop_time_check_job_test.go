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

// TestPreActivationJob tests that pre-activation job is created correctly and combines membership and stop-time checks
func TestPreActivationJob(t *testing.T) {
	tmpDir := testutil.TempDir(t, "pre-activation-job-test")

	compiler := NewCompiler()

	t.Run("pre_activation_job_created_with_stop_after", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch:
  stop-after: "+48h"
roles: [admin, maintainer]
engine: claude
---

# Stop-Time Workflow

This workflow has a stop-after configuration.
`
		workflowFile := filepath.Join(tmpDir, "stop-time-workflow.md")
		if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
			t.Fatal(err)
		}

		err := compiler.CompileWorkflow(workflowFile)
		if err != nil {
			t.Fatalf("Compilation failed: %v", err)
		}

		lockFile := stringutil.MarkdownToLockFile(workflowFile)
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatalf("Failed to read lock file: %v", err)
		}

		lockContentStr := string(lockContent)

		// Verify pre_activation job exists
		if !strings.Contains(lockContentStr, "pre_activation:") {
			t.Error("Expected pre_activation job to be created")
		}

		// Verify safety checks are in pre_activation job, not agent job
		// Note: With alphabetical job sorting, the order in the file is:
		// activation, agent, pre_activation
		preActivationStart := strings.Index(lockContentStr, "pre_activation:")
		agentStart := strings.Index(lockContentStr, "agent:")
		safetyChecksPos := strings.Index(lockContentStr, "Check stop-time limit")

		if safetyChecksPos == -1 {
			t.Error("Expected stop-time check to be present")
		}

		if preActivationStart == -1 {
			t.Error("Expected pre_activation job to exist")
		}

		if agentStart == -1 {
			t.Error("Expected agent job to exist")
		}

		// Safety checks should be in pre_activation job (after pre_activation start)
		if safetyChecksPos < preActivationStart {
			t.Error("Stop-time check should be in pre_activation job, not before it")
		}

		// Safety checks should not be in agent job
		// Agent job comes before pre_activation in alphabetical order
		if safetyChecksPos > agentStart && safetyChecksPos < preActivationStart {
			t.Error("Stop-time check should not be in agent job")
		}

		// Verify pre_activation job outputs "activated" as a direct expression combining both checks
		// Since workflow_dispatch requires permission checks by default, AND has stop-time
		// The expression builder adds parentheses around each condition
		expectedActivated := "activated: ${{ (steps.check_membership.outputs.is_team_member == 'true') && (steps.check_stop_time.outputs.stop_time_ok == 'true') }}"
		if !strings.Contains(lockContentStr, expectedActivated) {
			t.Error("Expected pre_activation job to have combined 'activated' output expression")
		}

		// Verify old jobs don't exist
		if strings.Contains(lockContentStr, "check_membership:") {
			t.Error("check_membership job should not exist with new architecture")
		}
		if strings.Contains(lockContentStr, "stop_time_check:") {
			t.Error("stop_time_check job should not exist with new architecture")
		}
	})

	t.Run("no_pre_activation_job_without_stop_after_or_roles", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch:
engine: claude
roles: all
---

# Normal Workflow

This workflow has no stop-after configuration and roles: all.
`
		workflowFile := filepath.Join(tmpDir, "normal-workflow.md")
		if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
			t.Fatal(err)
		}

		err := compiler.CompileWorkflow(workflowFile)
		if err != nil {
			t.Fatalf("Compilation failed: %v", err)
		}

		lockFile := stringutil.MarkdownToLockFile(workflowFile)
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatalf("Failed to read lock file: %v", err)
		}

		lockContentStr := string(lockContent)

		// Verify pre_activation job does not exist
		if strings.Contains(lockContentStr, "pre_activation:") {
			t.Error("Expected NO pre_activation job without stop-after or permission checks")
		}
	})

	t.Run("pre_activation_job_with_membership_check", func(t *testing.T) {
		workflowContent := `---
on:
  issues:
    types: [opened]
engine: claude
---

# Workflow with Membership Check

This workflow requires membership checks.
`
		workflowFile := filepath.Join(tmpDir, "membership-workflow.md")
		if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
			t.Fatal(err)
		}

		err := compiler.CompileWorkflow(workflowFile)
		if err != nil {
			t.Fatalf("Compilation failed: %v", err)
		}

		lockFile := stringutil.MarkdownToLockFile(workflowFile)
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatalf("Failed to read lock file: %v", err)
		}

		lockContentStr := string(lockContent)

		// Verify pre_activation job exists
		if !strings.Contains(lockContentStr, "pre_activation:") {
			t.Error("Expected pre_activation job for membership check")
		}

		// Verify membership check is in pre_activation job
		if !strings.Contains(lockContentStr, "Check team membership") {
			t.Error("Expected team membership check in pre_activation job")
		}

		// Verify activation job depends on pre_activation and checks activated output
		if !strings.Contains(lockContentStr, "activation:") {
			t.Error("Expected activation job")
		}

		activationIdx := strings.Index(lockContentStr, "activation:")
		agentIdx := strings.Index(lockContentStr, "agent:")

		// Extract activation job section
		activationSection := lockContentStr[activationIdx:agentIdx]

		// Verify activation job depends on pre_activation
		if !strings.Contains(activationSection, "needs: pre_activation") {
			t.Error("Expected activation job to depend on pre_activation job")
		}

		// Verify activation job checks activated output
		if !strings.Contains(activationSection, "needs.pre_activation.outputs.activated == 'true'") {
			t.Error("Expected activation job to check pre_activation.outputs.activated")
		}
	})

	t.Run("pre_activation_job_with_both_membership_and_stop_time", func(t *testing.T) {
		workflowContent := `---
on:
  issues:
    types: [opened]
  stop-after: "+24h"
engine: claude
---

# Workflow with Both Checks

This workflow has both membership check and stop-after.
`
		workflowFile := filepath.Join(tmpDir, "both-checks-workflow.md")
		if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
			t.Fatal(err)
		}

		err := compiler.CompileWorkflow(workflowFile)
		if err != nil {
			t.Fatalf("Compilation failed: %v", err)
		}

		lockFile := stringutil.MarkdownToLockFile(workflowFile)
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatalf("Failed to read lock file: %v", err)
		}

		lockContentStr := string(lockContent)

		// Verify pre_activation job exists
		if !strings.Contains(lockContentStr, "pre_activation:") {
			t.Error("Expected pre_activation job")
		}

		// Verify both membership check and stop-time check are present in the file
		if !strings.Contains(lockContentStr, "Check team membership") {
			t.Error("Expected team membership check in pre_activation job")
		}

		if !strings.Contains(lockContentStr, "Check stop-time limit") {
			t.Error("Expected stop-time check in pre_activation job")
		}

		// Verify the activated output combines both membership and stop-time checks
		// The expression builder adds parentheses around each condition
		expectedActivated := "activated: ${{ (steps.check_membership.outputs.is_team_member == 'true') && (steps.check_stop_time.outputs.stop_time_ok == 'true') }}"
		if !strings.Contains(lockContentStr, expectedActivated) {
			t.Error("Expected activated output to combine both membership and stop-time checks")
		}

		// Verify the structure: membership check happens before stop-time check
		membershipIdx := strings.Index(lockContentStr, "Check team membership")
		stopTimeIdx := strings.Index(lockContentStr, "Check stop-time limit")

		if membershipIdx == -1 {
			t.Error("Could not find membership check")
		}
		if stopTimeIdx == -1 {
			t.Error("Could not find stop-time check")
		}

		if membershipIdx > 0 && stopTimeIdx > 0 && membershipIdx > stopTimeIdx {
			t.Error("Membership check should come before stop-time check")
		}
	})
}
