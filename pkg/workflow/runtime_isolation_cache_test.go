package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestRuntimeCacheStableAcrossAgentAndUpdateJobs(t *testing.T) {
	source := `jobs:
  agent:
    runs-on: ubuntu-latest
    steps:
      - name: Restore cache-memory file share data
        uses: actions/cache/restore@immutable
        with:
          key: memory-key
          path: /tmp/gh-aw/cache-memory
      - run: test -d /tmp/gh-aw/cache-memory
  update_cache_memory:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@immutable
        with:
          path: /tmp/gh-aw/cache-memory
      - name: Save cache-memory to cache
        if: steps.check.outputs.has_content == 'true'
        uses: actions/cache/save@immutable
        with:
          key: memory-key
          path: /tmp/gh-aw/cache-memory
`
	actual, err := finalizeRunnerTempSafety(source)
	require.NoError(t, err)
	var parsed struct {
		Jobs map[string]struct {
			Steps []map[string]any
		}
	}
	require.NoError(t, yaml.Unmarshal([]byte(actual), &parsed))
	agent := parsed.Jobs["agent"].Steps
	update := parsed.Jobs["update_cache_memory"].Steps
	require.Equal(t, "actions/cache/restore@immutable", agent[2]["uses"])
	require.Equal(t, "Unpack isolated runtime cache", agent[3]["name"])
	require.Equal(t, "actions/cache/save@immutable", update[4]["uses"])
	require.Equal(t, "steps.check.outputs.has_content == 'true'", update[2]["if"])
	restored, saved := agent[2]["with"].(map[string]any), update[4]["with"].(map[string]any)
	require.Equal(t, restored["path"], saved["path"])
	require.Equal(t, "memory-key", restored["key"])
	require.Regexp(t, `^\.gh-aw-cache-v1-[0-9a-f]+/runtime\.tar$`, saved["path"])
	require.Equal(t, runtimeRootExpression+"/cache-memory", update[1]["with"].(map[string]any)["path"])
}

func TestRuntimeCacheWindowsSavesBeforeWrappedCleanup(t *testing.T) {
	source := `jobs:
  agent:
    runs-on: windows-latest
    steps:
      - name: Initialize WSL runtime
        run: Write-Host bootstrap
        shell: pwsh
      - uses: actions/cache@immutable
        with:
          key: memory-key
          path: //wsl.localhost/Ubuntu${{ env.GH_AW_RUNTIME_ROOT }}/cache-memory
          lookup-only: true
      - name: Clean Scripts
        run: Write-Host opaque-cleanup-wrapper
        shell: pwsh
`
	actual, err := finalizeRunnerTempSafety(source)
	require.NoError(t, err)
	require.Less(t, strings.Index(actual, "Initialize WSL runtime"), strings.Index(actual, "Allocate isolated runtime directories"))
	restore, pack, save, cleanup := strings.Index(actual, "actions/cache/restore@immutable"), strings.Index(actual, "Pack isolated runtime cache"), strings.Index(actual, "actions/cache/save@immutable"), strings.Index(actual, "name: Clean Scripts")
	require.Positive(t, restore)
	require.Less(t, restore, pack)
	require.Less(t, pack, save)
	require.Less(t, save, cleanup)
	require.Contains(t, actual[pack:save], "shell: pwsh")
	require.NotContains(t, actual[restore:cleanup], "path: //wsl.localhost")
}

func TestRuntimeCacheArchiveRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required for cache archive steps")
	}
	workspace, from, to := t.TempDir(), t.TempDir(), t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(from, "cache-memory", ".git"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(from, "cache-memory", ".git", "HEAD"), []byte("cache data"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(from, "unrelated"), []byte("not cached"), 0600))
	stage := newRuntimeCacheStage("/tmp/gh-aw/cache-memory", 0, false)
	require.NotNil(t, stage)
	output := filepath.Join(workspace, "output")
	run := func(script, root string, extra ...string) ([]byte, error) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "bash", "-c", script)
		cmd.Env = append(os.Environ(),
			"GITHUB_WORKSPACE="+workspace, "GITHUB_OUTPUT="+output,
			"GH_AW_RUNTIME_ROOT="+root, "GH_AW_CACHE_STAGE="+stage.dir,
			"GH_AW_CACHE_PATTERNS="+stage.patterns)
		cmd.Env = append(cmd.Env, extra...)
		return cmd.CombinedOutput()
	}
	data, err := run(acquireRuntimeCacheStage, from)
	require.NoError(t, err, "%s", data)
	identity, err := os.ReadFile(output)
	require.NoError(t, err)
	identityEnv := "GH_AW_CACHE_STAGE_ID=" + strings.TrimSpace(strings.TrimPrefix(string(identity), "identity="))
	_, err = run(acquireRuntimeCacheStage, to)
	require.Error(t, err, "a second job cannot borrow the first one's staging path")
	data, err = run(packRuntimeCacheStage, from, identityEnv)
	require.NoError(t, err, "%s", data)
	data, err = run(unpackRuntimeCacheStage, to, identityEnv, "GH_AW_CACHE_LOOKUP=true")
	require.NoError(t, err, "%s", data)
	require.NoDirExists(t, filepath.Join(to, "cache-memory"), "lookup-only never unpacks a cache")
	data, err = run(unpackRuntimeCacheStage, to, identityEnv, "GH_AW_CACHE_LOOKUP=false")
	require.NoError(t, err, "%s", data)
	head, err := os.ReadFile(filepath.Join(to, "cache-memory", ".git", "HEAD"))
	require.NoError(t, err)
	require.Equal(t, "cache data", string(head))
	require.NoFileExists(t, filepath.Join(to, "unrelated"))
	data, err = run(releaseRuntimeCacheStage, to, identityEnv)
	require.NoError(t, err, "%s", data)
	require.NoDirExists(t, filepath.Join(workspace, stage.dir))
}
