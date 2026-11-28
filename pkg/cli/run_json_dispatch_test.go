//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
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

func TestRepeatStopOnError(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(strconv.FormatBool(stop), func(t *testing.T) {
			calls, cleanups := 0, 0
			expectedErr := errors.New("repeat failed")
			err := ExecuteWithRepeat(RepeatOptions{
				Ctx: context.Background(), RepeatCount: 2, UseStderr: true, StopOnError: stop,
				ExecuteFunc: func() error {
					calls++
					if calls == 2 {
						return expectedErr
					}
					return nil
				},
				CleanupFunc: func() { cleanups++ },
			})
			if stop {
				require.ErrorIs(t, err, expectedErr)
				assert.Equal(t, 2, calls)
				assert.Equal(t, 1, cleanups)
			} else {
				require.NoError(t, err)
				assert.Equal(t, 3, calls)
				assert.Zero(t, cleanups)
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
	for _, output := range []string{"", "created", "https://github.com/owner/repo/actions/runs/0", "see https://github.com/owner/repo/actions/runs/123", "https://github.com/owner/repo/actions/runs/123suffix", "message\nhttps://github.com/owner/repo/actions/runs/123suffix\nmore", strings.Repeat("9", 100)} {
		t.Run(output, func(t *testing.T) {
			info, err := resolveWorkflowRunInfo("test.lock.yml", output, RunOptions{JSON: true})
			assert.Nil(t, info)
			require.ErrorContains(t, err, "did not return a workflow run identity")
		})
	}
}

func TestRunJSONMultilineDispatchOutput(t *testing.T) {
	output := "Created workflow_dispatch event\nTo see the workflow run, visit:\n  https://github.com/owner/repo/actions/runs/123\nUse gh run view for logs"
	info, err := resolveWorkflowRunInfo("test.lock.yml", output, RunOptions{JSON: true})
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, int64(123), info.DatabaseID)
	assert.Equal(t, "https://github.com/owner/repo/actions/runs/123", info.URL)

	info, err = resolveWorkflowRunInfo("test.lock.yml", output+"\nhttps://github.com/owner/repo/actions/runs/456", RunOptions{JSON: true})
	assert.Nil(t, info)
	require.ErrorContains(t, err, "multiple workflow run identities")
}

func TestRunJSONRepeatStopsOnLaterError(t *testing.T) {
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
  api) echo '[{"workflows":[{"id":1,"path":".github/workflows/test.lock.yml","state":"active"}]}]' ;;
  workflow)
    count=$(grep -c '^workflow run' "$RUN_JSON_CALLS")
    if [ "$count" -eq 2 ]; then
      case "$RUN_JSON_FAILURE" in
        dispatch) echo 'dispatch rejected' >&2; exit 1 ;;
        unknown) exit 0 ;;
      esac
    fi
    echo 'https://github.com/owner/repo/actions/runs/123' ;;
  run)
    count=$(grep -c '^run view' "$RUN_JSON_CALLS")
    if [ "$RUN_JSON_FAILURE" = poll ] && [ "$count" -eq "$RUN_JSON_POLL_FAILURE" ]; then
      echo 'polling forbidden' >&2; exit 1
    fi
    echo '{"status":"completed","conclusion":"success"}' ;;
  *) exit 1 ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	for _, tt := range []struct {
		name        string
		failure     string
		pollFailure string
		repeat      int
		dispatches  int
		wantError   string
	}{
		{"initial polling failure", "poll", "1", 2, 1, "could not check workflow status"},
		{"later polling failure", "poll", "2", 2, 2, "could not check workflow status"},
		{"later dispatch failure with repeat remaining", "dispatch", "", 2, 2, "failed to run workflow on GitHub Actions"},
		{"final dispatch failure", "dispatch", "", 1, 2, "failed to run workflow on GitHub Actions"},
		{"later unknown identity with repeat remaining", "unknown", "", 2, 2, "identity is unknown"},
		{"final unknown identity", "unknown", "", 1, 2, "identity is unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(logPath, nil, 0o600))
			t.Setenv("RUN_JSON_FAILURE", tt.failure)
			t.Setenv("RUN_JSON_POLL_FAILURE", tt.pollFailure)
			stdoutRead, stdoutWrite, err := os.Pipe()
			require.NoError(t, err)
			stderrRead, stderrWrite, err := os.Pipe()
			require.NoError(t, err)
			oldStdout, oldStderr := os.Stdout, os.Stderr
			os.Stdout, os.Stderr = stdoutWrite, stderrWrite
			runErr := RunWorkflowsOnGitHub(context.Background(), []string{"test"}, RunOptions{
				WorkflowRunFlags: WorkflowRunFlags{RepoOverride: "owner/repo"},
				RepeatCount:      tt.repeat,
				JSON:             true,
			})
			os.Stdout, os.Stderr = oldStdout, oldStderr
			require.NoError(t, stdoutWrite.Close())
			require.NoError(t, stderrWrite.Close())
			stdout, err := io.ReadAll(stdoutRead)
			require.NoError(t, err)
			stderr, err := io.ReadAll(stderrRead)
			require.NoError(t, err)
			require.NoError(t, stdoutRead.Close())
			require.NoError(t, stderrRead.Close())
			require.ErrorContains(t, runErr, tt.wantError)
			assert.NotContains(t, string(stderr), "Received interrupt signal")

			decoder := json.NewDecoder(strings.NewReader(string(stdout)))
			for i := range tt.dispatches {
				var results []WorkflowRunResult
				require.NoError(t, decoder.Decode(&results))
				require.Len(t, results, 1)
				expected := WorkflowRunResult{
					Workflow: "test", LockFile: "test.lock.yml",
					Status: "triggered", RunIdentity: "known",
					RunID: 123, RunURL: "https://github.com/owner/repo/actions/runs/123",
				}
				if i == tt.dispatches-1 {
					expected.Status = "error"
					expected.Error = runErr.Error()
					if tt.failure != "poll" {
						expected.RunIdentity, expected.RunID, expected.RunURL = "unknown", 0, ""
					}
				}
				assert.Equal(t, expected, results[0])
			}
			var extra any
			require.ErrorIs(t, decoder.Decode(&extra), io.EOF)
			calls, err := os.ReadFile(logPath)
			require.NoError(t, err)
			assert.Equal(t, tt.dispatches, strings.Count(string(calls), "workflow run"))
		})
	}
}
