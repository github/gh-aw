//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeUpdateEvents(t *testing.T, output string) []updateJSONLEvent {
	t.Helper()
	var events []updateJSONLEvent
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		var event updateJSONLEvent
		require.NoError(t, json.Unmarshal([]byte(line), &event), "stdout line must be JSON: %q", line)
		require.Equal(t, 1, event.SchemaVersion)
		events = append(events, event)
	}
	return events
}

func initUpdateJSONLRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	output, err := exec.Command("git", "init", "-q").CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.NoError(t, os.MkdirAll(".github/workflows", 0755))
	return dir
}

func runUpdateJSONLCommand(t *testing.T, cmd *cobra.Command, args ...string) ([]updateJSONLEvent, error) {
	t.Helper()
	cmd.SetArgs(append([]string{"--jsonl"}, args...))
	var runErr error
	// Capture process stdout too: a Cobra-only buffer would miss legacy leaks
	// from compilation, codemods, or subprocesses.
	stdout, _ := captureOutput(t, func() error {
		runErr = cmd.Execute()
		return nil
	})
	return decodeUpdateEvents(t, stdout), runErr
}

func TestUpdateJSONLLocalNoopAndError(t *testing.T) {
	initUpdateJSONLRepo(t)
	for _, name := range []string{"", "missing"} {
		t.Run("workflow="+name, func(t *testing.T) {
			args := []string{"--no-compile"}
			if name != "" {
				args = append(args, name)
			}
			events, err := runUpdateJSONLCommand(t, NewUpdateCommand(validateEngineStub), args...)
			require.GreaterOrEqual(t, len(events), 2)
			assert.Equal(t, "command_started", events[0].Event)
			final := events[len(events)-1]
			assert.Equal(t, "command_completed", final.Event)
			require.NotNil(t, final.ChangedFiles)
			assert.Zero(t, *final.ChangedFiles)
			if name == "" {
				require.NoError(t, err)
				assert.Equal(t, "success", final.Status)
			} else {
				require.Error(t, err)
				assert.Equal(t, "error", final.Status)
				assert.Contains(t, final.Error, "no workflows found")
			}
		})
	}
}

func TestUpdateJSONLUsesStdoutWhenRootRedirectsHumanOutput(t *testing.T) {
	initUpdateJSONLRepo(t)
	var commandErr error
	stdout, _ := captureOutput(t, func() error {
		root := &cobra.Command{Use: "gh"}
		root.SetOut(os.Stderr)
		root.SetErr(io.Discard)
		root.AddCommand(NewUpdateCommand(validateEngineStub))
		root.SetArgs([]string{"update", "--jsonl", "--no-compile"})
		commandErr = root.Execute()
		return nil
	})
	require.NoError(t, commandErr)
	events := decodeUpdateEvents(t, stdout)
	require.Len(t, events, 2)
	assert.Equal(t, "command_started", events[0].Event)
	assert.Equal(t, "command_completed", events[1].Event)
}

func TestUpdateJSONLSourceCommandActualChanges(t *testing.T) {
	dir := initUpdateJSONLRepo(t)
	for _, customDir := range []string{".github/workflows", filepath.Join(dir, "custom")} {
		t.Run(customDir, func(t *testing.T) {
			require.NoError(t, os.MkdirAll(customDir, 0755))
			path := filepath.Join(customDir, "review.md")
			require.NoError(t, os.WriteFile(path, []byte("---\non: push\nengine: claude\nsource: owner/repo/review.md@v1.0.0\n---\nOld\n"), 0600))
			resolveBefore, downloadBefore := resolveLatestRefFn, downloadWorkflowContentFn
			t.Cleanup(func() {
				resolveLatestRefFn, downloadWorkflowContentFn = resolveBefore, downloadBefore
			})
			resolveLatestRefFn = func(context.Context, string, string, bool, bool, time.Duration) (latestRefResolution, error) {
				return latestRefResolution{Ref: "v2.0.0"}, nil
			}
			downloadWorkflowContentFn = func(context.Context, string, string, string, bool) ([]byte, error) {
				return []byte("---\non: push\nengine: claude\n---\nNew source body\n"), nil
			}
			events, err := runUpdateJSONLCommand(t, NewUpdateCommand(validateEngineStub),
				"--dir", customDir, "--no-merge", "--no-compile", "--no-security-scanner", "review")
			require.NoError(t, err)
			var source, file *updateJSONLEvent
			for i := range events {
				if events[i].Event != "change" {
					continue
				}
				if events[i].Kind == "source" {
					source = &events[i]
				}
				if events[i].Kind == "workflow" && strings.HasSuffix(events[i].Path, "/review.md") {
					file = &events[i]
				}
			}
			require.NotNil(t, source, "%+v", events)
			assert.Equal(t, "owner/repo/review.md@v1.0.0", source.Before)
			assert.Equal(t, "owner/repo/review.md@v2.0.0", source.After)
			require.NotNil(t, file, "absolute custom directories must still classify workflows")
			assert.Len(t, file.Before, 64)
			assert.Len(t, file.After, 64)
			assert.NotEqual(t, file.Before, file.After)
			assert.GreaterOrEqual(t, *events[len(events)-1].ChangedFiles, 1)
			assert.NotContains(t, fmt.Sprint(events), "New source body")
		})
	}
}

func TestUpgradeJSONLActualCommandNoopAndCodemods(t *testing.T) {
	dir := initUpdateJSONLRepo(t)
	base := []string{"--skip-extension-upgrade", "--no-agent", "--no-mcp", "--no-skill"}
	events, err := runUpdateJSONLCommand(t, NewUpgradeCommand(validateEngineStub), append(base, "--no-fix")...)
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, "command_started", events[0].Event)
	assert.Equal(t, "success", events[1].Status)
	assert.Zero(t, *events[1].ChangedFiles)

	require.NoError(t, os.WriteFile(".github/workflows/review.md", []byte("---\non: push\nengine: claude\ntimeout_minutes: 10\n---\nTest\n"), 0600))
	var disabled []string
	for _, codemod := range GetAllCodemods() {
		if codemod.ID != "timeout-minutes-migration" {
			disabled = append(disabled, codemod.ID)
		}
	}
	args := append(base, "--no-actions", "--no-compile", "--dir", filepath.Join(dir, ".github/workflows"),
		"--disable-codemod="+strings.Join(disabled, ","))
	events, err = runUpdateJSONLCommand(t, NewUpgradeCommand(validateEngineStub), args...)
	require.NoError(t, err)
	var applied, changed bool
	for _, event := range events {
		if event.Event == "codemod" {
			applied = true
			require.NotEmpty(t, event.Codemods)
			assert.Equal(t, ".github/workflows/review.md", event.Path)
		}
		if event.Event == "change" && event.Kind == "workflow" {
			changed = true
		}
	}
	assert.True(t, applied)
	assert.True(t, changed)
	assert.GreaterOrEqual(t, *events[len(events)-1].ChangedFiles, 1)
	events, err = runUpdateJSONLCommand(t, NewUpgradeCommand(validateEngineStub), args...)
	require.NoError(t, err)
	for _, event := range events {
		assert.NotEqual(t, "codemod", event.Event, "unchanged workflows have no applied migrations")
	}
	assert.Zero(t, *events[len(events)-1].ChangedFiles)
}

func TestUpdateJSONLCompatibilityAndValidation(t *testing.T) {
	for _, constructor := range []func(func(string) error) *cobra.Command{NewUpdateCommand, NewUpgradeCommand} {
		for _, flag := range []string{"--interactive", "--auto-merge", "--repos=repo*"} {
			t.Run(constructor(validateEngineStub).Name()+flag, func(t *testing.T) {
				events, err := runUpdateJSONLCommand(t, constructor(validateEngineStub), flag)
				require.Error(t, err)
				require.Len(t, events, 2)
				assert.Equal(t, "error", events[1].Status)
			})
		}
	}
	for _, args := range [][]string{{"--audit"}, {"--json"}, {"unexpected-argument"}} {
		events, err := runUpdateJSONLCommand(t, NewUpgradeCommand(validateEngineStub), args...)
		require.Error(t, err)
		require.Len(t, events, 2)
		assert.Equal(t, "error", events[1].Status)
	}
}

type failUpdateJSONLWriter struct {
	calls int
	fail  int
}

func (w *failUpdateJSONLWriter) Write(data []byte) (int, error) {
	w.calls++
	if w.calls >= w.fail {
		return 0, io.ErrClosedPipe
	}
	return len(data), nil
}

func TestUpdateJSONLWriterFailure(t *testing.T) {
	for _, fail := range []int{1, 2} {
		cmd := NewUpdateCommand(validateEngineStub)
		writer := &failUpdateJSONLWriter{fail: fail}
		cmd.SetOut(writer)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--jsonl", "--auto-merge"})
		err := cmd.Execute()
		require.ErrorIs(t, err, io.ErrClosedPipe)
		assert.Equal(t, fail, writer.calls, "failed output must not be retried")
	}
}

func TestUpdateJSONLSnapshotsExcludePreexistingChangesAndReportPins(t *testing.T) {
	initUpdateJSONLRepo(t)
	require.NoError(t, os.MkdirAll(".github/aw", 0755))
	path := ".github/aw/actions-lock.json"
	require.NoError(t, os.WriteFile(path, []byte(`{"entries":{"one":{"repo":"actions/checkout","version":"v4","sha":"old"}}}`), 0600))
	require.NoError(t, os.WriteFile(".github/workflows/existing.md", []byte("already dirty"), 0600))
	var output bytes.Buffer
	reporter := &updateJSONLReporter{writer: &output, command: "update"}
	ctx := context.WithValue(context.Background(), updateJSONLKey{}, reporter)
	finish, err := observeUpdateChanges(ctx, "")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(`{"entries":{"one":{"repo":"actions/checkout","version":"v4","sha":"new"}}}`), 0600))
	require.NoError(t, finish())
	events := decodeUpdateEvents(t, output.String())
	require.Len(t, events, 2)
	assert.Equal(t, "artifact", events[0].Kind)
	assert.Equal(t, "action", events[1].Kind)
	assert.Equal(t, "actions/checkout@v4@old", events[1].Before)
	assert.Equal(t, "actions/checkout@v4@new", events[1].After)
	assert.Equal(t, 1, reporter.changed)
	assert.NotContains(t, output.String(), "existing.md")
}

func TestUpdateJSONLSnapshotsWithoutGitAndExternalTargets(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	workflowDir := filepath.Join(t.TempDir(), "workflows")
	target := filepath.Join(t.TempDir(), "custom", "workflows", "package.md")
	require.NoError(t, os.MkdirAll(workflowDir, 0755))
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
	require.NoError(t, os.WriteFile(target, []byte("---\nsource: owner/repo/package.md@v1\n---\nbefore\n"), 0600))

	var output bytes.Buffer
	reporter := &updateJSONLReporter{writer: &output, command: "update"}
	ctx := context.WithValue(context.Background(), updateJSONLKey{}, reporter)
	finish, err := observeUpdateChanges(ctx, workflowDir, target)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(target, []byte("---\nsource: owner/repo/package.md@v1\n---\nafter\n"), 0600))
	require.NoError(t, finish())

	events := decodeUpdateEvents(t, output.String())
	require.Len(t, events, 1)
	assert.Equal(t, "workflow", events[0].Kind)
	expectedPath, err := filepath.Rel(root, target)
	require.NoError(t, err)
	assert.Equal(t, filepath.ToSlash(expectedPath), events[0].Path)
	assert.Equal(t, 1, reporter.changed)
}

func TestUpdateJSONLChangedFilesDeduplicatePaths(t *testing.T) {
	var output bytes.Buffer
	reporter := &updateJSONLReporter{writer: &output, command: "update"}
	ctx := context.WithValue(context.Background(), updateJSONLKey{}, reporter)
	for _, kind := range []string{"workflow", "artifact", "source"} {
		require.NoError(t, emitUpdateEvent(ctx, updateJSONLEvent{Event: "change", Path: ".github/workflows/review.md", Kind: kind}))
	}
	require.NoError(t, reporter.complete(ctx, false, nil))
	events := decodeUpdateEvents(t, output.String())
	require.Len(t, events, 4)
	assert.Equal(t, 1, *events[3].ChangedFiles)
}

func TestUpdateJSONLBranchSourceCommitChange(t *testing.T) {
	initUpdateJSONLRepo(t)
	path := ".github/workflows/review.md"
	oldSHA, newSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	content := "---\nsource: owner/repo/review.md@main\nsource-sha: \"%s\"\n---\n"
	require.NoError(t, os.WriteFile(path, fmt.Appendf(nil, content, oldSHA), 0600))
	var output bytes.Buffer
	reporter := &updateJSONLReporter{writer: &output, command: "update"}
	ctx := context.WithValue(context.Background(), updateJSONLKey{}, reporter)
	finish, err := observeUpdateChanges(ctx, "")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, fmt.Appendf(nil, content, newSHA), 0600))
	require.NoError(t, finish())
	events := decodeUpdateEvents(t, output.String())
	require.Len(t, events, 2)
	assert.Equal(t, "workflow", events[0].Kind)
	assert.Equal(t, "source_sha", events[1].Kind)
	assert.Equal(t, oldSHA, events[1].Before)
	assert.Equal(t, newSHA, events[1].After)
	assert.Equal(t, 1, reporter.changed)
}

func TestUpdateJSONLWorkerCommandAndAggregation(t *testing.T) {
	original := runUpdateForTargetRepoFn
	t.Cleanup(func() { runUpdateForTargetRepoFn = original })
	runUpdateForTargetRepoFn = func(ctx context.Context, repo string, _ UpdateWorkflowsOptions, _, _ bool) error {
		require.NoError(t, emitUpdateEvent(ctx, updateJSONLEvent{Event: "change", Kind: "workflow", Path: ".github/workflows/review.md", Operation: "modified"}))
		require.NoError(t, emitUpdateEvent(ctx, updateJSONLEvent{Event: "pull_request", URL: "https://github.com/acme/repo/pull/42", Status: "created"}))
		require.NoError(t, emitOrgRolloutResult(orgRolloutResult{Status: "created", URL: "https://github.com/acme/repo/pull/42"}, ctx))
		return errors.New("auto-merge failed")
	}
	cmd := NewUpdateCommand(validateEngineStub)
	cmd.SetArgs([]string{"--jsonl", "--rollout-worker", "--repo=acme/repo", "--create-pull-request", "--yes"})
	var commandErr error
	stdout, _ := captureOutput(t, func() error {
		commandErr = cmd.Execute()
		return nil
	})
	require.Error(t, commandErr)
	require.True(t, strings.HasPrefix(stdout, orgRolloutResultPrefix))
	result, err := parseOrgRolloutResult([]byte(stdout))
	require.NoError(t, err)
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, "https://github.com/acme/repo/pull/42", result.URL)
	require.Len(t, result.Events, 2)
	result.Repo = "acme/repo"
	var output bytes.Buffer
	reporter := &updateJSONLReporter{writer: &output, command: "update"}
	ctx := context.WithValue(context.Background(), updateJSONLKey{}, reporter)
	require.NoError(t, reportUpdateRolloutResult(ctx, result))
	events := decodeUpdateEvents(t, output.String())
	require.Len(t, events, 3)
	assert.Equal(t, "pull_request", events[1].Event)
	assert.Equal(t, "created", events[1].Status)
	assert.Equal(t, "failed", events[2].Status)
	assert.Equal(t, 1, reporter.changed)
	for _, event := range events {
		assert.Equal(t, result.Repo, event.Repository)
	}
}

func TestUpdateJSONLRelaunchForwarding(t *testing.T) {
	for _, invalid := range []string{"not json\n", `{"schema_version":2,"command":"upgrade","event":"change"}` + "\n",
		`{"schema_version":1,"command":"upgrade","event":"command_completed"}` + "\n"} {
		forwarder := &updateJSONLForwarder{ctx: context.Background()}
		_, err := forwarder.Write([]byte(invalid))
		require.NoError(t, err)
		require.NoError(t, forwarder.finish())
	}
	var output bytes.Buffer
	reporter := &updateJSONLReporter{writer: &output, command: "upgrade"}
	ctx := context.WithValue(context.Background(), updateJSONLKey{}, reporter)
	forwarder := &updateJSONLForwarder{ctx: ctx}
	data := []byte("{\"schema_version\":1,\"command\":\"upgrade\",\"event\":\"change\",\"kind\":\"workflow\",\"path\":\".github/workflows/review.md\"}\n")
	_, err := forwarder.Write(append([]byte("legacy diagnostic\n"), data[:20]...))
	require.NoError(t, err)
	_, err = forwarder.Write(data[20:])
	require.NoError(t, err)
	require.NoError(t, forwarder.finish())
	assert.Equal(t, 1, reporter.changed)
	assert.JSONEq(t, string(data), output.String())
	_, err = forwarder.Write([]byte("{\"schema_version\":1,\"command\":\"upgrade\",\"event\":\"future_additive_event\",\"new_field\":true}\n"))
	require.NoError(t, err, "new schema-v1 events from a newer CLI must not break self-upgrade")
	assert.JSONEq(t, string(data), output.String(), "unknown events are ignored, not re-encoded with lost fields")
	_, err = forwarder.Write([]byte("{"))
	require.NoError(t, err)
	require.NoError(t, forwarder.finish(), "incomplete incidental output is ignored")
}

func TestUpdateJSONLCreatedIssueResponse(t *testing.T) {
	var output bytes.Buffer
	ctx := context.WithValue(context.Background(), updateJSONLKey{}, &updateJSONLReporter{writer: &output, command: "update"})
	require.NoError(t, reportCreatedOrgIssue(ctx, "acme/repo", []byte(`{"html_url":"https://github.com/acme/repo/issues/42"}`)))
	events := decodeUpdateEvents(t, output.String())
	require.Len(t, events, 1)
	assert.Equal(t, "issue", events[0].Event)
	assert.Equal(t, "created", events[0].Status)
	assert.Equal(t, "https://github.com/acme/repo/issues/42", events[0].URL)
	assert.Equal(t, "acme/repo", events[0].Repository)
	assert.Empty(t, events[0].Error)

	output.Reset()
	err := reportCreatedOrgIssue(ctx, "acme/repo", []byte("not json"))
	require.Error(t, err)
	events = decodeUpdateEvents(t, output.String())
	require.Len(t, events, 1)
	assert.Equal(t, "created", events[0].Status)
	assert.Empty(t, events[0].URL)
	assert.Contains(t, events[0].Error, "could not be decoded")
}

func TestUpdateJSONLWorkflowFailureWarnings(t *testing.T) {
	initUpdateJSONLRepo(t)
	require.NoError(t, os.WriteFile(".github/workflows/review.md", []byte("---\non: push\nengine: claude\nsource: owner/repo/review.md@v1.0.0\n---\n"), 0600))
	before := resolveLatestRefFn
	t.Cleanup(func() { resolveLatestRefFn = before })
	for _, rateLimited := range []bool{false, true} {
		t.Run(strconv.FormatBool(rateLimited), func(t *testing.T) {
			reason := "upstream source missing"
			if rateLimited {
				reason = "API rate limit exceeded"
			}
			resolveLatestRefFn = func(context.Context, string, string, bool, bool, time.Duration) (latestRefResolution, error) {
				return latestRefResolution{}, errors.New(reason)
			}
			events, err := runUpdateJSONLCommand(t, NewUpdateCommand(validateEngineStub), "--no-compile")
			if rateLimited {
				require.NoError(t, err, "existing rate-limit-only exit behavior is preserved")
			} else {
				require.Error(t, err)
			}
			found := false
			for _, event := range events {
				if event.Event == "warning" && event.Kind == "workflow" {
					found = true
					assert.Contains(t, event.Error, reason)
					assert.True(t, strings.HasSuffix(event.Path, "review.md"))
				}
			}
			assert.True(t, found, "the terminal aggregate error is not sufficient to diagnose individual failures")
		})
	}
}

func TestUpdateJSONLRelaunchSubprocess(t *testing.T) {
	if os.Getenv("GH_AW_JSONL_RELAUNCH_CANCEL_HELPER") == "1" {
		interrupts := make(chan os.Signal, 1)
		signal.Notify(interrupts, os.Interrupt)
		_ = os.WriteFile(os.Getenv("GH_AW_JSONL_RELAUNCH_READY_FILE"), []byte("ready"), 0600)
		<-interrupts
		_ = os.WriteFile(os.Getenv("GH_AW_JSONL_RELAUNCH_CLEANUP_FILE"), []byte("cleaned"), 0600)
		os.Exit(0)
	}
	if os.Getenv("GH_AW_JSONL_RELAUNCH_HELPER") == "1" {
		if !slices.Contains(os.Args, "--jsonl-continuation") || !slices.Contains(os.Args, "--skip-extension-upgrade") {
			os.Exit(2)
		}
		reporter := &updateJSONLReporter{writer: os.Stdout, command: "upgrade"}
		if err := reporter.emit(updateJSONLEvent{Event: "change", Kind: "workflow", Path: ".github/workflows/review.md", Operation: "modified"}); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	t.Setenv("GH_AW_JSONL_RELAUNCH_HELPER", "1")
	before := os.Args
	t.Cleanup(func() { os.Args = before })
	os.Args = []string{"test", "-test.run=^TestUpdateJSONLRelaunchSubprocess$", "--", "upgrade", "--jsonl"}
	executable, err := os.Executable()
	require.NoError(t, err)
	cmd := &cobra.Command{Use: "upgrade", RunE: func(cmd *cobra.Command, _ []string) error {
		return relaunchUpgradeWithContext(cmd.Context(), "--skip-extension-upgrade", executable)
	}}
	addUpdateJSONLFlag(cmd)
	events, err := runUpdateJSONLCommand(t, cmd)
	require.NoError(t, err)
	require.Len(t, events, 3)
	assert.Equal(t, "command_started", events[0].Event)
	assert.Equal(t, "change", events[1].Event)
	assert.Equal(t, "command_completed", events[2].Event)
	assert.Equal(t, "success", events[2].Status)
	assert.Equal(t, 1, *events[2].ChangedFiles, "the outer invocation must count relaunched changes")
}

func TestUpdateJSONLRelaunchCancellationAllowsCleanup(t *testing.T) {
	t.Setenv("GH_AW_JSONL_RELAUNCH_CANCEL_HELPER", "1")
	cleanupFile := filepath.Join(t.TempDir(), "cleanup")
	readyFile := filepath.Join(t.TempDir(), "ready")
	t.Setenv("GH_AW_JSONL_RELAUNCH_CLEANUP_FILE", cleanupFile)
	t.Setenv("GH_AW_JSONL_RELAUNCH_READY_FILE", readyFile)
	before := os.Args
	t.Cleanup(func() { os.Args = before })
	os.Args = []string{"test", "-test.run=^TestUpdateJSONLRelaunchSubprocess$", "--", "upgrade", "--jsonl"}
	executable, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := &cobra.Command{Use: "upgrade", RunE: func(cmd *cobra.Command, _ []string) error {
		return relaunchUpgradeWithContext(cmd.Context(), "--skip-extension-upgrade", executable)
	}}
	cmd.SetContext(ctx)
	addUpdateJSONLFlag(cmd)
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--jsonl"})
	result := make(chan error, 1)
	go func() {
		result <- cmd.Execute()
	}()
	assert.Eventually(t, func() bool {
		_, statErr := os.Stat(readyFile)
		return statErr == nil
	}, 10*time.Second, 10*time.Millisecond, "child should install its interrupt handler")
	cancel()
	err = <-result
	require.ErrorIs(t, err, context.Canceled)
	cleanup, err := os.ReadFile(cleanupFile)
	require.NoError(t, err)
	assert.Equal(t, "cleaned", string(cleanup))
}

func TestUpdateJSONLWorkerSubprocess(t *testing.T) {
	const url = "https://github.com/acme/repo/pull/42"
	if os.Getenv("GH_AW_JSONL_WORKER_HELPER") == "1" {
		fmt.Fprintln(os.Stdout, "untrusted repository output, not an event")
		_ = emitOrgRolloutResult(orgRolloutResult{Status: "created", URL: url, Events: []updateJSONLEvent{
			{SchemaVersion: 1, Command: "update", Event: "pull_request", URL: url, Status: "created"},
		}})
		os.Exit(1)
	}

	t.Setenv("GH_AW_JSONL_WORKER_HELPER", "1")
	result := runOrgRolloutWorker(context.Background(), "acme/repo", orgRolloutOptions{
		args: []string{"-test.run=^TestUpdateJSONLWorkerSubprocess$", "--"}, dryRun: true,
	})
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, url, result.URL)
	require.Len(t, result.Events, 1)
	result.Repo = "acme/repo"
	var output bytes.Buffer
	ctx := context.WithValue(context.Background(), updateJSONLKey{}, &updateJSONLReporter{writer: &output, command: "update"})
	require.NoError(t, reportUpdateRolloutResult(ctx, result))
	events := decodeUpdateEvents(t, output.String())
	require.Len(t, events, 2)
	assert.Equal(t, "pull_request", events[0].Event)
	assert.Equal(t, "failed", events[1].Status)
	assert.NotContains(t, output.String(), "untrusted repository output")
}

func TestOrgRolloutOutputPreservesLargeResultFrame(t *testing.T) {
	const limit = 1024
	payload := strings.Repeat("x", 8*1024*1024+1)
	resultData, err := json.Marshal(orgRolloutResult{
		Status: "created",
		Events: []updateJSONLEvent{{SchemaVersion: 1, Command: "update", Event: "warning", Error: payload}},
	})
	require.NoError(t, err)
	output := &orgRolloutOutput{limit: limit}
	_, err = output.Write([]byte("ordinary diagnostic\n" + orgRolloutResultPrefix + string(resultData) + "\n"))
	require.NoError(t, err)
	result, err := parseOrgRolloutResult(output.result)
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	assert.Len(t, result.Events[0].Error, len(payload))
	assert.LessOrEqual(t, len(output.tail), limit)
}

func TestUpdateJSONLCommandReuseRestoresContext(t *testing.T) {
	var calls int
	cmd := &cobra.Command{Use: "update", RunE: func(cmd *cobra.Command, _ []string) error {
		calls++
		if calls == 1 {
			require.NotNil(t, updateReporter(cmd.Context()))
		} else {
			assert.Nil(t, updateReporter(cmd.Context()))
		}
		return nil
	}}
	initialContext := context.WithValue(context.Background(), updateJSONLRepoKey{}, "acme/repo")
	cmd.SetContext(initialContext)
	addUpdateJSONLFlag(cmd)
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--jsonl"})
	require.NoError(t, cmd.Execute())
	assert.Same(t, initialContext, cmd.Context())
	assert.False(t, cmd.SilenceUsage)
	output.Reset()
	cmd.SetArgs([]string{"--jsonl=false"})
	require.NoError(t, cmd.Execute())
	assert.Empty(t, output.String())
	assert.Equal(t, 2, calls)
}
