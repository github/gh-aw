//go:build !integration

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrgRolloutFlags(t *testing.T) {
	for _, newCommand := range []func(func(string) error) *cobra.Command{NewUpdateCommand, NewUpgradeCommand} {
		for _, test := range []struct {
			args []string
			want string
		}{
			{[]string{"--concurrency=0", "--org=acme"}, "between 1 and 10"},
			{[]string{"--concurrency=11", "--org=acme"}, "between 1 and 10"},
			{[]string{"--enterprise=acme", "--org=acme"}, "cannot be combined"},
			{[]string{"--enterprise=acme", "--repo=acme/repo"}, "cannot be combined"},
			{[]string{"--enterprise=bad slug"}, "invalid --enterprise"},
			{[]string{"--org="}, "--org cannot be empty"},
			{[]string{"--enterprise="}, "--enterprise cannot be empty"},
			{[]string{"--dry-run"}, "require --org or --enterprise"},
			{[]string{"--replace-existing", "--org=acme"}, "requires --create-pull-request"},
			{[]string{"--rollout-worker", "--org=acme", "--pr"}, "--rollout-worker requires"},
		} {
			t.Run(strings.Join(test.args, " "), func(t *testing.T) {
				cmd := newCommand(func(string) error { return nil })
				cmd.SetArgs(test.args)
				cmd.SilenceUsage, cmd.SilenceErrors = true, true
				err := cmd.Execute()
				require.ErrorContains(t, err, test.want)
			})
		}
	}
}

func TestOrgRolloutWorkerArguments(t *testing.T) {
	cmd := NewUpdateCommand(func(string) error { return nil })
	require.NoError(t, cmd.ParseFlags([]string{"triage", "--org=acme", "--pr", "--dry-run", "--concurrency=4",
		"--major", "--force", "--pr-message=Quarterly refresh", "--repos=*-service"}))
	cmd.SetContext(context.Background())
	org, err := configureOrgRollout(cmd, "acme", "", true, false)
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	opts := orgRolloutFromContext(cmd.Context())
	assert.Equal(t, []string{"triage"}, opts.workflows)
	assert.Contains(t, opts.args, "--major=true")
	assert.Contains(t, opts.args, "--force=true")
	assert.Contains(t, opts.args, "--pr-message=Quarterly refresh")
	for _, arg := range opts.args {
		assert.NotContains(t, arg, "--org=")
		assert.NotContains(t, arg, "--repos=")
		assert.NotContains(t, arg, "--concurrency=")
		assert.NotContains(t, arg, "--dry-run")
		assert.NotContains(t, arg, "--pr=true")
	}

	upgrade := NewUpgradeCommand(func(string) error { return nil })
	require.NoError(t, upgrade.ParseFlags([]string{"--org=acme", "--disable-codemod=one,two", "--no-fix"}))
	args := orgRolloutWorkerArgs(upgrade)
	assert.Contains(t, args, "--disable-codemod=one,two")
	assert.Contains(t, args, "--skip-extension-upgrade")
}

func stubRolloutBudget(t *testing.T, check func(context.Context, string, bool) error) {
	t.Helper()
	original := waitForOrgRateLimitFn
	waitForOrgRateLimitFn = check
	t.Cleanup(func() { waitForOrgRateLimitFn = original })
}

func TestOrgRolloutConcurrentWorkers(t *testing.T) {
	budgetChecks := 0
	stubRolloutBudget(t, func(context.Context, string, bool) error {
		budgetChecks++
		return nil
	})
	rows := []orgRolloutRow{
		{repo: "acme/one", selected: true},
		{repo: "acme/two", selected: true},
		{repo: "acme/skip", selected: false},
		{repo: "acme/three", selected: true},
	}
	var active, maximum atomic.Int32
	release := make(chan struct{})
	worker := func(ctx context.Context, repo string, opts orgRolloutOptions) orgRolloutResult {
		current := active.Add(1)
		for old := maximum.Load(); current > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, current) {
				break
			}
		}
		<-release
		active.Add(-1)
		return orgRolloutResult{Status: "created", URL: repo + "/pull/1"}
	}
	events := startOrgRolloutJobs(context.Background(), rows, orgRolloutOptions{concurrency: 2}, 0, worker)
	started, completed := 0, 0
	for event := range events {
		if event.done {
			break
		}
		require.NotEqual(t, "acme/skip", event.result.Repo)
		if event.result.Status == "running" {
			started++
			if started == 2 {
				// Both slots were admitted before either worker could finish.
				require.Eventually(t, func() bool { return active.Load() == 2 }, time.Second, time.Millisecond)
				close(release)
			}
		} else {
			completed++
		}
	}
	assert.Equal(t, 3, started)
	assert.Equal(t, 3, completed)
	assert.EqualValues(t, 2, maximum.Load())
	assert.Equal(t, 1, budgetChecks, "reuse the budget check rather than spending a request per repo")
}

func TestOrgRolloutBudgetFailureStopsAdmission(t *testing.T) {
	stubRolloutBudget(t, func(context.Context, string, bool) error { return errOrgRateLimitCritical })
	calls := 0
	var results []orgRolloutResult
	for event := range startOrgRolloutJobs(context.Background(), []orgRolloutRow{
		{repo: "acme/one", selected: true}, {repo: "acme/two", selected: true},
	}, orgRolloutOptions{concurrency: 2}, 0, func(context.Context, string, orgRolloutOptions) orgRolloutResult {
		calls++
		return orgRolloutResult{}
	}) {
		if !event.done {
			results = append(results, event.result)
		}
	}
	assert.Zero(t, calls)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Error, "critical")
}

func TestOrgRolloutCancellationDrainsWorkers(t *testing.T) {
	stubRolloutBudget(t, func(context.Context, string, bool) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rows := []orgRolloutRow{{repo: "acme/one", selected: true}, {repo: "acme/two", selected: true}}
	finished := 0
	for event := range startOrgRolloutJobs(ctx, rows, orgRolloutOptions{concurrency: 1}, time.Second,
		func(ctx context.Context, _ string, _ orgRolloutOptions) orgRolloutResult {
			<-ctx.Done()
			return orgRolloutResult{Status: "failed", Error: ctx.Err().Error()}
		}) {
		if event.done {
			break
		}
		if event.result.Status == "running" {
			cancel()
		} else {
			finished++
		}
	}
	assert.Equal(t, 1, finished)
}

func TestOrgRolloutThrottlingStopsPendingWorkers(t *testing.T) {
	stubRolloutBudget(t, func(context.Context, string, bool) error { return nil })
	calls := 0
	for range startOrgRolloutJobs(context.Background(), []orgRolloutRow{
		{repo: "acme/one", selected: true}, {repo: "acme/two", selected: true},
	}, orgRolloutOptions{concurrency: 1}, 0, func(context.Context, string, orgRolloutOptions) orgRolloutResult {
		calls++
		return orgRolloutResult{Status: "failed", Error: "HTTP 429", Stop: true}
	}) {
	}
	assert.Equal(t, 1, calls, "throttling must stop admission, not hammer every pending repo")
}

func TestOrgRolloutWorkerStartsArePaced(t *testing.T) {
	stubRolloutBudget(t, func(context.Context, string, bool) error { return nil })
	var starts []time.Time
	gap := 20 * time.Millisecond
	for event := range startOrgRolloutJobs(context.Background(), []orgRolloutRow{
		{repo: "acme/one", selected: true}, {repo: "acme/two", selected: true},
	}, orgRolloutOptions{concurrency: 2}, gap, func(context.Context, string, orgRolloutOptions) orgRolloutResult {
		return orgRolloutResult{Status: "unchanged"}
	}) {
		if event.result.Status == "running" {
			starts = append(starts, time.Now())
		}
	}
	require.Len(t, starts, 2)
	assert.GreaterOrEqual(t, starts[1].Sub(starts[0]), gap)
}

func TestOrgRolloutWorkerProcess(t *testing.T) {
	if os.Getenv("GH_AW_ROLLOUT_HELPER") == "1" {
		if !slices.Contains(os.Args, "--dry-run") || !slices.Contains(os.Args, "--repo=acme/repo") ||
			!slices.Contains(os.Args, "triage") || os.Getenv("GH_PROMPT_DISABLED") != "1" ||
			slices.Contains(os.Args, "--create-pull-request") {
			fmt.Fprintln(os.Stderr, "worker flags or unattended environment missing")
			os.Exit(2)
		}
		if os.Getenv("GH_AW_ROLLOUT_HELPER_FAIL") == "1" {
			fmt.Fprintln(os.Stderr, "API authorization failed")
			os.Exit(1)
		}
		if os.Getenv("GH_AW_ROLLOUT_HELPER_WAIT") == "1" {
			time.Sleep(time.Minute)
		}
		require.NoError(t, emitOrgRolloutResult(orgRolloutResult{Status: "planned"}))
		os.Exit(0)
	}
	t.Setenv("GH_AW_ROLLOUT_HELPER", "1")
	opts := orgRolloutOptions{args: []string{"-test.run=^TestOrgRolloutWorkerProcess$", "--"},
		dryRun: true, workflows: []string{"triage"}}
	result := runOrgRolloutWorker(context.Background(), "acme/repo", opts)
	assert.Equal(t, "planned", result.Status)
	assert.Empty(t, result.Error)
	t.Setenv("GH_AW_ROLLOUT_HELPER_FAIL", "1")
	result = runOrgRolloutWorker(context.Background(), "acme/repo", opts)
	assert.Equal(t, "failed", result.Status)
	assert.Contains(t, result.Error, "authorization failed")
}

func TestOrgRolloutWorkerProcessCancellation(t *testing.T) {
	t.Setenv("GH_AW_ROLLOUT_HELPER", "1")
	t.Setenv("GH_AW_ROLLOUT_HELPER_WAIT", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	result := runOrgRolloutWorker(ctx, "acme/repo", orgRolloutOptions{
		args:   []string{"-test.run=^TestOrgRolloutWorkerProcess$", "--"},
		dryRun: true, workflows: []string{"triage"},
	})
	assert.Equal(t, "failed", result.Status)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func rolloutKey(t *testing.T, model orgRolloutTUI, key string) orgRolloutTUI {
	t.Helper()
	message := tea.KeyPressMsg{Text: key}
	switch key {
	case "space":
		message.Code, message.Text = ' ', " "
	case "enter":
		message.Code, message.Text = tea.KeyEnter, ""
	}
	next, _ := model.Update(message)
	return next.(orgRolloutTUI)
}

func TestOrgRolloutTUISelection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := orgRolloutTUI{ctx: ctx, cancel: cancel, width: 100, height: 20,
		rows: []orgRolloutRow{
			{repo: "acme/one", selected: true},
			{repo: "acme/two", prs: []orgRolloutPR{{Number: 1}}},
			{repo: "acme/other", selected: true},
		}}
	model.cursor = 1
	model = rolloutKey(t, model, "space")
	assert.False(t, model.rows[1].selected, "existing PRs are protected by default")
	model = rolloutKey(t, model, "o")
	model = rolloutKey(t, model, "space")
	assert.True(t, model.rows[1].selected)
	model.filter = "one"
	model = rolloutKey(t, model, "a")
	assert.False(t, model.rows[0].selected)
	assert.True(t, model.rows[2].selected, "filtering must not discard hidden selections")
	model.running = true
	model = rolloutKey(t, model, "o")
	assert.True(t, model.opts.replace, "policy cannot change while running")
	assert.NotContains(t, rolloutText("\x1b[31mred\x1b[0m\nname"), "\x1b")
}

func TestOrgRolloutOutputBounded(t *testing.T) {
	output := &orgRolloutOutput{}
	_, err := output.Write([]byte(strings.Repeat("x", 65536)))
	require.NoError(t, err)
	require.Len(t, output.tail, 32768)
	_, err = output.Write([]byte("last"))
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(string(output.tail), "last"))
}

func TestOrgRolloutWorkerPreviewDoesNotApply(t *testing.T) {
	originalPreview, originalScan := previewOrgRepoUpdatesFn, scanUpgradeRepoFn
	t.Cleanup(func() { previewOrgRepoUpdatesFn, scanUpgradeRepoFn = originalPreview, originalScan })
	ctx := context.WithValue(context.Background(), orgRolloutKey{}, &orgRolloutOptions{worker: true, dryRun: true})
	previewOrgRepoUpdatesFn = func(context.Context, string, UpdateWorkflowsOptions, bool) (orgRepoPreview, error) {
		return orgRepoPreview{}, errors.New("preview only")
	}
	scanUpgradeRepoFn = func(context.Context, string, bool) (orgRepoPreview, bool, error) {
		return orgRepoPreview{}, false, errors.New("preview only")
	}
	require.EqualError(t, runUpdateForTargetRepo(ctx, "acme/repo", UpdateWorkflowsOptions{}, true, false), "preview only")
	require.EqualError(t, runUpgradeForTargetRepo(ctx, "acme/repo", upgradeOptions{}, true, false), "preview only")
}
