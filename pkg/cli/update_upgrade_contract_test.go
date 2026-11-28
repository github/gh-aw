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
	"runtime"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceTargetDispatchContract(t *testing.T) {
	oldUpdate, oldUpgrade := runUpdateForTargetRepoFn, runUpgradeForTargetRepoFn
	t.Cleanup(func() {
		runUpdateForTargetRepoFn, runUpgradeForTargetRepoFn = oldUpdate, oldUpgrade
	})
	ctx := t.Context()
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

func TestMaintenanceAuditRejectsRemoteTargets(t *testing.T) {
	for _, target := range []struct {
		flag    string
		message string
	}{
		{"--repo=acme/target", "--repo"},
		{"--org=acme", "org or enterprise"},
		{"--enterprise=acme", "org or enterprise"},
	} {
		t.Run(target.flag, func(t *testing.T) {
			cmd := NewUpgradeCommand(validateEngineStub)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"--audit", target.flag})
			err := cmd.Execute()
			require.ErrorContains(t, err, "--audit")
			require.ErrorContains(t, err, target.message)
		})
	}
}

func TestMaintenanceUpgradeFailureStatus(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, initTestGitRepo(dir))
	t.Chdir(dir)
	outputPath := filepath.Join(dir, "upgrade-output")
	output, err := os.Create(outputPath)
	require.NoError(t, err)
	before := os.Stderr
	os.Stderr = output
	t.Cleanup(func() {
		os.Stderr = before
		_ = output.Close()
	})
	workflowsDir := filepath.Join(dir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "broken.md"), []byte("---\non: [\n---\nInvalid workflow.\n"), 0o600))
	cmd := NewUpgradeCommand(validateEngineStub)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--skip-extension-upgrade", "--no-agent", "--no-mcp", "--no-skill", "--no-actions"})
	err = cmd.Execute()
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to apply codemods")
	require.ErrorContains(t, err, "failed to compile workflows")
	require.NoError(t, output.Close())
	diagnostics, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.NotContains(t, string(diagnostics), "Upgrade complete")
}

func TestMaintenanceUpdateDependencyFailureStatus(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, initTestGitRepo(dir))
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll(".github/workflows", 0o755))
	require.NoError(t, os.MkdirAll(".github/aw", 0o755))
	require.NoError(t, os.WriteFile(".github/aw/actions-lock.json", []byte("{"), 0o600))
	require.NoError(t, os.WriteFile(".github/workflows/example.lock.yml", []byte("bash \"${RUNNER_TEMP}/gh-aw/actions/download_docker_images.sh\" alpine:3\n"), 0o600))
	err := RunUpdateWorkflows(context.Background(), UpdateWorkflowsOptions{NoCompile: true})
	require.ErrorContains(t, err, "failed to update actions-lock.json")
	require.ErrorContains(t, err, "failed to update container pins")
}

func TestMaintenanceUpgradeObserverFailureStatus(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, initTestGitRepo(dir))
	t.Chdir(dir)
	require.NoError(t, os.MkdirAll(".github/workflows", 0o755))
	require.NoError(t, os.WriteFile(".github/workflows/review.md", []byte("---\non: push\nengine: claude\ntimeout_minutes: 10\n---\nTest\n"), 0o600))
	outputPath := filepath.Join(dir, "upgrade-output")
	output, err := os.Create(outputPath)
	require.NoError(t, err)
	before := os.Stderr
	os.Stderr = output
	t.Cleanup(func() {
		os.Stderr = before
		_ = output.Close()
	})
	writer := &failUpdateJSONLWriter{fail: 2}
	ctx := context.WithValue(context.Background(), updateJSONLKey{}, &updateJSONLReporter{writer: writer, command: "upgrade"})
	err = runUpgradeCommand(upgradeOptions{
		ctx: ctx, skipExtensionUpgrade: true, noAgent: true, noMCP: true, noSkill: true, noActions: true, noCompile: true,
	})
	require.ErrorIs(t, err, io.ErrClosedPipe)
	require.NoError(t, output.Close())
	diagnostics, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.NotContains(t, string(diagnostics), "Upgrade complete")
}

func TestMaintenanceTargetIsolation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a shell executable fixture")
	}
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

func TestMaintenanceRemoteUpgradeSelectsReleaseBeforeCheckout(t *testing.T) {
	beforeVersion, beforeRelease := GetVersion(), workflow.IsRelease()
	beforeLookup, beforeTarget := getLatestExtensionReleaseFn, runUpgradeForTargetRepoFn
	t.Cleanup(func() {
		SetVersionInfo(beforeVersion)
		workflow.SetIsRelease(beforeRelease)
		getLatestExtensionReleaseFn, runUpgradeForTargetRepoFn = beforeLookup, beforeTarget
	})
	SetVersionInfo("v0.2.0")
	workflow.SetIsRelease(true)
	caller, err := os.Getwd()
	require.NoError(t, err)
	for _, prereleases := range []bool{false, true} {
		selected := false
		getLatestExtensionReleaseFn = func(_ context.Context, includePrereleases bool) (string, error) {
			selected = true
			assert.Equal(t, prereleases, includePrereleases)
			cwd, err := os.Getwd()
			require.NoError(t, err)
			assert.Equal(t, caller, cwd, "release selection must precede changing into a target checkout")
			return "v0.2.0", nil
		}
		runUpgradeForTargetRepoFn = func(_ context.Context, repo string, opts upgradeOptions, createPR, _ bool) error {
			assert.True(t, selected, "remote upgrade must actually select the extension release channel")
			assert.Equal(t, "acme/exact-repo", repo)
			assert.True(t, createPR)
			assert.True(t, opts.skipExtensionUpgrade, "checkout upgrade must not recursively relaunch --repo")
			return nil
		}
		cmd := NewUpgradeCommand(validateEngineStub)
		args := []string{"--repo", "acme/exact-repo", "--create-pull-request", "--yes"}
		if prereleases {
			args = append(args, "--pre-releases")
		}
		cmd.SetArgs(args)
		require.NoError(t, cmd.Execute())
		assert.True(t, selected)
	}
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
	if mode == "descendant" {
		child := exec.Command(os.Args[0], "-test.run=^TestMaintenanceSubprocessHelper$")
		child.Env = append(os.Environ(), "GH_AW_MAINTENANCE_HELPER=cancellation", "GH_AW_MAINTENANCE_READY=")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			os.Exit(2)
		}
		_ = os.WriteFile(os.Getenv("GH_AW_MAINTENANCE_READY"), []byte("ready"), 0o600)
		_ = child.Wait()
		os.Exit(0)
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
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv("GH_AW_MAINTENANCE_READY", ready)
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
	require.Eventually(t, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)
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

func TestMaintenanceCheckoutDescendantCancellation(t *testing.T) {
	t.Setenv("GH_AW_MAINTENANCE_HELPER", "descendant")
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv("GH_AW_MAINTENANCE_READY", ready)
	exe, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan error, 1)
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestMaintenanceSubprocessHelper$")
	go func() {
		_, err := runMaintenanceCheckoutCommand(ctx, cmd)
		completed <- err
	}()
	require.Eventually(t, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	}, 10*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-completed:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("checkout descendants kept output pipes open after cancellation")
	}
}
