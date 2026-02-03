//go:build !integration

package cli

import (
	"testing"
)

// TestParseGitHubURL tests the parseGitHubURL function directly
func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		name             string
		url              string
		wantRepo         string
		wantWorkflowPath string
		wantWorkflowName string
		wantVersion      string
		wantErr          bool
		errContains      string
	}{
		{
			name:             "blob URL with main branch",
			url:              "https://github.com/github/gh-aw-trial/blob/main/workflows/release-issue-linker.md",
			wantRepo:         "github/gh-aw-trial",
			wantWorkflowPath: "workflows/release-issue-linker.md",
			wantWorkflowName: "release-issue-linker",
			wantVersion:      "main",
			wantErr:          false,
		},
		{
			name:             "tree URL with develop branch",
			url:              "https://github.com/owner/repo/tree/develop/custom/path/workflow.md",
			wantRepo:         "owner/repo",
			wantWorkflowPath: "custom/path/workflow.md",
			wantWorkflowName: "workflow",
			wantVersion:      "develop",
			wantErr:          false,
		},
		{
			name:             "raw URL with version tag",
			url:              "https://github.com/owner/repo/raw/v2.0.0/workflows/helper.md",
			wantRepo:         "owner/repo",
			wantWorkflowPath: "workflows/helper.md",
			wantWorkflowName: "helper",
			wantVersion:      "v2.0.0",
			wantErr:          false,
		},
		{
			name:        "invalid - non-github domain",
			url:         "https://gitlab.com/owner/repo/blob/main/workflows/test.md",
			wantErr:     true,
			errContains: "must be from github.com",
		},
		{
			name:        "invalid - path too short",
			url:         "https://github.com/owner/repo/blob/main",
			wantErr:     true,
			errContains: "path too short",
		},
		{
			name:        "invalid - wrong URL type",
			url:         "https://github.com/owner/repo/commits/main/workflows/test.md",
			wantErr:     true,
			errContains: "expected /blob/, /tree/, or /raw/",
		},
		{
			name:        "invalid - missing .md extension",
			url:         "https://github.com/owner/repo/blob/main/workflows/test.txt",
			wantErr:     true,
			errContains: "must point to a .md file",
		},
		{
			name:             "raw.githubusercontent.com with refs/heads/branch",
			url:              "https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/.github/workflows/shared/mcp/serena.md",
			wantRepo:         "github/gh-aw",
			wantWorkflowPath: ".github/workflows/shared/mcp/serena.md",
			wantWorkflowName: "serena",
			wantVersion:      "main",
			wantErr:          false,
		},
		{
			name:             "raw.githubusercontent.com with commit SHA",
			url:              "https://raw.githubusercontent.com/github/gh-aw/fc7992627494253a869e177e5d1985d25f3bb316/.github/workflows/shared/mcp/serena.md",
			wantRepo:         "github/gh-aw",
			wantWorkflowPath: ".github/workflows/shared/mcp/serena.md",
			wantWorkflowName: "serena",
			wantVersion:      "fc7992627494253a869e177e5d1985d25f3bb316",
			wantErr:          false,
		},
		{
			name:             "raw.githubusercontent.com with refs/tags/tag",
			url:              "https://raw.githubusercontent.com/owner/repo/refs/tags/v1.0.0/workflows/helper.md",
			wantRepo:         "owner/repo",
			wantWorkflowPath: "workflows/helper.md",
			wantWorkflowName: "helper",
			wantVersion:      "v1.0.0",
			wantErr:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := parseGitHubURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseGitHubURL() expected error containing %q, got nil", tt.errContains)
					return
				}
				return
			}

			if err != nil {
				t.Errorf("parseGitHubURL() unexpected error: %v", err)
				return
			}

			if spec.RepoSlug != tt.wantRepo {
				t.Errorf("parseGitHubURL() repo = %q, want %q", spec.RepoSlug, tt.wantRepo)
			}
			if spec.WorkflowPath != tt.wantWorkflowPath {
				t.Errorf("parseGitHubURL() workflowPath = %q, want %q", spec.WorkflowPath, tt.wantWorkflowPath)
			}
			if spec.WorkflowName != tt.wantWorkflowName {
				t.Errorf("parseGitHubURL() workflowName = %q, want %q", spec.WorkflowName, tt.wantWorkflowName)
			}
			if spec.Version != tt.wantVersion {
				t.Errorf("parseGitHubURL() version = %q, want %q", spec.Version, tt.wantVersion)
			}
		})
	}
}
