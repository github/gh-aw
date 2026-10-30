//go:build !integration

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDoctorCommand(t *testing.T) {
	t.Parallel()
	cmd := NewDoctorCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "doctor", cmd.Use)
	assert.Equal(t, "Run diagnostics to verify CLI authentication and repository setup", cmd.Short)
	assert.False(t, cmd.Hidden)
	assert.NotNil(t, cmd.Flags().Lookup("json"), "should expose --json flag")
	assert.NotNil(t, cmd.Flags().Lookup("repo"), "should expose --repo flag")
	assert.NotNil(t, cmd.Flags().Lookup("dir"), "should expose --dir flag")
	assert.NotNil(t, cmd.Flags().Lookup("require-owner-type"), "should expose --require-owner-type flag")
	assert.False(t, cmd.HasSubCommands())
	assert.Contains(t, cmd.Flags().Lookup("repo").Usage, "owner/repo format only", "doctor --repo help should explicitly document its narrower repo format")
}

func TestDoctorCommandUsesNoArgs(t *testing.T) {
	t.Parallel()
	cmd := NewDoctorCommand()
	require.NotNil(t, cmd.Args)
	require.NoError(t, cmd.Args(cmd, []string{}))
	assert.Error(t, cmd.Args(cmd, []string{"extra"}))
}

func TestDoctorCommandAdvertisesJSONExample(t *testing.T) {
	t.Parallel()
	cmd := NewDoctorCommand()
	assert.Contains(t, cmd.Example, "gh aw doctor --json")
	assert.Contains(t, cmd.Example, "gh aw doctor --repo github/gh-aw --json")
}

func TestDoctorCommandLongMentionsEnterpriseHostFallback(t *testing.T) {
	t.Parallel()
	cmd := NewDoctorCommand()
	assert.Contains(t, cmd.Long, "auto-detects the host from the git remote")
	assert.Contains(t, cmd.Long, "gh auth login --hostname <host>")
}

func TestDoctorCommandExampleHasNoTabs(t *testing.T) {
	t.Parallel()
	cmd := NewDoctorCommand()
	assert.NotContains(t, cmd.Example, "\t")
}

func TestDoctorCommandRequireOwnerTypeDefault(t *testing.T) {
	t.Parallel()
	cmd := NewDoctorCommand()
	require.Equal(t, "any", cmd.Flags().Lookup("require-owner-type").DefValue)
}

func TestDoctorCommandInheritsVerboseFlagFromRoot(t *testing.T) {
	t.Parallel()
	cmd := NewDoctorCommand()
	assert.Nil(t, cmd.Flags().Lookup("verbose"))

	root := &cobra.Command{Use: "aw"}
	var verbose bool
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output showing detailed information")
	root.AddCommand(cmd)

	inherited := cmd.InheritedFlags().Lookup("verbose")
	require.NotNil(t, inherited)
	assert.Equal(t, "false", inherited.DefValue)
}

func TestDoctorCommandRunsAuthOnlyWhenRepoNotProvided(t *testing.T) {
	origAuth := runDoctorSetupAuth
	origRepo := runDoctorSetupRepositoryCheck
	t.Cleanup(func() {
		runDoctorSetupAuth = origAuth
		runDoctorSetupRepositoryCheck = origRepo
	})

	calledAuth := false
	var gotJSON bool
	runDoctorSetupAuth = func(opts SetupAuthOptions) error {
		calledAuth = true
		gotJSON = opts.JSON
		assert.True(t, opts.CheckAgentAssignment)
		return nil
	}
	runDoctorSetupRepositoryCheck = func(opts SetupRepositoryCheckOptions) error {
		return errors.New("unexpected repository check")
	}

	cmd := NewDoctorCommand()
	cmd.SetArgs([]string{"--json"})
	require.NoError(t, cmd.Execute())
	assert.True(t, calledAuth)
	assert.True(t, gotJSON)
}

func TestDoctorCommandRunsRepositoryCheckWhenRepoProvided(t *testing.T) {
	origAuth := runDoctorSetupAuth
	origRepo := runDoctorSetupRepositoryCheck
	t.Cleanup(func() {
		runDoctorSetupAuth = origAuth
		runDoctorSetupRepositoryCheck = origRepo
	})

	var got SetupRepositoryCheckOptions
	runDoctorSetupAuth = func(opts SetupAuthOptions) error {
		return errors.New("unexpected auth-only check")
	}
	runDoctorSetupRepositoryCheck = func(opts SetupRepositoryCheckOptions) error {
		got = opts
		return nil
	}

	root := &cobra.Command{Use: "aw"}
	var verbose bool
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output showing detailed information")
	root.AddCommand(NewDoctorCommand())
	root.SetArgs([]string{"doctor", "--repo", "github/gh-aw", "--dir", ".", "--require-owner-type", "org", "--json", "--verbose"})

	require.NoError(t, root.Execute())
	assert.Equal(t, "github/gh-aw", got.Repo)
	assert.Equal(t, ".", got.Dir)
	assert.Equal(t, "org", got.RequireOwnerType)
	assert.True(t, got.JSON)
	assert.True(t, got.Verbose)
	assert.True(t, got.CheckAgentAssignment)
}

func TestDoctorCommandUsesDefaultRequireOwnerTypeWhenOmitted(t *testing.T) {
	origAuth := runDoctorSetupAuth
	origRepo := runDoctorSetupRepositoryCheck
	t.Cleanup(func() {
		runDoctorSetupAuth = origAuth
		runDoctorSetupRepositoryCheck = origRepo
	})

	var got SetupRepositoryCheckOptions
	runDoctorSetupAuth = func(opts SetupAuthOptions) error {
		return errors.New("unexpected auth-only check")
	}
	runDoctorSetupRepositoryCheck = func(opts SetupRepositoryCheckOptions) error {
		got = opts
		return nil
	}

	root := &cobra.Command{Use: "aw"}
	root.AddCommand(NewDoctorCommand())
	root.SetArgs([]string{"doctor", "--repo", "github/gh-aw"})

	require.NoError(t, root.Execute())
	assert.Equal(t, "any", got.RequireOwnerType)
}

func TestDoctorCommandRejectsRepoOnlyFlagsWithoutRepo(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "dir only", args: []string{"doctor", "--dir", "."}},
		{name: "require-owner-type only", args: []string{"doctor", "--require-owner-type", "org"}},
		{name: "dir and require-owner-type", args: []string{"doctor", "--dir", ".", "--require-owner-type", "org"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origAuth := runDoctorSetupAuth
			origRepo := runDoctorSetupRepositoryCheck
			t.Cleanup(func() {
				runDoctorSetupAuth = origAuth
				runDoctorSetupRepositoryCheck = origRepo
			})

			runDoctorSetupAuth = func(opts SetupAuthOptions) error {
				return errors.New("should not run auth path with invalid flag combination")
			}
			runDoctorSetupRepositoryCheck = func(opts SetupRepositoryCheckOptions) error {
				return errors.New("should not run repo path without --repo")
			}

			root := &cobra.Command{Use: "aw"}
			var verbose bool
			root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output showing detailed information")
			root.AddCommand(NewDoctorCommand())
			root.SetArgs(tc.args)

			err := root.Execute()
			require.Error(t, err)
			assert.Equal(t, "--dir and --require-owner-type require --repo", err.Error())
		})
	}
}

func TestDoctorCommandAllowsVerboseWithoutRepo(t *testing.T) {
	origAuth := runDoctorSetupAuth
	origRepo := runDoctorSetupRepositoryCheck
	t.Cleanup(func() {
		runDoctorSetupAuth = origAuth
		runDoctorSetupRepositoryCheck = origRepo
	})

	authCalled := false
	runDoctorSetupAuth = func(opts SetupAuthOptions) error {
		authCalled = true
		return nil
	}
	runDoctorSetupRepositoryCheck = func(opts SetupRepositoryCheckOptions) error {
		return errors.New("should not run repo path without --repo")
	}

	root := &cobra.Command{Use: "aw"}
	var verbose bool
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output showing detailed information")
	root.AddCommand(NewDoctorCommand())
	root.SetArgs([]string{"doctor", "--verbose"})

	require.NoError(t, root.Execute())
	assert.True(t, authCalled)
	assert.True(t, verbose)
}

func TestRunSetupAuthAutoDetectsDefaultGHHost(t *testing.T) {
	workflow.SetDefaultGHHost("")
	t.Cleanup(func() { workflow.SetDefaultGHHost("") })
	if prev, ok := os.LookupEnv("GH_HOST"); ok {
		t.Cleanup(func() { _ = os.Setenv("GH_HOST", prev) })
	} else {
		t.Cleanup(func() { _ = os.Unsetenv("GH_HOST") })
	}
	require.NoError(t, os.Unsetenv("GH_HOST"))

	tempDir := testutil.TempDir(t, "doctor-gh-host-*")
	require.NoError(t, initTestGitRepo(tempDir))
	require.NoError(t, addOriginRemoteToTestRepo(tempDir, "https://ghes.example.com/owner/repo.git"))
	t.Chdir(tempDir)

	runtime := setupRepositoryRuntime{
		checkAuth: func(context.Context) error {
			assert.Equal(t, "ghes.example.com", getGHHostFromCommandEnv(workflow.ExecGH("auth", "status")))
			return nil
		},
	}

	require.NoError(t, runSetupAuthWithRuntime(SetupAuthOptions{}, runtime))
}

func TestRunSetupRepositoryCheckAutoDetectsDefaultGHHost(t *testing.T) {
	workflow.SetDefaultGHHost("")
	t.Cleanup(func() { workflow.SetDefaultGHHost("") })
	if prev, ok := os.LookupEnv("GH_HOST"); ok {
		t.Cleanup(func() { _ = os.Setenv("GH_HOST", prev) })
	} else {
		t.Cleanup(func() { _ = os.Unsetenv("GH_HOST") })
	}
	require.NoError(t, os.Unsetenv("GH_HOST"))

	tempDir := testutil.TempDir(t, "doctor-repo-gh-host-*")
	require.NoError(t, initTestGitRepo(tempDir))
	require.NoError(t, addOriginRemoteToTestRepo(tempDir, "git@ghes.example.com:owner/repo.git"))
	t.Chdir(tempDir)

	checkoutDir := filepath.Join(tempDir, "checkout")
	require.NoError(t, os.MkdirAll(checkoutDir, 0755))

	runtime := setupRepositoryRuntime{
		checkAuth: func(context.Context) error {
			assert.Equal(t, "ghes.example.com", getGHHostFromCommandEnv(workflow.ExecGH("auth", "status")))
			return nil
		},
		repoExists: func(context.Context, string) (bool, error) { return true, nil },
		ownerType:  func(context.Context, string) (string, error) { return "Organization", nil },
		dirOriginRepo: func(string) (string, error) {
			return "owner/repo", nil
		},
		checkCleanWorktree: func(bool) error { return nil },
	}

	err := runSetupRepositoryCheckWithRuntime(SetupRepositoryCheckOptions{
		Repo:             "owner/repo",
		Dir:              checkoutDir,
		RequireOwnerType: "any",
	}, runtime)
	require.Error(t, err)
	require.ErrorContains(t, err, "git checkout")
	assert.Equal(t, "ghes.example.com", getGHHostFromCommandEnv(workflow.ExecGH("auth", "status")))
}
