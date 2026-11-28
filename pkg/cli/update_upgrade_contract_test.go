//go:build !integration

package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceTargetDispatchContract(t *testing.T) {
	oldUpdate, oldUpgrade := runUpdateForTargetRepoFn, runUpgradeForTargetRepoFn
	t.Cleanup(func() {
		runUpdateForTargetRepoFn, runUpgradeForTargetRepoFn = oldUpdate, oldUpgrade
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sentinel := errors.New("target maintenance failed")
	target := "enterprise.example/acme/exact-repo"
	selected := []string{"review", "https://github.com/acme/package"}
	runUpdateForTargetRepoFn = func(gotCtx context.Context, repo string, opts UpdateWorkflowsOptions, createPR, _ bool) error {
		assert.Equal(t, ctx, gotCtx)
		assert.Equal(t, target, repo)
		assert.Equal(t, selected, opts.WorkflowNames)
		assert.True(t, opts.Yes)
		assert.True(t, createPR)
		return sentinel
	}
	runUpgradeForTargetRepoFn = func(gotCtx context.Context, repo string, opts upgradeOptions, createPR, _ bool) error {
		assert.Equal(t, ctx, gotCtx)
		assert.Equal(t, ctx, opts.ctx)
		assert.Equal(t, target, repo)
		assert.True(t, opts.yes)
		assert.True(t, opts.preReleases)
		assert.True(t, createPR)
		return sentinel
	}
	for _, newCommand := range []func(func(string) error) *cobra.Command{NewUpdateCommand, NewUpgradeCommand} {
		cmd := newCommand(validateEngineStub)
		cmd.SetContext(ctx)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		args := []string{"--repo", target, "--yes", "--create-pull-request"}
		if cmd.Name() == "update" {
			args = append(args, selected...)
		} else {
			args = append(args, "--pre-releases")
		}
		cmd.SetArgs(args)
		require.ErrorIs(t, cmd.Execute(), sentinel)
	}
}

func TestMaintenanceCanceledBeforeMutation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, RunUpdateWorkflows(ctx, UpdateWorkflowsOptions{}), context.Canceled)
	require.ErrorIs(t, runUpgradeCommand(upgradeOptions{ctx: ctx}), context.Canceled)
}

func TestMaintenanceUpgradeFailureStatus(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, initTestGitRepo(dir))
	t.Chdir(dir)
	workflowsDir := filepath.Join(dir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "broken.md"), []byte("---\non: [\n---\nInvalid workflow.\n"), 0o600))
	cmd := NewUpgradeCommand(validateEngineStub)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--skip-extension-upgrade", "--no-agent", "--no-mcp", "--no-skill", "--no-actions"})
	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to apply codemods")
	require.ErrorContains(t, err, "failed to compile workflows")
}

func TestMaintenanceTargetIsolation(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, initTestGitRepo(dir))
	source := filepath.Join(dir, "source")
	require.NoError(t, os.MkdirAll(source, 0o755))
	require.NoError(t, initTestGitRepo(source))
	require.NoError(t, os.MkdirAll(filepath.Join(source, ".github", "workflows"), 0o755))
	content := []byte("---\non: push\nengine: claude\n---\nTarget workflow.\n")
	require.NoError(t, os.WriteFile(filepath.Join(source, ".github", "workflows", "target.md"), content, 0o600))
	commit := exec.Command("git", "-C", source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "add", ".")
	require.NoError(t, commit.Run())
	commit = exec.Command("git", "-C", source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--quiet", "-m", "fixture")
	require.NoError(t, commit.Run())
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "gh"), []byte("#!/bin/sh\n[ \"$1\" = repo ] && [ \"$2\" = clone ] || exit 2\n[ \"$3\" = acme/target ] || exit 3\nexec git clone --quiet \"$MAINTENANCE_SOURCE\" \"$4\"\n"), 0o755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MAINTENANCE_SOURCE", source)
	t.Chdir(dir)
	require.NoError(t, os.WriteFile("caller.txt", []byte("untouched"), 0o600))
	require.NoError(t, runUpgradeForTargetRepo(context.Background(), "acme/target", upgradeOptions{
		noFix: true, noAgent: true, noMCP: true, noSkill: true,
	}, false, false))
	err := runUpdateForTargetRepo(context.Background(), "acme/target", UpdateWorkflowsOptions{
		WorkflowNames: []string{"missing"}, NoCompile: true, Yes: true,
	}, false, false)
	require.ErrorContains(t, err, "no workflows found matching")
	cwd, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, dir, cwd)
	caller, err := os.ReadFile("caller.txt")
	require.NoError(t, err)
	assert.Equal(t, "untouched", string(caller))
	checkedOut, err := os.ReadFile(filepath.Join(dir, updateTargetRepoCheckoutDir, "acme__target", ".github", "workflows", "target.md"))
	require.NoError(t, err)
	assert.Equal(t, content, checkedOut)
}

func TestMaintenanceCloneCancellationBeforeCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing-checkout")
	require.NoError(t, os.WriteFile(path, []byte("untouched"), 0o600))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, shallowCloneTargetRepo(ctx, "acme/target", path), context.Canceled)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "untouched", string(content))
}

func TestMaintenanceSubprocessHelper(t *testing.T) {
	mode := os.Getenv("GH_AW_MAINTENANCE_HELPER")
	if mode == "" {
		return
	}
	if expected := os.Getenv("GH_AW_MAINTENANCE_ARGS"); expected != "" {
		var args []string
		if json.Unmarshal([]byte(expected), &args) != nil {
			os.Exit(2)
		}
		actual, _ := json.Marshal(os.Args[1:])
		want, _ := json.Marshal(args)
		if string(actual) != string(want) {
			os.Exit(3)
		}
	}
	if mode == "failure" {
		os.Exit(7)
	}
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	fmt.Fprintln(os.Stdout, "streamed stdout")
	fmt.Fprintln(os.Stderr, "streamed stderr")
	if ready := os.Getenv("GH_AW_MAINTENANCE_READY"); ready != "" {
		_ = os.WriteFile(ready, []byte("ready"), 0o600)
	}
	<-interrupts
	os.Exit(9)
}

func TestMaintenanceRelaunchFlagAndFailureContract(t *testing.T) {
	before := os.Args
	t.Cleanup(func() { os.Args = before })
	os.Args = []string{"gh-aw", "-test.run=^TestMaintenanceSubprocessHelper$", "--", "upgrade",
		"--repo", "acme/exact-repo", "--yes", "--create-pull-request", "--pre-releases"}
	expected, err := json.Marshal(append(append([]string(nil), os.Args[1:]...), "--skip-extension-upgrade"))
	require.NoError(t, err)
	t.Setenv("GH_AW_MAINTENANCE_HELPER", "failure")
	t.Setenv("GH_AW_MAINTENANCE_ARGS", string(expected))
	exe, err := os.Executable()
	require.NoError(t, err)
	err = relaunchUpgradeWithContext(context.Background(), "--skip-extension-upgrade", exe)
	var exitErr *ExitCodeError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 7, exitErr.Code)
}

func TestMaintenanceRelaunchCancellationContract(t *testing.T) {
	before := os.Args
	t.Cleanup(func() { os.Args = before })
	os.Args = []string{"gh-aw", "-test.run=^TestMaintenanceSubprocessHelper$", "--"}
	t.Setenv("GH_AW_MAINTENANCE_HELPER", "cancellation")
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv("GH_AW_MAINTENANCE_READY", ready)
	exe, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- relaunchUpgradeWithContext(ctx, "--skip-extension-upgrade", exe) }()
	require.Eventually(t, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled, "a nonzero child exit during cancellation must retain the cancellation cause")
	case <-time.After(10 * time.Second):
		t.Fatal("relaunch did not stop after cancellation")
	}
}

func TestMaintenanceCheckoutStreamingAndCancellation(t *testing.T) {
	t.Setenv("GH_AW_MAINTENANCE_HELPER", "cancellation")
	exe, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	defer reader.Close()
	defer writer.Close()
	before := os.Stderr
	os.Stderr = writer
	t.Cleanup(func() { os.Stderr = before })
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestMaintenanceSubprocessHelper$")
	configureUpgradeRelaunchCancellation(cmd)
	type result struct {
		output []byte
		err    error
	}
	completed := make(chan result, 1)
	go func() {
		output, err := runMaintenanceCheckoutCommand(ctx, cmd)
		completed <- result{output, err}
	}()
	line := make(chan string, 1)
	go func() {
		text, _ := bufio.NewReader(reader).ReadString('\n')
		line <- text
	}()
	select {
	case text := <-line:
		assert.Contains(t, text, "streamed")
	case <-time.After(10 * time.Second):
		t.Fatal("checkout output was buffered until exit")
	}
	cancel()
	select {
	case got := <-completed:
		require.ErrorIs(t, got.err, context.Canceled)
		assert.Contains(t, string(got.output), "streamed stdout")
		assert.Contains(t, string(got.output), "streamed stderr")
	case <-time.After(10 * time.Second):
		t.Fatal("checkout did not stop after cancellation")
	}
}
