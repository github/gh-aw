//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseWorkflowSpecWithWildcard tests parsing workflow specs with wildcards
func TestParseWorkflowSpecWithWildcard(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		spec           string
		expectWildcard bool
		expectError    bool
		expectedRepo   string
		expectedVer    string
		expectedPath   string
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
			name:           "direct_workflow_path_with_branch_ref_containing_slash",
			spec:           "owner/repo/agentic-workflows/pr-review.md@feature/github-agentic-workflows",
			expectWildcard: false,
			expectError:    false,
			expectedRepo:   "owner/repo",
			expectedVer:    "feature/github-agentic-workflows",
			expectedPath:   "agentic-workflows/pr-review.md",
		},
		{
			name:           "direct_workflow_path_with_long_hyphenated_repo_name",
			spec:           "owner/long-repository-name-with-many-hyphens-in-it/agentic-workflows/business-deviation-tracker.md",
			expectWildcard: false,
			expectError:    false,
			expectedRepo:   "owner/long-repository-name-with-many-hyphens-in-it",
			expectedVer:    "",
			expectedPath:   "agentic-workflows/business-deviation-tracker.md",
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
			t.Parallel()
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

			if tt.expectedPath != "" && result.WorkflowPath != tt.expectedPath {
				t.Errorf("parseWorkflowSpec() WorkflowPath = %v, expected %v", result.WorkflowPath, tt.expectedPath)
			}
		})
	}
}

// TestExpandLocalWildcardWorkflows tests expanding local wildcard workflow specifications
func TestExpandLocalWildcardWorkflows(t *testing.T) {
	// Create a temporary directory with workflow files
	tempDir := testutil.TempDir(t, "test-*")

	// Create mock workflow files with valid frontmatter
	validWorkflowContent := `---
on: push
---

# Test Workflow
`

	workflowFiles := []string{"workflow1.md", "workflow2.md", "workflow3.md"}
	for _, wf := range workflowFiles {
		filePath := filepath.Join(tempDir, wf)
		if err := os.WriteFile(filePath, []byte(validWorkflowContent), 0644); err != nil {
			t.Fatalf("Failed to create test workflow %s: %v", wf, err)
		}
	}

	// Also create a non-workflow file that should be ignored
	if err := os.WriteFile(filepath.Join(tempDir, "README.txt"), []byte("not a workflow"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Change to temp dir to test relative paths
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	tests := []struct {
		name          string
		specs         []*WorkflowSpec
		expectedCount int
		expectError   bool
		errorContains string
	}{
		{
			name: "expand_local_wildcard",
			specs: []*WorkflowSpec{
				{
					RepoSpec:     RepoSpec{},
					WorkflowPath: "./*.md",
					WorkflowName: "*",
					IsWildcard:   true,
				},
			},
			expectedCount: 3,
			expectError:   false,
		},
		{
			name: "no_wildcard_specs",
			specs: []*WorkflowSpec{
				{
					RepoSpec:     RepoSpec{},
					WorkflowPath: "./workflow1.md",
					WorkflowName: "workflow1",
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
			result, err := expandLocalWildcardWorkflows(tt.specs, false)

			if tt.expectError {
				if err == nil {
					t.Errorf("expandLocalWildcardWorkflows() expected error, got nil")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("expandLocalWildcardWorkflows() error should contain '%s', got: %v", tt.errorContains, err)
				}
				return
			}

			if err != nil {
				t.Errorf("expandLocalWildcardWorkflows() unexpected error: %v", err)
				return
			}

			if len(result) != tt.expectedCount {
				t.Errorf("expandLocalWildcardWorkflows() returned %d workflows, expected %d", len(result), tt.expectedCount)
			}

			// Verify no wildcard specs remain in result
			for _, spec := range result {
				if spec.IsWildcard {
					t.Errorf("expandLocalWildcardWorkflows() result contains wildcard spec: %v", spec)
				}
			}
		})
	}
}

// TestExpandLocalWildcardWorkflows_NoMatches tests behavior when no files match the wildcard
func TestExpandLocalWildcardWorkflows_NoMatches(t *testing.T) {
	// Create an empty temporary directory
	tempDir := testutil.TempDir(t, "test-*")

	// Change to temp dir to test relative paths
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	specs := []*WorkflowSpec{
		{
			RepoSpec:     RepoSpec{},
			WorkflowPath: "./*.md",
			WorkflowName: "*",
			IsWildcard:   true,
		},
	}

	_, err = expandLocalWildcardWorkflows(specs, false)
	// Should error because no workflows found after expansion
	require.Error(t, err, "Should error when no workflows match")
	require.ErrorContains(t, err, "no workflows to add after expansion")
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

	// Create a mock package structure with the workflow
	packagePath := filepath.Join(tempDir, ".aw", "packages", "test-org", "test-repo", "workflows")
	if err := os.MkdirAll(packagePath, 0755); err != nil {
		t.Fatalf("Failed to create package directory: %v", err)
	}
	mockWorkflow := filepath.Join(packagePath, "test-workflow.md")
	if err := os.WriteFile(mockWorkflow, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create mock workflow: %v", err)
	}

}

// TestExpandLocalWildcard tests the expandLocalWildcard function directly,
// covering the filter (.md only) and map (WorkflowSpec construction) pipeline.
func TestExpandLocalWildcard(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create .md workflow files
	mdFiles := []string{"alpha.md", "beta.md"}
	for _, f := range mdFiles {
		require.NoError(t, os.WriteFile(filepath.Join(tempDir, f), []byte("# Test"), 0644))
	}
	// Create non-.md files that must be excluded
	nonMdFiles := []string{"README.txt", "config.yml", "notes.md.bak"}
	for _, f := range nonMdFiles {
		require.NoError(t, os.WriteFile(filepath.Join(tempDir, f), []byte("other"), 0644))
	}

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()
	require.NoError(t, os.Chdir(tempDir))

	t.Run("only .md files are returned", func(t *testing.T) {
		spec := &WorkflowSpec{
			RepoSpec:     RepoSpec{},
			WorkflowPath: "./*",
			WorkflowName: "*",
			IsWildcard:   true,
		}
		result, err := expandLocalWildcard(spec)
		require.NoError(t, err, "should not error for valid glob pattern")
		require.Len(t, result, 2, "should return exactly the 2 .md files")
		for _, s := range result {
			assert.True(t, strings.HasSuffix(s.WorkflowPath, ".md"), "every result path should end in .md: %s", s.WorkflowPath)
		}
	})

	t.Run("expanded specs are not wildcards", func(t *testing.T) {
		spec := &WorkflowSpec{
			RepoSpec:     RepoSpec{},
			WorkflowPath: "./*.md",
			WorkflowName: "*",
			IsWildcard:   true,
		}
		result, err := expandLocalWildcard(spec)
		require.NoError(t, err)
		for _, s := range result {
			assert.False(t, s.IsWildcard, "expanded spec should not be a wildcard")
		}
	})

	t.Run("repo slug and version are inherited from parent spec", func(t *testing.T) {
		spec := &WorkflowSpec{
			RepoSpec:     RepoSpec{RepoSlug: "owner/repo", Version: "v1.2.3"},
			WorkflowPath: "./*.md",
			WorkflowName: "*",
			IsWildcard:   true,
		}
		result, err := expandLocalWildcard(spec)
		require.NoError(t, err)
		require.NotEmpty(t, result, "should find .md files")
		for _, s := range result {
			assert.Equal(t, "owner/repo", s.RepoSlug, "repo slug should be inherited")
			assert.Equal(t, "v1.2.3", s.Version, "version should be inherited")
		}
	})

	t.Run("no matches returns nil without error", func(t *testing.T) {
		spec := &WorkflowSpec{
			RepoSpec:     RepoSpec{},
			WorkflowPath: "./*.nonexistent",
			WorkflowName: "*",
			IsWildcard:   true,
		}
		result, err := expandLocalWildcard(spec)
		require.NoError(t, err, "no matches should not be an error")
		assert.Nil(t, result, "result should be nil when nothing matches")
	})
}

func TestCheckWorkflowHasDispatchFromContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "mapping",
			content: "---\non:\n  workflow_dispatch:\n---\n",
			want:    true,
		},
		{
			name:    "sequence",
			content: "---\non: [push, workflow_dispatch]\n---\n",
			want:    true,
		},
		{
			name:    "scalar",
			content: "---\non: workflow_dispatch\n---\n",
			want:    true,
		},
		{
			name:    "no dispatch",
			content: "---\non: push\n---\n",
			want:    false,
		},
		{
			name:    "invalid frontmatter",
			content: "---\non: [\n---\n",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkWorkflowHasDispatchFromContent(tt.content); got != tt.want {
				t.Errorf("checkWorkflowHasDispatchFromContent() = %t, want %t", got, tt.want)
			}
		})
	}
}
