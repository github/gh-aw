//go:build !integration

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDeployCommand_BasicShape(t *testing.T) {
	cmd := NewDeployCommand(func(string) error { return nil })
	require.NotNil(t, cmd)
	assert.Equal(t, "deploy <workflow>...", cmd.Use)
	assert.Equal(t, "deploy", cmd.Name())
}

func TestNewDeployCommand_RequiresWorkflowArg(t *testing.T) {
	cmd := NewDeployCommand(func(string) error { return nil })
	require.NotNil(t, cmd)

	err := cmd.Args(cmd, []string{})
	require.Error(t, err)
	require.ErrorContains(t, err, "missing workflow specification")
}

func TestNewDeployCommand_RegistersCoreFlags(t *testing.T) {
	cmd := NewDeployCommand(func(string) error { return nil })
	require.NotNil(t, cmd)

	expectedFlags := []string{
		"repo",
		"org",
		"repos",
		"yes",
		"name",
		"engine",
		"force",
		"auto-merge",
		"create-pull-request",
		"pr",
		"append",
		"no-gitattributes",
		"dir",
		"no-stop-after",
		"stop-after",
		"no-security-scanner",
		"cool-down",
	}

	for _, flagName := range expectedFlags {
		t.Run(flagName, func(t *testing.T) {
			flag := cmd.Flags().Lookup(flagName)
			require.NotNil(t, flag, "expected flag %q to be registered", flagName)
		})
	}

	repoFlag := cmd.Flags().Lookup("repo")
	require.NotNil(t, repoFlag, "expected --repo flag to be registered")
	assert.Contains(t, repoFlag.Usage, "Required unless --org is provided")
}

func TestNewDeployCommand_CoolDownFlagUsageMatchesUpdate(t *testing.T) {
	cmd := NewDeployCommand(func(string) error { return nil })
	require.NotNil(t, cmd)

	coolDownFlag := cmd.Flags().Lookup("cool-down")
	require.NotNil(t, coolDownFlag)
	assert.Equal(t, coolDownFlagUsage, coolDownFlag.Usage)
}

func TestNewDeployCommand_RequiresRepoFlag(t *testing.T) {
	cmd := NewDeployCommand(func(string) error { return nil })
	require.NotNil(t, cmd)
	err := runDeployCommand(cmd, []string{"githubnext/agentics/ci-doctor"}, func(string) error { return nil })
	require.Error(t, err)
	require.ErrorContains(t, err, "either --repo (owner/repo) or --org must be provided")
}

func TestRunDeployCommand_RejectsRepoAndOrgTogether(t *testing.T) {
	cmd := NewDeployCommand(func(string) error { return nil })
	require.NotNil(t, cmd)
	require.NoError(t, cmd.Flags().Set("repo", "octo/repo"))
	require.NoError(t, cmd.Flags().Set("org", "octo"))
	err := runDeployCommand(cmd, []string{"githubnext/agentics/ci-doctor"}, func(string) error { return nil })
	require.Error(t, err)
	require.ErrorContains(t, err, "cannot specify both --repo and --org")
}

func TestRunDeployCommand_RequiresOrgWhenReposProvided(t *testing.T) {
	cmd := NewDeployCommand(func(string) error { return nil })
	require.NotNil(t, cmd)
	require.NoError(t, cmd.Flags().Set("repos", "api-*"))

	err := runDeployCommand(cmd, []string{"githubnext/agentics/ci-doctor"}, func(string) error { return nil })
	require.Error(t, err)
	require.ErrorContains(t, err, "--repos requires --org")
}

func TestRunDeployCommand_RoutesToOrgRunner(t *testing.T) {
	cmd := NewDeployCommand(func(string) error { return nil })
	require.NotNil(t, cmd)
	require.NoError(t, cmd.Flags().Set("org", "octo"))
	require.NoError(t, cmd.Flags().Set("repos", "api-*"))
	require.NoError(t, cmd.Flags().Set("yes", "true"))
	require.NoError(t, cmd.Flags().Set("cool-down", "24h"))
	require.NoError(t, cmd.Flags().Set("auto-merge", "true"))
	require.NoError(t, cmd.Flags().Set("pr", "true"))

	origRunDeployForOrg := runDeployForOrgFn
	origRunDeploy := runDeployFn
	defer func() {
		runDeployForOrgFn = origRunDeployForOrg
		runDeployFn = origRunDeploy
	}()

	calledOrg := false
	runDeployForOrgFn = func(ctx context.Context, org string, repoGlobs []string, workflows []string, addOpts AddOptions, coolDown time.Duration, yes bool, verbose bool) error {
		calledOrg = true
		assert.Equal(t, "octo", org)
		assert.Equal(t, []string{"api-*"}, repoGlobs)
		assert.Equal(t, []string{"githubnext/agentics/ci-doctor"}, workflows)
		assert.True(t, yes)
		assert.Equal(t, 24*time.Hour, coolDown)
		assert.True(t, addOpts.AutoMerge)
		return nil
	}
	runDeployFn = func(context.Context, string, []string, AddOptions, time.Duration) error {
		t.Fatalf("single-repo deploy runner should not be called in --org mode")
		return nil
	}

	err := runDeployCommand(cmd, []string{"githubnext/agentics/ci-doctor"}, func(string) error { return nil })
	require.NoError(t, err)
	assert.True(t, calledOrg)
}

func TestRunDeployCommandAutoMergeRequiresPR(t *testing.T) {
	cmd := NewDeployCommand(validateEngineStub)
	require.NoError(t, cmd.Flags().Set("auto-merge", "true"))
	err := cmd.RunE(cmd, []string{"owner/source/workflow"})
	require.ErrorContains(t, err, "--auto-merge requires --create-pull-request")
}

func TestRunDeployCommandAutoMergePropagation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		prFlag    string
		autoMerge bool
	}{
		{name: "default"},
		{name: "short PR alias", prFlag: "pr", autoMerge: true},
		{name: "long PR alias", prFlag: "create-pull-request", autoMerge: true},
		{name: "PR alias without auto-merge", prFlag: "pr"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewDeployCommand(validateEngineStub)
			require.NoError(t, cmd.Flags().Set("repo", "octo/repo"))
			if tc.prFlag != "" {
				require.NoError(t, cmd.Flags().Set(tc.prFlag, "true"))
			}
			if tc.autoMerge {
				require.NoError(t, cmd.Flags().Set("auto-merge", "true"))
			}
			assert.Contains(t, cmd.Long, "one is required with --auto-merge")
			flag := cmd.Flags().Lookup("auto-merge")
			require.NotNil(t, flag)
			assert.Equal(t, "false", flag.DefValue)

			originalRunDeploy := runDeployFn
			t.Cleanup(func() { runDeployFn = originalRunDeploy })
			called := false
			runDeployFn = func(_ context.Context, repo string, workflows []string, opts AddOptions, _ time.Duration) error {
				called = true
				assert.Equal(t, "octo/repo", repo)
				assert.Equal(t, []string{"owner/source/workflow"}, workflows)
				assert.Equal(t, tc.autoMerge, opts.AutoMerge)
				assert.False(t, opts.CreatePR, "the internal add pass must not create a separate PR")
				return nil
			}
			require.NoError(t, cmd.RunE(cmd, []string{"owner/source/workflow"}))
			assert.True(t, called)
		})
	}
}

func TestCreateDeployPRAutoMergePropagation(t *testing.T) {
	for _, autoMerge := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "enabled"}[autoMerge], func(t *testing.T) {
			repoDir := initPRTransferRepo(t)
			chdirForTest(t, repoDir)
			require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("deploy change\n"), 0o600))

			originalCreatePR := createPRRunGHContextWithHost
			originalAutoMerge := autoMergeRunGH
			t.Cleanup(func() {
				createPRRunGHContextWithHost = originalCreatePR
				autoMergeRunGH = originalAutoMerge
			})
			created := false
			createPRRunGHContextWithHost = func(_ context.Context, _, _ string, args ...string) ([]byte, error) {
				if args[0] == "repo" {
					return []byte(`{"owner":{"login":"octo"},"name":"repo"}`), nil
				}
				assert.Contains(t, args, deployCommitMessage)
				assert.Contains(t, args, "--body-file")
				for i, arg := range args {
					if arg == "--body-file" {
						require.Less(t, i+1, len(args))
						body, err := os.ReadFile(args[i+1])
						require.NoError(t, err)
						assert.Contains(t, string(body), "Deployment details.\n\nCustom deployment message.")
					}
				}
				created = true
				return []byte("https://github.com/octo/repo/pull/42"), nil
			}
			autoMergeCalls := 0
			enableErr := errors.New("auto-merge unavailable")
			autoMergeRunGH = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
				assert.True(t, created, "auto-merge must happen after PR creation")
				autoMergeCalls++
				return nil, enableErr
			}
			err := createDeployPR(context.Background(), []string{"owner/source/workflow"}, "octo/repo", false, autoMerge, "Deployment details.", "Custom deployment message.")
			if autoMerge {
				require.ErrorIs(t, err, enableErr)
				require.ErrorContains(t, err, "https://github.com/octo/repo/pull/42")
				assert.Equal(t, 1, autoMergeCalls)
			} else {
				require.NoError(t, err)
				assert.Zero(t, autoMergeCalls)
			}
			assert.True(t, created)
			assert.Equal(t, "main", strings.TrimSpace(gitOutput(t, repoDir, "branch", "--show-current")))
		})
	}
}

func TestBuildDeployPRMetadata_SingleWorkflow(t *testing.T) {
	title, body := buildDeployPRMetadata([]string{"githubnext/agentics/ci-doctor"}, "owner/repo")
	assert.Equal(t, deployCommitMessage, title)
	assert.Equal(t, "## Summary\n\nDeploy ci-doctor to owner/repo.\n\n## Workflows requested\n\n- `githubnext/agentics/ci-doctor`\n", body)
}

func TestBuildDeployPRMetadata_MultipleWorkflows(t *testing.T) {
	title, body := buildDeployPRMetadata([]string{"a", "b", "c"}, "owner/repo")
	assert.Equal(t, deployCommitMessage, title)
	assert.Contains(t, body, "Deploy 3 workflows to owner/repo.")
}

func TestBuildDeployPRMetadata_LocalPathsArePrivate(t *testing.T) {
	t.Parallel()

	baseDir := filepath.Join(string(filepath.Separator), "Users", "private-user", "private-project")
	workflows := resolveDeployWorkflowSpecs(
		[]string{"./my-workflow.md", filepath.Join(baseDir, "absolute-workflow.md"), "./*.md", "owner/source/remote.md@v1", "https://example.com/workflows/remote.md"},
		baseDir,
	)
	_, body := buildDeployPRMetadata(workflows, "owner/repo")

	assert.Contains(t, body, "- `my-workflow.md`\n")
	assert.Contains(t, body, "- `absolute-workflow.md`\n")
	assert.Contains(t, body, "- `*.md`\n")
	assert.Contains(t, body, "- `owner/source/remote.md@v1`\n")
	assert.Contains(t, body, "- `https://example.com/workflows/remote.md`\n")
	assert.NotContains(t, body, baseDir)
	assert.NotContains(t, body, "private-user")
	assert.NotContains(t, body, "private-project")

	_, singleBody := buildDeployPRMetadata(workflows[:1], "owner/repo")
	assert.Contains(t, singleBody, "Deploy my-workflow to owner/repo.")
	assert.NotContains(t, singleBody, baseDir)
}

func TestExcludeExistingSourcedWorkflows_SkipsExistingSourcedWorkflow(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "ci-doctor.md"), []byte(`---
source: githubnext/agentics/ci-doctor.md@v1
---

# Existing
`), 0o644))

	toAdd, skipped, err := excludeExistingSourcedWorkflows([]string{"githubnext/agentics/ci-doctor"}, AddOptions{WorkflowDir: workflowsDir})
	require.NoError(t, err)
	assert.Empty(t, toAdd)
	assert.Equal(t, []string{"ci-doctor"}, skipped)
}

func TestExcludeExistingSourcedWorkflows_LeavesExistingNonSourcedWorkflowForAdd(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "ci-doctor.md"), []byte(`---
name: CI Doctor
---

# Existing
`), 0o644))

	toAdd, skipped, err := excludeExistingSourcedWorkflows([]string{"githubnext/agentics/ci-doctor"}, AddOptions{WorkflowDir: workflowsDir})
	require.NoError(t, err)
	assert.Equal(t, []string{"githubnext/agentics/ci-doctor"}, toAdd)
	assert.Empty(t, skipped)
}

func TestExcludeExistingSourcedWorkflows_UsesNameFlagForSingleWorkflow(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "custom-name.md"), []byte(`---
source: githubnext/agentics/ci-doctor.md@v1
---

# Existing
`), 0o644))

	toAdd, skipped, err := excludeExistingSourcedWorkflows(
		[]string{"githubnext/agentics/ci-doctor"},
		AddOptions{WorkflowDir: workflowsDir, Name: "custom-name"},
	)
	require.NoError(t, err)
	assert.Empty(t, toAdd)
	assert.Equal(t, []string{"custom-name"}, skipped)
}

func TestExcludeExistingSourcedWorkflows_MalformedFrontmatterNotSkipped(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "ci-doctor.md"), []byte(`---
source: [unterminated
---

# Existing
`), 0o644))

	toAdd, skipped, err := excludeExistingSourcedWorkflows([]string{"githubnext/agentics/ci-doctor"}, AddOptions{WorkflowDir: workflowsDir})
	require.NoError(t, err)
	assert.Equal(t, []string{"githubnext/agentics/ci-doctor"}, toAdd)
	assert.Empty(t, skipped)
}

func TestResolveDeployWorkflowSpecs_ResolvesRelativeLocalPathsAgainstOriginalDirectory(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	absoluteWorkflow := filepath.Join(baseDir, "absolute-workflow.md")
	workflows := resolveDeployWorkflowSpecs(
		[]string{"./my-workflow.md", absoluteWorkflow, "githubnext/agentics/ci-doctor"},
		baseDir,
	)
	require.Len(t, workflows, 3)

	assert.Equal(t, filepath.Join(baseDir, "my-workflow.md"), workflows[0])
	assert.Equal(t, absoluteWorkflow, workflows[1])
	assert.Equal(t, "githubnext/agentics/ci-doctor", workflows[2])
}

func TestResolveDeployWorkflowSpecs_ResolvesRelativeWildcardLocalPaths(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	workflows := resolveDeployWorkflowSpecs([]string{"./*.md"}, baseDir)
	require.Len(t, workflows, 1)

	assert.Equal(t, filepath.Join(baseDir, "*.md"), workflows[0])
}

func TestParseDeployCommandOptions_NameFlagWithMultipleWorkflows(t *testing.T) {
	t.Parallel()

	cmd := NewDeployCommand(func(string) error { return nil })
	require.NotNil(t, cmd)
	require.NoError(t, cmd.Flags().Set("name", "custom-workflow"))

	validateEngineCalled := false
	opts, coolDown, err := parseDeployCommandOptions(cmd, []string{"a", "b"}, func(string) error {
		validateEngineCalled = true
		return nil
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "--name flag cannot be used when adding multiple workflows at once")
	assert.Equal(t, AddOptions{}, opts)
	assert.Zero(t, coolDown)
	assert.False(t, validateEngineCalled)
}

func TestParseDeployCommandOptions_InvalidCoolDown(t *testing.T) {
	t.Parallel()

	cmd := NewDeployCommand(func(string) error { return nil })
	require.NotNil(t, cmd)
	require.NoError(t, cmd.Flags().Set("cool-down", "not-a-duration"))

	opts, coolDown, err := parseDeployCommandOptions(cmd, []string{"a"}, func(string) error { return nil })
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid --cool-down value")
	assert.Equal(t, AddOptions{}, opts)
	assert.Zero(t, coolDown)
}

func TestParseDeployCommandOptions_EngineValidationError(t *testing.T) {
	t.Parallel()

	cmd := NewDeployCommand(func(string) error { return nil })
	require.NotNil(t, cmd)
	require.NoError(t, cmd.Flags().Set("engine", "custom-engine"))

	var validatedEngine string
	expectedErr := errors.New("engine invalid")
	opts, coolDown, err := parseDeployCommandOptions(cmd, []string{"a"}, func(engine string) error {
		validatedEngine = engine
		return expectedErr
	})
	require.Error(t, err)
	require.ErrorIs(t, err, expectedErr)
	assert.Equal(t, "custom-engine", validatedEngine)
	assert.Equal(t, AddOptions{}, opts)
	assert.Zero(t, coolDown)
}
