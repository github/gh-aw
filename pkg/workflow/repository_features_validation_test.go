//go:build integration

package workflow

import (
	"strings"
	"testing"
)

func TestValidateRepositoryFeatures(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expectError  bool
		description  string
	}{
		{
			name: "no safe-outputs configured",
			workflowData: &WorkflowData{
				SafeOutputs: nil,
			},
			expectError: false,
			description: "should pass when no safe-outputs are configured",
		},
		{
			name: "safe-outputs without discussions or issues",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					AddComments: &AddCommentsConfig{},
				},
			},
			expectError: false,
			description: "should pass when safe-outputs don't require discussions or issues",
		},
		{
			name: "create-discussion configured",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					CreateDiscussions: &CreateDiscussionsConfig{},
				},
			},
			expectError: false, // Will not error if getCurrentRepository fails or API call fails
			description: "validation will check discussions but won't fail on API errors",
		},
		{
			name: "create-issue configured",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					CreateIssues: &CreateIssuesConfig{},
				},
			},
			expectError: false, // Will not error if getCurrentRepository fails or API call fails
			description: "validation will check issues but won't fail on API errors",
		},
		{
			name: "both discussions and issues configured",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					CreateDiscussions: &CreateDiscussionsConfig{},
					CreateIssues:      &CreateIssuesConfig{},
				},
			},
			expectError: false, // Will not error if getCurrentRepository fails or API call fails
			description: "validation will check both features but won't fail on API errors",
		},
		{
			name: "add-comment with discussion: true",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					AddComments: &AddCommentsConfig{
						Discussion: boolPtr(true),
					},
				},
			},
			expectError: false, // Will not error if getCurrentRepository fails or API call fails
			description: "validation will check discussions for add-comment but won't fail on API errors",
		},
		{
			name: "add-comment with discussion: false",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					AddComments: &AddCommentsConfig{
						Discussion: boolPtr(false),
					},
				},
			},
			expectError: false,
			description: "should pass when add-comment targets issues/PRs, not discussions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			err := compiler.validateRepositoryFeatures(tt.workflowData)

			if tt.expectError && err == nil {
				t.Errorf("%s: expected error but got none", tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("%s: unexpected error: %v", tt.description, err)
			}
		})
	}
}

func TestGetCurrentRepository(t *testing.T) {
	// This test will only pass when running in a git repository with GitHub remote
	// It's expected to fail in non-git environments
	repo, err := getCurrentRepository()

	if err != nil {
		t.Logf("getCurrentRepository failed (expected in non-git environment): %v", err)
		// Don't fail the test - this is expected when not in a git repo
		return
	}

	if repo == "" {
		t.Error("expected non-empty repository name")
	}

	// Verify format is owner/repo
	if len(repo) < 3 || !strings.Contains(repo, "/") {
		t.Errorf("repository name %q doesn't match expected format owner/repo", repo)
	}

	t.Logf("Current repository: %s", repo)
}

func TestCheckRepositoryHasDiscussions(t *testing.T) {
	// Test with the current repository (github/gh-aw)
	// This test will only pass when GitHub CLI is authenticated
	repo := "github/gh-aw"

	hasDiscussions, err := checkRepositoryHasDiscussions(repo, false)
	if err != nil {
		t.Logf("checkRepositoryHasDiscussions failed (may be auth issue): %v", err)
		// Don't fail - this could be due to auth or network issues
		return
	}

	t.Logf("Repository %s has discussions enabled: %v", repo, hasDiscussions)
}

func TestCheckRepositoryHasIssues(t *testing.T) {
	// Test with the current repository (github/gh-aw)
	// This test will only pass when GitHub CLI is authenticated
	repo := "github/gh-aw"

	hasIssues, err := checkRepositoryHasIssues(repo, false)
	if err != nil {
		t.Logf("checkRepositoryHasIssues failed (may be auth issue): %v", err)
		// Don't fail - this could be due to auth or network issues
		return
	}

	t.Logf("Repository %s has issues enabled: %v", repo, hasIssues)

	// Issues should definitely be enabled for github/gh-aw
	if !hasIssues {
		t.Error("Expected github/gh-aw to have issues enabled")
	}
}

func TestCheckRepositoryInvalidFormat(t *testing.T) {
	// Test with invalid repository format
	_, err := checkRepositoryHasDiscussions("invalid-format", false)
	if err == nil {
		t.Error("expected error for invalid repository format")
	}

	_, err = checkRepositoryHasIssues("invalid/format/too/many/slashes", false)
	if err != nil {
		// This might actually succeed if the API is lenient
		t.Logf("Got error for invalid format (expected): %v", err)
	}
}

func TestCheckRepositoryHasIssuesUncached(t *testing.T) {
	// Test the REST client code path directly
	// This test exercises the api.DefaultRESTClient() and client.Get() path
	repo := "github/gh-aw"

	hasIssues, err := checkRepositoryHasIssuesUncached(repo)
	if err != nil {
		t.Logf("checkRepositoryHasIssuesUncached failed (may be auth issue): %v", err)
		// Don't fail - this could be due to auth or network issues
		return
	}

	t.Logf("Repository %s has issues enabled: %v", repo, hasIssues)

	// Issues should definitely be enabled for github/gh-aw
	if !hasIssues {
		t.Error("Expected github/gh-aw to have issues enabled")
	}
}

func TestCheckRepositoryHasIssuesUncachedWithInvalidRepo(t *testing.T) {
	// Test REST client error handling with non-existent repository
	repo := "githubnext/this-repo-definitely-does-not-exist-12345"

	_, err := checkRepositoryHasIssuesUncached(repo)
	if err == nil {
		t.Error("expected error for non-existent repository")
	}

	// The error should mention either "failed to query repository" or "failed to create REST client"
	// (depending on whether authentication is available)
	if err != nil {
		errMsg := err.Error()
		if !strings.Contains(errMsg, "failed to query repository") && !strings.Contains(errMsg, "failed to create REST client") {
			t.Errorf("expected error to contain 'failed to query repository' or 'failed to create REST client', got: %v", err)
		}
	}
}

func TestCheckRepositoryHasIssuesWithCaching(t *testing.T) {
	// Test that caching works correctly with the REST client
	// Clear cache first to ensure clean state
	ClearRepositoryFeaturesCache()

	repo := "github/gh-aw"

	// First call - should fetch from API
	hasIssues1, err1 := checkRepositoryHasIssues(repo, false)
	if err1 != nil {
		t.Logf("First call failed (may be auth issue): %v", err1)
		// Don't fail - this could be due to auth or network issues
		return
	}

	// Second call - should return cached result
	hasIssues2, err2 := checkRepositoryHasIssues(repo, false)
	if err2 != nil {
		t.Fatalf("Second call failed unexpectedly: %v", err2)
	}

	// Both calls should return the same result
	if hasIssues1 != hasIssues2 {
		t.Errorf("cached result differs from first result: first=%v, second=%v", hasIssues1, hasIssues2)
	}

	// Issues should definitely be enabled for github/gh-aw
	if !hasIssues1 {
		t.Error("Expected github/gh-aw to have issues enabled")
	}
}
