package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

// updateCompileVersionGlobalsMu guards tests in this file that mutate the
// process-wide compiler version and release-build globals (via SetVersionInfo /
// workflow.SetIsRelease).
//
// The primary safety guarantee is that these tests never call t.Parallel():
// Go's test runner completes every non-parallel test in a package — including
// its t.Cleanup restoration — before releasing any t.Parallel()-marked tests
// to run concurrently, so the mutation is fully reverted before parallel
// tests observe the globals. This mutex is a defense-in-depth measure only:
// it does not synchronize with the many other pkg/cli tests elsewhere in the
// package that mutate the same globals without taking this lock, so it
// cannot prevent a race if one of those tests were changed to call
// t.Parallel() in the future. Any test added to this file that mutates these
// globals must acquire this lock and must not call t.Parallel().
var updateCompileVersionGlobalsMu sync.Mutex

func TestNewUpdateCompileConfigMatchesCompileCommandDefaults(t *testing.T) {
	t.Parallel()

	files := []string{".github/workflows/first.md", ".github/workflows/second.md"}

	got := newUpdateCompileConfig(files, "custom/workflows", "copilot", true, true)

	require.Equal(t, CompileConfig{
		MarkdownFiles:  files,
		Verbose:        true,
		EngineOverride: "copilot",
		WorkflowDir:    "custom/workflows",
		Approve:        true,
	}, got)
	require.False(t, got.RefreshStopTime, "update compilation must preserve stop times like compile")
}

func TestCompileWorkflowsForUpdatePropagatesCompileErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := compileWorkflowsForUpdate(ctx, []string{"workflow.md"}, "", "", false, false)

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	var compilationErr *updateCompilationError
	require.ErrorAs(t, err, &compilationErr)
}

func TestRecompileAllWorkflowsPropagatesCompileErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := recompileAllWorkflows(ctx, ".github/workflows", "", false, false)

	require.ErrorIs(t, err, context.Canceled)
}

func TestUpdateCompilation_ReconcilesDefaultActionFailureExpiry(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	t.Chdir(tempDir)

	initCmd := exec.Command("git", "init", "--quiet")
	initCmd.Dir = tempDir
	require.NoError(t, initCmd.Run())

	workflowPath := filepath.Join(workflowsDir, "example.md")
	workflowContent := `---
name: Example
on:
  workflow_dispatch:
engine: copilot
safe-outputs:
  add-comment:
---

Say hello.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o644))

	require.NoError(t, compileWorkflowsForUpdate(context.Background(), nil, "", "", false, false))

	lockContent, err := os.ReadFile(filepath.Join(workflowsDir, "example.lock.yml"))
	require.NoError(t, err)
	require.Contains(t, string(lockContent), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "0"`)
	require.NotContains(t, string(lockContent), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "168"`)

	_, err = os.Stat(filepath.Join(workflowsDir, "agentics-maintenance.yml"))
	require.True(t, os.IsNotExist(err), "implicit expiry alone must not generate maintenance")

	require.NoError(t, recompileAllWorkflows(context.Background(), "", "", false, false))
	lockContent, err = os.ReadFile(filepath.Join(workflowsDir, "example.lock.yml"))
	require.NoError(t, err)
	require.Contains(t, string(lockContent), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "0"`)
	require.NotContains(t, string(lockContent), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "168"`)
}

func TestUpdateCompilation_RegeneratesGeneratedWorkflowsWithCurrentVersion(t *testing.T) {
	// Deliberately not t.Parallel(): this test mutates process-wide compiler
	// version/release globals. The mutex below isolates that mutation so this
	// test cannot race with a future parallel pkg/cli test that reads them.
	updateCompileVersionGlobalsMu.Lock()
	defer updateCompileVersionGlobalsMu.Unlock()

	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	t.Chdir(tempDir)

	initCmd := exec.Command("git", "init", "--quiet")
	initCmd.Dir = tempDir
	require.NoError(t, initCmd.Run())

	originalVersion := GetVersion()
	originalRelease := workflow.IsRelease()
	SetVersionInfo("v1.2.3")
	workflow.SetIsRelease(true)
	t.Cleanup(func() {
		SetVersionInfo(originalVersion)
		workflow.SetIsRelease(originalRelease)
	})

	workflowPath := filepath.Join(workflowsDir, "example.md")
	workflowContent := `---
name: Example
on:
  slash_command:
    name: example
    strategy: centralized
engine: copilot
safe-outputs:
  create-issue:
    expires: 24
---

Say hello.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o644))

	require.NoError(t, compileWorkflowsForUpdate(context.Background(), nil, "", "", false, false))

	for _, filename := range []string{"agentic_commands.yml", "agentics-maintenance.yml"} {
		content, err := os.ReadFile(filepath.Join(workflowsDir, filename))
		require.NoError(t, err, "expected update compilation to generate %s", filename)
		require.Contains(t, string(content), "github/gh-aw-actions/setup@v1.2.3")
		require.NotContains(t, string(content), "github/gh-aw-actions/setup@dev")
	}
}
