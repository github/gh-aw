package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func newWorkDispatchTestCommand(t *testing.T, run func(context.Context, string, RunOptions) error) *cobra.Command {
	t.Helper()
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
		t.Errorf("dispatch made an API request outside its runner: %s %s", r.Method, r.URL.Path)
		return nil, fmt.Errorf("unexpected API request %s", r.URL.Path)
	})
	command := NewWorkCommand()
	existing, _, err := command.Find([]string{"dispatch"})
	require.NoError(t, err)
	command.RemoveCommand(existing)
	command.AddCommand(workDispatchCommandWithRunner(run))
	return command
}

func TestWorkQueueDispatchRunsDispatcherOnTargetRepository(t *testing.T) {
	for _, tc := range []struct {
		name          string
		preview, wait bool
	}{
		{name: "activate"},
		{name: "activate and wait", wait: true},
		{name: "preview", preview: true, wait: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			command := newWorkDispatchTestCommand(t, func(_ context.Context, name string, options RunOptions) error {
				calls++
				require.Equal(t, "dispatcher", name)
				require.Equal(t, RunOptions{WorkflowRunFlags: WorkflowRunFlags{RepoOverride: "owner/repo"}, WaitForCompletion: tc.wait}, options,
					"dispatch must run the dispatcher on the default branch without inputs, refs or other overrides")
				return nil
			})
			var output bytes.Buffer
			command.SetOut(&output)
			args := []string{"--repo", "owner/repo", "--json", "dispatch", "dispatcher"}
			if tc.wait {
				args = append(args, "--wait")
			}
			if tc.preview {
				args = append(args, "--dry-run")
			}
			command.SetArgs(args)
			require.NoError(t, command.Execute())
			var result map[string]string
			require.NoError(t, json.Unmarshal(output.Bytes(), &result))
			wantCalls, wantStatus := 1, "activated"
			if tc.preview {
				wantCalls, wantStatus = 0, "preview"
			}
			require.Equal(t, wantCalls, calls)
			require.Equal(t, map[string]string{"dispatcher": "dispatcher", "repository": "owner/repo", "status": wantStatus}, result)
		})
	}
}

func TestWorkQueueDispatchPropagatesRunFailure(t *testing.T) {
	denied := errors.New("workflow dispatch denied")
	calls := 0
	command := newWorkDispatchTestCommand(t, func(context.Context, string, RunOptions) error {
		calls++
		return denied
	})
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"--repo", "owner/repo", "dispatch", "dispatcher"})
	err := command.Execute()
	require.ErrorIs(t, err, denied)
	require.Equal(t, 1, calls, "dispatch failure was hidden or retried")
}

func TestWorkQueueDispatchRejectsAuthorityOverrides(t *testing.T) {
	for _, args := range [][]string{
		{"dispatch"},
		{"dispatch", "dispatcher", "another"},
		{"dispatch", ""},
		{"dispatch", "../dispatcher"},
		{"dispatch", `workflows\dispatcher`},
		{"dispatch", "dispatcher", "--claim-id", "chosen"},
		{"dispatch", "dispatcher", "--work-id", "chosen"},
		{"dispatch", "dispatcher", "--worker-profile", "worker"},
		{"dispatch", "dispatcher", "--ref", "unreviewed"},
		{"dispatch", "dispatcher", "--request-id", "forged-origin"},
	} {
		command := newWorkDispatchTestCommand(t, func(context.Context, string, RunOptions) error {
			t.Fatalf("invalid command reached workflow dispatch: %v", args)
			return nil
		})
		command.SetOut(io.Discard)
		command.SetErr(io.Discard)
		command.SetArgs(append([]string{"--repo", "owner/repo"}, args...))
		require.Error(t, command.Execute(), "unsafe dispatch override was accepted: %v", args)
	}
}
