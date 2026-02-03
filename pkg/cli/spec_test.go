//go:build !integration

package cli

import (
	"strings"
	"testing"
)

func TestParseRepoSpec(t *testing.T) {
	tests := []struct {
		name        string
		repoSpec    string
		wantRepo    string
		wantVersion string
		wantErr     bool
		errContains string
	}{
		{
			name:        "repo with version tag",
			repoSpec:    "owner/repo@v1.0.0",
			wantRepo:    "owner/repo",
			wantVersion: "v1.0.0",
			wantErr:     false,
		},
		{
			name:        "repo with branch",
			repoSpec:    "owner/repo@main",
			wantRepo:    "owner/repo",
			wantVersion: "main",
			wantErr:     false,
		},
		{
			name:        "repo without version",
			repoSpec:    "owner/repo",
			wantRepo:    "owner/repo",
			wantVersion: "",
			wantErr:     false,
		},
		{
			name:        "repo with commit SHA",
			repoSpec:    "owner/repo@abc123def456",
			wantRepo:    "owner/repo",
			wantVersion: "abc123def456",
			wantErr:     false,
		},
		{
			name:        "invalid format - missing owner",
			repoSpec:    "repo@v1.0.0",
			wantErr:     true,
			errContains: "must be in format 'org/repo'",
		},
		{
			name:        "invalid format - missing repo",
			repoSpec:    "owner/@v1.0.0",
			wantErr:     true,
			errContains: "must be in format 'org/repo'",
		},
		{
			name:        "invalid format - no slash",
			repoSpec:    "ownerrepo@v1.0.0",
			wantErr:     true,
			errContains: "must be in format 'org/repo'",
		},
		{
			name:        "GitHub URL without version",
			repoSpec:    "https://github.com/owner/repo",
			wantRepo:    "owner/repo",
			wantVersion: "",
			wantErr:     false,
		},
		{
			name:        "GitHub URL with version",
			repoSpec:    "https://github.com/owner/repo@v1.0.0",
			wantRepo:    "owner/repo",
			wantVersion: "v1.0.0",
			wantErr:     false,
		},
		{
			name:        "GitHub URL with branch",
			repoSpec:    "https://github.com/githubnext/agentics@main",
			wantRepo:    "githubnext/agentics",
			wantVersion: "main",
			wantErr:     false,
		},
		{
			name:        "invalid GitHub URL - missing repo",
			repoSpec:    "https://github.com/owner",
			wantErr:     true,
			errContains: "invalid GitHub URL: must be https://github.com/owner/repo",
		},
		{
			name:        "invalid GitHub URL - too many path parts",
			repoSpec:    "https://github.com/owner/repo/extra/path",
			wantErr:     true,
			errContains: "invalid GitHub URL: must be https://github.com/owner/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := parseRepoSpec(tt.repoSpec)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseRepoSpec() expected error, got nil")
					return
				}
				return
			}

			if err != nil {
				t.Errorf("parseRepoSpec() unexpected error: %v", err)
				return
			}

			if spec.RepoSlug != tt.wantRepo {
				t.Errorf("parseRepoSpec() repo = %q, want %q", spec.RepoSlug, tt.wantRepo)
			}
			if spec.Version != tt.wantVersion {
				t.Errorf("parseRepoSpec() version = %q, want %q", spec.Version, tt.wantVersion)
			}
		})
	}
}

func TestParseWorkflowSpec(t *testing.T) {
	tests := []struct {
		name             string
		spec             string
		wantRepo         string
		wantWorkflowPath string
		wantWorkflowName string
		wantVersion      string
		wantErr          bool
		errContains      string
	}{
		{
			name:             "GitHub URL - blob with main branch",
			spec:             "https://github.com/github/gh-aw-trial/blob/main/workflows/release-issue-linker.md",
			wantRepo:         "github/gh-aw-trial",
			wantWorkflowPath: "workflows/release-issue-linker.md",
			wantWorkflowName: "release-issue-linker",
			wantVersion:      "main",
			wantErr:          false,
		},
		{
			name:             "GitHub URL - blob with version tag",
			spec:             "https://github.com/owner/repo/blob/v1.0.0/workflows/ci-doctor.md",
			wantRepo:         "owner/repo",
			wantWorkflowPath: "workflows/ci-doctor.md",
			wantWorkflowName: "ci-doctor",
			wantVersion:      "v1.0.0",
			wantErr:          false,
		},
		{
			name:             "GitHub URL - tree with branch",
			spec:             "https://github.com/owner/repo/tree/develop/custom/path/workflow.md",
			wantRepo:         "owner/repo",
			wantWorkflowPath: "custom/path/workflow.md",
			wantWorkflowName: "workflow",
			wantVersion:      "develop",
			wantErr:          false,
		},
		{
			name:             "GitHub URL - raw format",
			spec:             "https://github.com/owner/repo/raw/main/workflows/helper.md",
			wantRepo:         "owner/repo",
			wantWorkflowPath: "workflows/helper.md",
			wantWorkflowName: "helper",
			wantVersion:      "main",
			wantErr:          false,
		},
		{
			name:             "GitHub URL - commit SHA",
			spec:             "https://github.com/owner/repo/blob/abc123def456789012345678901234567890abcd/workflows/test.md",
			wantRepo:         "owner/repo",
			wantWorkflowPath: "workflows/test.md",
			wantWorkflowName: "test",
			wantVersion:      "abc123def456789012345678901234567890abcd",
			wantErr:          false,
		},
		{
			name:        "GitHub URL - invalid domain",
			spec:        "https://gitlab.com/owner/repo/blob/main/workflows/test.md",
			wantErr:     true,
			errContains: "must be from github.com",
		},
		{
			name:        "GitHub URL - missing file extension",
			spec:        "https://github.com/owner/repo/blob/main/workflows/test",
			wantErr:     true,
			errContains: "must point to a .md file",
		},
		{
			name:        "GitHub URL - invalid path (too short)",
			spec:        "https://github.com/owner/repo/blob/main",
			wantErr:     true,
			errContains: "path too short",
		},
		{
			name:        "GitHub URL - invalid type",
			spec:        "https://github.com/owner/repo/commits/main/workflows/test.md",
			wantErr:     true,
			errContains: "expected /blob/, /tree/, or /raw/",
		},
		{
			name:             "three-part spec with version",
			spec:             "owner/repo/workflow@v1.0.0",
			wantRepo:         "owner/repo",
			wantWorkflowPath: "workflows/workflow.md",
			wantWorkflowName: "workflow",
			wantVersion:      "v1.0.0",
			wantErr:          false,
		},
		{
			name:             "three-part spec without version",
			spec:             "owner/repo/workflow",
			wantRepo:         "owner/repo",
			wantWorkflowPath: "workflows/workflow.md",
			wantWorkflowName: "workflow",
			wantVersion:      "",
			wantErr:          false,
		},
		{
			name:             "four-part spec with workflows prefix",
			spec:             "owner/repo/workflows/ci-doctor.md@v1.0.0",
			wantRepo:         "owner/repo",
			wantWorkflowPath: "workflows/ci-doctor.md",
			wantWorkflowName: "ci-doctor",
			wantVersion:      "v1.0.0",
			wantErr:          false,
		},
		{
			name:             "nested path with version",
			spec:             "owner/repo/path/to/workflow.md@main",
			wantRepo:         "owner/repo",
			wantWorkflowPath: "path/to/workflow.md",
			wantWorkflowName: "workflow",
			wantVersion:      "main",
			wantErr:          false,
		},
		{
			name:        "invalid - too few parts",
			spec:        "owner/repo@v1.0.0",
			wantErr:     true,
			errContains: "must be in format",
		},
		{
			name:        "invalid - four parts without .md extension",
			spec:        "owner/repo/workflows/workflow@v1.0.0",
			wantErr:     true,
			errContains: "must end with '.md' extension",
		},
		{
			name:        "invalid - empty owner",
			spec:        "/repo/workflow@v1.0.0",
			wantErr:     true,
			errContains: "owner and repo cannot be empty",
		},
		{
			name:        "invalid - empty repo",
			spec:        "owner//workflow@v1.0.0",
			wantErr:     true,
			errContains: "owner and repo cannot be empty",
		},
		{
			name:        "invalid - bad GitHub identifier",
			spec:        "owner-/repo/workflow@v1.0.0",
			wantErr:     true,
			errContains: "does not look like a valid GitHub repository",
		},
		{
			name:             "/files/ format with branch",
			spec:             "github/gh-aw/files/main/.github/workflows/shared/mcp/serena.md",
			wantRepo:         "github/gh-aw",
			wantWorkflowPath: ".github/workflows/shared/mcp/serena.md",
			wantWorkflowName: "serena",
			wantVersion:      "main",
			wantErr:          false,
		},
		{
			name:             "/files/ format with commit SHA",
			spec:             "github/gh-aw/files/fc7992627494253a869e177e5d1985d25f3bb316/.github/workflows/shared/mcp/serena.md",
			wantRepo:         "github/gh-aw",
			wantWorkflowPath: ".github/workflows/shared/mcp/serena.md",
			wantWorkflowName: "serena",
			wantVersion:      "fc7992627494253a869e177e5d1985d25f3bb316",
			wantErr:          false,
		},
		{
			name:             "/files/ format with tag",
			spec:             "owner/repo/files/v1.0.0/workflows/helper.md",
			wantRepo:         "owner/repo",
			wantWorkflowPath: "workflows/helper.md",
			wantWorkflowName: "helper",
			wantVersion:      "v1.0.0",
			wantErr:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := parseWorkflowSpec(tt.spec)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseWorkflowSpec() expected error, got nil")
					return
				}
				return
			}

			if err != nil {
				t.Errorf("parseWorkflowSpec() unexpected error: %v", err)
				return
			}

			if spec.RepoSlug != tt.wantRepo {
				t.Errorf("parseWorkflowSpec() repo = %q, want %q", spec.RepoSlug, tt.wantRepo)
			}
			if spec.WorkflowPath != tt.wantWorkflowPath {
				t.Errorf("parseWorkflowSpec() workflowPath = %q, want %q", spec.WorkflowPath, tt.wantWorkflowPath)
			}
			if spec.WorkflowName != tt.wantWorkflowName {
				t.Errorf("parseWorkflowSpec() workflowName = %q, want %q", spec.WorkflowName, tt.wantWorkflowName)
			}
			if spec.Version != tt.wantVersion {
				t.Errorf("parseWorkflowSpec() version = %q, want %q", spec.Version, tt.wantVersion)
			}
		})
	}
}

func TestParseLocalWorkflowSpec(t *testing.T) {
	// Clear the repository slug cache to ensure clean test state
	ClearCurrentRepoSlugCache()

	tests := []struct {
		name             string
		spec             string
		wantWorkflowPath string
		wantWorkflowName string
		wantErr          bool
		errContains      string
	}{
		{
			name:             "valid local workflow",
			spec:             "./workflows/my-workflow.md",
			wantWorkflowPath: "./workflows/my-workflow.md",
			wantWorkflowName: "my-workflow",
			wantErr:          false,
		},
		{
			name:             "local workflow in current directory",
			spec:             "./test.md",
			wantWorkflowPath: "./test.md",
			wantWorkflowName: "test",
			wantErr:          false,
		},
		{
			name:        "local workflow without .md extension",
			spec:        "./workflows/test",
			wantErr:     true,
			errContains: "must end with '.md' extension",
		},
		{
			name:             "nested local workflow",
			spec:             "./path/to/nested/workflow.md",
			wantWorkflowPath: "./path/to/nested/workflow.md",
			wantWorkflowName: "workflow",
			wantErr:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := parseWorkflowSpec(tt.spec)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseWorkflowSpec() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("parseWorkflowSpec() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("parseWorkflowSpec() unexpected error: %v", err)
				return
			}

			if spec.WorkflowPath != tt.wantWorkflowPath {
				t.Errorf("parseWorkflowSpec() workflowPath = %q, want %q", spec.WorkflowPath, tt.wantWorkflowPath)
			}
			if spec.WorkflowName != tt.wantWorkflowName {
				t.Errorf("parseWorkflowSpec() workflowName = %q, want %q", spec.WorkflowName, tt.wantWorkflowName)
			}

			// For local workflows, verify repo is current repo and version is empty
			if spec.Version != "" {
				t.Errorf("parseWorkflowSpec() version = %q, want empty string for local workflow", spec.Version)
			}
			if spec.RepoSlug == "" {
				t.Errorf("parseWorkflowSpec() repo should not be empty for local workflow")
			}
		})
	}
}

func TestWorkflowSpecString(t *testing.T) {
	tests := []struct {
		name     string
		spec     *WorkflowSpec
		expected string
	}{
		{
			name: "with version",
			spec: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "v1.0.0",
				},
				WorkflowPath: "workflows/ci-doctor.md",
			},
			expected: "owner/repo/workflows/ci-doctor.md@v1.0.0",
		},
		{
			name: "without version",
			spec: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "",
				},
				WorkflowPath: "workflows/helper.md",
			},
			expected: "owner/repo/workflows/helper.md",
		},
		{
			name: "with branch",
			spec: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "githubnext/agentics",
					Version:  "main",
				},
				WorkflowPath: "workflows/weekly-research.md",
			},
			expected: "githubnext/agentics/workflows/weekly-research.md@main",
		},
		{
			name: "local workflow",
			spec: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "",
				},
				WorkflowPath: "./workflows/local-workflow.md",
			},
			expected: "./workflows/local-workflow.md",
		},
		{
			name: "local workflow in current directory",
			spec: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "",
				},
				WorkflowPath: "./test.md",
			},
			expected: "./test.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.spec.String()
			if got != tt.expected {
				t.Errorf("WorkflowSpec.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestParseSourceSpec(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		wantRepo    string
		wantPath    string
		wantRef     string
		wantErr     bool
		errContains string
	}{
		{
			name:     "full spec with tag",
			source:   "githubnext/agentics/workflows/ci-doctor.md@v1.0.0",
			wantRepo: "githubnext/agentics",
			wantPath: "workflows/ci-doctor.md",
			wantRef:  "v1.0.0",
			wantErr:  false,
		},
		{
			name:     "full spec with branch",
			source:   "githubnext/agentics/workflows/ci-doctor.md@main",
			wantRepo: "githubnext/agentics",
			wantPath: "workflows/ci-doctor.md",
			wantRef:  "main",
			wantErr:  false,
		},
		{
			name:     "spec without ref",
			source:   "githubnext/agentics/workflows/ci-doctor.md",
			wantRepo: "githubnext/agentics",
			wantPath: "workflows/ci-doctor.md",
			wantRef:  "",
			wantErr:  false,
		},
		{
			name:     "nested path",
			source:   "owner/repo/path/to/workflow.md@v2.0.0",
			wantRepo: "owner/repo",
			wantPath: "path/to/workflow.md",
			wantRef:  "v2.0.0",
			wantErr:  false,
		},
		{
			name:        "invalid format - too few parts",
			source:      "owner/repo@v1.0.0",
			wantErr:     true,
			errContains: "invalid source format",
		},
		{
			name:        "invalid format - missing owner",
			source:      "@v1.0.0",
			wantErr:     true,
			errContains: "invalid source format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := parseSourceSpec(tt.source)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseSourceSpec() expected error containing %q, got nil", tt.errContains)
					return
				}
				return
			}

			if err != nil {
				t.Errorf("parseSourceSpec() unexpected error: %v", err)
				return
			}

			if spec.Repo != tt.wantRepo {
				t.Errorf("parseSourceSpec() repo = %q, want %q", spec.Repo, tt.wantRepo)
			}
			if spec.Path != tt.wantPath {
				t.Errorf("parseSourceSpec() path = %q, want %q", spec.Path, tt.wantPath)
			}
			if spec.Ref != tt.wantRef {
				t.Errorf("parseSourceSpec() ref = %q, want %q", spec.Ref, tt.wantRef)
			}
		})
	}
}

func TestBuildSourceString(t *testing.T) {
	tests := []struct {
		name     string
		workflow *WorkflowSpec
		expected string
	}{
		{
			name: "workflow with version",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "v1.0.0",
				},
				WorkflowPath: "workflows/ci-doctor.md",
			},
			expected: "owner/repo/workflows/ci-doctor.md@v1.0.0",
		},
		{
			name: "workflow with branch",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "main",
				},
				WorkflowPath: "workflows/helper.md",
			},
			expected: "owner/repo/workflows/helper.md@main",
		},
		{
			name: "workflow without version",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "",
				},
				WorkflowPath: "workflows/test.md",
			},
			expected: "owner/repo/workflows/test.md",
		},
		{
			name: "workflow with nested path",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "v2.0.0",
				},
				WorkflowPath: "path/to/workflow.md",
			},
			expected: "owner/repo/path/to/workflow.md@v2.0.0",
		},
		{
			name: "empty repo",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "",
					Version:  "v1.0.0",
				},
				WorkflowPath: "workflows/test.md",
			},
			expected: "",
		},
		{
			name: "empty workflow path",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "v1.0.0",
				},
				WorkflowPath: "",
			},
			expected: "",
		},
		{
			name: "local workflow without version",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "",
				},
				WorkflowPath: "./workflows/local.md",
			},
			expected: "owner/repo/workflows/local.md",
		},
		{
			name: "local workflow with version (should still work)",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "v1.0.0",
				},
				WorkflowPath: "./test.md",
			},
			expected: "owner/repo/test.md@v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildSourceString(tt.workflow)
			if result != tt.expected {
				t.Errorf("buildSourceString() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBuildSourceStringWithCommitSHA(t *testing.T) {
	tests := []struct {
		name      string
		workflow  *WorkflowSpec
		commitSHA string
		expected  string
	}{
		{
			name: "with commit SHA",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "v1.0.0",
				},
				WorkflowPath: "workflows/ci-doctor.md",
			},
			commitSHA: "abc123def456789012345678901234567890abcd",
			expected:  "owner/repo/workflows/ci-doctor.md@abc123def456789012345678901234567890abcd",
		},
		{
			name: "with commit SHA overrides version",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "main",
				},
				WorkflowPath: "workflows/helper.md",
			},
			commitSHA: "1234567890abcdef1234567890abcdef12345678",
			expected:  "owner/repo/workflows/helper.md@1234567890abcdef1234567890abcdef12345678",
		},
		{
			name: "without commit SHA falls back to version",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "v2.0.0",
				},
				WorkflowPath: "workflows/test.md",
			},
			commitSHA: "",
			expected:  "owner/repo/workflows/test.md@v2.0.0",
		},
		{
			name: "without commit SHA or version",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "",
				},
				WorkflowPath: "workflows/test.md",
			},
			commitSHA: "",
			expected:  "owner/repo/workflows/test.md",
		},
		{
			name: "empty repo with commit SHA",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "",
					Version:  "v1.0.0",
				},
				WorkflowPath: "workflows/test.md",
			},
			commitSHA: "abc123",
			expected:  "",
		},
		{
			name: "local workflow with commit SHA",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "",
				},
				WorkflowPath: "./workflows/local.md",
			},
			commitSHA: "abc123def456789012345678901234567890abcd",
			expected:  "owner/repo/workflows/local.md@abc123def456789012345678901234567890abcd",
		},
		{
			name: "local workflow without commit SHA",
			workflow: &WorkflowSpec{
				RepoSpec: RepoSpec{
					RepoSlug: "owner/repo",
					Version:  "",
				},
				WorkflowPath: "./test.md",
			},
			commitSHA: "",
			expected:  "owner/repo/test.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildSourceStringWithCommitSHA(tt.workflow, tt.commitSHA)
			if result != tt.expected {
				t.Errorf("buildSourceStringWithCommitSHA() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsCommitSHA(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{"valid SHA", "abc123def456789012345678901234567890abcd", true},
		{"valid SHA lowercase", "abcdef1234567890123456789012345678901234", true},
		{"valid SHA uppercase", "ABCDEF1234567890123456789012345678901234", true},
		{"valid SHA mixed case", "AbCdEf1234567890123456789012345678901234", true},
		{"invalid - too short", "abc123def456", false},
		{"invalid - too long", "abc123def456789012345678901234567890abcdef", false},
		{"invalid - contains non-hex", "abc123def456789012345678901234567890abcg", false},
		{"invalid - empty", "", false},
		{"invalid - branch name", "main", false},
		{"invalid - version tag", "v1.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCommitSHA(tt.version)
			if got != tt.want {
				t.Errorf("IsCommitSHA(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}
