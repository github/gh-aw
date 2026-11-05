//go:build !integration

package cli

import "testing"

func TestBuildSourceStringWithCommitSHA_ManifestSource(t *testing.T) {
	t.Parallel()
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tests := []struct {
		name      string
		version   string
		commitSHA string
		wantRef   string
	}{
		{name: "release tag", version: "v1.2.0", commitSHA: sha, wantRef: "v1.2.0"},
		{name: "major ref", version: "v1", commitSHA: sha, wantRef: "v1"},
		{name: "branch", version: "release/stable", commitSHA: sha, wantRef: "release/stable"},
		{name: "commit SHA", version: sha, commitSHA: sha, wantRef: sha},
		{name: "no ref", commitSHA: sha, wantRef: sha},
		{name: "no commit SHA", version: "v1.2.0", wantRef: "v1.2.0"},
		{name: "no ref or commit SHA"},
	}

	for _, tt := range tests {
		for _, packagePath := range []string{"", "packages/repo-assist"} {
			t.Run(tt.name+"/"+packagePath, func(t *testing.T) {
				t.Parallel()
				workflow := &WorkflowSpec{
					RepoSpec: RepoSpec{
						RepoSlug:    "owner/repo",
						Version:     tt.version,
						PackagePath: packagePath,
					},
					WorkflowPath:           "workflows/triage.md",
					FromRepositoryManifest: true,
				}

				got := buildSourceStringWithCommitSHA(workflow, tt.commitSHA)
				want := "owner/repo"
				if packagePath != "" {
					want += "/" + packagePath
				}
				if tt.wantRef != "" {
					want += "@" + tt.wantRef
				}
				if got != want {
					t.Fatalf("expected %q, got %q", want, got)
				}
			})
		}
	}
}

func TestBuildSourceStringWithCommitSHA_ManifestSourceRoot(t *testing.T) {
	t.Parallel()
	workflow := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "owner/repo",
			Version:  "v1.2.3",
		},
		WorkflowPath:           "workflows/triage.md",
		FromRepositoryManifest: true,
	}

	got := buildSourceStringWithCommitSHA(workflow, "")
	want := "owner/repo@v1.2.3"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestParseManifestSourceSpec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		source       string
		wantManifest bool
		wantRepo     string
		wantPackage  string
		wantVersion  string
	}{
		{source: "owner/repo@v1.0.0", wantManifest: true, wantRepo: "owner/repo", wantVersion: "v1.0.0"},
		{source: "owner/repo@feature/github-agentic-workflow", wantManifest: true, wantRepo: "owner/repo", wantVersion: "feature/github-agentic-workflow"},
		{source: "owner/repo@release/2026.05.27-rc_1", wantManifest: true, wantRepo: "owner/repo", wantVersion: "release/2026.05.27-rc_1"},
		{source: "owner/repo/packages/repo-assist@main", wantManifest: true, wantRepo: "owner/repo", wantPackage: "packages/repo-assist", wantVersion: "main"},
		{source: "owner/repo/agentic-workflows@hotfix/github-aw_fix-1.2.3", wantManifest: true, wantRepo: "owner/repo", wantPackage: "agentic-workflows", wantVersion: "hotfix/github-aw_fix-1.2.3"},
		{source: "owner/repo/workflows/triage.md@v1.0.0", wantManifest: false},
		{source: "owner/repo/workflows/triage.md@release/2026.05.27-rc_1", wantManifest: false},
	}

	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			spec, isManifest, err := parseManifestSourceSpec(tt.source)
			if err != nil {
				t.Fatalf("parseManifestSourceSpec returned error: %v", err)
			}
			if isManifest != tt.wantManifest {
				t.Fatalf("expected manifest=%v, got %v", tt.wantManifest, isManifest)
			}
			if !tt.wantManifest {
				return
			}
			if spec.RepoSlug != tt.wantRepo || spec.PackagePath != tt.wantPackage || spec.Version != tt.wantVersion {
				t.Fatalf("unexpected repo spec: %+v", spec)
			}
		})
	}
}
