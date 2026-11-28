//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunJSONDispatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake gh is not supported on Windows")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls")
	t.Setenv("RUN_JSON_CALLS", logPath)
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$RUN_JSON_CALLS"
case "$1" in
  --version) exit 0 ;;
  api) printf '%s\n' '[{"workflows":[{"id":1,"path":".github/workflows/known.lock.yml","state":"active"},{"id":2,"path":".github/workflows/unknown.lock.yml","state":"active"},{"id":3,"path":".github/workflows/fail.lock.yml","state":"active"}]}]' ;;
  workflow)
    case "$3" in
      known.lock.yml) echo 'https://github.com/owner/repo/actions/runs/123' ;;
      unknown.lock.yml) : ;;
      fail.lock.yml) echo 'dispatch rejected' >&2; exit 1 ;;
      *) exit 1 ;;
    esac ;;
  *) exit 1 ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	tests := []struct {
		name       string
		workflows  []string
		dryRun     bool
		wait       bool
		repeat     int
		wantErr    bool
		identities []string
		statuses   []string
	}{
		{"known and unknown", []string{"known", "unknown"}, false, false, 0, false, []string{"known", "unknown"}, []string{"triggered", "triggered"}},
		{"unavailable identity with wait", []string{"unknown"}, false, true, 0, false, []string{"unknown"}, []string{"triggered"}},
		{"unknown identity stops repeat", []string{"unknown"}, false, false, 2, true, []string{"unknown"}, []string{"error"}},
		{"error after dispatch", []string{"known", "fail"}, false, false, 0, true, []string{"known", "unknown"}, []string{"error", "error"}},
		{"dispatch error", []string{"fail"}, false, false, 0, true, []string{"unknown"}, []string{"error"}},
		{"dry run", []string{"known"}, true, false, 0, false, []string{"unknown"}, []string{"dry_run"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(logPath, nil, 0o600))
			read, write, err := os.Pipe()
			require.NoError(t, err)
			oldStdout := os.Stdout
			os.Stdout = write
			runErr := RunWorkflowsOnGitHub(context.Background(), tt.workflows, RunOptions{
				WorkflowRunFlags:  WorkflowRunFlags{RepoOverride: "owner/repo", DryRun: tt.dryRun},
				WaitForCompletion: tt.wait,
				RepeatCount:       tt.repeat,
				JSON:              true,
			})
			os.Stdout = oldStdout
			require.NoError(t, write.Close())
			output, err := io.ReadAll(read)
			require.NoError(t, err)
			var results []WorkflowRunResult
			require.NoError(t, json.Unmarshal(output, &results))
			var rawResults []map[string]any
			require.NoError(t, json.Unmarshal(output, &rawResults))
			require.NoError(t, read.Close())
			require.Equal(t, tt.wantErr, runErr != nil)
			require.Len(t, results, len(tt.workflows))
			for i, result := range results {
				assert.Equal(t, tt.identities[i], result.RunIdentity)
				assert.Equal(t, tt.statuses[i], result.Status)
				if result.RunIdentity == "known" {
					assert.Equal(t, int64(123), result.RunID)
					assert.Equal(t, "https://github.com/owner/repo/actions/runs/123", result.RunURL)
				} else {
					assert.Zero(t, result.RunID)
					assert.Empty(t, result.RunURL)
					assert.NotContains(t, rawResults[i], "run_id")
					assert.NotContains(t, rawResults[i], "run_url")
				}
				if tt.wantErr {
					assert.Contains(t, result.Error, "failed to run workflow")
				}
			}
			calls, err := os.ReadFile(logPath)
			require.NoError(t, err)
			assert.NotContains(t, string(calls), "run list")
			assert.NotContains(t, string(calls), "/runs")
			if tt.repeat > 0 {
				assert.Equal(t, 1, strings.Count(string(calls), "workflow run"))
			}
		})
	}
}

func TestRunJSONConcurrentDispatches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake gh is not supported on Windows")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
for arg in "$@"; do
  case "$arg" in
    identity=*) echo "https://github.com/owner/repo/actions/runs/${arg#identity=}" ;;
  esac
done
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var wg sync.WaitGroup
	for _, id := range []string{"101", "202"} {
		wg.Go(func() {
			result, err := executeWorkflowRun(context.Background(), "test.lock.yml", []string{"workflow", "run", "test.lock.yml", "-f", "identity=" + id}, RunOptions{JSON: true})
			if assert.NoError(t, err) && assert.NotNil(t, result.runInfo) {
				assert.Equal(t, "https://github.com/owner/repo/actions/runs/"+id, result.runInfo.URL)
				assert.Equal(t, id, strconv.FormatInt(result.runInfo.DatabaseID, 10))
			}
		})
	}
	wg.Wait()
}

func TestRunJSONUnknownDispatchOutput(t *testing.T) {
	for _, output := range []string{"", "created", "https://github.com/owner/repo/actions/runs/0", "see https://github.com/owner/repo/actions/runs/123", "https://github.com/owner/repo/actions/runs/123suffix", strings.Repeat("9", 100)} {
		t.Run(output, func(t *testing.T) {
			info, err := resolveWorkflowRunInfo("test.lock.yml", output, RunOptions{JSON: true})
			assert.Nil(t, info)
			require.ErrorContains(t, err, "did not return a workflow run identity")
		})
	}
}
