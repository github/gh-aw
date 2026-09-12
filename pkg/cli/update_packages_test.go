//go:build !integration

package cli

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindInstalledPackageRecord(t *testing.T) {
	t.Parallel()
	records := []packageOwnershipRecord{
		{Package: "owner/repo", Source: "owner/repo@abc123"},
		{Package: "owner/repo/packages/reviewer", Source: "owner/repo/packages/reviewer@def456"},
	}

	tests := []struct {
		name        string
		target      string
		wantPackage string
		packageLike bool
	}{
		{name: "root package URL", target: "https://github.com/owner/repo", wantPackage: "owner/repo", packageLike: true},
		{name: "nested package tree URL", target: "https://github.com/owner/repo/tree/main/packages/reviewer", wantPackage: "owner/repo/packages/reviewer", packageLike: true},
		{name: "nested manifest URL", target: "https://github.com/owner/repo/blob/main/packages/reviewer/aw.yml", wantPackage: "owner/repo/packages/reviewer", packageLike: true},
		{name: "workflow name", target: "repo-assist", packageLike: false},
		{name: "package identifier", target: "owner/repo", packageLike: false},
		{name: "package identifier with ref", target: "owner/repo/packages/reviewer@v2", packageLike: false},
		{name: "missing package URL", target: "https://github.com/other/repo", packageLike: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			record, packageLike, err := findInstalledPackageRecord(records, tt.target)
			require.NoError(t, err)
			assert.Equal(t, tt.packageLike, packageLike)
			if tt.wantPackage == "" {
				assert.Nil(t, record)
				return
			}
			require.NotNil(t, record)
			assert.Equal(t, tt.wantPackage, record.Package)
		})
	}
}

func TestFindInstalledPackageRecordRejectsUnsupportedURL(t *testing.T) {
	t.Parallel()
	record, packageLike, err := findInstalledPackageRecord(nil, "https://example.com/owner/repo")
	require.Error(t, err)
	assert.True(t, packageLike)
	assert.Nil(t, record)
	assert.ErrorContains(t, err, "package URL host")
}

func TestPackageURLMatchesRecord(t *testing.T) {
	t.Parallel()
	record := packageOwnershipRecord{Package: "owner/repo/packages/reviewer"}

	for _, rawURL := range []string{
		"https://github.com/owner/repo/packages/reviewer",
		"https://github.com/owner/repo/tree/main/packages/reviewer",
		"https://github.com/owner/repo/blob/release/v2/packages/reviewer/aw.yml",
	} {
		parsed, err := url.Parse(rawURL)
		require.NoError(t, err)
		assert.True(t, packageURLMatchesRecord(parsed, record), rawURL)
	}

	parsed, err := url.Parse("https://github.com/owner/repo/tree/main/packages/other")
	require.NoError(t, err)
	assert.False(t, packageURLMatchesRecord(parsed, record))

	parsed, err = url.Parse("https://github.com/owner/repo/packages/reviewer")
	require.NoError(t, err)
	assert.True(t, packageURLMatchesRecord(parsed, packageOwnershipRecord{Package: "Owner/Repo/packages/reviewer"}))
}

func TestPackageWorkflowsFromOwnershipRecord(t *testing.T) {
	t.Parallel()
	gitRoot := t.TempDir()
	workflowPath := filepath.Join(gitRoot, ".github", "workflows", "review.md")
	skillPath := filepath.Join(gitRoot, ".github", "skills", "reviewer", "SKILL.md")
	agentPath := filepath.Join(gitRoot, ".github", "agents", "reviewer.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(workflowPath), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(agentPath), 0o755))
	require.NoError(t, os.WriteFile(workflowPath, []byte("---\nsource: owner/repo@main\n---\n"), 0o644))
	require.NoError(t, os.WriteFile(skillPath, []byte("# Skill\n"), 0o644))
	require.NoError(t, os.WriteFile(agentPath, []byte("# Agent\n"), 0o644))

	record := packageOwnershipRecord{
		Package: "owner/repo",
		Source:  "owner/repo@abc123",
		Files: []packageOwnershipFileEntry{
			{Destination: ".github/workflows/review.md"},
			{Destination: ".github/skills/reviewer/SKILL.md"},
			{Destination: ".github/CODEOWNERS"},
		},
	}
	workflows, err := packageWorkflowsFromOwnershipRecord(gitRoot, record)
	require.NoError(t, err)
	require.Len(t, workflows, 1)
	assert.Equal(t, "review", workflows[0].Name)
	assert.Equal(t, workflowPath, workflows[0].Path)
	assert.Equal(t, "owner/repo@main", workflows[0].SourceSpec)
}

func TestPackageInstallContext(t *testing.T) {
	t.Parallel()
	gitRoot := t.TempDir()
	record := packageOwnershipRecord{
		Files: []packageOwnershipFileEntry{
			{Source: ".github/workflows/review.yml", Destination: "custom/workflows/review.yml"},
			{Source: "skills/reviewer/SKILL.md", Destination: ".claude/skills/reviewer/SKILL.md"},
		},
	}

	workflowsDir, engineOverride := packageInstallContext(gitRoot, record)
	assert.Equal(t, filepath.Join(gitRoot, "custom", "workflows"), workflowsDir)
	assert.Equal(t, "claude", engineOverride)
}
