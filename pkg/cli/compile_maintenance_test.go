//go:build !integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestCompileSpecificFiles_GeneratesMaintenanceWorkflow(t *testing.T) {
	// Create temporary directory structure
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github/workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Change to temp directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repo: %v", err)
	}

	// Create a workflow with expires field
	workflowContent := `---
name: "Test Workflow with Expires"
on:
  workflow_dispatch:
engine: copilot
safe-outputs:
  create-issue:
    max: 1
    expires: 24
---

Test workflow that creates issues with expiration.
`
	workflowPath := filepath.Join(workflowsDir, "test-expires.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile all workflows in the directory (maintenance workflow is only generated
	// when compiling entire directory, not specific files)
	config := CompileConfig{
		MarkdownFiles:        []string{}, // Empty = compile all
		Verbose:              false,
		EngineOverride:       "",
		Validate:             false,
		Watch:                false,
		WorkflowDir:          "", // Use default directory (empty = .github/workflows)
		NoEmit:               false,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",
		Strict:               false,
	}

	_, err := CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Fatalf("CompileWorkflows failed: %v", err)
	}

	// Verify that the maintenance workflow was generated
	maintenancePath := filepath.Join(workflowsDir, "agentics-maintenance.yml")
	if _, err := os.Stat(maintenancePath); os.IsNotExist(err) {
		t.Error("Expected maintenance workflow to be generated, but it was not")
	} else if err != nil {
		t.Errorf("Error checking maintenance workflow: %v", err)
	}

	// Read the maintenance workflow and verify it contains expected content
	if content, err := os.ReadFile(maintenancePath); err == nil {
		contentStr := string(content)
		if !strings.Contains(contentStr, "Agentic Maintenance") {
			t.Error("Maintenance workflow does not contain expected workflow name 'Agentic Maintenance'")
		}
		if !strings.Contains(contentStr, "schedule:") {
			t.Error("Maintenance workflow does not contain schedule trigger")
		}
	} else {
		t.Errorf("Failed to read maintenance workflow: %v", err)
	}
}

func TestCompileSpecificFiles_DeletesMaintenanceWorkflow(t *testing.T) {
	// Create temporary directory structure
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github/workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Change to temp directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repo: %v", err)
	}

	// Create a maintenance workflow file manually
	maintenancePath := filepath.Join(workflowsDir, "agentics-maintenance.yml")
	maintenanceContent := `name: agentics-maintenance
on:
  schedule:
    - cron: '37 0 * * *'
`
	if err := os.WriteFile(maintenancePath, []byte(maintenanceContent), 0644); err != nil {
		t.Fatalf("Failed to create maintenance workflow: %v", err)
	}

	// Create a workflow WITHOUT expires field
	workflowContent := `---
name: "Test Workflow No Expires"
on:
  workflow_dispatch:
engine: copilot
safe-outputs:
  create-issue:
    max: 1
  noop: false
---

Test workflow that creates issues without expiration.
`
	workflowPath := filepath.Join(workflowsDir, "test-no-expires.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile all workflows in the directory (maintenance workflow is only generated
	// when compiling entire directory, not specific files)
	config := CompileConfig{
		MarkdownFiles:        []string{}, // Empty = compile all
		Verbose:              false,
		EngineOverride:       "",
		Validate:             false,
		Watch:                false,
		WorkflowDir:          "", // Use default directory (empty = .github/workflows)
		NoEmit:               false,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",
		Strict:               false,
	}

	_, err := CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Fatalf("CompileWorkflows failed: %v", err)
	}

	// Verify that the maintenance workflow WAS deleted
	// When compiling specific files, we parse ALL workflows in the directory
	// and if NONE of them have expires, the maintenance workflow should be deleted
	if _, err := os.Stat(maintenancePath); !os.IsNotExist(err) {
		t.Error("Maintenance workflow should be deleted when no workflows have expires field")
	}
}

func TestCompileSpecificFiles_PreservesDisabledImplicitActionFailureExpiry(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github/workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	t.Chdir(tempDir)

	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repo: %v", err)
	}

	workflowContent := `---
name: "Test Workflow No Expires"
on:
  workflow_dispatch:
engine: copilot
---

Test workflow without expiration.
`
	workflowPath := filepath.Join(workflowsDir, "test-no-expires.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	config := CompileConfig{}
	if _, err := CompileWorkflows(context.Background(), config); err != nil {
		t.Fatalf("Full CompileWorkflows failed: %v", err)
	}

	lockPath := filepath.Join(workflowsDir, "test-no-expires.lock.yml")
	fullCompileContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read full compile output: %v", err)
	}
	if !strings.Contains(string(fullCompileContent), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "0"`) {
		t.Fatal("Full compile should disable the implicit action failure expiry")
	}

	config.MarkdownFiles = []string{"test-no-expires"}
	if _, err := CompileWorkflows(context.Background(), config); err != nil {
		t.Fatalf("Targeted CompileWorkflows failed: %v", err)
	}

	targetedCompileContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read targeted compile output: %v", err)
	}
	if string(targetedCompileContent) != string(fullCompileContent) {
		t.Fatal("Targeted compile should produce the same lock file as a full compile")
	}
}

// TestCompileSpecificFiles_PreservesExplicitActionFailureExpiry verifies that a
// targeted compile does not zero out an explicitly configured
// maintenance.action_failure_issue_expires value, even when no
// agentics-maintenance.yml exists yet (the first-compile case).
func TestCompileSpecificFiles_PreservesExplicitActionFailureExpiry(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github/workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	t.Chdir(tempDir)

	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repo: %v", err)
	}

	awConfig := `{
  "maintenance": {
    "action_failure_issue_expires": 48
  }
}
`
	if err := os.WriteFile(filepath.Join(workflowsDir, "aw.json"), []byte(awConfig), 0644); err != nil {
		t.Fatalf("Failed to write aw.json: %v", err)
	}

	workflowContent := `---
name: "Test Workflow No Expires"
on:
  workflow_dispatch:
engine: copilot
---

Test workflow without expiration.
`
	workflowPath := filepath.Join(workflowsDir, "test-no-expires.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Targeted compile, with no agentics-maintenance.yml present yet.
	config := CompileConfig{MarkdownFiles: []string{"test-no-expires"}}
	if _, err := CompileWorkflows(context.Background(), config); err != nil {
		t.Fatalf("Targeted CompileWorkflows failed: %v", err)
	}

	lockPath := filepath.Join(workflowsDir, "test-no-expires.lock.yml")
	content, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read targeted compile output: %v", err)
	}
	if !strings.Contains(string(content), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "48"`) {
		t.Fatal("Targeted compile must preserve the explicit 48-hour action failure expiry, not reset it to 0")
	}
}

// TestCompileSpecificFiles_PatchesDirectPathOutsideDefaultWorkflowDir verifies
// that a targeted compile of a direct file path outside the default workflow
// directory patches the lock file actually emitted next to that input, and
// does not touch an unrelated lock file of the same workflow ID in the
// default workflow directory.
func TestCompileSpecificFiles_PatchesDirectPathOutsideDefaultWorkflowDir(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github/workflows")
	customDir := filepath.Join(tempDir, "custom")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}
	if err := os.MkdirAll(customDir, 0755); err != nil {
		t.Fatalf("Failed to create custom directory: %v", err)
	}

	t.Chdir(tempDir)

	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repo: %v", err)
	}

	workflowContent := `---
name: "Test Workflow No Expires"
on:
  workflow_dispatch:
engine: copilot
---

Test workflow without expiration.
`
	// Same workflow ID (derived from base file name) in both the default
	// workflow directory and an unrelated custom directory.
	defaultPath := filepath.Join(workflowsDir, "foo.md")
	if err := os.WriteFile(defaultPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write default workflow file: %v", err)
	}
	customPath := filepath.Join(customDir, "foo.md")
	if err := os.WriteFile(customPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write custom workflow file: %v", err)
	}

	// Seed the default lock file with an explicit non-zero marker to make sure
	// it is left untouched by the targeted compile of the unrelated custom path.
	defaultLockPath := filepath.Join(workflowsDir, "foo.lock.yml")
	sentinelContent := "GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: \"168\"\n"
	if err := os.WriteFile(defaultLockPath, []byte(sentinelContent), 0644); err != nil {
		t.Fatalf("Failed to seed default lock file: %v", err)
	}

	// Targeted compile of the direct path outside the default workflow directory.
	config := CompileConfig{MarkdownFiles: []string{customPath}}
	if _, err := CompileWorkflows(context.Background(), config); err != nil {
		t.Fatalf("Targeted CompileWorkflows failed: %v", err)
	}

	customLockPath := filepath.Join(customDir, "foo.lock.yml")
	customLockContent, err := os.ReadFile(customLockPath)
	if err != nil {
		t.Fatalf("Failed to read custom compile output: %v", err)
	}
	if !strings.Contains(string(customLockContent), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "0"`) {
		t.Fatal("Compiling a direct path outside the default workflow directory should still reconcile its own lock file")
	}

	// The unrelated default-directory lock file (sharing the same workflow ID)
	// must remain untouched by the targeted compile of the custom path.
	defaultLockContent, err := os.ReadFile(defaultLockPath)
	if err != nil {
		t.Fatalf("Failed to read default lock file: %v", err)
	}
	if string(defaultLockContent) != sentinelContent {
		t.Fatal("Targeted compile of a direct path must not rewrite an unrelated lock file with the same workflow ID")
	}
}

// TestCompileSpecificFiles_DisablesExpiryWhenCloseExpiredJobDisabled verifies
// that the implicit action-failure expiry marker is disabled when
// agentics-maintenance.yml exists but its close-expired-entities job has been
// disabled via maintenance.disabled_jobs, since nothing would consume the marker.
func TestCompileSpecificFiles_DisablesExpiryWhenCloseExpiredJobDisabled(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github/workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	t.Chdir(tempDir)

	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repo: %v", err)
	}

	awConfig := `{
  "maintenance": {
    "disabled_jobs": ["close-expired-entities"]
  }
}
`
	if err := os.WriteFile(filepath.Join(workflowsDir, "aw.json"), []byte(awConfig), 0644); err != nil {
		t.Fatalf("Failed to write aw.json: %v", err)
	}

	// A pre-existing maintenance workflow file, present but (per aw.json above)
	// not actually running the close-expired-entities job.
	maintenancePath := filepath.Join(workflowsDir, "agentics-maintenance.yml")
	maintenanceContent := "name: agentics-maintenance\non:\n  schedule:\n    - cron: '37 0 * * *'\n"
	if err := os.WriteFile(maintenancePath, []byte(maintenanceContent), 0644); err != nil {
		t.Fatalf("Failed to create maintenance workflow: %v", err)
	}

	workflowContent := `---
name: "Test Workflow No Expires"
on:
  workflow_dispatch:
engine: copilot
---

Test workflow without expiration.
`
	workflowPath := filepath.Join(workflowsDir, "test-no-expires.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	config := CompileConfig{MarkdownFiles: []string{"test-no-expires"}}
	if _, err := CompileWorkflows(context.Background(), config); err != nil {
		t.Fatalf("Targeted CompileWorkflows failed: %v", err)
	}

	lockPath := filepath.Join(workflowsDir, "test-no-expires.lock.yml")
	content, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read targeted compile output: %v", err)
	}
	if !strings.Contains(string(content), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "0"`) {
		t.Fatal("Implicit expiry marker should be disabled when the close-expired-entities job is disabled, even if agentics-maintenance.yml exists")
	}
}

func TestCompileWithCustomDir_SkipsMaintenanceWorkflow(t *testing.T) {
	// Create temporary directory structure
	tempDir := testutil.TempDir(t, "test-*")
	customDir := filepath.Join(tempDir, "custom/workflows")
	if err := os.MkdirAll(customDir, 0755); err != nil {
		t.Fatalf("Failed to create custom workflows directory: %v", err)
	}

	// Change to temp directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repo: %v", err)
	}

	// Create a workflow with expires field in custom directory
	workflowContent := `---
name: "Test Workflow with Expires"
on:
  workflow_dispatch:
engine: copilot
safe-outputs:
  create-issue:
    max: 1
    expires: 24
---

Test workflow that creates issues with expiration.
`
	workflowPath := filepath.Join(customDir, "test-expires.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile all workflows in custom --dir
	config := CompileConfig{
		MarkdownFiles:        []string{}, // Empty = compile all files in directory
		Verbose:              false,
		EngineOverride:       "",
		Validate:             false,
		Watch:                false,
		WorkflowDir:          "custom/workflows", // Custom directory
		NoEmit:               false,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",
		Strict:               false,
	}

	_, err := CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Fatalf("CompileWorkflows failed: %v", err)
	}

	// Verify that the maintenance workflow was NOT generated in custom directory
	maintenancePath := filepath.Join(customDir, "agentics-maintenance.yml")
	if _, err := os.Stat(maintenancePath); !os.IsNotExist(err) {
		t.Error("Maintenance workflow should NOT be generated when using custom --dir option")
	}
}

func TestCompileOnlySharedWorkflow_DoesNotPanic(t *testing.T) {
	// Create temporary directory structure
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github/workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Change to temp directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repo: %v", err)
	}

	// Create a shared workflow component (missing top-level "on")
	sharedWorkflowContent := `---
description: "Shared Component"
engine: copilot
---

Shared workflow component.
`
	sharedWorkflowPath := filepath.Join(workflowsDir, "shared-component.md")
	if err := os.WriteFile(sharedWorkflowPath, []byte(sharedWorkflowContent), 0644); err != nil {
		t.Fatalf("Failed to write shared workflow file: %v", err)
	}

	for _, strict := range []bool{false, true} {
		t.Run("strict="+strconv.FormatBool(strict), func(t *testing.T) {
			maintenancePath := filepath.Join(workflowsDir, "agentics-maintenance.yml")
			if err := os.WriteFile(maintenancePath, []byte("stale maintenance workflow"), 0644); err != nil {
				t.Fatalf("Failed to write stale maintenance workflow: %v", err)
			}

			commandsPath := filepath.Join(workflowsDir, "agentic_commands.yml")
			if err := os.WriteFile(commandsPath, []byte("stale centralized commands workflow"), 0644); err != nil {
				t.Fatalf("Failed to write stale centralized commands workflow: %v", err)
			}

			config := CompileConfig{
				MarkdownFiles:        []string{},
				Verbose:              false,
				EngineOverride:       "",
				Validate:             false,
				Watch:                false,
				WorkflowDir:          "",
				NoEmit:               false,
				Purge:                false,
				TrialMode:            false,
				TrialLogicalRepoSlug: "",
				Strict:               strict,
			}

			if _, err := CompileWorkflows(context.Background(), config); err != nil {
				t.Fatalf("CompileWorkflows should succeed for shared-only workflow directory: %v", err)
			}

			if _, err := os.Stat(maintenancePath); !os.IsNotExist(err) {
				t.Error("Stale maintenance workflow should be deleted for shared-only workflow directory")
			}
			if _, err := os.Stat(commandsPath); !os.IsNotExist(err) {
				t.Error("Stale centralized command workflow should be deleted for shared-only workflow directory")
			}
		})
	}
}
