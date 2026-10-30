//go:build !integration

package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockScanUpgradeRepo returns a simple scan stub that includes every repo with
// one workflow. Use it to isolate upgrade-org tests from real checkout logic.
func mockScanUpgradeRepo(_ context.Context, repo string, _ bool) (orgRepoPreview, bool, error) {
	return orgRepoPreview{Repo: repo, TotalWorkflows: 1}, true, nil
}

func TestNewUpgradeCommandOrgFlags(t *testing.T) {
	cmd := NewUpgradeCommand(func(string) error { return nil })

	require.NotNil(t, cmd.Flags().Lookup("org"))
	require.NotNil(t, cmd.Flags().Lookup("repos"))
	require.NotNil(t, cmd.Flags().Lookup("create-issue"))
	require.NotNil(t, cmd.Flags().Lookup("yes"))
	assert.Contains(t, cmd.Example, "--org my-org")
	assert.Contains(t, cmd.Example, "--repos '*-service'")
	assert.Contains(t, cmd.Example, "--create-issue")
	assert.Contains(t, cmd.Example, "--create-pull-request --yes")
}

func TestRunUpgradeForOrgCreateIssueAutoAcceptsWithYes(t *testing.T) {
	origSearch := searchOrgLockWorkflowReposFn
	origScan := scanUpgradeRepoFn
	origIssue := createIssueForUpgradeOrgRepoFn
	origWait := waitForOrgRateLimitFn
	origConfirm := orgConfirmActionFn
	origIsCI := isRunningInCIFn
	searchOrgLockWorkflowReposFn = func(ctx context.Context, org string, verbose bool) ([]string, error) {
		return []string{"octo/api"}, nil
	}
	scanUpgradeRepoFn = mockScanUpgradeRepo
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	orgConfirmActionFn = func(title, affirmative, negative string) (bool, error) {
		t.Fatalf("confirmation prompt should be skipped when --yes is set")
		return false, nil
	}
	isRunningInCIFn = func() bool { return true }
	var issued []string
	createIssueForUpgradeOrgRepoFn = func(ctx context.Context, repo string, verbose bool) error {
		issued = append(issued, repo)
		return nil
	}
	defer func() {
		searchOrgLockWorkflowReposFn = origSearch
		scanUpgradeRepoFn = origScan
		createIssueForUpgradeOrgRepoFn = origIssue
		waitForOrgRateLimitFn = origWait
		orgConfirmActionFn = origConfirm
		isRunningInCIFn = origIsCI
	}()

	err := runUpgradeForOrg(context.Background(), "octo", nil, upgradeOptions{ctx: context.Background(), yes: true}, false, true, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"octo/api"}, issued)
}

func TestRunUpgradeForOrgCreateIssueRequiresYesInCI(t *testing.T) {
	origIsCI := isRunningInCIFn
	isRunningInCIFn = func() bool { return true }
	defer func() {
		isRunningInCIFn = origIsCI
	}()

	err := runUpgradeForOrg(context.Background(), "octo", nil, upgradeOptions{ctx: context.Background()}, false, true, false)
	require.Error(t, err)
	require.ErrorContains(t, err, "--yes")
}

func TestRunUpgradeForOrgCreatePRRequiresYesInCI(t *testing.T) {
	origIsCI := isRunningInCIFn
	isRunningInCIFn = func() bool { return true }
	defer func() {
		isRunningInCIFn = origIsCI
	}()

	err := runUpgradeForOrg(context.Background(), "octo", nil, upgradeOptions{ctx: context.Background()}, true, false, false)
	require.Error(t, err)
	require.ErrorContains(t, err, "--yes")
}

func TestRunUpgradeForOrgEmptyOrg(t *testing.T) {
	err := runUpgradeForOrg(context.Background(), "  ", nil, upgradeOptions{ctx: context.Background()}, false, false, false)
	require.Error(t, err)
	require.ErrorContains(t, err, "--org cannot be empty")
}

func TestRunUpgradeForOrgInvalidRepoGlob(t *testing.T) {
	err := runUpgradeForOrg(context.Background(), "octo", []string{"["}, upgradeOptions{ctx: context.Background()}, false, false, false)
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid --repos pattern")
}

func TestRunUpgradeForOrgNoReposFound(t *testing.T) {
	origSearch := searchOrgLockWorkflowReposFn
	searchOrgLockWorkflowReposFn = func(ctx context.Context, org string, verbose bool) ([]string, error) {
		return nil, nil
	}
	defer func() { searchOrgLockWorkflowReposFn = origSearch }()

	output := captureUpgradeOrgStderr(t, func() {
		err := runUpgradeForOrg(context.Background(), "octo", nil, upgradeOptions{ctx: context.Background()}, false, false, false)
		require.NoError(t, err)
	})

	assert.Contains(t, output, "No repositories found with agentic workflows")
}

func TestRunUpgradeForOrgNoReposMatchFilter(t *testing.T) {
	origSearch := searchOrgLockWorkflowReposFn
	origScan := scanUpgradeRepoFn
	origWait := waitForOrgRateLimitFn
	searchOrgLockWorkflowReposFn = func(ctx context.Context, org string, verbose bool) ([]string, error) {
		return []string{"octo/api"}, nil
	}
	scanUpgradeRepoFn = mockScanUpgradeRepo
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	defer func() {
		searchOrgLockWorkflowReposFn = origSearch
		scanUpgradeRepoFn = origScan
		waitForOrgRateLimitFn = origWait
	}()

	output := captureUpgradeOrgStderr(t, func() {
		err := runUpgradeForOrg(context.Background(), "octo", []string{"nomatch-*"}, upgradeOptions{ctx: context.Background()}, false, false, false)
		require.NoError(t, err)
	})

	assert.Contains(t, output, "No repositories matched the requested --repos filters")
}

func TestRunUpgradeForOrgDryRun(t *testing.T) {
	origSearch := searchOrgLockWorkflowReposFn
	origScan := scanUpgradeRepoFn
	origUpgrade := runUpgradeForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	searchOrgLockWorkflowReposFn = func(ctx context.Context, org string, verbose bool) ([]string, error) {
		return []string{"octo/api", "octo/web"}, nil
	}
	scanUpgradeRepoFn = mockScanUpgradeRepo
	runUpgradeForTargetRepoFn = func(ctx context.Context, repo string, opts upgradeOptions, createPR bool, verbose bool) error {
		t.Fatalf("unexpected upgrade call for %s", repo)
		return nil
	}
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	defer func() {
		searchOrgLockWorkflowReposFn = origSearch
		scanUpgradeRepoFn = origScan
		runUpgradeForTargetRepoFn = origUpgrade
		waitForOrgRateLimitFn = origWait
	}()

	output := captureUpgradeOrgStderr(t, func() {
		err := runUpgradeForOrg(context.Background(), "octo", nil, upgradeOptions{ctx: context.Background()}, false, false, false)
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Dry-run preview of upgrade pull requests")
	assert.Contains(t, output, "octo/api")
	assert.Contains(t, output, "octo/web")
}

func TestRunUpgradeForOrgDryRunShowsVersion(t *testing.T) {
	origSearch := searchOrgLockWorkflowReposFn
	origScan := scanUpgradeRepoFn
	origUpgrade := runUpgradeForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	searchOrgLockWorkflowReposFn = func(ctx context.Context, org string, verbose bool) ([]string, error) {
		return []string{"octo/api"}, nil
	}
	scanUpgradeRepoFn = func(_ context.Context, repo string, _ bool) (orgRepoPreview, bool, error) {
		return orgRepoPreview{Repo: repo, TotalWorkflows: 2, CurrentVersions: []string{"v1.2.3"}}, true, nil
	}
	runUpgradeForTargetRepoFn = func(ctx context.Context, repo string, opts upgradeOptions, createPR bool, verbose bool) error {
		t.Fatalf("unexpected upgrade call for %s", repo)
		return nil
	}
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	defer func() {
		searchOrgLockWorkflowReposFn = origSearch
		scanUpgradeRepoFn = origScan
		runUpgradeForTargetRepoFn = origUpgrade
		waitForOrgRateLimitFn = origWait
	}()

	output := captureUpgradeOrgStderr(t, func() {
		err := runUpgradeForOrg(context.Background(), "octo", nil, upgradeOptions{ctx: context.Background()}, false, false, false)
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Dry-run preview of upgrade pull requests")
	assert.Contains(t, output, "octo/api")
	assert.Contains(t, output, "(v1.2.3 -> "+normalizeDisplayVersion(GetVersion())+"; 2 workflow(s))")
}

func TestRunUpgradeForOrgCreatePR(t *testing.T) {
	origSearch := searchOrgLockWorkflowReposFn
	origScan := scanUpgradeRepoFn
	origUpgrade := runUpgradeForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	searchOrgLockWorkflowReposFn = func(ctx context.Context, org string, verbose bool) ([]string, error) {
		return []string{"octo/api", "octo/web"}, nil
	}
	scanUpgradeRepoFn = mockScanUpgradeRepo
	var upgraded []string
	runUpgradeForTargetRepoFn = func(ctx context.Context, repo string, opts upgradeOptions, createPR bool, verbose bool) error {
		upgraded = append(upgraded, repo)
		return nil
	}
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	defer func() {
		searchOrgLockWorkflowReposFn = origSearch
		scanUpgradeRepoFn = origScan
		runUpgradeForTargetRepoFn = origUpgrade
		waitForOrgRateLimitFn = origWait
	}()

	err := runUpgradeForOrg(context.Background(), "octo", nil, upgradeOptions{ctx: context.Background(), yes: true}, true, false, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"octo/api", "octo/web"}, upgraded)
}

func TestRunUpgradeForOrgRepoFilter(t *testing.T) {
	origSearch := searchOrgLockWorkflowReposFn
	origScan := scanUpgradeRepoFn
	origUpgrade := runUpgradeForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	searchOrgLockWorkflowReposFn = func(ctx context.Context, org string, verbose bool) ([]string, error) {
		return []string{"octo/api-service", "octo/web", "octo/worker-service"}, nil
	}
	scanUpgradeRepoFn = mockScanUpgradeRepo
	var upgraded []string
	runUpgradeForTargetRepoFn = func(ctx context.Context, repo string, opts upgradeOptions, createPR bool, verbose bool) error {
		upgraded = append(upgraded, repo)
		return nil
	}
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	defer func() {
		searchOrgLockWorkflowReposFn = origSearch
		scanUpgradeRepoFn = origScan
		runUpgradeForTargetRepoFn = origUpgrade
		waitForOrgRateLimitFn = origWait
	}()

	err := runUpgradeForOrg(context.Background(), "octo", []string{"*-service"}, upgradeOptions{ctx: context.Background(), yes: true}, true, false, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"octo/api-service", "octo/worker-service"}, upgraded)
}

func TestSearchOrgLockWorkflowReposRejectsInvalidOrgBeforeSearch(t *testing.T) {
	origWait := waitForOrgRateLimitFn
	waitForOrgRateLimitFn = func(context.Context, string, bool) error {
		t.Fatal("waitForOrgRateLimitFn should not be called for invalid org")
		return nil
	}
	defer func() {
		waitForOrgRateLimitFn = origWait
	}()

	repos, err := searchOrgLockWorkflowRepos(context.Background(), "bad org", false)
	require.Error(t, err)
	assert.Nil(t, repos)
	assert.EqualError(t, err, `invalid organization name "bad org": `+orgSlugConstraintDescription)
}

func TestRunUpgradeForOrgCreateIssue(t *testing.T) {
	origSearch := searchOrgLockWorkflowReposFn
	origScan := scanUpgradeRepoFn
	origUpgrade := runUpgradeForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	origIssue := createIssueForUpgradeOrgRepoFn
	searchOrgLockWorkflowReposFn = func(ctx context.Context, org string, verbose bool) ([]string, error) {
		return []string{"octo/api", "octo/web"}, nil
	}
	scanUpgradeRepoFn = mockScanUpgradeRepo
	runUpgradeForTargetRepoFn = func(ctx context.Context, repo string, opts upgradeOptions, createPR bool, verbose bool) error {
		t.Fatalf("unexpected upgrade call for %s", repo)
		return nil
	}
	var issuedRepos []string
	createIssueForUpgradeOrgRepoFn = func(ctx context.Context, repo string, verbose bool) error {
		issuedRepos = append(issuedRepos, repo)
		return nil
	}
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	defer func() {
		searchOrgLockWorkflowReposFn = origSearch
		scanUpgradeRepoFn = origScan
		runUpgradeForTargetRepoFn = origUpgrade
		waitForOrgRateLimitFn = origWait
		createIssueForUpgradeOrgRepoFn = origIssue
	}()

	err := runUpgradeForOrg(context.Background(), "octo", nil, upgradeOptions{ctx: context.Background(), yes: true}, false, true, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"octo/api", "octo/web"}, issuedRepos)
}

func TestRunUpgradeCommandCreateIssueRequiresOrg(t *testing.T) {
	cmd := NewUpgradeCommand(func(string) error { return nil })
	cmd.SetArgs([]string{"--create-issue"})
	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorContains(t, err, "--create-issue requires --org")
}

func TestRunUpgradeCommandCreateIssueAndPRMutuallyExclusive(t *testing.T) {
	cmd := NewUpgradeCommand(func(string) error { return nil })
	cmd.SetArgs([]string{"--org", "octo", "--create-issue", "--create-pull-request"})
	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorContains(t, err, "cannot specify both --create-pull-request and --create-issue")
}

func TestRunUpgradeCommandReposRequiresOrg(t *testing.T) {
	cmd := NewUpgradeCommand(func(string) error { return nil })
	cmd.SetArgs([]string{"--repos", "*-svc"})
	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorContains(t, err, "--repos requires --org")
}

func TestRunUpgradeForOrgSkipsFailedRepos(t *testing.T) {
	origSearch := searchOrgLockWorkflowReposFn
	origScan := scanUpgradeRepoFn
	origUpgrade := runUpgradeForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	searchOrgLockWorkflowReposFn = func(_ context.Context, _ string, _ bool) ([]string, error) {
		return []string{"octo/api", "octo/web"}, nil
	}
	scanUpgradeRepoFn = mockScanUpgradeRepo
	boom := errors.New("upgrade failed")
	var called []string
	runUpgradeForTargetRepoFn = func(_ context.Context, repo string, _ upgradeOptions, _ bool, _ bool) error {
		called = append(called, repo)
		return boom
	}
	waitForOrgRateLimitFn = func(_ context.Context, _ string, _ bool) error { return nil }
	defer func() {
		searchOrgLockWorkflowReposFn = origSearch
		scanUpgradeRepoFn = origScan
		runUpgradeForTargetRepoFn = origUpgrade
		waitForOrgRateLimitFn = origWait
	}()

	err := runUpgradeForOrg(context.Background(), "octo", nil, upgradeOptions{ctx: context.Background(), yes: true}, true, false, false)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to upgrade any repository")
	assert.Equal(t, []string{"octo/api", "octo/web"}, called, "should attempt all repos and skip failures")
}

func TestRunUpgradeForOrgCreateIssueSkipsFailedRepos(t *testing.T) {
	origSearch := searchOrgLockWorkflowReposFn
	origScan := scanUpgradeRepoFn
	origUpgrade := runUpgradeForTargetRepoFn
	origIssue := createIssueForUpgradeOrgRepoFn
	origWait := waitForOrgRateLimitFn
	searchOrgLockWorkflowReposFn = func(_ context.Context, _ string, _ bool) ([]string, error) {
		return []string{"octo/api", "octo/web"}, nil
	}
	scanUpgradeRepoFn = mockScanUpgradeRepo
	runUpgradeForTargetRepoFn = func(_ context.Context, repo string, _ upgradeOptions, _ bool, _ bool) error {
		t.Fatalf("unexpected upgrade call for %s", repo)
		return nil
	}
	boom := errors.New("issue failed")
	var called []string
	createIssueForUpgradeOrgRepoFn = func(_ context.Context, repo string, _ bool) error {
		called = append(called, repo)
		return boom
	}
	waitForOrgRateLimitFn = func(_ context.Context, _ string, _ bool) error { return nil }
	defer func() {
		searchOrgLockWorkflowReposFn = origSearch
		scanUpgradeRepoFn = origScan
		runUpgradeForTargetRepoFn = origUpgrade
		createIssueForUpgradeOrgRepoFn = origIssue
		waitForOrgRateLimitFn = origWait
	}()

	err := runUpgradeForOrg(context.Background(), "octo", nil, upgradeOptions{ctx: context.Background(), yes: true}, false, true, false)
	require.Error(t, err)
	require.ErrorContains(t, err, "failed to create issues in any repository")
	assert.Equal(t, []string{"octo/api", "octo/web"}, called, "should attempt all repos and skip failures")
}

func TestRunUpgradeForOrgSortsAlphabetically(t *testing.T) {
	origSearch := searchOrgLockWorkflowReposFn
	origScan := scanUpgradeRepoFn
	origUpgrade := runUpgradeForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	searchOrgLockWorkflowReposFn = func(_ context.Context, _ string, _ bool) ([]string, error) {
		return []string{"octo/zoo", "octo/alpha", "octo/middle"}, nil
	}
	scanUpgradeRepoFn = mockScanUpgradeRepo
	var called []string
	runUpgradeForTargetRepoFn = func(_ context.Context, repo string, _ upgradeOptions, _ bool, _ bool) error {
		called = append(called, repo)
		return nil
	}
	waitForOrgRateLimitFn = func(_ context.Context, _ string, _ bool) error { return nil }
	defer func() {
		searchOrgLockWorkflowReposFn = origSearch
		scanUpgradeRepoFn = origScan
		runUpgradeForTargetRepoFn = origUpgrade
		waitForOrgRateLimitFn = origWait
	}()

	err := runUpgradeForOrg(context.Background(), "octo", nil, upgradeOptions{ctx: context.Background(), yes: true}, true, false, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"octo/alpha", "octo/middle", "octo/zoo"}, called)
}

func captureUpgradeOrgStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() {
		_ = r.Close()
		os.Stderr = orig
	}()

	fn()

	require.NoError(t, w.Close())
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(data)
}
