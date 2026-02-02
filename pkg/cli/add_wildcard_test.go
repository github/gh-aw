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

// TestParseWorkflowSpecWithWildcard tests parsing workflow specs with wildcards
func TestParseWorkflowSpecWithWildcard(t *testing.T) {
	tests := []struct {
		name           string
		spec           string
		expectWildcard bool
		expectError    bool
		expectedRepo   string
		expectedVer    string
	}{
		{
			name:           "wildcard_without_version",
			spec:           "githubnext/agentics/*",
			expectWildcard: true,
			expectError:    false,
			expectedRepo:   "githubnext/agentics",
			expectedVer:    "",
		},
		{
			name:           "wildcard_with_version",
			spec:           "githubnext/agentics/*@v1.0.0",
			expectWildcard: true,
			expectError:    false,
			expectedRepo:   "githubnext/agentics",
			expectedVer:    "v1.0.0",
		},
		{
			name:           "wildcard_with_branch",
			spec:           "owner/repo/*@main",
			expectWildcard: true,
			expectError:    false,
			expectedRepo:   "owner/repo",
			expectedVer:    "main",
		},
		{
			name:           "non_wildcard_spec",
			spec:           "githubnext/agentics/workflow-name",
			expectWildcard: false,
			expectError:    false,
			expectedRepo:   "githubnext/agentics",
			expectedVer:    "",
		},
		{
			name:           "invalid_spec_too_few_parts",
			spec:           "owner/*",
			expectWildcard: false,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseWorkflowSpec(tt.spec)

			if tt.expectError {
				if err == nil {
					t.Errorf("parseWorkflowSpec() expected error for spec '%s', got nil", tt.spec)
				}
				return
			}

			if err != nil {
				t.Errorf("parseWorkflowSpec() unexpected error: %v", err)
				return
			}

			if result.IsWildcard != tt.expectWildcard {
				t.Errorf("parseWorkflowSpec() IsWildcard = %v, expected %v", result.IsWildcard, tt.expectWildcard)
			}

			if tt.expectWildcard {
				if result.WorkflowPath != "*" {
					t.Errorf("parseWorkflowSpec() WorkflowPath = %v, expected '*'", result.WorkflowPath)
				}
				if result.WorkflowName != "*" {
					t.Errorf("parseWorkflowSpec() WorkflowName = %v, expected '*'", result.WorkflowName)
				}
			}

			if result.RepoSlug != tt.expectedRepo {
				t.Errorf("parseWorkflowSpec() RepoSlug = %v, expected %v", result.RepoSlug, tt.expectedRepo)
			}

			if result.Version != tt.expectedVer {
				t.Errorf("parseWorkflowSpec() Version = %v, expected %v", result.Version, tt.expectedVer)
			}
		})
	}
}

// TestDiscoverWorkflowsInPackage tests discovering workflows in an installed package
func TestDiscoverWorkflowsInPackage(t *testing.T) {
	// Create a temporary packages directory structure
	tempDir := testutil.TempDir(t, "test-*")

	// Override packages directory for testing
	t.Setenv("HOME", tempDir)

	// Create a mock package structure (use .aw/packages, not .gh-aw/packages)
	packagePath := filepath.Join(tempDir, ".aw", "packages", "test-owner", "test-repo")
	workflowsDir := filepath.Join(packagePath, "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create test directories: %v", err)
	}

	// Create some mock workflow files with valid frontmatter
	workflows := []string{
		"workflow1.md",
		"workflow2.md",
		"nested/workflow3.md",
	}

	validWorkflowContent := `---
on: push
---

# Test Workflow
`

	for _, wf := range workflows {
		filePath := filepath.Join(packagePath, wf)
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
		if err := os.WriteFile(filePath, []byte(validWorkflowContent), 0644); err != nil {
			t.Fatalf("Failed to create test workflow %s: %v", wf, err)
		}
	}

	// Test discovery
	discovered, err := discoverWorkflowsInPackage("test-owner/test-repo", "", false)
	if err != nil {
		t.Fatalf("discoverWorkflowsInPackage() error = %v", err)
	}

	if len(discovered) != len(workflows) {
		t.Errorf("discoverWorkflowsInPackage() found %d workflows, expected %d", len(discovered), len(workflows))
	}

	// Verify discovered workflow paths
	discoveredPaths := make(map[string]bool)
	for _, spec := range discovered {
		discoveredPaths[spec.WorkflowPath] = true
	}

	for _, expectedPath := range workflows {
		if !discoveredPaths[expectedPath] {
			t.Errorf("Expected workflow %s not found in discovered workflows", expectedPath)
		}
	}

	// Verify all specs have correct repo info
	for _, spec := range discovered {
		if spec.RepoSlug != "test-owner/test-repo" {
			t.Errorf("Workflow spec has incorrect RepoSlug: %s, expected test-owner/test-repo", spec.RepoSlug)
		}
		if spec.IsWildcard {
			t.Errorf("Discovered workflow spec should not be marked as wildcard")
		}
	}
}

// TestDiscoverWorkflowsInPackage_NotFound tests behavior when package is not found
func TestDiscoverWorkflowsInPackage_NotFound(t *testing.T) {
	// Create a temporary packages directory
	tempDir := testutil.TempDir(t, "test-*")

	// Override packages directory for testing
	t.Setenv("HOME", tempDir)

	// Try to discover workflows in a non-existent package
	_, err := discoverWorkflowsInPackage("nonexistent/repo", "", false)
	if err == nil {
		t.Error("discoverWorkflowsInPackage() expected error for non-existent package, got nil")
	}

	if !strings.Contains(err.Error(), "package not found") {
		t.Errorf("discoverWorkflowsInPackage() error should mention 'package not found', got: %v", err)
	}
}

// TestDiscoverWorkflowsInPackage_EmptyPackage tests behavior with empty package
func TestDiscoverWorkflowsInPackage_EmptyPackage(t *testing.T) {
	// Create a temporary packages directory
	tempDir := testutil.TempDir(t, "test-*")

	// Override packages directory for testing
	t.Setenv("HOME", tempDir)

	// Create an empty package directory (use .aw/packages, not .gh-aw/packages)
	packagePath := filepath.Join(tempDir, ".aw", "packages", "empty-owner", "empty-repo")
	if err := os.MkdirAll(packagePath, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Test discovery
	discovered, err := discoverWorkflowsInPackage("empty-owner/empty-repo", "", false)
	if err != nil {
		t.Fatalf("discoverWorkflowsInPackage() error = %v", err)
	}

	if len(discovered) != 0 {
		t.Errorf("discoverWorkflowsInPackage() found %d workflows in empty package, expected 0", len(discovered))
	}
}

// TestExpandWildcardWorkflows tests expanding wildcard workflow specifications
func TestExpandWildcardWorkflows(t *testing.T) {
	// Create a temporary packages directory structure
	tempDir := testutil.TempDir(t, "test-*")

	// Override packages directory for testing
	t.Setenv("HOME", tempDir)

	// Create a mock package with workflows
	packagePath := filepath.Join(tempDir, ".aw", "packages", "test-org", "test-repo")
	workflowsDir := filepath.Join(packagePath, "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create test directories: %v", err)
	}

	// Create mock workflow files with valid frontmatter
	workflows := []string{
		"workflows/workflow1.md",
		"workflows/workflow2.md",
	}

	validWorkflowContent := `---
on: push
---

# Test Workflow
`

	for _, wf := range workflows {
		filePath := filepath.Join(packagePath, wf)
		if err := os.WriteFile(filePath, []byte(validWorkflowContent), 0644); err != nil {
			t.Fatalf("Failed to create test workflow %s: %v", wf, err)
		}
	}

	tests := []struct {
		name          string
		specs         []*WorkflowSpec
		expectedCount int
		expectError   bool
		errorContains string
	}{
		{
			name: "expand_single_wildcard",
			specs: []*WorkflowSpec{
				{
					RepoSpec: RepoSpec{
						RepoSlug: "test-org/test-repo",
						Version:  "",
					},
					WorkflowPath: "*",
					WorkflowName: "*",
					IsWildcard:   true,
				},
			},
			expectedCount: 2,
			expectError:   false,
		},
		{
			name: "mixed_wildcard_and_specific",
			specs: []*WorkflowSpec{
				{
					RepoSpec: RepoSpec{
						RepoSlug: "test-org/test-repo",
						Version:  "",
					},
					WorkflowPath: "*",
					WorkflowName: "*",
					IsWildcard:   true,
				},
				{
					RepoSpec: RepoSpec{
						RepoSlug: "other-org/other-repo",
						Version:  "",
					},
					WorkflowPath: "workflows/specific.md",
					WorkflowName: "specific",
					IsWildcard:   false,
				},
			},
			expectedCount: 3, // 2 from wildcard + 1 specific
			expectError:   false,
		},
		{
			name: "no_wildcard_specs",
			specs: []*WorkflowSpec{
				{
					RepoSpec: RepoSpec{
						RepoSlug: "other-org/other-repo",
						Version:  "",
					},
					WorkflowPath: "workflows/specific.md",
					WorkflowName: "specific",
					IsWildcard:   false,
				},
			},
			expectedCount: 1,
			expectError:   false,
		},
		{
			name:          "empty_input",
			specs:         []*WorkflowSpec{},
			expectedCount: 0,
			expectError:   true,
			errorContains: "no workflows to add after expansion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := expandWildcardWorkflows(tt.specs, false)

			if tt.expectError {
				if err == nil {
					t.Errorf("expandWildcardWorkflows() expected error, got nil")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expandWildcardWorkflows() error should contain '%s', got: %v", tt.errorContains, err)
				}
				return
			}

			if err != nil {
				t.Errorf("expandWildcardWorkflows() unexpected error: %v", err)
				return
			}

			if len(result) != tt.expectedCount {
				t.Errorf("expandWildcardWorkflows() returned %d workflows, expected %d", len(result), tt.expectedCount)
			}

			// Verify no wildcard specs remain in result
			for _, spec := range result {
				if spec.IsWildcard {
					t.Errorf("expandWildcardWorkflows() result contains wildcard spec: %v", spec)
				}
			}
		})
	}
}

// TestExpandWildcardWorkflows_ErrorHandling tests error cases for wildcard expansion
func TestExpandWildcardWorkflows_ErrorHandling(t *testing.T) {
	// Create a temporary packages directory
	tempDir := testutil.TempDir(t, "test-*")

	// Override packages directory for testing
	t.Setenv("HOME", tempDir)

	tests := []struct {
		name          string
		specs         []*WorkflowSpec
		expectError   bool
		errorContains string
	}{
		{
			name: "nonexistent_package",
			specs: []*WorkflowSpec{
				{
					RepoSpec: RepoSpec{
						RepoSlug: "nonexistent/repo",
						Version:  "",
					},
					WorkflowPath: "*",
					WorkflowName: "*",
					IsWildcard:   true,
				},
			},
			expectError:   true,
			errorContains: "failed to discover workflows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := expandWildcardWorkflows(tt.specs, false)

			if tt.expectError {
				if err == nil {
					t.Errorf("expandWildcardWorkflows() expected error, got nil")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expandWildcardWorkflows() error should contain '%s', got: %v", tt.errorContains, err)
				}
			} else if err != nil {
				t.Errorf("expandWildcardWorkflows() unexpected error: %v", err)
			}
		})
	}
}

// TestAddWorkflowWithTracking_WildcardDuplicateHandling tests that when adding workflows from wildcard,
// existing workflows emit warnings and are skipped instead of erroring
func TestAddWorkflowWithTracking_WildcardDuplicateHandling(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := testutil.TempDir(t, "test-*")

	// Override HOME for package discovery
	t.Setenv("HOME", tempDir)

	// Change to the temp directory
	t.Chdir(tempDir)

	// Initialize a git repository
	if err := os.MkdirAll(filepath.Join(tempDir, ".git"), 0755); err != nil {
		t.Fatalf("Failed to create .git directory: %v", err)
	}

	// Run git init to properly initialize the repository
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		t.Logf("Warning: git init failed, trying to continue anyway: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create an existing workflow file
	existingWorkflow := filepath.Join(workflowsDir, "test-workflow.md")
	existingContent := `---
on: push
---

# Test Workflow
`
	if err := os.WriteFile(existingWorkflow, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create existing workflow: %v", err)
	}

	// Create a WorkflowSpec for the same workflow
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "test-org/test-repo",
			Version:  "",
		},
		WorkflowPath: "workflows/test-workflow.md",
		WorkflowName: "test-workflow",
		IsWildcard:   false,
	}

	// Create a mock package structure with the workflow
	packagePath := filepath.Join(tempDir, ".aw", "packages", "test-org", "test-repo", "workflows")
	if err := os.MkdirAll(packagePath, 0755); err != nil {
		t.Fatalf("Failed to create package directory: %v", err)
	}
	mockWorkflow := filepath.Join(packagePath, "test-workflow.md")
	if err := os.WriteFile(mockWorkflow, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create mock workflow: %v", err)
	}

	// Test 1: Non-wildcard duplicate should return error
	t.Run("non_wildcard_duplicate_returns_error", func(t *testing.T) {
		err := addWorkflowWithTracking(spec, 1, false, false, "", "", false, "", nil, false, "", false, "")
		if err == nil {
			t.Error("Expected error for non-wildcard duplicate, got nil")
		}
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			t.Errorf("Expected 'already exists' error, got: %v", err)
		}
	})

	// Test 2: Wildcard duplicate should return nil (skip with warning)
	t.Run("wildcard_duplicate_returns_nil", func(t *testing.T) {
		err := addWorkflowWithTracking(spec, 1, false, false, "", "", false, "", nil, true, "", false, "")
		if err != nil {
			t.Errorf("Expected nil for wildcard duplicate (should skip), got error: %v", err)
		}
	})

	// Test 3: Wildcard duplicate with force flag should succeed
	t.Run("wildcard_duplicate_with_force_succeeds", func(t *testing.T) {
		err := addWorkflowWithTracking(spec, 1, false, false, "", "", true, "", nil, true, "", false, "")
		// This should succeed or return nil
		if err != nil && strings.Contains(err.Error(), "already exists") {
			t.Errorf("Expected success with force flag, got 'already exists' error: %v", err)
		}
	})
}
