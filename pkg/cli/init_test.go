//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestInitRepository(t *testing.T) {
	tests := []struct {
		name      string
		setupRepo bool
		wantError bool
	}{
		{
			name:      "successfully initializes repository",
			setupRepo: true,
			wantError: false,
		},
		{
			name:      "fails when not in git repository",
			setupRepo: false,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for testing
			tempDir := testutil.TempDir(t, "test-*")

			// Change to temp directory
			oldWd, err := os.Getwd()
			if err != nil {
				t.Fatalf("Failed to get current directory: %v", err)
			}
			defer func() {
				_ = os.Chdir(oldWd)
			}()
			err = os.Chdir(tempDir)
			if err != nil {
				t.Fatalf("Failed to change directory: %v", err)
			}

			// Initialize git repo if needed
			if tt.setupRepo {
				if err := exec.Command("git", "init").Run(); err != nil {
					t.Fatalf("Failed to init git repo: %v", err)
				}
			}

			// Call the function (no MCP or campaign)
			err = InitRepository(false, false, false, "", []string{}, false, false, false, false, nil)

			// Check error expectation
			if tt.wantError {
				if err == nil {
					t.Errorf("InitRepository(, false, false, false, nil) expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("InitRepository(, false, false, false, nil) returned unexpected error: %v", err)
			}

			// Verify .gitattributes was created
			gitAttributesPath := filepath.Join(tempDir, ".gitattributes")
			if _, err := os.Stat(gitAttributesPath); os.IsNotExist(err) {
				t.Errorf("Expected .gitattributes file to exist")
			}

			// Note: The .github/aw/logs/.gitignore file is no longer created by init.
			// It is now created by the logs download command on every invocation.

			// Verify .gitattributes contains the correct entry
			content, err := os.ReadFile(gitAttributesPath)
			if err != nil {
				t.Fatalf("Failed to read .gitattributes: %v", err)
			}
			if !strings.Contains(string(content), ".github/workflows/*.lock.yml linguist-generated=true merge=ours") {
				t.Errorf("Expected .gitattributes to contain '.github/workflows/*.lock.yml linguist-generated=true merge=ours'")
			}
		})
	}
}

func TestInitRepository_Idempotent(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := testutil.TempDir(t, "test-*")

	// Change to temp directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Call the function first time
	err = InitRepository(false, false, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) returned error on first call: %v", err)
	}

	// Call the function second time
	err = InitRepository(false, false, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) returned error on second call: %v", err)
	}

	// Verify files still exist and are correct
	gitAttributesPath := filepath.Join(tempDir, ".gitattributes")
	if _, err := os.Stat(gitAttributesPath); os.IsNotExist(err) {
		t.Errorf("Expected .gitattributes file to exist after second call")
	}

	// Note: The .github/aw/logs/.gitignore file is no longer created by init.
	// It is now created by the logs download command on every invocation.
}

func TestInitRepository_Verbose(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := testutil.TempDir(t, "test-*")

	// Change to temp directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Call the function with verbose=true (should not error)
	err = InitRepository(true, false, false, "", []string{}, false, false, false, false, nil)
	if err != nil {
		t.Fatalf("InitRepository(, false, false, false, nil) returned error with verbose=true: %v", err)
	}

	// Verify files were created
	gitAttributesPath := filepath.Join(tempDir, ".gitattributes")
	if _, err := os.Stat(gitAttributesPath); os.IsNotExist(err) {
		t.Errorf("Expected .gitattributes file to exist with verbose=true")
	}
}

func TestEnsureMaintenanceWorkflow(t *testing.T) {
	tests := []struct {
		name                    string
		setupWorkflows          bool
		workflowsWithExpires    bool
		expectMaintenanceFile   bool
		expectMaintenanceDelete bool
	}{
		{
			name:                  "generates maintenance workflow when expires field present",
			setupWorkflows:        true,
			workflowsWithExpires:  true,
			expectMaintenanceFile: true,
		},
		{
			name:                    "deletes maintenance workflow when no expires field",
			setupWorkflows:          true,
			workflowsWithExpires:    false,
			expectMaintenanceDelete: true,
		},
		{
			name:                  "skips when no workflows directory",
			setupWorkflows:        false,
			expectMaintenanceFile: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for testing
			tempDir := testutil.TempDir(t, "test-maintenance-*")

			// Change to temp directory
			oldWd, err := os.Getwd()
			if err != nil {
				t.Fatalf("Failed to get current directory: %v", err)
			}
			defer func() {
				_ = os.Chdir(oldWd)
			}()
			err = os.Chdir(tempDir)
			if err != nil {
				t.Fatalf("Failed to change directory: %v", err)
			}

			// Initialize git repo
			if err := exec.Command("git", "init").Run(); err != nil {
				t.Fatalf("Failed to init git repo: %v", err)
			}

			maintenanceFile := filepath.Join(tempDir, ".github", "workflows", "agentics-maintenance.yml")

			// Setup workflows if needed
			if tt.setupWorkflows {
				workflowsDir := filepath.Join(tempDir, ".github", "workflows")
				if err := os.MkdirAll(workflowsDir, 0755); err != nil {
					t.Fatalf("Failed to create workflows directory: %v", err)
				}

				// Create an existing maintenance file if we're testing deletion
				if tt.expectMaintenanceDelete {
					if err := os.WriteFile(maintenanceFile, []byte("# Test maintenance file\n"), 0644); err != nil {
						t.Fatalf("Failed to create test maintenance file: %v", err)
					}
				}

				// Create a sample workflow with or without expires
				// Note: For the no-expires case, we don't include create-discussion at all
				// because the schema sets a default of 7 days if create-discussion is present
				workflowContent := `---
on:
  issues:
    types: [opened]
`
				if tt.workflowsWithExpires {
					workflowContent += `safe-outputs:
  create-discussion:
    expires: 168
`
				}
				workflowContent += `---

# Test Workflow

This is a test workflow.
`
				workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
				if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
					t.Fatalf("Failed to create test workflow: %v", err)
				}
			}

			// Call ensureMaintenanceWorkflow
			err = ensureMaintenanceWorkflow(false)
			if err != nil {
				t.Logf("ensureMaintenanceWorkflow returned error (may be expected): %v", err)
			}

			// Check if maintenance file exists/was deleted based on expectations
			_, statErr := os.Stat(maintenanceFile)

			if tt.expectMaintenanceFile {
				if os.IsNotExist(statErr) {
					t.Errorf("Expected maintenance workflow file to be created at %s", maintenanceFile)
				}
			}

			if tt.expectMaintenanceDelete {
				if !os.IsNotExist(statErr) {
					t.Errorf("Expected maintenance workflow file to be deleted at %s", maintenanceFile)
				}
			}

			if !tt.expectMaintenanceFile && !tt.expectMaintenanceDelete && !tt.setupWorkflows {
				// When no workflows directory, maintenance file should not exist
				if !os.IsNotExist(statErr) {
					t.Errorf("Did not expect maintenance workflow file to exist when no workflows directory")
				}
			}
		})
	}
}
