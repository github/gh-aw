//go:build !integration

package parser

import (
	"strconv"
	"strings"
	"testing"
)

func TestParseGitHubURL_RunURLs(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantHost  string
		wantRunID int64
		wantErr   bool
	}{
		{
			name:      "Standard run URL with /actions/",
			url:       "https://github.com/owner/repo/actions/runs/12345678",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.com",
			wantRunID: 12345678,
			wantErr:   false,
		},
		{
			name:      "Run URL with job",
			url:       "https://github.com/owner/repo/actions/runs/12345678/job/98765432",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.com",
			wantRunID: 12345678,
			wantErr:   false,
		},
		{
			name:      "Run URL with attempts",
			url:       "https://github.com/owner/repo/actions/runs/12345678/attempts/2",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.com",
			wantRunID: 12345678,
			wantErr:   false,
		},
		{
			name:      "Short run URL without /actions/",
			url:       "https://github.com/owner/repo/runs/12345678",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.com",
			wantRunID: 12345678,
			wantErr:   false,
		},
		{
			name:      "Enterprise run URL",
			url:       "https://github.example.com/owner/repo/actions/runs/12345678",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.example.com",
			wantRunID: 12345678,
			wantErr:   false,
		},
		{
			name:    "Invalid run ID",
			url:     "https://github.com/owner/repo/actions/runs/notanumber",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			components, err := ParseGitHubURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseGitHubURL() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseGitHubURL() unexpected error: %v", err)
				return
			}

			if components.Type != URLTypeRun {
				t.Errorf("ParseGitHubURL() type = %v, want %v", components.Type, URLTypeRun)
			}

			if components.Owner != tt.wantOwner {
				t.Errorf("ParseGitHubURL() owner = %v, want %v", components.Owner, tt.wantOwner)
			}

			if components.Repo != tt.wantRepo {
				t.Errorf("ParseGitHubURL() repo = %v, want %v", components.Repo, tt.wantRepo)
			}

			if components.Host != tt.wantHost {
				t.Errorf("ParseGitHubURL() host = %v, want %v", components.Host, tt.wantHost)
			}

			if components.Number != tt.wantRunID {
				t.Errorf("ParseGitHubURL() runID = %v, want %v", components.Number, tt.wantRunID)
			}
		})
	}
}

func TestParseGitHubURL_PRURLs(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantPR    int64
		wantErr   bool
	}{
		{
			name:      "Valid PR URL",
			url:       "https://github.com/trial/repo/pull/234",
			wantOwner: "trial",
			wantRepo:  "repo",
			wantPR:    234,
			wantErr:   false,
		},
		{
			name:      "PR URL with hyphenated repo name",
			url:       "https://github.com/PR-OWNER/PR-REPO/pull/456",
			wantOwner: "PR-OWNER",
			wantRepo:  "PR-REPO",
			wantPR:    456,
			wantErr:   false,
		},
		{
			name:      "PR URL with underscores",
			url:       "https://github.com/test_owner/test_repo/pull/789",
			wantOwner: "test_owner",
			wantRepo:  "test_repo",
			wantPR:    789,
			wantErr:   false,
		},
		{
			name:    "Invalid PR number",
			url:     "https://github.com/owner/repo/pull/abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			components, err := ParseGitHubURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseGitHubURL() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseGitHubURL() unexpected error: %v", err)
				return
			}

			if components.Type != URLTypePullRequest {
				t.Errorf("ParseGitHubURL() type = %v, want %v", components.Type, URLTypePullRequest)
			}

			if components.Owner != tt.wantOwner {
				t.Errorf("ParseGitHubURL() owner = %v, want %v", components.Owner, tt.wantOwner)
			}

			if components.Repo != tt.wantRepo {
				t.Errorf("ParseGitHubURL() repo = %v, want %v", components.Repo, tt.wantRepo)
			}

			if components.Number != tt.wantPR {
				t.Errorf("ParseGitHubURL() prNumber = %v, want %v", components.Number, tt.wantPR)
			}
		})
	}
}

func TestParseGitHubURL_FileURLs(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantType  GitHubURLType
		wantOwner string
		wantRepo  string
		wantRef   string
		wantPath  string
		wantErr   bool
	}{
		{
			name:      "Blob URL with main branch",
			url:       "https://github.com/github/gh-aw-trial/blob/main/workflows/release-issue-linker.md",
			wantType:  URLTypeBlob,
			wantOwner: "github",
			wantRepo:  "gh-aw-trial",
			wantRef:   "main",
			wantPath:  "workflows/release-issue-linker.md",
			wantErr:   false,
		},
		{
			name:      "Tree URL with develop branch",
			url:       "https://github.com/owner/repo/tree/develop/custom/path/workflow.md",
			wantType:  URLTypeTree,
			wantOwner: "owner",
			wantRepo:  "repo",
			wantRef:   "develop",
			wantPath:  "custom/path/workflow.md",
			wantErr:   false,
		},
		{
			name:      "Raw URL with version tag",
			url:       "https://github.com/owner/repo/raw/v2.0.0/workflows/helper.md",
			wantType:  URLTypeRaw,
			wantOwner: "owner",
			wantRepo:  "repo",
			wantRef:   "v2.0.0",
			wantPath:  "workflows/helper.md",
			wantErr:   false,
		},
		{
			name:      "Raw githubusercontent with refs/heads/branch",
			url:       "https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/.github/workflows/shared/mcp/serena.md",
			wantType:  URLTypeRawContent,
			wantOwner: "github",
			wantRepo:  "gh-aw",
			wantRef:   "main",
			wantPath:  ".github/workflows/shared/mcp/serena.md",
			wantErr:   false,
		},
		{
			name:      "Raw githubusercontent with commit SHA",
			url:       "https://raw.githubusercontent.com/github/gh-aw/fc7992627494253a869e177e5d1985d25f3bb316/.github/workflows/shared/mcp/serena.md",
			wantType:  URLTypeRawContent,
			wantOwner: "github",
			wantRepo:  "gh-aw",
			wantRef:   "fc7992627494253a869e177e5d1985d25f3bb316",
			wantPath:  ".github/workflows/shared/mcp/serena.md",
			wantErr:   false,
		},
		{
			name:      "Raw githubusercontent with refs/tags/tag",
			url:       "https://raw.githubusercontent.com/owner/repo/refs/tags/v1.0.0/workflows/helper.md",
			wantType:  URLTypeRawContent,
			wantOwner: "owner",
			wantRepo:  "repo",
			wantRef:   "v1.0.0",
			wantPath:  "workflows/helper.md",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			components, err := ParseGitHubURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseGitHubURL() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseGitHubURL() unexpected error: %v", err)
				return
			}

			if components.Type != tt.wantType {
				t.Errorf("ParseGitHubURL() type = %v, want %v", components.Type, tt.wantType)
			}

			if components.Owner != tt.wantOwner {
				t.Errorf("ParseGitHubURL() owner = %v, want %v", components.Owner, tt.wantOwner)
			}

			if components.Repo != tt.wantRepo {
				t.Errorf("ParseGitHubURL() repo = %v, want %v", components.Repo, tt.wantRepo)
			}

			if components.Ref != tt.wantRef {
				t.Errorf("ParseGitHubURL() ref = %v, want %v", components.Ref, tt.wantRef)
			}

			if components.Path != tt.wantPath {
				t.Errorf("ParseGitHubURL() path = %v, want %v", components.Path, tt.wantPath)
			}
		})
	}
}

func TestParseGitHubURL_IssueURLs(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantIssue int64
		wantErr   bool
	}{
		{
			name:      "Valid issue URL",
			url:       "https://github.com/owner/repo/issues/123",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantIssue: 123,
			wantErr:   false,
		},
		{
			name:    "Invalid issue number",
			url:     "https://github.com/owner/repo/issues/abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			components, err := ParseGitHubURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseGitHubURL() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseGitHubURL() unexpected error: %v", err)
				return
			}

			if components.Type != URLTypeIssue {
				t.Errorf("ParseGitHubURL() type = %v, want %v", components.Type, URLTypeIssue)
			}

			if components.Number != tt.wantIssue {
				t.Errorf("ParseGitHubURL() issueNumber = %v, want %v", components.Number, tt.wantIssue)
			}
		})
	}
}

func TestParseGitHubURL_Errors(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		errContains string
	}{
		{
			name:        "Invalid URL",
			url:         "not-a-url",
			errContains: "host",
		},
		{
			name:        "Path too short",
			url:         "https://github.com/owner",
			errContains: "path too short",
		},
		{
			name:        "Unrecognized format",
			url:         "https://github.com/owner/repo/unknown/path",
			errContains: "unrecognized GitHub URL format",
		},
		{
			name:        "Actions path without runs",
			url:         "https://github.com/owner/repo/actions/workflows",
			errContains: "unrecognized GitHub URL format",
		},
		{
			name:        "Actions/runs path without run ID",
			url:         "https://github.com/owner/repo/actions/runs",
			errContains: "unrecognized",
		},
		{
			name:        "Raw.githubusercontent.com path too short",
			url:         "https://raw.githubusercontent.com/owner/repo",
			errContains: "path too short",
		},
		{
			name:        "Raw.githubusercontent.com refs path too short",
			url:         "https://raw.githubusercontent.com/owner/repo/refs/heads",
			errContains: "refs path too short",
		},
		{
			name:        "Blob path without ref",
			url:         "https://github.com/owner/repo/blob",
			errContains: "unrecognized GitHub URL format",
		},
		{
			name:        "Tree path without ref",
			url:         "https://github.com/owner/repo/tree",
			errContains: "unrecognized GitHub URL format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseGitHubURL(tt.url)

			if err == nil {
				t.Errorf("ParseGitHubURL() expected error but got none")
				return
			}

			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("ParseGitHubURL() error = %v, want error containing %v", err, tt.errContains)
			}
		})
	}
}

func TestParseRunURL(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantRunID    int64
		wantOwner    string
		wantRepo     string
		wantHost     string
		wantJobID    int64
		wantStepNum  int
		wantStepLine int
		wantErr      bool
	}{
		{
			name:      "Numeric run ID",
			input:     "1234567890",
			wantRunID: 1234567890,
			wantOwner: "",
			wantRepo:  "",
			wantHost:  "",
			wantErr:   false,
		},
		{
			name:      "Run URL",
			input:     "https://github.com/owner/repo/actions/runs/12345678",
			wantRunID: 12345678,
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.com",
			wantErr:   false,
		},
		{
			name:      "Job URL",
			input:     "https://github.com/owner/repo/actions/runs/12345678/job/98765432",
			wantRunID: 12345678,
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.com",
			wantJobID: 98765432,
			wantErr:   false,
		},
		{
			name:         "Job URL with step fragment",
			input:        "https://github.com/owner/repo/actions/runs/12345678/job/98765432#step:7:1",
			wantRunID:    12345678,
			wantOwner:    "owner",
			wantRepo:     "repo",
			wantHost:     "github.com",
			wantJobID:    98765432,
			wantStepNum:  7,
			wantStepLine: 1,
			wantErr:      false,
		},
		{
			name:        "Job URL with step fragment (no line)",
			input:       "https://github.com/github/gh-aw/actions/runs/20623556740/job/59230494223#step:7",
			wantRunID:   20623556740,
			wantOwner:   "github",
			wantRepo:    "gh-aw",
			wantHost:    "github.com",
			wantJobID:   59230494223,
			wantStepNum: 7,
			wantErr:     false,
		},
		{
			name:      "Short run URL",
			input:     "https://github.com/owner/repo/runs/12345678",
			wantRunID: 12345678,
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.com",
			wantErr:   false,
		},
		{
			name:      "Enterprise URL",
			input:     "https://github.example.com/owner/repo/actions/runs/12345678",
			wantRunID: 12345678,
			wantOwner: "owner",
			wantRepo:  "repo",
			wantHost:  "github.example.com",
			wantErr:   false,
		},
		{
			name:    "Invalid format",
			input:   "not-a-number",
			wantErr: true,
		},
		{
			name:    "Invalid URL without run ID",
			input:   "https://github.com/owner/repo/actions",
			wantErr: true,
		},
		{
			name:    "Empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For numeric IDs, use ParseRunURLExtended
			// For URLs, use ParseGitHubURL
			var components *GitHubURLComponents
			var err error

			if _, numErr := strconv.ParseInt(tt.input, 10, 64); numErr == nil {
				// It's a numeric ID
				components, err = ParseRunURLExtended(tt.input)
			} else {
				// It's a URL
				components, err = ParseGitHubURL(tt.input)
			}

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseRunURL() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseRunURL() unexpected error: %v", err)
				return
			}

			if components.Number != tt.wantRunID {
				t.Errorf("ParseRunURL() runID = %v, want %v", components.Number, tt.wantRunID)
			}

			if components.Owner != tt.wantOwner {
				t.Errorf("ParseRunURL() owner = %v, want %v", components.Owner, tt.wantOwner)
			}

			if components.Repo != tt.wantRepo {
				t.Errorf("ParseRunURL() repo = %v, want %v", components.Repo, tt.wantRepo)
			}

			if components.Host != tt.wantHost {
				t.Errorf("ParseRunURL() host = %v, want %v", components.Host, tt.wantHost)
			}

			if components.JobID != tt.wantJobID {
				t.Errorf("ParseRunURL() jobID = %v, want %v", components.JobID, tt.wantJobID)
			}

			if components.StepNumber != tt.wantStepNum {
				t.Errorf("ParseRunURL() stepNumber = %v, want %v", components.StepNumber, tt.wantStepNum)
			}

			if components.StepLine != tt.wantStepLine {
				t.Errorf("ParseRunURL() stepLine = %v, want %v", components.StepLine, tt.wantStepLine)
			}
		})
	}
}

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantPR    int
		wantErr   bool
	}{
		{
			name:      "Valid GitHub PR URL",
			url:       "https://github.com/trial/repo/pull/234",
			wantOwner: "trial",
			wantRepo:  "repo",
			wantPR:    234,
			wantErr:   false,
		},
		{
			name:      "Valid GitHub PR URL with hyphenated repo name",
			url:       "https://github.com/PR-OWNER/PR-REPO/pull/456",
			wantOwner: "PR-OWNER",
			wantRepo:  "PR-REPO",
			wantPR:    456,
			wantErr:   false,
		},
		{
			name:      "Valid GitHub PR URL with underscores",
			url:       "https://github.com/test_owner/test_repo/pull/789",
			wantOwner: "test_owner",
			wantRepo:  "test_repo",
			wantPR:    789,
			wantErr:   false,
		},
		{
			name:    "Invalid URL format",
			url:     "not-a-url",
			wantErr: true,
		},
		{
			name:      "Enterprise GitHub URL now accepted",
			url:       "https://github.example.com/owner/repo/pull/123",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantPR:    123,
			wantErr:   false,
		},
		{
			name:    "Invalid GitHub URL path - missing pull",
			url:     "https://github.com/owner/repo/123",
			wantErr: true,
		},
		{
			name:    "Invalid GitHub URL path - wrong format",
			url:     "https://github.com/owner/repo/pulls/123",
			wantErr: true,
		},
		{
			name:    "Invalid PR number",
			url:     "https://github.com/owner/repo/pull/abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, prNumber, err := ParsePRURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParsePRURL() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParsePRURL() unexpected error: %v", err)
				return
			}

			if owner != tt.wantOwner {
				t.Errorf("ParsePRURL() owner = %v, want %v", owner, tt.wantOwner)
			}

			if repo != tt.wantRepo {
				t.Errorf("ParsePRURL() repo = %v, want %v", repo, tt.wantRepo)
			}

			if prNumber != tt.wantPR {
				t.Errorf("ParsePRURL() prNumber = %v, want %v", prNumber, tt.wantPR)
			}
		})
	}
}

func TestParseRepoFileURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantRef   string
		wantPath  string
		wantErr   bool
	}{
		{
			name:      "Blob URL",
			url:       "https://github.com/owner/repo/blob/main/path/to/file.md",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantRef:   "main",
			wantPath:  "path/to/file.md",
			wantErr:   false,
		},
		{
			name:      "Tree URL",
			url:       "https://github.com/owner/repo/tree/develop/src",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantRef:   "develop",
			wantPath:  "src",
			wantErr:   false,
		},
		{
			name:      "Raw URL",
			url:       "https://github.com/owner/repo/raw/v1.0.0/README.md",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantRef:   "v1.0.0",
			wantPath:  "README.md",
			wantErr:   false,
		},
		{
			name:      "Raw githubusercontent URL",
			url:       "https://raw.githubusercontent.com/owner/repo/main/path/to/file.md",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantRef:   "main",
			wantPath:  "path/to/file.md",
			wantErr:   false,
		},
		{
			name:      "Raw githubusercontent with refs/heads",
			url:       "https://raw.githubusercontent.com/owner/repo/refs/heads/feature/path/file.md",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantRef:   "feature",
			wantPath:  "path/file.md",
			wantErr:   false,
		},
		{
			name:    "Not a file URL - PR",
			url:     "https://github.com/owner/repo/pull/123",
			wantErr: true,
		},
		{
			name:    "Not a file URL - Issue",
			url:     "https://github.com/owner/repo/issues/456",
			wantErr: true,
		},
		{
			name:    "Not a file URL - Run",
			url:     "https://github.com/owner/repo/actions/runs/789",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ref, path, err := ParseRepoFileURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseRepoFileURL() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParseRepoFileURL() unexpected error: %v", err)
				return
			}

			if owner != tt.wantOwner {
				t.Errorf("ParseRepoFileURL() owner = %v, want %v", owner, tt.wantOwner)
			}

			if repo != tt.wantRepo {
				t.Errorf("ParseRepoFileURL() repo = %v, want %v", repo, tt.wantRepo)
			}

			if ref != tt.wantRef {
				t.Errorf("ParseRepoFileURL() ref = %v, want %v", ref, tt.wantRef)
			}

			if path != tt.wantPath {
				t.Errorf("ParseRepoFileURL() path = %v, want %v", path, tt.wantPath)
			}
		})
	}
}

func TestIsValidGitHubIdentifier(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "Valid simple name",
			input: "owner",
			want:  true,
		},
		{
			name:  "Valid with hyphen",
			input: "my-repo",
			want:  true,
		},
		{
			name:  "Valid with underscore",
			input: "my_repo",
			want:  true,
		},
		{
			name:  "Valid with numbers",
			input: "repo123",
			want:  true,
		},
		{
			name:  "Invalid - starts with hyphen",
			input: "-repo",
			want:  false,
		},
		{
			name:  "Invalid - ends with hyphen",
			input: "repo-",
			want:  false,
		},
		{
			name:  "Invalid - too long",
			input: "this-is-a-very-long-repository-name-that-exceeds-the-limit",
			want:  false,
		},
		{
			name:  "Invalid - empty",
			input: "",
			want:  false,
		},
		{
			name:  "Invalid - special characters",
			input: "repo@name",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidGitHubIdentifier(tt.input)
			if got != tt.want {
				t.Errorf("IsValidGitHubIdentifier() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGitHubURLComponents_GetRepoSlug(t *testing.T) {
	c := &GitHubURLComponents{
		Owner: "owner",
		Repo:  "repo",
	}

	got := c.GetRepoSlug()
	want := "owner/repo"

	if got != want {
		t.Errorf("GetRepoSlug() = %v, want %v", got, want)
	}
}

func TestGitHubURLComponents_GetWorkflowName(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "Markdown file",
			path: "workflows/my-workflow.md",
			want: "my-workflow",
		},
		{
			name: "Nested path",
			path: ".github/workflows/release.md",
			want: "release",
		},
		{
			name: "Empty path",
			path: "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &GitHubURLComponents{
				Path: tt.path,
			}

			got := c.GetWorkflowName()

			if got != tt.want {
				t.Errorf("GetWorkflowName() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestParseGitHubURL_AdditionalEdgeCases tests additional edge cases for comprehensive coverage
func TestParseGitHubURL_AdditionalEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		wantType    GitHubURLType
		wantErr     bool
		errContains string
	}{
		{
			name:     "Issue URL",
			url:      "https://github.com/owner/repo/issues/100",
			wantType: URLTypeIssue,
			wantErr:  false,
		},
		{
			name:        "Actions path without enough parts",
			url:         "https://github.com/owner/repo/actions",
			wantErr:     true,
			errContains: "unrecognized",
		},
		{
			name:        "Runs path without enough parts",
			url:         "https://github.com/owner/repo/runs",
			wantErr:     true,
			errContains: "unrecognized",
		},
		{
			name:        "Pull path without number",
			url:         "https://github.com/owner/repo/pull",
			wantErr:     true,
			errContains: "unrecognized",
		},
		{
			name:        "Issues path without number",
			url:         "https://github.com/owner/repo/issues",
			wantErr:     true,
			errContains: "unrecognized",
		},
		{
			name:     "Blob URL with single file",
			url:      "https://github.com/owner/repo/blob/main/file.txt",
			wantType: URLTypeBlob,
			wantErr:  false,
		},
		{
			name:     "Tree URL with single dir",
			url:      "https://github.com/owner/repo/tree/dev/src",
			wantType: URLTypeTree,
			wantErr:  false,
		},
		{
			name:     "Raw URL with single file",
			url:      "https://github.com/owner/repo/raw/v1/README",
			wantType: URLTypeRaw,
			wantErr:  false,
		},
		{
			name:     "Raw githubusercontent simple path",
			url:      "https://raw.githubusercontent.com/org/name/sha/file.go",
			wantType: URLTypeRawContent,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseGitHubURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseGitHubURL() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ParseGitHubURL() error = %v, want error containing %v", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseGitHubURL() unexpected error: %v", err)
				return
			}

			if result.Type != tt.wantType {
				t.Errorf("ParseGitHubURL() type = %v, want %v", result.Type, tt.wantType)
			}
		})
	}
}
