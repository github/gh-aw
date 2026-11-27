package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/stretchr/testify/require"
)

func TestWorkClearConfirmationAndPreview(t *testing.T) {
	for _, scenario := range []string{"yes", "no", "error", "dry_run"} {
		t.Run(scenario, func(t *testing.T) {
			workClearEnvironment(t)
			log, ids := workQueueMixedFixture(t)
			ids["secret"] = log.submit(log.work("secret", func(work *workqueue.WorkDefinition) {
				work.Payload = json.RawMessage(`{"task":"PAYLOAD-MUST-STAY-PRIVATE","token":"SECRET-MUST-STAY-PRIVATE"}`)
			}))[0]
			state := log.state()
			ledger := log.jsonl()
			queue := newWorkQueueGitRemote(t, ledger)
			head := queue.head(t)
			cmd := NewWorkCommand()
			var output, preview bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&preview)
			args := []string{"--repo", workQueueTestRepository, "--json", "clear"}
			if scenario == "dry_run" {
				args = append(args, "--dry-run")
			}
			cmd.SetArgs(args)
			confirmations := 0
			workClearConfirmAction = func(string, string, string) (bool, error) {
				confirmations++
				text := preview.String()
				for _, want := range []string{
					`repository="owner/repo"`, "branch=" + workQueueText(workqueue.DefaultBranch), "Snapshot: " + workQueueText(state.Tip),
					"Work: 5; available: 2; claimed: 1; completed: 1; cancelled: 1",
					"Release 1 dispatches, then cancel 3 Work (reason operator).",
					"RELEASE " + workQueueText(ids["running_dispatch"]) + " state=bound workflow=" + workQueueText(workQueueTestWorkerWorkflow),
					"CANCEL " + workQueueText(ids["running"]) + " state=claimed",
					"CANCEL " + workQueueText(ids["waiting"]) + " state=available",
					"CANCEL " + workQueueText(ids["secret"]) + " state=available",
				} {
					require.Contains(t, text, want)
				}
				require.NotContains(t, text, ids["done"], "preview listed completed Work")
				require.NotContains(t, text, ids["dropped"], "preview listed cancelled Work")
				require.NotContains(t, text, "MUST-STAY-PRIVATE", "preview leaked a Work payload")
				require.Equal(t, head, queue.head(t), "clear wrote before confirmation")
				require.Zero(t, output.Len(), "clear printed a result before confirmation")
				if scenario == "error" {
					return false, errors.New("prompt unavailable")
				}
				return scenario == "yes", nil
			}
			err := cmd.Execute()
			switch scenario {
			case "no":
				require.ErrorContains(t, err, "clear_cancelled")
			case "error":
				require.ErrorContains(t, err, "clear_confirmation_failed")
			default:
				require.NoError(t, err)
			}
			if scenario != "yes" {
				wantConfirmations := 1
				if scenario == "dry_run" {
					wantConfirmations = 0
				}
				require.Equal(t, wantConfirmations, confirmations)
				require.Zero(t, queue.pushes(t))
				require.Equal(t, ledger, queue.read(t))
				return
			}
			require.Equal(t, 1, confirmations)
			require.Equal(t, 2, queue.pushes(t))
			var result workClearResult
			require.NoError(t, json.Unmarshal(output.Bytes(), &result))
			require.Equal(t, "cleared", result.Status)
			require.NotContains(t, output.String(), "MUST-STAY-PRIVATE")
		})
	}
}

func TestWorkClearRefusesNonInteractiveConfirmation(t *testing.T) {
	workClearEnvironment(t)
	log, _ := workQueueMixedFixture(t)
	ledger := log.jsonl()
	queue := newWorkQueueGitRemote(t, ledger)
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	stdin := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() { os.Stdin = stdin })
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	workClearConfirmAction = console.ConfirmAction
	_, err = workClearExecute(t)
	require.ErrorContains(t, err, "clear_confirmation_failed")
	require.ErrorContains(t, err, "not a TTY")
	require.Zero(t, queue.pushes(t))
	require.Equal(t, ledger, queue.read(t))
}

func TestWorkClearNoopDoesNotPrompt(t *testing.T) {
	workClearEnvironment(t)
	log, _ := workQueueMixedFixture(t)
	queue := newWorkQueueGitRemote(t, log.jsonl())
	_, err := workClearExecute(t)
	require.NoError(t, err)
	pushes := queue.pushes(t)
	workClearConfirmAction = func(string, string, string) (bool, error) {
		t.Fatal("clear of an already cleared queue requested confirmation")
		return false, nil
	}
	result, err := workClearExecute(t)
	require.NoError(t, err)
	require.Equal(t, "no_work", result.Status)
	require.Equal(t, pushes, queue.pushes(t))
}
