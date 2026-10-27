//go:build !integration

package cli

import (
	"context"
	"fmt"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateUpgradeAutoMergeFlag(t *testing.T) {
	t.Parallel()

	for _, newCommand := range []func(func(string) error) *cobra.Command{NewUpdateCommand, NewUpgradeCommand} {
		cmd := newCommand(func(string) error { return nil })
		t.Run(cmd.Name(), func(t *testing.T) {
			flag := cmd.Flags().Lookup("auto-merge")
			require.NotNil(t, flag)
			assert.Equal(t, "false", flag.DefValue)
			assert.Contains(t, flag.Usage, "--pr or --create-pull-request")
			assert.Contains(t, cmd.Example, "--pr --auto-merge")
			assert.Contains(t, cmd.Example, "--org my-org --pr --auto-merge --yes")
		})
	}
}

func TestUpdateUpgradeAutoMergeRequiresPREarly(t *testing.T) {
	t.Parallel()

	for _, newCommand := range []func(func(string) error) *cobra.Command{NewUpdateCommand, NewUpgradeCommand} {
		name := newCommand(func(string) error { return nil }).Name()
		t.Run(name, func(t *testing.T) {
			scenarios := [][]string{
				{"--auto-merge"},
				{"--auto-merge", "--repo", "owner/repo"},
				{"--auto-merge", "--org", "my-org"},
				{"--auto-merge", "--org", "my-org", "--create-issue"},
				{"--auto-merge", "--pr=false"},
				{"--auto-merge", "--create-pull-request=false"},
			}
			if name == "upgrade" {
				scenarios = append(scenarios, []string{"--auto-merge", "--audit"})
			}
			for _, args := range scenarios {
				t.Run(args[len(args)-1], func(t *testing.T) {
					validated := false
					cmd := newCommand(func(string) error {
						validated = true
						return assert.AnError
					})
					cmd.SetArgs(args)
					require.EqualError(t, cmd.Execute(), "--auto-merge requires --pr or --create-pull-request")
					assert.False(t, validated, "invalid auto-merge usage must fail before engine validation or any dispatch")
				})
			}
		})
	}
}

func TestUpdateUpgradeAutoMergeAcceptsPRAliases(t *testing.T) {
	t.Parallel()

	for _, newCommand := range []func(func(string) error) *cobra.Command{NewUpdateCommand, NewUpgradeCommand} {
		name := newCommand(func(string) error { return nil }).Name()
		t.Run(name, func(t *testing.T) {
			for _, prFlag := range []string{"--pr", "--create-pull-request"} {
				t.Run(prFlag, func(t *testing.T) {
					cmd := newCommand(func(string) error { return assert.AnError })
					cmd.SetArgs([]string{prFlag, "--auto-merge"})
					require.ErrorIs(t, cmd.Execute(), assert.AnError, "valid auto-merge usage should proceed to engine validation")
				})
			}
		})
	}
}

func TestUpdateUpgradeAutoMergeTargetRepoDispatch(t *testing.T) {
	originalUpdate := runUpdateForTargetRepoFn
	originalUpgrade := runUpgradeForTargetRepoFn
	t.Cleanup(func() {
		runUpdateForTargetRepoFn = originalUpdate
		runUpgradeForTargetRepoFn = originalUpgrade
	})

	for _, newCommand := range []func(func(string) error) *cobra.Command{NewUpdateCommand, NewUpgradeCommand} {
		name := newCommand(func(string) error { return nil }).Name()
		t.Run(name, func(t *testing.T) {
			for _, prFlag := range []string{"--pr", "--create-pull-request"} {
				t.Run(prFlag, func(t *testing.T) {
					for _, autoMerge := range []bool{false, true} {
						called := false
						check := func(repo string, createPR, gotAutoMerge bool) error {
							called = true
							assert.Equal(t, "owner/repo", repo)
							assert.True(t, createPR)
							assert.Equal(t, autoMerge, gotAutoMerge)
							return nil
						}
						runUpdateForTargetRepoFn = func(_ context.Context, repo string, opts UpdateWorkflowsOptions, createPR, _ bool) error {
							return check(repo, createPR, opts.AutoMerge)
						}
						runUpgradeForTargetRepoFn = func(_ context.Context, repo string, opts upgradeOptions, createPR, _ bool) error {
							return check(repo, createPR, opts.autoMerge)
						}
						args := []string{"--repo", "owner/repo", prFlag}
						if autoMerge {
							args = append(args, "--auto-merge")
						}
						cmd := newCommand(func(string) error { return nil })
						cmd.SetArgs(args)
						require.NoError(t, cmd.Execute())
						assert.True(t, called)
					}
				})
			}
		})
	}
}

func TestUpdateUpgradeAutoMergeOrgDispatchErrors(t *testing.T) {
	originalWait := waitForOrgRateLimitFn
	t.Cleanup(func() {
		waitForOrgRateLimitFn = originalWait
	})
	waitForOrgRateLimitFn = func(context.Context, string, bool) error { return nil }

	for _, newCommand := range []func(func(string) error) *cobra.Command{NewUpdateCommand, NewUpgradeCommand} {
		name := newCommand(func(string) error { return nil }).Name()
		t.Run(name, func(t *testing.T) {
			for _, prFlag := range []string{"--pr", "--create-pull-request"} {
				t.Run(prFlag, func(t *testing.T) {
					cmd := newCommand(func(string) error { return nil })
					require.NoError(t, cmd.ParseFlags([]string{"--org", "octo", prFlag, "--auto-merge", "--yes"}))
					workerArgs := orgRolloutWorkerArgs(cmd)
					assert.Contains(t, workerArgs, "--auto-merge=true")

					for _, allFail := range []bool{false, true} {
						t.Run(fmt.Sprintf("all-fail=%t", allFail), func(t *testing.T) {
							var called []string
							opts := orgRolloutOptions{command: name, concurrency: 1, args: workerArgs}
							var results []orgRolloutResult
							var runErr error
							output := captureUpdateOrgStderr(t, func() {
								results, runErr = runOrgRolloutPlain(context.Background(), []orgRolloutRow{
									{repo: "octo/api", selected: true},
									{repo: "octo/web", selected: true},
								}, opts, true, func(_ context.Context, repo string, workerOpts orgRolloutOptions) orgRolloutResult {
									assert.Equal(t, workerArgs, workerOpts.args)
									called = append(called, repo)
									result := orgRolloutResult{Status: "created", URL: "https://github.com/" + repo + "/pull/1"}
									if allFail || repo == "octo/api" {
										result.Status = "failed"
										result.Error = fmt.Sprintf("auto-merge failed for https://github.com/%s/pull/1; pull request remains open", repo)
									}
									return result
								})
							})
							require.NoError(t, runErr)
							err := reportOrgRolloutResults(context.Background(), results, orgRunCallbacks{}, opts, runErr)
							require.ErrorContains(t, err, "auto-merge failed")
							assert.Contains(t, err.Error(), "remains open")
							assert.Contains(t, output, "https://github.com/octo/api/pull/1")
							assert.Contains(t, output, "https://github.com/octo/web/pull/1")
							if allFail {
								assert.Contains(t, err.Error(), "octo/web")
							} else {
								assert.NotContains(t, err.Error(), "octo/web")
							}
							assert.ElementsMatch(t, []string{"octo/api", "octo/web"}, called)
						})
					}
				})
			}
		})
	}
}
