package cli

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

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

	implicitEntries, err := os.ReadDir(workflowsDir)
	require.NoError(t, err)
	implicitFiles := make([]string, len(implicitEntries))
	for i, entry := range implicitEntries {
		implicitFiles[i] = entry.Name()
	}

	// An explicit default directory must compile identically to the implicit one.
	for _, explicitDir := range []string{".github/workflows", "./.github/workflows/"} {
		require.NoError(t, recompileAllWorkflows(context.Background(), explicitDir, "", false, false))
		lockContent, err = os.ReadFile(filepath.Join(workflowsDir, "example.lock.yml"))
		require.NoError(t, err)
		require.Contains(t, string(lockContent), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "0"`, "dir=%q", explicitDir)
		require.NotContains(t, string(lockContent), `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "168"`, "dir=%q", explicitDir)

		explicitEntries, err := os.ReadDir(workflowsDir)
		require.NoError(t, err)
		explicitFiles := make([]string, len(explicitEntries))
		for i, entry := range explicitEntries {
			explicitFiles[i] = entry.Name()
		}
		require.Equal(t, implicitFiles, explicitFiles, "generated files with dir=%q", explicitDir)
	}
}

func TestUpdateCompilation_RegeneratesGeneratedWorkflowsWithCurrentVersion(t *testing.T) {
	// Deliberately not t.Parallel(): this test mutates the process-wide
	// compiler version/release globals (SetVersionInfo / workflow.SetIsRelease),
	// following the same convention used by other pkg/cli tests that touch
	// these globals. Go's test runner completes every non-parallel test in a
	// package — including its t.Cleanup restoration below — before releasing
	// any t.Parallel()-marked tests to run concurrently, so the mutation is
	// fully reverted before any parallel test can observe it.
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
threat-detection-suppress:
  - rule: CTR-002
    reason: This test verifies generated workflow versions, not action pin enforcement.
---

Say hello.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o644))

	const setupSHA = "1234567890abcdef1234567890abcdef12345678"
	cache := workflow.NewActionCache(tempDir)
	require.True(t, cache.Set("github/gh-aw-actions/setup", "v1.2.3", setupSHA))
	require.NoError(t, cache.Save())

	require.NoError(t, compileWorkflowsForUpdate(context.Background(), nil, "", "", false, false))

	for _, filename := range []string{"agentic_commands.yml", "agentics-maintenance.yml"} {
		content, err := os.ReadFile(filepath.Join(workflowsDir, filename))
		require.NoError(t, err, "expected update compilation to generate %s", filename)
		require.Contains(t, string(content), "github/gh-aw-actions/setup@"+setupSHA+" # v1.2.3")
		require.NotContains(t, string(content), "github/gh-aw-actions/setup@dev")
	}
}

// TestUpdate_CompileThenUpdateKeepsCompilerPinnedArtifactsConsistent compiles a workflow,
// runs the update passes with network lookups that report newer releases and drifted
// registry digests, then recompiles. The lock file must be unchanged: the setup action
// stays on the compiler version and built-in containers stay on the compiler's pins.
func TestUpdate_CompileThenUpdateKeepsCompilerPinnedArtifactsConsistent(t *testing.T) {
	// Not parallel: mutates the process-wide compiler version/release globals.
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

	workflowContent := `---
name: Example
on:
  workflow_dispatch:
engine: copilot
tools:
  github:
    toolsets: [repos]
threat-detection-suppress:
  - rule: CTR-002
    reason: This test verifies update consistency, not action pin enforcement.
---

Say hello.
`
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "example.md"), []byte(workflowContent), 0o644))

	const setupSHA = "1234567890abcdef1234567890abcdef12345678"
	cache := workflow.NewActionCache(tempDir)
	require.True(t, cache.Set("github/gh-aw-actions/setup", "v1.2.3", setupSHA))
	require.NoError(t, cache.Save())

	ctx := context.Background()
	require.NoError(t, compileWorkflowsForUpdate(ctx, nil, "", "", false, false))
	lockPath := filepath.Join(workflowsDir, "example.lock.yml")
	compiled, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	require.Contains(t, string(compiled), "github/gh-aw-actions/setup@"+setupSHA+" # v1.2.3")

	images, err := collectImagesFromLockFiles(workflowsDir)
	require.NoError(t, err)
	var builtinImages []string
	for _, image := range images {
		if isBuiltinContainerImage(image) {
			builtinImages = append(builtinImages, image)
		}
	}
	require.NotEmpty(t, builtinImages, "expected the compiled workflow to reference built-in containers")

	// Network stubs: every action has a newer release and every image a different digest.
	const newerSetupSHA = "fedcba0987654321fedcba0987654321fedcba09"
	const driftedDigest = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	actionDeps := defaultActionUpdateDeps()
	actionDeps.getLatestRelease = func(_ context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
		if isGhAwSetupAction(repo) {
			return "v1.3.0", newerSetupSHA, nil
		}
		return currentVersion, "", fmt.Errorf("unexpected latest-release lookup for %s", repo)
	}
	actionDeps.getActionSHAForTag = func(_ context.Context, repo, tag string) (string, error) {
		if repo == "github/gh-aw-actions" && tag == "v1.2.3" {
			return setupSHA, nil
		}
		return newerSetupSHA, nil
	}
	var mu sync.Mutex
	var fetched []string
	containerDeps := defaultContainerPinUpdateDeps()
	containerDeps.fetchDigest = func(_ context.Context, image string, verbose bool) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		fetched = append(fetched, image)
		return driftedDigest, nil
	}

	// Same passes, options and order as RunUpdateWorkflows with the default --dir.
	relWorkflowsDir := ""
	require.NoError(t, updateActions(ctx, actionDeps, false, false, false, 0))
	require.NoError(t, updateActionsInWorkflowFiles(ctx, actionDeps, updateActionsOptions{workflowsDir: relWorkflowsDir}))
	newPins, err := updateContainerPins(ctx, containerDeps, relWorkflowsDir, false, containerPinUpdateOptions{refreshExisting: true})
	require.NoError(t, err)
	if newPins {
		require.NoError(t, recompileAllWorkflows(ctx, relWorkflowsDir, "", false, false))
	}

	for _, image := range builtinImages {
		require.NotContains(t, fetched, image, "built-in image must not be re-resolved from the registry")
	}

	updated, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	require.Equal(t, string(compiled), string(updated), "update must leave the compiled lock file consistent with the compiler")

	saved := workflow.NewActionCache(tempDir)
	require.NoError(t, saved.Load())
	entry, ok := saved.Entries["github/gh-aw-actions/setup@v1.2.3"]
	require.True(t, ok, "setup action must stay pinned to the compiler version, got %v", slices.Collect(maps.Keys(saved.Entries)))
	require.Equal(t, setupSHA, entry.SHA)
	for _, image := range builtinImages {
		pin, ok := saved.GetContainerPin(image)
		if !ok {
			continue
		}
		require.NotEqual(t, driftedDigest, pin.Digest, "built-in image %s must keep the compiler pin", image)
	}
}
