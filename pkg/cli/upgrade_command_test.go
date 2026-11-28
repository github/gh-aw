//go:build !integration

package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// upgradeValidateEngineStub is a no-op engine validator for upgrade command tests.
func upgradeValidateEngineStub(engine string) error { return nil }

func TestUpgradeCommandHelpTextConsistency(t *testing.T) {
	t.Parallel()

	cmd := NewUpgradeCommand(upgradeValidateEngineStub)
	require.NotNil(t, cmd, "upgrade command should be created")

	assert.Contains(t, cmd.Long, "Upgrade the repository to the latest version of agentic workflows.", "long description should use correct grammar")

	approveFlag := cmd.Flags().Lookup("approve")
	require.NotNil(t, approveFlag, "--approve flag should exist")
	assert.Contains(t, approveFlag.Usage, "When strict mode is active", "--approve description should match compile semantics")

	preReleasesFlag := cmd.Flags().Lookup("pre-releases")
	require.NotNil(t, preReleasesFlag, "--pre-releases flag should exist")
	assert.Contains(t, preReleasesFlag.Usage, "Include pre-release versions", "--pre-releases description should mention pre-release upgrades")
	assert.Contains(t, preReleasesFlag.Usage, "installed by exact tag", "--pre-releases description should explain prerelease pinning")
	assert.Contains(t, cmd.Example, "stable releases are the default", "help text should distinguish stable releases from prereleases")

	disableCodemodFlag := cmd.Flags().Lookup("disable-codemod")
	require.NotNil(t, disableCodemodFlag, "--disable-codemod flag should exist")
	assert.Equal(t, "stringSlice", disableCodemodFlag.Value.Type())
	assert.Contains(t, disableCodemodFlag.Usage, "Disable specific codemod IDs", "--disable-codemod usage should describe codemod exclusion")
}

func TestUpgradeCommandNewFlags(t *testing.T) {
	t.Parallel()

	cmd := NewUpgradeCommand(upgradeValidateEngineStub)
	require.NotNil(t, cmd, "upgrade command should be created")

	// F1: --engine flag
	engineFlag := cmd.Flags().Lookup("engine")
	require.NotNil(t, engineFlag, "--engine/-e flag should exist on upgrade command")
	assert.Equal(t, "e", engineFlag.Shorthand, "--engine flag should have -e shorthand")
	assert.Contains(t, engineFlag.Usage, "Override AI engine", "--engine description should describe engine override")
	assert.Contains(t, cmd.Example, "--engine", "upgrade examples should show --engine usage")

	// F4: --repo flag
	repoFlag := cmd.Flags().Lookup("repo")
	require.NotNil(t, repoFlag, "--repo/-r flag should exist on upgrade command")
	assert.Equal(t, "r", repoFlag.Shorthand, "--repo flag should have -r shorthand")
	assert.Contains(t, repoFlag.Usage, "Target repository", "--repo description should describe target repository")
	assert.Contains(t, cmd.Example, "--repo", "upgrade examples should show --repo usage")
}

func TestUpgradeCommandEngineValidationRunsEarly(t *testing.T) {
	t.Parallel()

	validated := false
	validate := func(engine string) error {
		validated = true
		assert.Equal(t, "bad-engine", engine)
		return assert.AnError
	}
	cmd := NewUpgradeCommand(validate)
	cmd.SetArgs([]string{"--engine", "bad-engine"})
	err := cmd.Execute()
	require.Error(t, err, "invalid engine should fail early")
	assert.True(t, validated, "engine validator should have been called")
}

func TestUpgradeCommandRepoOrgMutualExclusion(t *testing.T) {
	t.Parallel()

	cmd := NewUpgradeCommand(upgradeValidateEngineStub)
	cmd.SetArgs([]string{"--repo", "owner/repo", "--org", "my-org"})
	err := cmd.Execute()
	require.Error(t, err, "should error when both --repo and --org are specified")
	require.ErrorContains(t, err, "--repo", "error should mention --repo flag")
	require.ErrorContains(t, err, "--org", "error should mention --org flag")
}

func TestUpgradeCommandFlagRegistration(t *testing.T) {
	t.Parallel()

	cmd := NewUpgradeCommand(upgradeValidateEngineStub)
	require.NotNil(t, cmd, "upgrade command should be created")

	// --repo alone should be accepted at flag parse time (RunE will proceed)
	repoFlag := cmd.Flags().Lookup("repo")
	require.NotNil(t, repoFlag, "--repo flag should be registered")

	// --org alone should be accepted at flag parse time (RunE will proceed)
	orgFlag := cmd.Flags().Lookup("org")
	require.NotNil(t, orgFlag, "--org flag should be registered")

	// --engine alone should be accepted
	engineFlag := cmd.Flags().Lookup("engine")
	require.NotNil(t, engineFlag, "--engine flag should be registered")

	// --repos without --org should fail at RunE
	cmd2 := NewUpgradeCommand(upgradeValidateEngineStub)
	cmd2.SetArgs([]string{"--repos", "foo-*"})
	err := cmd2.Execute()
	require.Error(t, err, "should error when --repos is specified without --org")
	require.ErrorContains(t, err, "--repos", "error should mention --repos flag")
}

func TestUpgradeCommandArtifactFlagsForwarded(t *testing.T) {
	original := runUpgradeForTargetRepoFn
	t.Cleanup(func() { runUpgradeForTargetRepoFn = original })

	for _, flag := range []string{"no-agent", "no-mcp", "no-skill"} {
		t.Run(flag, func(t *testing.T) {
			var captured upgradeOptions
			runUpgradeForTargetRepoFn = func(_ context.Context, _ string, opts upgradeOptions, _ bool, _ bool) error {
				captured = opts
				return nil
			}
			cmd := NewUpgradeCommand(upgradeValidateEngineStub)
			skipFlag := cmd.Flags().Lookup(flag)
			require.NotNil(t, skipFlag)
			assert.Equal(t, "false", skipFlag.DefValue)
			cmd.SetArgs([]string{"--repo", "owner/repo", "--" + flag})
			require.NoError(t, cmd.Execute())
			assert.Equal(t, flag == "no-agent", captured.noAgent)
			assert.Equal(t, flag == "no-mcp", captured.noMCP)
			assert.Equal(t, flag == "no-skill", captured.noSkill)
		})
	}
}

func TestUpgradeCommandCopilotArtifacts(t *testing.T) {
	withMockAWMarkdownFileList(t, []string{"create-agentic-workflow.md"}, nil)
	t.Setenv("GH_AW_ACTION_MODE", "action")
	t.Setenv("GH_AW_WORKFLOWS_DIR", ".github/workflows")

	tests := []struct {
		name          string
		engines       []string
		args          []string
		existing      []string
		wantAgent     bool
		wantSetup     bool
		wantSkill     bool
		customDir     bool
		nested        bool
		plainMarkdown bool
	}{
		{name: "claude", engines: []string{"engine: claude"}, wantSkill: true},
		{name: "claude override", engines: []string{"engine: claude"}, args: []string{"--engine", "claude"}, wantSkill: true},
		{name: "codex", engines: []string{"engine: codex"}, wantSkill: true},
		{name: "copilot", engines: []string{"engine: copilot"}, wantAgent: true, wantSetup: true, wantSkill: true},
		{name: "object engine", engines: []string{"engine:\n  id: copilot"}, wantAgent: true, wantSetup: true, wantSkill: true},
		{name: "object claude", engines: []string{"engine:\n  id: claude"}, wantSkill: true},
		{name: "default engine", engines: []string{""}, wantAgent: true, wantSetup: true, wantSkill: true},
		{name: "mixed engines", engines: []string{"engine: claude", "engine: copilot"}, wantAgent: true, wantSetup: true, wantSkill: true},
		{name: "copilot override", engines: []string{"engine: claude"}, args: []string{"--engine", "copilot"}, wantAgent: true, wantSetup: true, wantSkill: true},
		{name: "mixed case copilot override", engines: []string{"engine: claude"}, args: []string{"--engine", "COPILOT"}, wantAgent: true, wantSetup: true, wantSkill: true},
		{name: "copilot with no fix", engines: []string{"engine: copilot"}, args: []string{"--no-fix"}, wantAgent: true, wantSetup: true, wantSkill: true},
		{name: "non-copilot override", engines: []string{"engine: copilot"}, args: []string{"--engine", "claude"}, wantSkill: true},
		{name: "plain markdown is not a workflow", plainMarkdown: true, wantSkill: true},
		{name: "inherited non-copilot engine", engines: []string{"imports:\n  - ./shared.md"}, wantSkill: true},
		{name: "existing agent only", engines: []string{"engine: claude"}, existing: []string{"agent"}, wantAgent: true, wantSkill: true},
		{name: "existing setup only", engines: []string{"engine: claude"}, existing: []string{"setup"}, wantSetup: true, wantSkill: true},
		{name: "existing setup in custom workflow directory", engines: []string{"engine: claude"}, existing: []string{"setup"}, customDir: true, nested: true, wantSetup: true, wantSkill: true},
		{name: "no agent", engines: []string{"engine: copilot"}, args: []string{"--no-agent"}, wantSetup: true, wantSkill: true},
		{name: "no mcp", engines: []string{"engine: copilot"}, args: []string{"--no-mcp"}, wantAgent: true, wantSkill: true},
		{name: "no skill", engines: []string{"engine: copilot"}, args: []string{"--no-skill"}, wantAgent: true, wantSetup: true},
		{name: "skip existing files", engines: []string{"engine: copilot"}, existing: []string{"agent", "setup", "skill"}, args: []string{"--no-agent", "--no-mcp", "--no-skill"}, wantAgent: true, wantSetup: true, wantSkill: true},
		{name: "no workflows", wantSkill: true},
		{name: "custom workflow directory", engines: []string{"engine: copilot"}, customDir: true, wantAgent: true, wantSetup: true, wantSkill: true},
		{name: "nested working directory", engines: []string{"engine: copilot"}, nested: true, wantAgent: true, wantSetup: true, wantSkill: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, initTestGitRepo(dir))
			workflowsDir := filepath.Join(dir, ".github", "workflows")
			args := []string{"--no-actions", "--no-compile", "--skip-extension-upgrade"}
			if tt.customDir {
				workflowsDir = filepath.Join(dir, "custom", "workflows")
				args = append(args, "--dir", filepath.Join("custom", "workflows"))
			}
			if tt.nested {
				nestedDir := filepath.Join(dir, "nested")
				require.NoError(t, os.MkdirAll(nestedDir, 0o755))
				chdirForTest(t, nestedDir)
			} else {
				chdirForTest(t, dir)
			}
			if len(tt.engines) > 0 {
				require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
				for i, engine := range tt.engines {
					content := "---\non: workflow_dispatch\n" + engine + "\n---\nSay hello.\n"
					path := filepath.Join(workflowsDir, string(rune('a'+i))+".md")
					require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
				}
				if tt.name == "inherited non-copilot engine" {
					shared := "---\nengine: claude\n---\nShared engine config.\n"
					require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "shared.md"), []byte(shared), 0o644))
				}
				require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "README.md"), []byte("# Workflows\n"), 0o644))
			}
			if tt.plainMarkdown {
				require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "notes.md"), []byte("# Notes\n"), 0o644))
			}
			paths := map[string]string{
				"agent": filepath.Join(dir, ".github", "agents", "agentic-workflows.md"),
				"setup": filepath.Join(workflowsDir, "copilot-setup-steps.yml"),
				"skill": filepath.Join(dir, ".github", "skills", "agentic-workflows", "SKILL.md"),
			}
			const existingContent = "existing content\n"
			for _, artifact := range tt.existing {
				require.NoError(t, os.MkdirAll(filepath.Dir(paths[artifact]), 0o755))
				require.NoError(t, os.WriteFile(paths[artifact], []byte(existingContent), 0o644))
			}

			cmd := NewUpgradeCommand(upgradeValidateEngineStub)
			cmd.SetArgs(append(args, tt.args...))
			require.NoError(t, cmd.Execute())

			for artifact, want := range map[string]bool{"agent": tt.wantAgent, "setup": tt.wantSetup, "skill": tt.wantSkill} {
				if want {
					assert.FileExists(t, paths[artifact])
				} else {
					assert.NoFileExists(t, paths[artifact])
				}
			}
			if tt.name == "existing agent only" {
				content, err := os.ReadFile(paths["agent"])
				require.NoError(t, err)
				assert.NotEqual(t, existingContent, string(content), "existing agents should be refreshed")
			}
			if tt.name == "skip existing files" {
				for _, path := range paths {
					content, err := os.ReadFile(path)
					require.NoError(t, err)
					assert.Equal(t, existingContent, string(content), "opt-out flags should preserve existing files")
				}
			}
		})
	}
}

// TestUpgradeCommandRepoDispatchNoPR verifies that plain `upgrade --repo`
// dispatches to the target-repo runner without requesting PR creation.
func TestUpgradeCommandRepoDispatchNoPR(t *testing.T) {
	origFn := runUpgradeForTargetRepoFn
	defer func() { runUpgradeForTargetRepoFn = origFn }()

	var capturedCreatePR bool
	runUpgradeForTargetRepoFn = func(_ context.Context, repo string, _ upgradeOptions, createPR bool, _ bool) error {
		capturedCreatePR = createPR
		return nil
	}

	cmd := NewUpgradeCommand(upgradeValidateEngineStub)
	cmd.SetArgs([]string{"--repo", "owner/repo"})
	err := cmd.Execute()
	require.NoError(t, err)
	assert.False(t, capturedCreatePR, "plain --repo must not request PR creation")
}

// TestUpgradeCommandRepoDispatchWithPR verifies that `upgrade --repo --create-pull-request`
// dispatches to the target-repo runner with PR creation requested.
func TestUpgradeCommandRepoDispatchWithPR(t *testing.T) {
	origFn := runUpgradeForTargetRepoFn
	defer func() { runUpgradeForTargetRepoFn = origFn }()

	var capturedCreatePR bool
	runUpgradeForTargetRepoFn = func(_ context.Context, repo string, _ upgradeOptions, createPR bool, _ bool) error {
		capturedCreatePR = createPR
		return nil
	}

	cmd := NewUpgradeCommand(upgradeValidateEngineStub)
	cmd.SetArgs([]string{"--repo", "owner/repo", "--create-pull-request"})
	err := cmd.Execute()
	require.NoError(t, err)
	assert.True(t, capturedCreatePR, "--repo --create-pull-request must request PR creation")
}

func TestRelaunchUpgradeWithContextRejectsRelativeExecutableOverride(t *testing.T) {
	err := relaunchUpgradeWithContext(context.Background(), "--skip-extension-upgrade", "relative/gh-aw")
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid executable path")
}

func TestRelaunchUpgradeWithContextRejectsNullByteArgument(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	os.Args = []string{"gh-aw", "compile", "bad\x00arg"}

	err := relaunchUpgradeWithContext(context.Background(), "--skip-extension-upgrade", "/bin/echo")
	require.Error(t, err)
	require.ErrorContains(t, err, "argument contains invalid control characters")
}

func TestRelaunchUpgradeWithContextAllowsEmptyForwardedArgument(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })
	os.Args = []string{"gh-aw", "compile", ""}

	err := relaunchUpgradeWithContext(context.Background(), "--skip-extension-upgrade", "/bin/echo")
	require.NoError(t, err)
}

func TestRelaunchUpgradeWithContextPassesShellMetacharactersLiterally(t *testing.T) {
	origArgs := os.Args
	origStdout := os.Stdout
	t.Cleanup(func() {
		os.Args = origArgs
		os.Stdout = origStdout
	})
	os.Args = []string{"gh-aw", "upgrade", ";", "$(whoami)"}

	readOutput, writeOutput, err := os.Pipe()
	require.NoError(t, err)
	writeOutputClosed := false
	t.Cleanup(func() {
		_ = readOutput.Close()
		if !writeOutputClosed {
			_ = writeOutput.Close()
		}
	})
	os.Stdout = writeOutput

	require.NoError(t, relaunchUpgradeWithContext(context.Background(), "--skip-extension-upgrade", "/bin/echo"))
	require.NoError(t, writeOutput.Close())
	writeOutputClosed = true
	output, err := io.ReadAll(readOutput)
	require.NoError(t, err)
	require.Equal(t, "upgrade ; $(whoami) --skip-extension-upgrade\n", string(output))
}

func TestRelaunchUpgradeWithContextRejectsUnknownExtraFlag(t *testing.T) {
	err := relaunchUpgradeWithContext(context.Background(), "--unknown-flag", "/bin/echo")
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid relaunch flag")
}

func TestUpgradeTargetWorkflowEnvironmentIsolation(t *testing.T) {
	original := runUpgradeForTargetRepoFn
	t.Cleanup(func() { runUpgradeForTargetRepoFn = original })
	outside := filepath.Join(t.TempDir(), "outside-workflows")
	t.Setenv("GH_AW_WORKFLOWS_DIR", outside)
	runUpgradeForTargetRepoFn = func(_ context.Context, _ string, _ upgradeOptions, _ bool, _ bool) error {
		assert.Equal(t, outside, os.Getenv("GH_AW_WORKFLOWS_DIR"))
		return assert.AnError
	}
	cmd := NewUpgradeCommand(upgradeValidateEngineStub)
	cmd.SetArgs([]string{"--repo", "owner/repo", "--skip-extension-upgrade"})
	require.ErrorIs(t, cmd.Execute(), assert.AnError)
	assert.Equal(t, outside, os.Getenv("GH_AW_WORKFLOWS_DIR"))
}

func TestUpgradeOrgPreviewsDoNotCheckExtension(t *testing.T) {
	version, lookup, search, scan := GetVersion(), getLatestExtensionReleaseFn, searchOrgLockWorkflowReposFn, scanUpgradeRepoFn
	t.Cleanup(func() {
		SetVersionInfo(version)
		getLatestExtensionReleaseFn, searchOrgLockWorkflowReposFn, scanUpgradeRepoFn = lookup, search, scan
	})
	SetVersionInfo("v1.0.0")
	getLatestExtensionReleaseFn = func(context.Context, bool) (string, error) {
		t.Error("preview checked the extension")
		return "", nil
	}
	outside := filepath.Join(t.TempDir(), "caller")
	searchOrgLockWorkflowReposFn = func(context.Context, string, bool) ([]string, error) {
		assert.Equal(t, outside, os.Getenv("GH_AW_WORKFLOWS_DIR"))
		return nil, nil
	}
	scanUpgradeRepoFn = func(context.Context, string, bool) (orgRepoPreview, bool, error) {
		assert.Equal(t, outside, os.Getenv("GH_AW_WORKFLOWS_DIR"))
		return orgRepoPreview{}, false, nil
	}
	t.Setenv("GH_AW_WORKFLOWS_DIR", outside)
	for _, args := range [][]string{
		{"--org", "example"},
		{"--org", "example", "--dry-run", "--create-pull-request"},
		{"--repo", "example/repo", "--rollout-worker", "--dry-run"},
	} {
		cmd := NewUpgradeCommand(upgradeValidateEngineStub)
		cmd.SetArgs(args)
		require.NoError(t, cmd.Execute())
		assert.Equal(t, outside, os.Getenv("GH_AW_WORKFLOWS_DIR"))
	}
}

func TestUpgradeMissingWorkflowDirectorySkipsDependentPhases(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, initTestGitRepo(dir))
	chdirForTest(t, dir)
	t.Setenv("GH_AW_WORKFLOWS_DIR", "")
	cmd := NewUpgradeCommand(upgradeValidateEngineStub)
	cmd.SetArgs([]string{"--skip-extension-upgrade", "--no-agent", "--no-mcp", "--no-skill"})
	require.NoError(t, cmd.Execute())
	assert.NoDirExists(t, filepath.Join(dir, ".github", "workflows"))
}

func TestUpgradeCompilesRelativeWorkflowDirectories(t *testing.T) {
	for _, test := range []struct {
		name, directory, option string
	}{
		{"default", ".github/workflows", ""},
		{"explicit default", ".github/workflows", ".github/workflows"},
		{"custom", "custom/workflows", "custom/workflows"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, initTestGitRepo(dir))
			chdirForTest(t, dir)
			t.Setenv("GH_AW_WORKFLOWS_DIR", "")
			require.NoError(t, os.MkdirAll(filepath.Join(dir, test.directory), 0o755))
			err := runUpgradeCommand(upgradeOptions{
				ctx: context.Background(), workflowDir: test.option, skipExtensionUpgrade: true,
				noAgent: true, noMCP: true, noSkill: true, noActions: true,
			})
			require.ErrorContains(t, err, "no workflow markdown files")
		})
	}
}

func TestUpgradePreviewUsesCheckoutWorkflowDirectory(t *testing.T) {
	checkout, outside := t.TempDir(), t.TempDir()
	workflows := filepath.Join(checkout, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflows, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workflows, "test.md"), []byte("---\non: workflow_dispatch\n---\n"), 0o644))
	t.Setenv("GH_AW_WORKFLOWS_DIR", outside)
	preview, include, err := previewUpgradeRepoCheckout("owner/repo", checkout, false)
	require.NoError(t, err)
	require.True(t, include)
	assert.Equal(t, 1, preview.TotalWorkflows)
	assert.Equal(t, outside, os.Getenv("GH_AW_WORKFLOWS_DIR"))
}

func TestUpgradeTargetUsesCheckoutWorkflowDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a shell script")
	}
	gitPath, err := exec.LookPath("git")
	require.NoError(t, err)
	source, workspace, bin := t.TempDir(), t.TempDir(), t.TempDir()
	require.NoError(t, initTestGitRepo(source))
	require.NoError(t, initTestGitRepo(workspace))
	workflows := filepath.Join(source, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflows, 0o755))
	content := "---\non: workflow_dispatch\nrate-limit:\n  max-runs: 5\n  window: 60\n---\n\n# Test workflow\n"
	require.NoError(t, os.WriteFile(filepath.Join(workflows, "test.md"), []byte(content), 0o644))
	for _, args := range [][]string{
		{"-C", source, "add", "."},
		{"-C", source, "-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture"},
	} {
		output, err := exec.Command(gitPath, args...).CombinedOutput()
		require.NoError(t, err, "%s", output)
	}
	script := "#!/bin/sh\nexec \"$GH_AW_TEST_GIT\" clone --no-local --depth=1 \"$GH_AW_TEST_SOURCE\" \"$4\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_AW_TEST_GIT", gitPath)
	t.Setenv("GH_AW_TEST_SOURCE", source)
	chdirForTest(t, workspace)
	outside := filepath.Join(t.TempDir(), "caller")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "test.md"), []byte(content), 0o644))
	t.Setenv("GH_AW_WORKFLOWS_DIR", outside)
	err = runUpgradeForTargetRepo(context.Background(), "owner/repo", upgradeOptions{
		workflowDir: outside,
		noAgent:     true, noMCP: true, noSkill: true, noActions: true, noCompile: true,
	}, false, false)
	require.NoError(t, err)
	upgraded, err := os.ReadFile(filepath.Join(workspace, ".github", "aw", "updates", "owner__repo", ".github", "workflows", "test.md"))
	require.NoError(t, err)
	assert.Contains(t, string(upgraded), "user-rate-limit:")
	original, err := os.ReadFile(filepath.Join(outside, "test.md"))
	require.NoError(t, err)
	assert.Equal(t, content, string(original))
	assert.Equal(t, outside, os.Getenv("GH_AW_WORKFLOWS_DIR"))
}

func TestUpgradeContainerPinsRemainBestEffort(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses shell scripts")
	}
	dir := t.TempDir()
	require.NoError(t, initTestGitRepo(dir))
	chdirForTest(t, dir)
	workflows := filepath.Join(dir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflows, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workflows, "test.lock.yml"),
		[]byte("run: bash download_docker_images.sh ghcr.io/example/agent:v1\n"), 0o644))
	t.Setenv("GH_AW_WORKFLOWS_DIR", "")
	bin := t.TempDir()
	for _, name := range []string{"docker", "crane"} {
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 1\n"), 0o755))
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var output bytes.Buffer
	reporter := &updateJSONLReporter{writer: &output, command: "upgrade"}
	ctx := context.WithValue(context.Background(), updateJSONLKey{}, reporter)
	err := runUpgradeCommand(upgradeOptions{
		ctx: ctx, skipExtensionUpgrade: true, noAgent: true, noMCP: true, noSkill: true, noCompile: true,
	})
	require.NoError(t, err)
	assert.Contains(t, output.String(), "registry metadata lookup failed")
	assert.Contains(t, output.String(), `"containers"`)

	lockFile := filepath.Join(dir, ".github", "aw", "actions-lock.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(lockFile), 0o755))
	require.NoError(t, os.WriteFile(lockFile, []byte("invalid json"), 0o644))
	err = runUpgradeCommand(upgradeOptions{
		ctx: context.Background(), skipExtensionUpgrade: true, noAgent: true, noMCP: true, noSkill: true, noCompile: true,
	})
	require.ErrorContains(t, err, "failed to update container pins")

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = runUpgradeCommand(upgradeOptions{
		ctx: cancelCtx, skipExtensionUpgrade: true, noAgent: true, noMCP: true, noSkill: true, noCompile: true,
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestUpgradeRelaunchFlagsPrecedeTerminator(t *testing.T) {
	original := runUpgradeForTargetRepoFn
	t.Cleanup(func() { runUpgradeForTargetRepoFn = original })
	runUpgradeForTargetRepoFn = func(_ context.Context, _ string, opts upgradeOptions, _ bool, _ bool) error {
		assert.True(t, opts.skipExtensionUpgrade)
		assert.Equal(t, "--", opts.prMessage)
		return nil
	}
	cmd := NewUpgradeCommand(upgradeValidateEngineStub)
	args := upgradeRelaunchArgs(
		[]string{"upgrade", "--repo", "owner/repo", "--pr-message", "--", "--yes", "--jsonl", "--"},
		[]string{"--skip-extension-upgrade", "--jsonl-continuation"},
		cmd.Flags(),
	)
	assert.Equal(t, []string{"upgrade", "--repo", "owner/repo", "--pr-message", "--", "--yes", "--jsonl", "--skip-extension-upgrade", "--jsonl-continuation", "--"}, args)
	cmd.SetArgs(args[1:])
	require.NoError(t, cmd.Execute())
}

func TestUpgradeRelaunchUsesInheritedFlagsAndShorthandClusters(t *testing.T) {
	original := runUpgradeForTargetRepoFn
	t.Cleanup(func() { runUpgradeForTargetRepoFn = original })
	root := &cobra.Command{Use: "gh-aw"}
	root.PersistentFlags().BoolP("verbose", "v", false, "")
	root.PersistentFlags().StringP("config", "c", "", "")
	upgrade := NewUpgradeCommand(upgradeValidateEngineStub)
	root.AddCommand(upgrade)
	runUpgradeForTargetRepoFn = func(_ context.Context, _ string, opts upgradeOptions, _ bool, _ bool) error {
		flags := opts.relaunchFlags
		require.NotNil(t, flags)
		extra := []string{"--skip-extension-upgrade", "--jsonl-continuation"}
		for _, test := range []struct {
			args, want []string
		}{
			{[]string{"upgrade", "--config", "--", "--"}, []string{"upgrade", "--config", "--", "--skip-extension-upgrade", "--jsonl-continuation", "--"}},
			{[]string{"upgrade", "-vc", "--", "--"}, []string{"upgrade", "-vc", "--", "--skip-extension-upgrade", "--jsonl-continuation", "--"}},
			{[]string{"upgrade", "-vcvalue", "--"}, []string{"upgrade", "-vcvalue", "--skip-extension-upgrade", "--jsonl-continuation", "--"}},
		} {
			assert.Equal(t, test.want, upgradeRelaunchArgs(test.args, extra, flags))
		}
		return nil
	}
	root.SetArgs([]string{"upgrade", "--repo", "owner/repo", "--skip-extension-upgrade"})
	require.NoError(t, root.Execute())
}
