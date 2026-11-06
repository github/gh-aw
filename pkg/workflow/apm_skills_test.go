//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/apm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCommit = "0123456789abcdef0123456789abcdef01234567"

func TestAPMLockedSkills(t *testing.T) {
	manifest, err := apm.ParseManifest([]byte("name: project\nversion: 1.0.0\ndependencies:\n  apm:\n    - owner/repo/skills/review#main\n    - git: owner/other\n      path: skills/security\n      ref: v1.0.0\n"))
	require.NoError(t, err)
	lockContent := "lockfile_version: '1'\ndependencies:\n" +
		"  - repo_url: https://github.com/owner/repo\n    virtual_path: skills/review\n    resolved_ref: main\n    resolved_commit: " + testCommit + "\n    depth: !!int 1\n" +
		"  - repo_url: github.com/owner/other\n    virtual_path: skills/security\n    resolved_ref: v1.0.0\n    resolved_commit: " + testCommit + "\n    depth: !!int 1\n"
	lock, err := apm.ParseLockfile([]byte(lockContent))
	require.NoError(t, err)
	skills, err := lockedAPMSkills(manifest, lock)
	require.NoError(t, err)
	assert.Equal(t, []string{"owner/repo/skills/review@" + testCommit, "owner/other/skills/security@" + testCommit}, skills)
	moved := strings.ReplaceAll(lockContent, testCommit, strings.Repeat("a", 40))
	newLock, err := apm.ParseLockfile([]byte(moved))
	require.NoError(t, err)
	newSkills, err := lockedAPMSkills(manifest, newLock)
	require.NoError(t, err)
	assert.NotEqual(t, skills, newSkills, "branch-only lockfile updates change consumed pins")
}

func TestAPMLockedSkillsFailClosed(t *testing.T) {
	manifestContent := "name: project\nversion: 1.0.0\ndependencies:\n  apm:\n    - owner/repo/skills/review#main\n"
	lockContent := "lockfile_version: '1'\ndependencies:\n  - repo_url: owner/repo\n    virtual_path: skills/review\n    resolved_ref: main\n    resolved_commit: " + testCommit + "\n    depth: !!int 1\n"
	for _, tc := range []struct {
		name, manifest, lock, message string
	}{
		{"stale-ref", manifestContent, strings.Replace(lockContent, "resolved_ref: main", "resolved_ref: old", 1), "stale"},
		{"missing", manifestContent, "lockfile_version: '1'\ndependencies: []\n", "missing"},
		{"sha-mismatch", strings.Replace(manifestContent, "#main", "#"+strings.Repeat("b", 40), 1), strings.Replace(lockContent, "resolved_ref: main", "resolved_ref: "+strings.Repeat("b", 40), 1), "differs"},
		{"no-ref", strings.Replace(manifestContent, "#main", "", 1), lockContent, "explicit #ref"},
		{"unsupported-scripts", manifestContent + "scripts: {build: 'echo unsafe'}\n", lockContent, "scripts"},
		{"unsupported-policy", manifestContent + "policy: {}\n", lockContent, "policy"},
		{"unsupported-mcp", strings.Replace(manifestContent, "  apm:", "  mcp: [server]\n  apm:", 1), lockContent, "dependencies.mcp"},
		{"undeclared", "name: project\nversion: 1.0.0\n", lockContent, "undeclared"},
		{"transitive", manifestContent, lockContent + "  - repo_url: owner/transitive\n    virtual_path: skills/other\n    resolved_commit: " + testCommit + "\n    depth: !!int 2\n", "transitive"},
		{"collision", manifestContent, lockContent + strings.TrimPrefix(lockContent, "lockfile_version: '1'\ndependencies:\n"), "duplicate"},
		{"materialization", manifestContent, lockContent + "    materialization_repo_url: attacker/repo\n", "same repository"},
		{"alias", manifestContent, lockContent + "    alias: other\n", "aliases"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest, err := apm.ParseManifest([]byte(tc.manifest))
			require.NoError(t, err)
			lock, err := apm.ParseLockfile([]byte(tc.lock))
			require.NoError(t, err)
			_, err = lockedAPMSkills(manifest, lock)
			require.ErrorContains(t, err, tc.message)
		})
	}
}

func TestAPMGitHubReferences(t *testing.T) {
	for _, value := range []string{"owner/repo/path#main", "https://github.com/owner/repo.git/path#v1", "github.com/owner/repo/path#" + testCommit} {
		ref, err := ParseGitHubReference(value)
		require.NoError(t, err)
		assert.Equal(t, "owner/repo", ref.Repository)
		assert.Equal(t, "path", ref.Path)
	}
	for _, value := range []string{
		"../repo", "owner/repo/../escape", "gitlab.com/owner/repo", "http://github.com/owner/repo",
		"https://user:password@github.com/owner/repo", "https://github.com:443/owner/repo",
		"https://github.com/owner/repo?query=1", "owner/repo#$(echo unsafe)",
		"owner/repo/path#main#other", "owner/repo#--option", "owner/repo/skills\\escape",
	} {
		_, err := ParseGitHubReference(value)
		require.Error(t, err, value)
	}
}

func TestAPMSkillPathsDoNotDiscardRefFragments(t *testing.T) {
	manifest, err := apm.ParseManifest([]byte("name: project\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: owner/repo\n      path: skills/review#ignored\n      ref: main\n"))
	require.NoError(t, err)
	lock, err := apm.ParseLockfile([]byte("lockfile_version: '1'\ndependencies:\n  - repo_url: owner/repo\n    virtual_path: skills/review\n    resolved_ref: main\n    resolved_commit: " + testCommit + "\n    depth: 1\n"))
	require.NoError(t, err)
	_, err = lockedAPMSkills(manifest, lock)
	require.ErrorContains(t, err, "path must not contain a ref")

	_, err = lockedSkillReference(apm.LockedDependency{
		RepoURL: "owner/repo", VirtualPath: "skills/review#ignored", ResolvedCommit: testCommit,
	})
	require.ErrorContains(t, err, "virtual_path must not contain a ref")
}
