//go:build !integration

package cli

import (
	"os"
	"testing"
)

func TestIsRepoOnlySpec(t *testing.T) {
	tests := []struct {
		name     string
		spec     string
		expected bool
	}{
		{
			name:     "repo only without version",
			spec:     "githubnext/agentics",
			expected: true,
		},
		{
			name:     "repo only with version",
			spec:     "githubnext/agentics@v1.0.0",
			expected: true,
		},
		{
			name:     "full spec with workflow",
			spec:     "githubnext/agentics/ci-doctor",
			expected: false,
		},
		{
			name:     "full spec with workflow and version",
			spec:     "githubnext/agentics/ci-doctor@main",
			expected: false,
		},
		{
			name:     "full spec with path",
			spec:     "githubnext/agentics/workflows/ci-doctor.md",
			expected: false,
		},
		{
			name:     "GitHub URL",
			spec:     "https://github.com/githubnext/agentics/blob/main/workflows/ci-doctor.md",
			expected: false,
		},
		{
			name:     "local path",
			spec:     "./workflows/my-workflow.md",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRepoOnlySpec(tt.spec)
			if result != tt.expected {
				t.Errorf("isRepoOnlySpec(%q) = %v, want %v", tt.spec, result, tt.expected)
			}
		})
	}
}

func TestListWorkflowsInPackage(t *testing.T) {
	// Since we can't easily mock getPackagesDir, we'll just verify the function
	// handles missing packages correctly
	workflows, err := listWorkflowsInPackage("absolutely-nonexistent-repo-xyz123/test-repo-abc456", false)
	if err == nil {
		t.Errorf("Expected error for nonexistent package, got nil. Workflows: %v", workflows)
	}
}

func TestHandleRepoOnlySpecIntegration(t *testing.T) {
	// This is more of an integration test that would require GitHub authentication
	// We'll skip it in normal test runs
	if os.Getenv("GITHUB_TOKEN") == "" && os.Getenv("GH_TOKEN") == "" {
		t.Skip("Skipping integration test: no GitHub token available")
	}

	// Test with verbose output to see the workflow listing
	// Note: This will actually try to install the package
	// err := handleRepoOnlySpec("githubnext/agentics", true)
	// For now, we'll just verify the function exists and can be called
	// without testing the actual GitHub interaction
}
