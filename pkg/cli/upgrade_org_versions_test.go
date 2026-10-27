//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadCompilerVersionFromLockHeader(t *testing.T) {
	for _, test := range []struct {
		name, content, want, wantError string
	}{
		{"current", "# gh-aw-metadata: {\"compiler_version\":\"v0.93.2\"}\nname: test\n", "v0.93.2", ""},
		{"unprefixed", "# gh-aw-metadata: {\"compiler_version\":\"0.81.6\"}\n", "v0.81.6", ""},
		{"older header", "# Generated workflow\n\n#   gh-aw-metadata: {\"compiler_version\":\"v0.67.1\"}\nname: test\n", "v0.67.1", ""},
		{"no trailing newline", "# gh-aw-metadata: {\"compiler_version\":\"v1.0.0\"}", "v1.0.0", ""},
		{"development", "# gh-aw-metadata: {\"compiler_version\":\"v0.87.1-84-g0c3f46dc99\"}\n", "v0.87.1-84-g0c3f46dc99", ""},
		{"missing compiler", "# gh-aw-metadata: {\"schema_version\":\"v1\"}\nname: test\n", "", ""},
		{"legacy", "# frontmatter-hash: " + strings.Repeat("a", 64) + "\nname: test\n", "", ""},
		{"empty", "", "", ""},
		{"invalid JSON", "# gh-aw-metadata: {invalid json}\n", "", "lock metadata JSON"},
		{"truncated JSON", "# gh-aw-metadata: {\"compiler_version\":\"v1.0.0\"\n", "", "lock metadata JSON"},
		{"empty metadata", "# gh-aw-metadata: \nname: test\n", "", "invalid gh-aw-metadata header"},
		{"empty object", "# gh-aw-metadata: {}\n", "", ""},
		{"wrong type", "# gh-aw-metadata: {\"compiler_version\":123}\n", "", "lock metadata JSON"},
		{"ignore body", "name: test\n# gh-aw-metadata: {\"compiler_version\":\"v9.9.9\"}\n", "", ""},
		{"ignore large body", "# gh-aw-metadata: {\"compiler_version\":\"v1.0.0\"}\nname: test\n" + strings.Repeat("x", 1024*1024), "v1.0.0", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.lock.yml")
			require.NoError(t, os.WriteFile(path, []byte(test.content), 0600))
			version, err := readCompilerVersionFromLockHeader(path)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.want, version)
			}
		})
	}
}

func TestPreviewUpgradeRepoCheckoutVersionSetWithoutAPI(t *testing.T) {
	originalAPI := orgRolloutAPIFn
	orgRolloutAPIFn = func(context.Context, string, string, ...string) ([]byte, error) {
		t.Fatal("local metadata scanning must not call the GitHub API")
		return nil, nil
	}
	t.Cleanup(func() { orgRolloutAPIFn = originalAPI })

	checkout := t.TempDir()
	directory := filepath.Join(checkout, ".github", "workflows")
	require.NoError(t, os.MkdirAll(directory, 0700))
	for name, content := range map[string]string{
		"one.md":             "# One",
		"two.md":             "# Two",
		"README.md":          "# Ignore",
		"a.lock.yml":         "# gh-aw-metadata: {\"compiler_version\":\"v1.2.3\"}\nname: a\n",
		"b.lock.yml":         "# gh-aw-metadata: {\"compiler_version\":\"v1.0.0\"}\n",
		"duplicate.lock.yml": "# gh-aw-metadata: {\"compiler_version\":\"1.2.3\"}\n",
		"unknown.lock.yml":   "# gh-aw-metadata: {\"schema_version\":\"v1\"}\n",
		"legacy.lock.yml":    "name: old\n",
		"ignore.yml":         "# gh-aw-metadata: {\"compiler_version\":\"v9.9.9\"}\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(content), 0600))
	}
	require.NoError(t, os.Mkdir(filepath.Join(directory, "ignore.lock.yml"), 0700))
	preview, include, err := previewUpgradeRepoCheckout("acme/repo", checkout, false)
	require.NoError(t, err)
	require.True(t, include)
	assert.Equal(t, 2, preview.TotalWorkflows)
	assert.Equal(t, []string{"v1.0.0", "v1.2.3"}, preview.CurrentVersions)
	assert.Equal(t, 2, preview.UnknownVersionLocks)

	encoded, err := json.Marshal(orgRolloutResult{Status: "planned", Preview: &preview})
	require.NoError(t, err)
	var result orgRolloutResult
	require.NoError(t, json.Unmarshal(encoded, &result))
	require.NotNil(t, result.Preview)
	assert.Equal(t, preview, *result.Preview, "all versions must survive worker result transport")
}

func TestExtractCompilerVersionsFromWorkflowsDirErrors(t *testing.T) {
	_, _, err := extractCompilerVersionsFromWorkflowsDir(filepath.Join(t.TempDir(), "missing"))
	require.ErrorIs(t, err, os.ErrNotExist)

	directory := t.TempDir()
	lock := filepath.Join(directory, "broken.lock.yml")
	require.NoError(t, os.WriteFile(lock, []byte("# gh-aw-metadata: {invalid json}\n"), 0600))
	_, _, err = extractCompilerVersionsFromWorkflowsDir(directory)
	require.ErrorContains(t, err, "broken.lock.yml")
	require.ErrorContains(t, err, "lock metadata JSON")
	require.NoError(t, os.Remove(lock))
	require.NoError(t, os.Symlink(filepath.Join(directory, "missing"), lock))
	_, _, err = extractCompilerVersionsFromWorkflowsDir(directory)
	require.ErrorContains(t, err, "broken.lock.yml")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRenderOrgUpgradeReportVersionSets(t *testing.T) {
	for _, applying := range []bool{false, true} {
		output := captureUpgradeOrgStderr(t, func() {
			renderOrgUpgradeReport([]orgRepoPreview{
				{Repo: "acme/mixed", TotalWorkflows: 3, CurrentVersions: []string{"v1.2.3", "1.0.0", "v1.2.3"}, UnknownVersionLocks: 1},
				{Repo: "acme/other", TotalWorkflows: 1, CurrentVersions: []string{"v1.0.0"}},
				{Repo: "acme/no-lock", TotalWorkflows: 1},
			}, applying)
		})
		assert.Contains(t, output, "- acme/mixed (v1.0.0, v1.2.3, unknown (1 lock file(s)) -> ")
		assert.Contains(t, output, "- acme/no-lock (unknown -> ")
		assert.Contains(t, output, "Detected compiler versions: v1.0.0, v1.2.3, unknown (1 lock file(s))")
	}
}

func TestOrgRolloutTUIUpgradeVersions(t *testing.T) {
	for _, status := range []string{"planned", "created", "unchanged"} {
		model := orgRolloutTUI{opts: orgRolloutOptions{command: "upgrade"}, rows: []orgRolloutRow{{repo: "acme/repo"}}}
		next, _ := model.Update(orgRolloutEvent{index: 0, result: orgRolloutResult{
			Status: status, Preview: &orgRepoPreview{CurrentVersions: []string{"v1.0.0", "v1.2.3"}},
		}})
		state, ok := next.(orgRolloutTUI)
		require.True(t, ok)
		assert.Equal(t, status+" | compilers: v1.0.0, v1.2.3", state.rows[0].status)
	}
}

func TestReportOrgRolloutAppliedUpgradeVersions(t *testing.T) {
	output := captureUpgradeOrgStderr(t, func() {
		err := reportOrgRolloutResults(context.Background(), []orgRolloutResult{{
			Repo: "acme/repo", Status: "created",
			Preview: &orgRepoPreview{Repo: "acme/repo", CurrentVersions: []string{"v1.0.0", "v1.2.3"}},
		}}, orgRunCallbacks{ReportFn: renderOrgUpgradeReport}, orgRolloutOptions{command: "upgrade"}, nil)
		require.NoError(t, err)
	})
	assert.Contains(t, output, "Repositories with agentic workflows (1)")
	assert.Contains(t, output, "Detected compiler versions: v1.0.0, v1.2.3")
}
