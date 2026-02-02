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

// TestSkipIfMatchPreActivationJob tests that skip-if-match check is created correctly in pre-activation job
func TestSkipIfMatchPreActivationJob(t *testing.T) {
	tmpDir := testutil.TempDir(t, "skip-if-match-test")

	compiler := NewCompiler()

	t.Run("pre_activation_job_created_with_skip_if_match", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch:
  skip-if-match: "is:issue is:open label:in-progress"
engine: claude
---

# Skip If Match Workflow

This workflow has a skip-if-match configuration.
`
		workflowFile := filepath.Join(tmpDir, "skip-if-match-workflow.md")
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

		// Verify skip-if-match check is present
		if !strings.Contains(lockContentStr, "Check skip-if-match query") {
			t.Error("Expected skip-if-match check to be present")
		}

		// Verify the skip query environment variable is set correctly
		if !strings.Contains(lockContentStr, `GH_AW_SKIP_QUERY: "is:issue is:open label:in-progress"`) {
			t.Error("Expected GH_AW_SKIP_QUERY environment variable with correct value")
		}

		// Verify the check_skip_if_match step ID is present
		if !strings.Contains(lockContentStr, "id: check_skip_if_match") {
			t.Error("Expected check_skip_if_match step ID")
		}

		// Verify the activated output includes skip_check_ok condition
		if !strings.Contains(lockContentStr, "steps.check_skip_if_match.outputs.skip_check_ok") {
			t.Error("Expected activated output to include skip_check_ok condition")
		}

		// Verify skip-if-match is commented out in the frontmatter
		if !strings.Contains(lockContentStr, "# skip-if-match:") {
			t.Error("Expected skip-if-match to be commented out in lock file")
		}

		if !strings.Contains(lockContentStr, "Skip-if-match processed as search check in pre-activation job") {
			t.Error("Expected comment explaining skip-if-match processing")
		}
	})

	t.Run("pre_activation_job_with_multiple_checks", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch:
  stop-after: "+48h"
  skip-if-match: "is:pr is:open"
roles: [admin, maintainer]
engine: claude
---

# Multiple Checks Workflow

This workflow has both stop-after and skip-if-match.
`
		workflowFile := filepath.Join(tmpDir, "multiple-checks-workflow.md")
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

		// Verify both checks are present
		if !strings.Contains(lockContentStr, "Check stop-time limit") {
			t.Error("Expected stop-time check to be present")
		}

		if !strings.Contains(lockContentStr, "Check skip-if-match query") {
			t.Error("Expected skip-if-match check to be present")
		}

		// Verify the activated output includes both conditions
		// The actual format has nested parentheses: ((a && b) && c)
		if !strings.Contains(lockContentStr, "steps.check_membership.outputs.is_team_member == 'true'") ||
			!strings.Contains(lockContentStr, "steps.check_stop_time.outputs.stop_time_ok == 'true'") ||
			!strings.Contains(lockContentStr, "steps.check_skip_if_match.outputs.skip_check_ok == 'true'") {
			t.Error("Expected activated output to include all three conditions")
		}
	})

	t.Run("skip_if_match_without_roles", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch:
  skip-if-match: "is:issue label:bug"
engine: claude
---

# Skip If Match Without Roles

This workflow has skip-if-match but no role restrictions.
`
		workflowFile := filepath.Join(tmpDir, "skip-no-roles-workflow.md")
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

		// Verify pre_activation job exists (created due to skip-if-match)
		if !strings.Contains(lockContentStr, "pre_activation:") {
			t.Error("Expected pre_activation job to be created even without role checks")
		}

		// Verify skip-if-match check is present
		if !strings.Contains(lockContentStr, "Check skip-if-match query") {
			t.Error("Expected skip-if-match check to be present")
		}

		// Since there's no role check, activated should only depend on skip_check_ok
		// Note: There's still a membership check with default roles, so both will be present
		if !strings.Contains(lockContentStr, "steps.check_skip_if_match.outputs.skip_check_ok") {
			t.Error("Expected activated output to include skip_check_ok condition")
		}
	})

	t.Run("skip_if_match_object_format_with_max", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch:
  skip-if-match:
    query: "is:pr is:open"
    max: 3
engine: claude
---

# Skip If Match Object Format

This workflow uses object format with max parameter.
`
		workflowFile := filepath.Join(tmpDir, "skip-object-format-workflow.md")
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

		// Verify skip-if-match check is present
		if !strings.Contains(lockContentStr, "Check skip-if-match query") {
			t.Error("Expected skip-if-match check to be present")
		}

		// Verify the skip query environment variable is set correctly
		if !strings.Contains(lockContentStr, `GH_AW_SKIP_QUERY: "is:pr is:open"`) {
			t.Error("Expected GH_AW_SKIP_QUERY environment variable with correct value")
		}

		// Verify the max matches parameter is set
		if !strings.Contains(lockContentStr, `GH_AW_SKIP_MAX_MATCHES: "3"`) {
			t.Error("Expected GH_AW_SKIP_MAX_MATCHES environment variable with value 3")
		}

		// Verify skip_check_ok condition is used
		if !strings.Contains(lockContentStr, "steps.check_skip_if_match.outputs.skip_check_ok") {
			t.Error("Expected activated output to include skip_check_ok condition")
		}
	})

	t.Run("skip_if_match_object_format_without_max", func(t *testing.T) {
		workflowContent := `---
on:
  workflow_dispatch:
  skip-if-match:
    query: "is:issue is:open label:urgent"
engine: claude
---

# Skip If Match Object Format Without Max

This workflow uses object format but omits max (defaults to 1).
`
		workflowFile := filepath.Join(tmpDir, "skip-object-no-max-workflow.md")
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

		// Verify skip-if-match check is present
		if !strings.Contains(lockContentStr, "Check skip-if-match query") {
			t.Error("Expected skip-if-match check to be present")
		}

		// Verify the skip query environment variable is set correctly
		if !strings.Contains(lockContentStr, `GH_AW_SKIP_QUERY: "is:issue is:open label:urgent"`) {
			t.Error("Expected GH_AW_SKIP_QUERY environment variable with correct value")
		}

		// Verify the max matches parameter defaults to 1
		if !strings.Contains(lockContentStr, `GH_AW_SKIP_MAX_MATCHES: "1"`) {
			t.Error("Expected GH_AW_SKIP_MAX_MATCHES environment variable with default value 1")
		}
	})
}
