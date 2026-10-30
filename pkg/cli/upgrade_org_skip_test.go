//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgradeRepoMatchesVersion(t *testing.T) {
	for _, test := range []struct {
		name     string
		versions []string
		target   string
		unknown  int
		missing  int
		want     bool
	}{
		{"matching", []string{"v1.2.3"}, "v1.2.3", 0, 0, true},
		{"unprefixed compiler", []string{"1.2.3"}, "v1.2.3", 0, 0, true},
		{"unprefixed target", []string{"v1.2.3"}, "1.2.3", 0, 0, true},
		{"duplicate versions", []string{"v1.2.3", "1.2.3"}, "v1.2.3", 0, 0, true},
		{"mixed versions", []string{"v1.2.2", "v1.2.3"}, "v1.2.3", 0, 0, false},
		{"older", []string{"v1.2.2"}, "v1.2.3", 0, 0, false},
		{"newer", []string{"v1.2.4"}, "v1.2.3", 0, 0, false},
		{"unknown metadata", []string{"v1.2.3"}, "v1.2.3", 1, 0, false},
		{"uncompiled workflow", []string{"v1.2.3"}, "v1.2.3", 0, 1, false},
		{"no versions", nil, "v1.2.3", 0, 0, false},
		{"empty version", []string{""}, "v1.2.3", 0, 0, false},
		{"empty target", []string{"v1.2.3"}, "", 0, 0, false},
		{"prerelease mismatch", []string{"v1.2.3-rc.1"}, "v1.2.3", 0, 0, false},
		{"development match", []string{"v1.2.3-4-gabcd-dirty"}, "1.2.3-4-gabcd-dirty", 0, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			preview := orgRepoPreview{
				TotalWorkflows: 2, CurrentVersions: test.versions,
				UnknownVersionLocks: test.unknown, MissingWorkflowLocks: test.missing,
			}
			assert.Equal(t, test.want, upgradeRepoMatchesVersion(preview, test.target))
			preview.TotalWorkflows = 0
			assert.False(t, upgradeRepoMatchesVersion(preview, test.target), "empty repositories cannot be classified by version")
		})
	}
}

func TestCountUpgradeWorkflowFiles(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"one.md", "one.lock.yml", "two.md", "README.md", "notes.yml"} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), nil, 0600))
	}
	require.NoError(t, os.Mkdir(filepath.Join(directory, "two.lock.yml"), 0700))
	count, missing, err := countUpgradeWorkflowFiles(directory)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, 1, missing, "a directory cannot stand in for a compiled workflow")
}

func TestPreviewUpgradeRepoCheckoutWithoutMarkdown(t *testing.T) {
	for _, test := range []struct {
		name  string
		files []string
	}{
		{"missing directory", nil},
		{"empty directory", []string{}},
		{"YAML only", []string{"test.yml", "test.lock.yml"}},
		{"README only", []string{"README.md", "readme.md"}},
		{"nested Markdown only", []string{"shared/test.md"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			checkout := t.TempDir()
			directory := filepath.Join(checkout, ".github", "workflows")
			if test.files != nil {
				require.NoError(t, os.MkdirAll(directory, 0700))
			}
			for _, name := range test.files {
				path := filepath.Join(directory, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
				require.NoError(t, os.WriteFile(path, []byte("# gh-aw-metadata: {invalid json}\n"), 0600))
			}
			preview, include, err := previewUpgradeRepoCheckout("acme/empty", checkout, false)
			require.NoError(t, err, "metadata must not be parsed when no workflow Markdown exists")
			assert.False(t, include)
			assert.Empty(t, preview)
		})
	}
}

func TestOrgUpgradeWorkerDryRunSkipsMatchingVersion(t *testing.T) {
	originalScan, originalVersion := scanUpgradeRepoFn, GetVersion()
	t.Cleanup(func() { scanUpgradeRepoFn = originalScan; SetVersionInfo(originalVersion) })
	SetVersionInfo("v1.2.3")
	ctx := context.WithValue(context.Background(), orgRolloutKey{}, &orgRolloutOptions{worker: true, dryRun: true})
	for _, test := range []struct {
		name, version, status string
		unknown, missing      int
		include               bool
	}{
		{"match", "1.2.3", orgUpgradeVersionMatchStatus, 0, 0, true},
		{"old version", "v1.2.2", "planned", 0, 0, true},
		{"unknown lock", "v1.2.3", "planned", 1, 0, true},
		{"missing lock", "v1.2.3", "planned", 0, 1, true},
		{"no Markdown workflows", "", orgUpgradeNoWorkflowsStatus, 0, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			scanUpgradeRepoFn = func(context.Context, string, bool) (orgRepoPreview, bool, error) {
				return orgRepoPreview{
					Repo: "acme/repo", TotalWorkflows: 1, CurrentVersions: []string{test.version},
					UnknownVersionLocks: test.unknown, MissingWorkflowLocks: test.missing,
				}, test.include, nil
			}
			stdout, _ := captureOutput(t, func() error {
				return runUpgradeForTargetRepo(ctx, "acme/repo", upgradeOptions{}, true, false)
			})
			var result orgRolloutResult
			require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(stdout, orgRolloutResultPrefix))), &result))
			assert.Equal(t, test.status, result.Status)
			if !test.include {
				assert.Nil(t, result.Preview, "empty repositories must not appear as planned upgrades")
				return
			}
			require.NotNil(t, result.Preview)
			assert.Equal(t, []string{test.version}, result.Preview.CurrentVersions)
		})
	}
}

func TestOrgUpgradeReportSkippedVersion(t *testing.T) {
	originalVersion := GetVersion()
	t.Cleanup(func() { SetVersionInfo(originalVersion) })
	SetVersionInfo("v1.2.3")
	for _, applying := range []bool{false, true} {
		output := captureUpgradeOrgStderr(t, func() {
			err := reportOrgRolloutResults(context.Background(), []orgRolloutResult{{
				Repo: "acme/current", Status: orgUpgradeVersionMatchStatus,
				Preview: &orgRepoPreview{Repo: "acme/current", TotalWorkflows: 2, CurrentVersions: []string{"v1.2.3"}},
			}}, orgRunCallbacks{ReportFn: renderOrgUpgradeReport}, orgRolloutOptions{command: "upgrade", dryRun: !applying}, nil)
			require.NoError(t, err)
		})
		assert.Contains(t, output, "- acme/current (v1.2.3; 2 workflow(s); skipped: compiler version matches)")
		assert.Contains(t, output, "Detected compiler versions: v1.2.3")
		assert.NotContains(t, output, " -> ")
	}
}

func TestOrgUpgradeLegacySkipsMatchingVersion(t *testing.T) {
	originalSearch, originalScan := searchOrgLockWorkflowReposFn, scanUpgradeRepoFn
	originalUpgrade, originalIssue := runUpgradeForTargetRepoFn, createIssueForUpgradeOrgRepoFn
	originalVersion := GetVersion()
	t.Cleanup(func() {
		searchOrgLockWorkflowReposFn, scanUpgradeRepoFn = originalSearch, originalScan
		runUpgradeForTargetRepoFn, createIssueForUpgradeOrgRepoFn = originalUpgrade, originalIssue
		SetVersionInfo(originalVersion)
	})
	SetVersionInfo("v1.2.3")
	stubRolloutBudget(t, func(context.Context, string, bool) error { return nil })
	searchOrgLockWorkflowReposFn = func(context.Context, string, bool) ([]string, error) {
		return []string{"acme/current"}, nil
	}
	scanUpgradeRepoFn = func(context.Context, string, bool) (orgRepoPreview, bool, error) {
		return orgRepoPreview{Repo: "acme/current", TotalWorkflows: 1, CurrentVersions: []string{"1.2.3"}}, true, nil
	}
	runUpgradeForTargetRepoFn = func(context.Context, string, upgradeOptions, bool, bool) error {
		t.Fatal("matching repositories must not run the upgrade pipeline")
		return nil
	}
	createIssueForUpgradeOrgRepoFn = func(context.Context, string, bool) error {
		t.Fatal("matching repositories must not create upgrade issues")
		return nil
	}
	for _, createIssue := range []bool{false, true} {
		output := captureUpgradeOrgStderr(t, func() {
			require.NoError(t, runUpgradeForOrg(context.Background(), "acme", nil, upgradeOptions{yes: true}, !createIssue, createIssue, false))
		})
		assert.Contains(t, output, "skipped: compiler version matches")
		assert.Contains(t, output, "No repositories require an agentic workflow upgrade")
	}
}

func TestOrgUpgradeApplySkipsBeforeMutations(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake gh uses a shell script")
	}
	for _, test := range []struct {
		name, status string
		markdown     bool
	}{
		{"matching version", orgUpgradeVersionMatchStatus, true},
		{"no workflow Markdown", orgUpgradeNoWorkflowsStatus, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			gitPath, err := exec.LookPath("git")
			require.NoError(t, err)
			source, workspace, bin := t.TempDir(), t.TempDir(), t.TempDir()
			for _, directory := range []string{source, workspace} {
				output, err := exec.Command(gitPath, "init", "-q", directory).CombinedOutput()
				require.NoError(t, err, "%s", output)
			}
			directory := filepath.Join(source, ".github", "workflows")
			require.NoError(t, os.MkdirAll(directory, 0700))
			if test.markdown {
				require.NoError(t, os.WriteFile(filepath.Join(directory, "test.md"), []byte("must not be compiled"), 0600))
			}
			lock := []byte("# gh-aw-metadata: {\"compiler_version\":\"v1.2.3\"}\nname: test\n")
			require.NoError(t, os.WriteFile(filepath.Join(directory, "test.lock.yml"), lock, 0600))
			for _, args := range [][]string{
				{"-C", source, "add", "."},
				{"-C", source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture"},
			} {
				output, err := exec.Command(gitPath, args...).CombinedOutput()
				require.NoError(t, err, "%s", output)
			}
			script := `#!/bin/sh
if [ "$1" != "repo" ] || [ "$2" != "clone" ]; then
  echo "unexpected gh invocation" >&2
  exit 99
fi
exec "$GH_AW_TEST_GIT" clone --no-local --depth=1 "$GH_AW_TEST_SOURCE" "$4"
`
			require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0700))
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("GH_AW_TEST_GIT", gitPath)
			t.Setenv("GH_AW_TEST_SOURCE", source)
			t.Chdir(workspace)

			originalAPI, originalVersion := orgRolloutAPIFn, GetVersion()
			t.Cleanup(func() { orgRolloutAPIFn = originalAPI; SetVersionInfo(originalVersion) })
			SetVersionInfo("1.2.3")
			apiCalls := 0
			orgRolloutAPIFn = func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
				apiCalls++
				assert.Equal(t, []string{"/repos/acme/current/pulls?state=open&per_page=100&page=1"}, args, "only the existing-PR read is allowed")
				return []byte(`[{"number":42,"body":"<!-- gh-aw-upgrade: v1.2.2 -->","html_url":"https://github.com/acme/current/pull/42"}]`), nil
			}
			ctx := context.WithValue(context.Background(), orgRolloutKey{}, &orgRolloutOptions{worker: true, replace: true, command: "upgrade"})
			stdout, _ := captureOutput(t, func() error {
				return runUpgradeForTargetRepo(ctx, "acme/current", upgradeOptions{}, true, false)
			})
			var result orgRolloutResult
			require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(stdout, orgRolloutResultPrefix))), &result))
			assert.Equal(t, test.status, result.Status)
			assert.Empty(t, result.URL)
			assert.Equal(t, 1, apiCalls, "the old PR must not be closed even with replacement enabled")
			actual, err := os.ReadFile(filepath.Join(workspace, ".github", "aw", "updates", "acme__current", ".github", "workflows", "test.lock.yml"))
			require.NoError(t, err)
			assert.Equal(t, lock, actual)
		})
	}
}
