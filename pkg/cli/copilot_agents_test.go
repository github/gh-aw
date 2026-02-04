//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestDeleteOldAgentFiles tests deletion of old agent files
func TestDeleteOldAgentFiles(t *testing.T) {
	tests := []struct {
		name            string
		filesToCreate   []string // Paths relative to git root
		expectedDeleted []string // Files that should be deleted
	}{
		{
			name: "deletes old agent files from .github/agents",
			filesToCreate: []string{
				".github/agents/create-agentic-workflow.agent.md",
				".github/agents/debug-agentic-workflow.agent.md",
				".github/agents/create-shared-agentic-workflow.agent.md",
			},
			expectedDeleted: []string{
				".github/agents/create-agentic-workflow.agent.md",
				".github/agents/debug-agentic-workflow.agent.md",
				".github/agents/create-shared-agentic-workflow.agent.md",
			},
		},
		{
			name: "deletes singular upgrade-agentic-workflow.md from .github/aw",
			filesToCreate: []string{
				".github/aw/upgrade-agentic-workflow.md",
			},
			expectedDeleted: []string{
				".github/aw/upgrade-agentic-workflow.md",
			},
		},
		{
			name: "deletes both agent and aw files",
			filesToCreate: []string{
				".github/agents/create-agentic-workflow.agent.md",
				".github/aw/upgrade-agentic-workflow.md",
			},
			expectedDeleted: []string{
				".github/agents/create-agentic-workflow.agent.md",
				".github/aw/upgrade-agentic-workflow.md",
			},
		},
		{
			name: "deletes old non-.agent.md files from .github/agents",
			filesToCreate: []string{
				".github/agents/create-agentic-workflow.md",
				".github/agents/create-shared-agentic-workflow.md",
				".github/agents/setup-agentic-workflows.md",
				".github/agents/update-agentic-workflows.md",
				".github/agents/upgrade-agentic-workflows.md",
			},
			expectedDeleted: []string{
				".github/agents/create-agentic-workflow.md",
				".github/agents/create-shared-agentic-workflow.md",
				".github/agents/setup-agentic-workflows.md",
				".github/agents/update-agentic-workflows.md",
				".github/agents/upgrade-agentic-workflows.md",
			},
		},
		{
			name: "deletes all old agent files together",
			filesToCreate: []string{
				".github/agents/create-agentic-workflow.agent.md",
				".github/agents/debug-agentic-workflow.agent.md",
				".github/agents/create-shared-agentic-workflow.agent.md",
				".github/agents/create-agentic-workflow.md",
				".github/agents/create-shared-agentic-workflow.md",
				".github/agents/setup-agentic-workflows.md",
				".github/agents/update-agentic-workflows.md",
				".github/agents/upgrade-agentic-workflows.md",
				".github/aw/upgrade-agentic-workflow.md",
			},
			expectedDeleted: []string{
				".github/agents/create-agentic-workflow.agent.md",
				".github/agents/debug-agentic-workflow.agent.md",
				".github/agents/create-shared-agentic-workflow.agent.md",
				".github/agents/create-agentic-workflow.md",
				".github/agents/create-shared-agentic-workflow.md",
				".github/agents/setup-agentic-workflows.md",
				".github/agents/update-agentic-workflows.md",
				".github/agents/upgrade-agentic-workflows.md",
				".github/aw/upgrade-agentic-workflow.md",
			},
		},
		{
			name:            "handles no files to delete",
			filesToCreate:   []string{},
			expectedDeleted: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for testing
			tempDir := testutil.TempDir(t, "test-*")

			// Change to temp directory and initialize git repo
			oldWd, _ := os.Getwd()
			defer func() {
				_ = os.Chdir(oldWd)
			}()
			err := os.Chdir(tempDir)
			if err != nil {
				t.Fatalf("Failed to change directory: %v", err)
			}

			// Initialize git repo
			if err := exec.Command("git", "init").Run(); err != nil {
				t.Fatalf("Failed to init git repo: %v", err)
			}

			// Create test files
			for _, filePath := range tt.filesToCreate {
				fullPath := filepath.Join(tempDir, filePath)
				dir := filepath.Dir(fullPath)
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatalf("Failed to create directory %s: %v", dir, err)
				}
				if err := os.WriteFile(fullPath, []byte("test content"), 0644); err != nil {
					t.Fatalf("Failed to create file %s: %v", fullPath, err)
				}
			}

			// Call deleteOldAgentFiles
			err = deleteOldAgentFiles(false)
			if err != nil {
				t.Fatalf("deleteOldAgentFiles() returned error: %v", err)
			}

			// Verify expected files were deleted
			for _, filePath := range tt.expectedDeleted {
				fullPath := filepath.Join(tempDir, filePath)
				if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
					t.Errorf("Expected file %s to be deleted, but it still exists", filePath)
				}
			}

			// Verify other files weren't affected (if any exist)
			// For example, the plural form should not be deleted
			pluralPath := filepath.Join(tempDir, ".github/aw/upgrade-agentic-workflows.md")
			if _, err := os.Stat(pluralPath); err == nil {
				// If it existed, it should still exist
				t.Logf("Correctly preserved .github/aw/upgrade-agentic-workflows.md (plural)")
			}
		})
	}
}

// TestDeleteOldTemplateFiles tests deletion of old template files
func TestDeleteOldTemplateFiles(t *testing.T) {
	tests := []struct {
		name             string
		filesToCreate    []string // Files to create in pkg/cli/templates/
		expectedDeleted  []string // Files that should be deleted
		expectDirRemoved bool     // Whether the templates directory should be removed
	}{
		{
			name: "deletes all old template files including agent file and removes directory",
			filesToCreate: []string{
				"agentic-workflows.agent.md",
				"create-agentic-workflow.md",
				"github-agentic-workflows.md",
			},
			expectedDeleted: []string{
				"agentic-workflows.agent.md",
				"create-agentic-workflow.md",
				"github-agentic-workflows.md",
			},
			expectDirRemoved: true,
		},
		{
			name: "deletes all template files",
			filesToCreate: []string{
				"agentic-workflows.agent.md",
				"create-agentic-workflow.md",
				"create-shared-agentic-workflow.md",
				"debug-agentic-workflow.md",
				"github-agentic-workflows.md",
				"serena-tool.md",
				"update-agentic-workflow.md",
				"upgrade-agentic-workflows.md",
			},
			expectedDeleted: []string{
				"agentic-workflows.agent.md",
				"create-agentic-workflow.md",
				"create-shared-agentic-workflow.md",
				"debug-agentic-workflow.md",
				"github-agentic-workflows.md",
				"serena-tool.md",
				"update-agentic-workflow.md",
				"upgrade-agentic-workflows.md",
			},
			expectDirRemoved: true,
		},
		{
			name:             "handles no files to delete",
			filesToCreate:    []string{},
			expectedDeleted:  []string{},
			expectDirRemoved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for testing
			tempDir := testutil.TempDir(t, "test-*")

			// Change to temp directory and initialize git repo
			oldWd, _ := os.Getwd()
			defer func() {
				_ = os.Chdir(oldWd)
			}()
			err := os.Chdir(tempDir)
			if err != nil {
				t.Fatalf("Failed to change directory: %v", err)
			}

			// Initialize git repo
			if err := exec.Command("git", "init").Run(); err != nil {
				t.Fatalf("Failed to init git repo: %v", err)
			}

			// Create templates directory and files
			templatesDir := filepath.Join(tempDir, "pkg", "cli", "templates")
			if len(tt.filesToCreate) > 0 {
				if err := os.MkdirAll(templatesDir, 0755); err != nil {
					t.Fatalf("Failed to create templates directory: %v", err)
				}

				for _, file := range tt.filesToCreate {
					path := filepath.Join(templatesDir, file)
					if err := os.WriteFile(path, []byte("# Test template content"), 0644); err != nil {
						t.Fatalf("Failed to create file %s: %v", file, err)
					}
				}
			}

			// Call deleteOldTemplateFiles
			err = deleteOldTemplateFiles(false)
			if err != nil {
				t.Fatalf("deleteOldTemplateFiles() returned error: %v", err)
			}

			// Check that expected files were deleted
			for _, file := range tt.expectedDeleted {
				path := filepath.Join(templatesDir, file)
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("Expected file %s to be deleted, but it still exists", file)
				}
			}

			// Check if directory was removed
			if tt.expectDirRemoved {
				if _, err := os.Stat(templatesDir); !os.IsNotExist(err) {
					t.Errorf("Expected templates directory to be removed, but it still exists")
				}
			}
		})
	}
}
