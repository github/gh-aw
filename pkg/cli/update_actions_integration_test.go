//go:build integration

package cli

import (
	"testing"
)

func TestExtractBaseRepo_Integration(t *testing.T) {
	// Test that extractBaseRepo correctly handles real-world action paths
	// from actions-lock.json
	realWorldCases := map[string]string{
		// Actions with subfolders (the problematic cases)
		"actions/cache/restore":             "actions/cache",
		"actions/cache/save":                "actions/cache",
		"github/codeql-action/upload-sarif": "github/codeql-action",
		"github/codeql-action/analyze":      "github/codeql-action",
		"github/codeql-action/init":         "github/codeql-action",

		// Regular actions (should remain unchanged)
		"actions/checkout":                "actions/checkout",
		"actions/setup-node":              "actions/setup-node",
		"actions/setup-python":            "actions/setup-python",
		"actions/setup-go":                "actions/setup-go",
		"github/stale-repos":              "github/stale-repos",
		"actions/ai-inference":            "actions/ai-inference",
		"actions/create-github-app-token": "actions/create-github-app-token",
	}

	for actionPath, expectedBase := range realWorldCases {
		t.Run(actionPath, func(t *testing.T) {
			got := extractBaseRepo(actionPath)
			if got != expectedBase {
				t.Errorf("extractBaseRepo(%q) = %q, want %q", actionPath, got, expectedBase)
			}
		})
	}
}

// TestExtractBaseRepoAPICompatibility verifies that the base repo can be used
// to construct valid GitHub API URLs
func TestExtractBaseRepoAPICompatibility(t *testing.T) {
	testCases := []struct {
		actionPath   string
		wantRepoPath string
	}{
		{
			actionPath:   "actions/cache/restore",
			wantRepoPath: "actions/cache",
		},
		{
			actionPath:   "github/codeql-action/upload-sarif",
			wantRepoPath: "github/codeql-action",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.actionPath, func(t *testing.T) {
			baseRepo := extractBaseRepo(tc.actionPath)

			// Verify the base repo matches expected format
			if baseRepo != tc.wantRepoPath {
				t.Errorf("extractBaseRepo(%q) = %q, want %q", tc.actionPath, baseRepo, tc.wantRepoPath)
			}

			// Verify it can be used to construct a valid API path
			apiPath := "/repos/" + baseRepo + "/releases"
			expectedAPIPath := "/repos/" + tc.wantRepoPath + "/releases"
			if apiPath != expectedAPIPath {
				t.Errorf("API path = %q, want %q", apiPath, expectedAPIPath)
			}
		})
	}
}
