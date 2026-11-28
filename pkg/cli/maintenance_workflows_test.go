//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeMaintenanceGH(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake gh requires a POSIX shell")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls")
	t.Setenv("MAINTENANCE_CALLS", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("MAINTENANCE_FAIL", "")
	t.Setenv("MAINTENANCE_BLOCK", "")
	t.Setenv("MAINTENANCE_RUNS", "")
	t.Setenv("MAINTENANCE_STATE", "active")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$MAINTENANCE_CALLS"
case "$*" in
  *"$MAINTENANCE_FAIL"*) if [ -n "$MAINTENANCE_FAIL" ]; then echo "simulated failure" >&2; exit 7; fi ;;
esac
case "$*" in
  *"$MAINTENANCE_BLOCK"*) if [ -n "$MAINTENANCE_BLOCK" ]; then echo "streamed before completion"; exec sleep 30; fi ;;
esac
case "$*" in
  *actions/workflows\?*)
    printf '[{"workflows":[{"id":11,"path":".github/workflows/first.lock.yml","state":"%s"},{"id":12,"path":".github/workflows/second.lock.yml","state":"active"},{"id":13,"path":".github/workflows/ci.yml","state":"active"},{"id":14,"path":".github/workflows/first.yml","state":"active"}]}]\n' "$MAINTENANCE_STATE" ;;
  *actions/workflows/11/runs\?*)
    if [ -n "$MAINTENANCE_RUNS" ]; then echo "$MAINTENANCE_RUNS"; else echo '[{"workflow_runs":[{"id":101}]},{"workflow_runs":[{"id":102}]}]'; fi ;;
  *actions/workflows/12/runs\?*) echo '[{"workflow_runs":[]}]' ;;
  *) echo "streamed gh output" ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755))
	// A remote operation must not consult workflow files in the caller's checkout.
	t.Chdir(t.TempDir())
	return logPath
}

func maintenanceCalls(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestMaintenanceDisableExactRepository(t *testing.T) {
	logPath := fakeMaintenanceGH(t)
	require.NoError(t, DisableWorkflowsByNames(context.Background(), []string{"first.md"}, "example.com/owner/target"))
	calls := maintenanceCalls(t, logPath)
	assert.Contains(t, calls, "repos/owner/target/actions/workflows?per_page=100 --hostname example.com")
	assert.Contains(t, calls, "api --paginate --slurp repos/owner/target/actions/workflows/11/runs?status=in_progress&per_page=100 --hostname example.com")
	assert.Contains(t, calls, "run cancel 101 --repo example.com/owner/target")
	assert.Contains(t, calls, "run cancel 102 --repo example.com/owner/target")
	assert.Contains(t, calls, "workflow disable 11 --repo example.com/owner/target")
	assert.Less(t, strings.Index(calls, "run cancel 102"), strings.Index(calls, "workflow disable"))
	assert.NotContains(t, calls, "workflows/12/runs")
	assert.NotContains(t, calls, "workflow disable 12")
}

func TestMaintenanceRepositoryWideSelection(t *testing.T) {
	for _, enable := range []bool{true, false} {
		t.Run(map[bool]string{true: "enable", false: "disable"}[enable], func(t *testing.T) {
			logPath := fakeMaintenanceGH(t)
			t.Setenv("MAINTENANCE_STATE", "disabled_manually")
			if enable {
				require.NoError(t, EnableWorkflowsByNames(context.Background(), nil, "owner/target"))
			} else {
				require.NoError(t, DisableWorkflowsByNames(context.Background(), nil, "owner/target"))
			}
			calls := maintenanceCalls(t, logPath)
			assert.NotContains(t, calls, "workflow enable 13")
			assert.NotContains(t, calls, "workflow disable 13")
			assert.NotContains(t, calls, "workflow enable 14")
			assert.NotContains(t, calls, "workflow disable 14")
			if enable {
				assert.Contains(t, calls, "workflow enable 11 --repo owner/target")
				assert.NotContains(t, calls, "run cancel")
			} else {
				assert.Contains(t, calls, "run cancel 101 --repo owner/target", "already disabled workflows may still have running jobs")
				assert.Contains(t, calls, "workflow disable 12 --repo owner/target")
			}
		})
	}
}

func TestMaintenanceSelectionValidatedBeforeMutation(t *testing.T) {
	logPath := fakeMaintenanceGH(t)
	err := DisableWorkflowsByNames(context.Background(), []string{"first", "missing"}, "owner/target")
	require.ErrorContains(t, err, "not found")
	calls := maintenanceCalls(t, logPath)
	assert.NotContains(t, calls, "run cancel")
	assert.NotContains(t, calls, "workflow disable")
}

func TestMaintenanceFailuresDoNotReportSuccess(t *testing.T) {
	for _, failure := range []string{"actions/workflows?", "workflows/11/runs?", "run cancel 101", "workflow disable 11", "workflow enable 11"} {
		t.Run(failure, func(t *testing.T) {
			logPath := fakeMaintenanceGH(t)
			t.Setenv("MAINTENANCE_FAIL", failure)
			var err error
			if strings.Contains(failure, "enable") {
				t.Setenv("MAINTENANCE_STATE", "disabled_manually")
				err = EnableWorkflowsByNames(context.Background(), []string{"first"}, "owner/target")
			} else {
				err = DisableWorkflowsByNames(context.Background(), []string{"first"}, "owner/target")
			}
			require.Error(t, err)
			calls := maintenanceCalls(t, logPath)
			if !strings.Contains(failure, "workflow disable") {
				assert.NotContains(t, calls, "workflow disable")
			}
		})
	}
}

func TestMaintenanceCancellation(t *testing.T) {
	withFakeBlockingGH(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	start := time.Now()
	err := cancelMaintenanceWorkflowRuns(ctx, "11", "owner/target")
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestMaintenanceLocalWorkflowSelection(t *testing.T) {
	logPath := fakeMaintenanceGH(t)
	require.NoError(t, os.MkdirAll(".github/workflows", 0o755))
	require.NoError(t, os.WriteFile(".github/workflows/first.md", []byte("---\non: push\n---\nTest workflow."), 0o600))
	require.NoError(t, os.WriteFile(".github/workflows/first.lock.yml", []byte("name: Test"), 0o600))
	require.NoError(t, DisableWorkflowsByNames(context.Background(), []string{"first.md"}, ""))
	calls := maintenanceCalls(t, logPath)
	assert.Contains(t, calls, "repos/{owner}/{repo}/actions/workflows/11/runs?status=in_progress")
	assert.Contains(t, calls, "run cancel 101\n")
	assert.Contains(t, calls, "workflow disable first.lock.yml\n")
	assert.NotContains(t, calls, "--repo")
}

func TestMaintenanceEnableCompilationFailure(t *testing.T) {
	logPath := fakeMaintenanceGH(t)
	require.NoError(t, os.MkdirAll(".github/workflows", 0o755))
	require.NoError(t, os.WriteFile(".github/workflows/unregistered.md", []byte("---\non: [\n---\nInvalid workflow."), 0o600))
	err := EnableWorkflowsByNames(context.Background(), []string{"unregistered"}, "")
	require.ErrorContains(t, err, "failed to compile workflow unregistered")
	assert.NotContains(t, maintenanceCalls(t, logPath), "workflow enable")
}

func TestMaintenanceRejectsMalformedRuns(t *testing.T) {
	for _, response := range []string{"not json", "null", "[]", `[{"workflows":[]}]`, `[{"workflow_runs":null}]`, `[{"workflow_runs":[{"id":0}]}]`} {
		t.Run(response, func(t *testing.T) {
			logPath := fakeMaintenanceGH(t)
			t.Setenv("MAINTENANCE_RUNS", response)
			require.Error(t, DisableWorkflowsByNames(context.Background(), []string{"first"}, "owner/target"))
			calls := maintenanceCalls(t, logPath)
			assert.NotContains(t, calls, "run cancel")
			assert.NotContains(t, calls, "workflow disable")
		})
	}
}

func TestMaintenanceStreamsAndCancelsMutation(t *testing.T) {
	for _, action := range []string{"enable", "disable"} {
		t.Run(action, func(t *testing.T) {
			logPath := fakeMaintenanceGH(t)
			t.Setenv("MAINTENANCE_STATE", "disabled_manually")
			if action == "disable" {
				t.Setenv("MAINTENANCE_STATE", "active")
			}
			t.Setenv("MAINTENANCE_BLOCK", "workflow "+action)
			reader, writer, err := os.Pipe()
			require.NoError(t, err)
			original := os.Stdout
			os.Stdout = writer
			t.Cleanup(func() {
				os.Stdout = original
				_ = reader.Close()
				_ = writer.Close()
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				result <- toggleWorkflowsByNames(ctx, []string{"first"}, action == "enable", "owner/target")
			}()
			output := make(chan string, 1)
			go func() {
				buffer := make([]byte, 1024)
				n, _ := reader.Read(buffer)
				output <- string(buffer[:n])
			}()
			select {
			case text := <-output:
				// Disable first streams cancellation output; drain until the mutation starts.
				assert.NotEmpty(t, text)
			case <-time.After(5 * time.Second):
				t.Fatal("subprocess output was not streamed")
			}
			require.Eventually(t, func() bool {
				data, _ := os.ReadFile(logPath)
				return strings.Contains(string(data), "workflow "+action)
			}, 5*time.Second, 10*time.Millisecond)
			cancel()
			select {
			case err := <-result:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(5 * time.Second):
				t.Fatal("mutation did not stop after cancellation")
			}
		})
	}
}
