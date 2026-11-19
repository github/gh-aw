//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/require"
)

func TestMCPRuntimePathsResolvePerInvocation(t *testing.T) {
	for _, root := range []string{"", newCLIRuntimeRoot(t), newCLIRuntimeRoot(t)} {
		t.Setenv("GH_AW_RUNTIME_ROOT", root)
		wantRoot := root
		if wantRoot == "" {
			wantRoot = constants.TmpGhAwDir
		}
		want := wantRoot + "/aw-mcp/logs"
		logArgs, _, _, err := buildLogsCommandArgs(context.Background(), logsArgs{})
		require.NoError(t, err)
		require.Equal(t, want, logArgs[2])
		episodeArgs, _, _, err := buildLogsCommandArgs(context.Background(), logsArgs{EpisodeRun: "123"})
		require.NoError(t, err)
		require.Equal(t, want, episodeArgs[4])
		auditArgs, err := buildAuditCommandArgs(auditArgs{}, []string{"123"})
		require.NoError(t, err)
		require.Equal(t, want, auditArgs[2])
		cacheDir, err := cliRuntimePath(mcpLogsCacheDir)
		require.NoError(t, err)
		require.Equal(t, wantRoot+"/logs-cache", cacheDir)
	}
}

func TestMCPRuntimePathsRejectUnownedRoots(t *testing.T) {
	root := newCLIRuntimeRoot(t)
	t.Setenv("GH_AW_RUNTIME_ROOT", root)
	t.Setenv("GH_AW_RUNTIME_OWNER", strings.Repeat("b", 64))
	_, err := cliRuntimePath(mcpLogsCacheDir)
	require.ErrorContains(t, err, "runtime owner marker does not match")

	t.Setenv("GH_AW_RUNTIME_OWNER", strings.Repeat("a", 64))
	require.NoError(t, os.Remove(filepath.Join(root, cliRuntimeOwnerFile)))
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(root, cliRuntimeOwnerFile)))
	_, err = cliRuntimePath(mcpLogsCacheDir)
	require.ErrorContains(t, err, "runtime owner marker is not a private regular file")

	link := filepath.Join(t.TempDir(), "runtime-link")
	require.NoError(t, os.Symlink(root, link))
	t.Setenv("GH_AW_RUNTIME_ROOT", link)
	_, err = cliRuntimePath(mcpLogsCacheDir)
	require.ErrorContains(t, err, "invalid GH_AW_RUNTIME_ROOT")
}

func TestMCPRuntimePathsRejectInvalidRoots(t *testing.T) {
	for _, root := range []string{
		"/tmp/gh-aw", "/tmp/gh-aw-run.", "/tmp/gh-aw-run.one/two",
		"/tmp/gh-aw-run.one/../two", "/tmp/gh-aw-run.one\n",
		"/tmp/gh-aw-run.one-two", "/other/gh-aw-run.one", "relative",
	} {
		t.Run(root, func(t *testing.T) {
			t.Setenv("GH_AW_RUNTIME_ROOT", root)
			logArgs, _, _, err := buildLogsCommandArgs(context.Background(), logsArgs{})
			require.Error(t, err)
			require.Nil(t, logArgs)
			episodeArgs, _, _, err := buildLogsCommandArgs(context.Background(), logsArgs{EpisodeRun: "123"})
			require.Error(t, err)
			require.Nil(t, episodeArgs)
			auditArgs, err := buildAuditCommandArgs(auditArgs{}, []string{"123"})
			require.Error(t, err)
			require.Nil(t, auditArgs)
			require.Contains(t, buildLogsFileResponse("{}"), "invalid GH_AW_RUNTIME_ROOT")
		})
	}
}

func newCLIRuntimeRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "gh-aw-run.")
	require.NoError(t, err)
	require.NoError(t, os.Chmod(root, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, cliRuntimeOwnerFile), []byte(strings.Repeat("a", 64)), 0o600))
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("GH_AW_RUNTIME_OWNER", strings.Repeat("a", 64))
	return root
}

func TestMCPRuntimeAuditDiffPathsResolvePerInvocation(t *testing.T) {
	for _, root := range []string{newCLIRuntimeRoot(t), newCLIRuntimeRoot(t)} {
		t.Setenv("GH_AW_RUNTIME_ROOT", root)
		called := false
		execCmd := func(ctx context.Context, args ...string) *exec.Cmd {
			called = true
			require.Equal(t, root+"/aw-mcp/logs", args[3])
			return exec.CommandContext(ctx, "echo", "{}")
		}
		result, _, err := newAuditDiffToolHandler(execCmd, "", false)(
			context.Background(), nil, auditDiffArgs{BaseRunID: "123", CompareRunIDs: []string{"456"}})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.True(t, called)
	}
	t.Setenv("GH_AW_RUNTIME_ROOT", "/untrusted")
	execCmd := func(_ context.Context, _ ...string) *exec.Cmd {
		t.Fatal("invalid runtime root must not start a subprocess")
		return nil
	}
	_, _, err := newAuditDiffToolHandler(execCmd, "", false)(
		context.Background(), nil, auditDiffArgs{BaseRunID: "123", CompareRunIDs: []string{"456"}})
	require.Error(t, err)
	_, err = runMCPAuditSubprocess(context.Background(), time.Now().Add(time.Minute), execCmd, auditArgs{}, []string{"123"})
	require.ErrorContains(t, err, "invalid GH_AW_RUNTIME_ROOT")
	_, _, err = newLogsToolHandler(execCmd, "", false)(context.Background(), nil, logsArgs{})
	require.Error(t, err)
}

func TestMCPRuntimeCacheIsolation(t *testing.T) {
	var paths []string
	for range 2 {
		root := privateRuntimeCacheTestDir(t)
		cacheDir := filepath.Join(root, "logs-cache")
		output := `{"runs":[]}`
		var response MCPLogsGuardrailResponse
		require.NoError(t, json.Unmarshal([]byte(buildLogsFileResponseAt(output, cacheDir, true)), &response))
		require.NotEmpty(t, response.FilePath)
		require.Equal(t, cacheDir, filepath.Dir(response.FilePath))
		var repeated MCPLogsGuardrailResponse
		require.NoError(t, json.Unmarshal([]byte(buildLogsFileResponseAt(output, cacheDir, true)), &repeated))
		require.Equal(t, response.FilePath, repeated.FilePath, "preserve content-addressed deduplication")
		content, err := os.ReadFile(response.FilePath)
		require.NoError(t, err)
		require.Equal(t, output, string(content))
		info, err := os.Stat(root)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "never widen the private runtime root")
		paths = append(paths, response.FilePath)
	}
	require.NotEqual(t, paths[0], paths[1], "identical content must stay invocation-local")
}

func TestMCPRuntimeCacheLegacyLayout(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "standalone", "logs-cache")
	var response MCPLogsGuardrailResponse
	require.NoError(t, json.Unmarshal([]byte(buildLogsFileResponseAt("{}", cacheDir, false)), &response))
	require.NotEmpty(t, response.FilePath)
	require.Equal(t, cacheDir, filepath.Dir(response.FilePath))
	cacheInfo, err := os.Stat(cacheDir)
	require.NoError(t, err)
	fileInfo, err := os.Stat(response.FilePath)
	require.NoError(t, err)
	if cliRuntimeOwnedByCurrentUser(cacheInfo) {
		require.Equal(t, os.FileMode(0o755), cacheInfo.Mode().Perm())
		require.Equal(t, os.FileMode(0o644), fileInfo.Mode().Perm())
	}
}

func TestMCPRuntimeCacheRejectsUnsafeDirectories(t *testing.T) {
	private := privateRuntimeCacheTestDir(t)
	public := t.TempDir()
	require.NoError(t, os.Chmod(public, 0o755))
	require.Contains(t, buildLogsFileResponseAt("{}", filepath.Join(public, "logs-cache"), true), "private directory")
	link := filepath.Join(t.TempDir(), "root-link")
	require.NoError(t, os.Symlink(private, link))
	require.Contains(t, buildLogsFileResponseAt("{}", filepath.Join(link, "logs-cache"), true), "private directory")
	require.NoError(t, os.Symlink(public, filepath.Join(private, "logs-cache")))
	require.Contains(t, buildLogsFileResponseAt("{}", filepath.Join(private, "logs-cache"), true), "symlink")
	entries, err := os.ReadDir(public)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestMCPRuntimeCacheRejectsSymlinkFile(t *testing.T) {
	root := privateRuntimeCacheTestDir(t)
	cacheDir := filepath.Join(root, "logs-cache")
	var response MCPLogsGuardrailResponse
	require.NoError(t, json.Unmarshal([]byte(buildLogsFileResponseAt("{}", cacheDir, true)), &response))
	require.NotEmpty(t, response.FilePath)
	require.NoError(t, os.Remove(response.FilePath))
	outside := filepath.Join(t.TempDir(), "outside.json")
	require.NoError(t, os.WriteFile(outside, []byte("unchanged"), 0o600))
	require.NoError(t, os.Symlink(outside, response.FilePath))
	require.Contains(t, buildLogsFileResponseAt("{}", cacheDir, true), "symlink")
	content, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "unchanged", string(content))
}

func privateRuntimeCacheTestDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	info, err := os.Stat(root)
	require.NoError(t, err)
	if !cliRuntimeOwnedByCurrentUser(info) {
		t.Skip("platform does not expose Unix runtime directory ownership")
	}
	require.NoError(t, os.Chmod(root, 0o700))
	return root
}

func TestFlattenedAgentLogPathRuntimeRoots(t *testing.T) {
	for _, path := range []string{
		"/tmp/gh-aw/pi-streaming.jsonl",
		"/tmp/gh-aw-run.First123/pi-streaming.jsonl",
		"/tmp/gh-aw-run.Second456/pi-streaming.jsonl",
	} {
		relative, ok := flattenedAgentLogPath(path)
		require.True(t, ok)
		require.Equal(t, "pi-streaming.jsonl", relative)
	}
	for _, path := range []string{
		"/tmp/gh-aw-run./log", "/tmp/gh-aw-run.bad-root/log", "/other/log",
		"gh-aw-run.fake/log", "/tmp/gh-aw-run.good/../../outside", "/tmp/gh-aw/../outside",
	} {
		_, ok := flattenedAgentLogPath(path)
		require.False(t, ok)
	}
}
