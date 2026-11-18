//go:build !integration

package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRepoGlobs(t *testing.T) {
	require.NoError(t, validateRepoGlobs([]string{"api-*", "octo/*"}))

	err := validateRepoGlobs([]string{"["})
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid --repos pattern")
}

func TestPreviewOrgRepoCheckoutUsesCheckoutRelativePaths(t *testing.T) {
	checkoutDir := t.TempDir()
	workflowsDir := filepath.Join(checkoutDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "review.md"),
		[]byte("---\non: push\nengine: claude\nsource: owner/repo/review.md@v1.0.0\n---\n"), 0600))
	beforeResolve, beforeDownload := resolveLatestRefFn, downloadWorkflowContentFn
	t.Cleanup(func() {
		resolveLatestRefFn, downloadWorkflowContentFn = beforeResolve, beforeDownload
	})
	resolveLatestRefFn = func(context.Context, string, string, bool, bool, time.Duration) (latestRefResolution, error) {
		return latestRefResolution{Ref: "v2.0.0"}, nil
	}
	downloadWorkflowContentFn = func(context.Context, string, string, string, bool) ([]byte, error) {
		return []byte("---\non: push\nengine: claude\n---\n"), nil
	}

	preview, err := previewOrgRepoCheckout(context.Background(), "acme/repo", checkoutDir, UpdateWorkflowsOptions{}, false)
	require.NoError(t, err)
	require.Len(t, preview.Workflows, 1)
	assert.Equal(t, ".github/workflows/review.md", preview.Workflows[0].Path)
	assert.NotContains(t, preview.Workflows[0].Path, checkoutDir)
}

func TestFilterOrgRepos(t *testing.T) {
	repos := []string{"octo/api-service", "octo/web", "octo/worker"}

	assert.Equal(t, repos, filterOrgRepos(repos, nil))
	assert.Equal(t, []string{"octo/api-service"}, filterOrgRepos(repos, []string{"api-*"}))
	assert.Equal(t, []string{"octo/web"}, filterOrgRepos(repos, []string{"octo/web"}))
}

func TestNewUpdateCommandOrgFlags(t *testing.T) {
	cmd := NewUpdateCommand(func(string) error { return nil })

	require.NotNil(t, cmd.Flags().Lookup("org"))
	require.NotNil(t, cmd.Flags().Lookup("repos"))
	require.NotNil(t, cmd.Flags().Lookup("create-issue"))
	require.NotNil(t, cmd.Flags().Lookup("yes"))
	assert.Contains(t, cmd.Example, "--org my-org")
	assert.Contains(t, cmd.Example, "--repos '*-service'")
	assert.Contains(t, cmd.Example, "--create-issue")
	assert.Contains(t, cmd.Example, "--create-pull-request --yes")
}

func TestRunUpdateForOrgCreateIssueRequiresYesInCI(t *testing.T) {
	origSearch := searchOrgWorkflowReposFn
	origPreview := previewOrgRepoUpdatesFn
	origWait := waitForOrgRateLimitFn
	origCreateIssue := createIssueForOrgRepoFn
	origIsCI := isRunningInCIFn
	searchOrgWorkflowReposFn = func(ctx context.Context, org string, workflowNames []string, verbose bool) ([]string, error) {
		return []string{"octo/api"}, nil
	}
	previewOrgRepoUpdatesFn = func(ctx context.Context, repo string, opts UpdateWorkflowsOptions, verbose bool) (orgRepoPreview, error) {
		return orgRepoPreview{
			Repo:           repo,
			TotalWorkflows: 1,
			Workflows: []orgWorkflowPreview{{
				Name: "repo-assist", CurrentRef: "v1.0.0", LatestRef: "v1.1.0",
			}},
		}, nil
	}
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	createIssueForOrgRepoFn = func(ctx context.Context, preview orgRepoPreview, verbose bool) error {
		t.Fatalf("issue creation should not run in CI without --yes")
		return nil
	}
	isRunningInCIFn = func() bool { return true }
	defer func() {
		searchOrgWorkflowReposFn = origSearch
		previewOrgRepoUpdatesFn = origPreview
		waitForOrgRateLimitFn = origWait
		createIssueForOrgRepoFn = origCreateIssue
		isRunningInCIFn = origIsCI
	}()

	err := runUpdateForOrg(context.Background(), "octo", nil, UpdateWorkflowsOptions{}, false, true, false)
	require.Error(t, err)
	require.ErrorContains(t, err, "--yes")
}

func TestRunUpdateForOrgCreateIssueSkipsWhenDeclined(t *testing.T) {
	origSearch := searchOrgWorkflowReposFn
	origPreview := previewOrgRepoUpdatesFn
	origWait := waitForOrgRateLimitFn
	origCreateIssue := createIssueForOrgRepoFn
	origConfirm := orgConfirmActionFn
	origIsCI := isRunningInCIFn
	searchOrgWorkflowReposFn = func(ctx context.Context, org string, workflowNames []string, verbose bool) ([]string, error) {
		return []string{"octo/api"}, nil
	}
	previewOrgRepoUpdatesFn = func(ctx context.Context, repo string, opts UpdateWorkflowsOptions, verbose bool) (orgRepoPreview, error) {
		return orgRepoPreview{
			Repo:           repo,
			TotalWorkflows: 1,
			Workflows: []orgWorkflowPreview{{
				Name: "repo-assist", CurrentRef: "v1.0.0", LatestRef: "v1.1.0",
			}},
		}, nil
	}
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	createIssueForOrgRepoFn = func(ctx context.Context, preview orgRepoPreview, verbose bool) error {
		t.Fatalf("issue creation should be skipped when confirmation is declined")
		return nil
	}
	orgConfirmActionFn = func(title, affirmative, negative string) (bool, error) { return false, nil }
	isRunningInCIFn = func() bool { return false }
	defer func() {
		searchOrgWorkflowReposFn = origSearch
		previewOrgRepoUpdatesFn = origPreview
		waitForOrgRateLimitFn = origWait
		createIssueForOrgRepoFn = origCreateIssue
		orgConfirmActionFn = origConfirm
		isRunningInCIFn = origIsCI
	}()

	output := captureUpdateOrgStderr(t, func() {
		err := runUpdateForOrg(context.Background(), "octo", nil, UpdateWorkflowsOptions{}, false, true, false)
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Repository: octo/api")
	assert.Contains(t, output, "repo-assist: v1.0.0 -> v1.1.0")
	assert.Contains(t, output, "Skipped octo/api")
}

func TestRunUpdateForOrgDryRun(t *testing.T) {
	origSearch := searchOrgWorkflowReposFn
	origPreview := previewOrgRepoUpdatesFn
	origUpdate := runUpdateForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	searchOrgWorkflowReposFn = func(ctx context.Context, org string, workflowNames []string, verbose bool) ([]string, error) {
		return []string{"octo/api", "octo/web"}, nil
	}

	previewOrgRepoUpdatesFn = func(ctx context.Context, repo string, opts UpdateWorkflowsOptions, verbose bool) (orgRepoPreview, error) {
		if repo == "octo/api" {
			return orgRepoPreview{
				Repo:           repo,
				TotalWorkflows: 1,
				OldestEdit:     time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
				Workflows: []orgWorkflowPreview{{
					Name:       "repo-assist",
					CurrentRef: "v1.0.0",
					LatestRef:  "v1.1.0",
				}},
			}, nil
		}
		return orgRepoPreview{Repo: repo}, nil
	}
	runUpdateForTargetRepoFn = func(ctx context.Context, targetRepo string, opts UpdateWorkflowsOptions, createPR bool, verbose bool) error {
		t.Fatalf("unexpected update call for %s", targetRepo)
		return nil
	}
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	defer func() {
		searchOrgWorkflowReposFn = origSearch
		previewOrgRepoUpdatesFn = origPreview
		runUpdateForTargetRepoFn = origUpdate
		waitForOrgRateLimitFn = origWait
	}()

	output := captureUpdateOrgStderr(t, func() {
		err := runUpdateForOrg(context.Background(), "octo", nil, UpdateWorkflowsOptions{}, false, false, false)
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Dry-run preview of update pull requests")
	assert.Contains(t, output, "octo/api")
	assert.Contains(t, output, "octo/api (1 workflow(s))")
	assert.Contains(t, output, "repo-assist: v1.0.0 -> v1.1.0")
	assert.NotContains(t, output, "- octo/web\n")
}

func TestRunUpdateForOrgPassesWorkflowNameFilters(t *testing.T) {
	origSearch := searchOrgWorkflowReposFn
	origPreview := previewOrgRepoUpdatesFn
	origUpdate := runUpdateForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	var gotFilters []string
	searchOrgWorkflowReposFn = func(ctx context.Context, org string, workflowNames []string, verbose bool) ([]string, error) {
		gotFilters = append([]string(nil), workflowNames...)
		return []string{"octo/api"}, nil
	}
	previewOrgRepoUpdatesFn = func(ctx context.Context, repo string, opts UpdateWorkflowsOptions, verbose bool) (orgRepoPreview, error) {
		assert.Equal(t, []string{"repo-assist", "triage.md"}, opts.WorkflowNames, "org preview should receive the same workflow filters as org discovery")
		return orgRepoPreview{Repo: repo}, nil
	}
	runUpdateForTargetRepoFn = func(ctx context.Context, targetRepo string, opts UpdateWorkflowsOptions, createPR bool, verbose bool) error {
		t.Fatalf("unexpected update call for %s", targetRepo)
		return nil
	}
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	defer func() {
		searchOrgWorkflowReposFn = origSearch
		previewOrgRepoUpdatesFn = origPreview
		runUpdateForTargetRepoFn = origUpdate
		waitForOrgRateLimitFn = origWait
	}()

	err := runUpdateForOrg(context.Background(), "octo", nil, UpdateWorkflowsOptions{
		WorkflowNames: []string{"repo-assist", "triage.md"},
	}, false, false, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"repo-assist", "triage.md"}, gotFilters, "org search should receive the requested workflow filters")
}

func TestRunUpdateForOrgNoReposIncludesWorkflowFilters(t *testing.T) {
	origSearch := searchOrgWorkflowReposFn
	searchOrgWorkflowReposFn = func(ctx context.Context, org string, workflowNames []string, verbose bool) ([]string, error) {
		return nil, nil
	}
	defer func() {
		searchOrgWorkflowReposFn = origSearch
	}()

	output := captureUpdateOrgStderr(t, func() {
		err := runUpdateForOrg(context.Background(), "octo", nil, UpdateWorkflowsOptions{
			WorkflowNames: []string{"triage.md", "repo-assist"},
		}, false, false, false)
		require.NoError(t, err)
	})

	assert.Contains(
		t,
		output,
		"No repositories found with source-managed workflows matching: repo-assist, triage",
		"workflow-filtered org discovery should report which requested workflows had no matching repositories",
	)
}

func TestRunUpdateForOrgCreatePRSortsOldestFirst(t *testing.T) {
	origSearch := searchOrgWorkflowReposFn
	origPreview := previewOrgRepoUpdatesFn
	origUpdate := runUpdateForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	searchOrgWorkflowReposFn = func(ctx context.Context, org string, workflowNames []string, verbose bool) ([]string, error) {
		return []string{"octo/newer", "octo/older"}, nil
	}
	previewOrgRepoUpdatesFn = func(ctx context.Context, repo string, opts UpdateWorkflowsOptions, verbose bool) (orgRepoPreview, error) {
		edited := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		if strings.HasSuffix(repo, "older") {
			edited = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		return orgRepoPreview{
			Repo:           repo,
			TotalWorkflows: 1,
			OldestEdit:     edited,
			Workflows: []orgWorkflowPreview{{
				Name:       "repo-assist",
				CurrentRef: "v1.0.0",
				LatestRef:  "v1.1.0",
			}},
		}, nil
	}
	var processed []string
	runUpdateForTargetRepoFn = func(ctx context.Context, targetRepo string, opts UpdateWorkflowsOptions, createPR bool, verbose bool) error {
		processed = append(processed, targetRepo)
		return nil
	}
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	defer func() {
		searchOrgWorkflowReposFn = origSearch
		previewOrgRepoUpdatesFn = origPreview
		runUpdateForTargetRepoFn = origUpdate
		waitForOrgRateLimitFn = origWait
	}()

	err := runUpdateForOrg(context.Background(), "octo", nil, UpdateWorkflowsOptions{Yes: true}, true, false, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"octo/older", "octo/newer"}, processed)
}

func TestRunUpdateForOrgCreateIssueSortsOldestFirst(t *testing.T) {
	origSearch := searchOrgWorkflowReposFn
	origPreview := previewOrgRepoUpdatesFn
	origUpdate := runUpdateForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	origCreateIssue := createIssueForOrgRepoFn
	searchOrgWorkflowReposFn = func(ctx context.Context, org string, workflowNames []string, verbose bool) ([]string, error) {
		return []string{"octo/newer", "octo/older"}, nil
	}
	previewOrgRepoUpdatesFn = func(ctx context.Context, repo string, opts UpdateWorkflowsOptions, verbose bool) (orgRepoPreview, error) {
		edited := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		if strings.HasSuffix(repo, "older") {
			edited = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		return orgRepoPreview{
			Repo:           repo,
			TotalWorkflows: 1,
			OldestEdit:     edited,
			Workflows: []orgWorkflowPreview{{
				Name:       "repo-assist",
				CurrentRef: "v1.0.0",
				LatestRef:  "v1.1.0",
			}},
		}, nil
	}
	runUpdateForTargetRepoFn = func(ctx context.Context, targetRepo string, opts UpdateWorkflowsOptions, createPR bool, verbose bool) error {
		t.Fatalf("unexpected update call for %s", targetRepo)
		return nil
	}
	var issuedFor []string
	createIssueForOrgRepoFn = func(ctx context.Context, preview orgRepoPreview, verbose bool) error {
		issuedFor = append(issuedFor, preview.Repo)
		return nil
	}
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	defer func() {
		searchOrgWorkflowReposFn = origSearch
		previewOrgRepoUpdatesFn = origPreview
		runUpdateForTargetRepoFn = origUpdate
		waitForOrgRateLimitFn = origWait
		createIssueForOrgRepoFn = origCreateIssue
	}()

	err := runUpdateForOrg(context.Background(), "octo", nil, UpdateWorkflowsOptions{Yes: true}, false, true, false)
	require.NoError(t, err)
	assert.Equal(t, []string{"octo/older", "octo/newer"}, issuedFor)
}

func TestRunUpdateForOrgContinuesAfterPreviewError(t *testing.T) {
	origSearch := searchOrgWorkflowReposFn
	origPreview := previewOrgRepoUpdatesFn
	origUpdate := runUpdateForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	searchOrgWorkflowReposFn = func(ctx context.Context, org string, workflowNames []string, verbose bool) ([]string, error) {
		return []string{"octo/broken", "octo/good"}, nil
	}
	previewOrgRepoUpdatesFn = func(ctx context.Context, repo string, opts UpdateWorkflowsOptions, verbose bool) (orgRepoPreview, error) {
		if repo == "octo/broken" {
			return orgRepoPreview{}, errors.New("failed to parse frontmatter")
		}
		return orgRepoPreview{
			Repo:           repo,
			TotalWorkflows: 1,
			Workflows: []orgWorkflowPreview{{
				Name:       "repo-assist",
				CurrentRef: "v1.0.0",
				LatestRef:  "v1.1.0",
			}},
		}, nil
	}
	runUpdateForTargetRepoFn = func(ctx context.Context, targetRepo string, opts UpdateWorkflowsOptions, createPR bool, verbose bool) error {
		t.Fatalf("unexpected update call for %s", targetRepo)
		return nil
	}
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	defer func() {
		searchOrgWorkflowReposFn = origSearch
		previewOrgRepoUpdatesFn = origPreview
		runUpdateForTargetRepoFn = origUpdate
		waitForOrgRateLimitFn = origWait
	}()

	output := captureUpdateOrgStderr(t, func() {
		err := runUpdateForOrg(context.Background(), "octo", nil, UpdateWorkflowsOptions{}, false, false, false)
		require.NoError(t, err)
	})

	// The broken repo must be skipped (not abort the run), and the good repo reported.
	assert.Contains(t, output, "Skipping octo/broken")
	assert.Contains(t, output, "- octo/good")
	assert.Contains(t, output, "repo-assist: v1.0.0 -> v1.1.0")
}

func TestRunUpdateForOrgStopsOnCriticalRateLimit(t *testing.T) {
	origSearch := searchOrgWorkflowReposFn
	origPreview := previewOrgRepoUpdatesFn
	origUpdate := runUpdateForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	searchOrgWorkflowReposFn = func(ctx context.Context, org string, workflowNames []string, verbose bool) ([]string, error) {
		return []string{"octo/a", "octo/b", "octo/c"}, nil
	}
	var previewed []string
	previewOrgRepoUpdatesFn = func(ctx context.Context, repo string, opts UpdateWorkflowsOptions, verbose bool) (orgRepoPreview, error) {
		previewed = append(previewed, repo)
		return orgRepoPreview{
			Repo:           repo,
			TotalWorkflows: 1,
			Workflows: []orgWorkflowPreview{{
				Name:       "repo-assist",
				CurrentRef: "v1.0.0",
				LatestRef:  "v1.1.0",
			}},
		}, nil
	}
	runUpdateForTargetRepoFn = func(ctx context.Context, targetRepo string, opts UpdateWorkflowsOptions, createPR bool, verbose bool) error {
		return nil
	}
	calls := 0
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error {
		calls++
		if calls >= 2 {
			return errOrgRateLimitCritical
		}
		return nil
	}
	defer func() {
		searchOrgWorkflowReposFn = origSearch
		previewOrgRepoUpdatesFn = origPreview
		runUpdateForTargetRepoFn = origUpdate
		waitForOrgRateLimitFn = origWait
	}()

	output := captureUpdateOrgStderr(t, func() {
		err := runUpdateForOrg(context.Background(), "octo", nil, UpdateWorkflowsOptions{}, false, false, false)
		require.NoError(t, err)
	})

	// Critical budget on the second repo stops the scan but still shows the report.
	assert.Equal(t, []string{"octo/a"}, previewed)
	assert.Contains(t, output, "budget critical")
	assert.Contains(t, output, "- octo/a")
}

func TestRunUpdateForOrgStopsOnCancellation(t *testing.T) {
	origSearch := searchOrgWorkflowReposFn
	origPreview := previewOrgRepoUpdatesFn
	origUpdate := runUpdateForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	searchOrgWorkflowReposFn = func(ctx context.Context, org string, workflowNames []string, verbose bool) ([]string, error) {
		return []string{"octo/a", "octo/b"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	previewOrgRepoUpdatesFn = func(c context.Context, repo string, opts UpdateWorkflowsOptions, verbose bool) (orgRepoPreview, error) {
		// Cancel after the first repo is previewed.
		cancel()
		return orgRepoPreview{
			Repo:           repo,
			TotalWorkflows: 1,
			Workflows: []orgWorkflowPreview{{
				Name:       "repo-assist",
				CurrentRef: "v1.0.0",
				LatestRef:  "v1.1.0",
			}},
		}, nil
	}
	runUpdateForTargetRepoFn = func(c context.Context, targetRepo string, opts UpdateWorkflowsOptions, createPR bool, verbose bool) error {
		return nil
	}
	waitForOrgRateLimitFn = func(c context.Context, resource string, verbose bool) error { return nil }
	defer func() {
		searchOrgWorkflowReposFn = origSearch
		previewOrgRepoUpdatesFn = origPreview
		runUpdateForTargetRepoFn = origUpdate
		waitForOrgRateLimitFn = origWait
	}()

	output := captureUpdateOrgStderr(t, func() {
		err := runUpdateForOrg(ctx, "octo", nil, UpdateWorkflowsOptions{}, false, false, false)
		require.NoError(t, err)
	})

	assert.Contains(t, output, "Cancellation requested")
	assert.Contains(t, output, "- octo/a")
}

func TestRunUpdateForOrgCreateIssueReturnsErrorWhenAllIssueCreatesFail(t *testing.T) {
	origSearch := searchOrgWorkflowReposFn
	origPreview := previewOrgRepoUpdatesFn
	origUpdate := runUpdateForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	origCreateIssue := createIssueForOrgRepoFn
	searchOrgWorkflowReposFn = func(ctx context.Context, org string, workflowNames []string, verbose bool) ([]string, error) {
		return []string{"octo/a"}, nil
	}
	previewOrgRepoUpdatesFn = func(ctx context.Context, repo string, opts UpdateWorkflowsOptions, verbose bool) (orgRepoPreview, error) {
		return orgRepoPreview{
			Repo:           repo,
			TotalWorkflows: 1,
			Workflows: []orgWorkflowPreview{{
				Name:       "repo-assist",
				CurrentRef: "v1.0.0",
				LatestRef:  "v1.1.0",
			}},
		}, nil
	}
	runUpdateForTargetRepoFn = func(ctx context.Context, targetRepo string, opts UpdateWorkflowsOptions, createPR bool, verbose bool) error {
		t.Fatalf("unexpected update call for %s", targetRepo)
		return nil
	}
	createIssueForOrgRepoFn = func(ctx context.Context, preview orgRepoPreview, verbose bool) error {
		return errors.New("boom")
	}
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	defer func() {
		searchOrgWorkflowReposFn = origSearch
		previewOrgRepoUpdatesFn = origPreview
		runUpdateForTargetRepoFn = origUpdate
		waitForOrgRateLimitFn = origWait
		createIssueForOrgRepoFn = origCreateIssue
	}()

	err := runUpdateForOrg(context.Background(), "octo", nil, UpdateWorkflowsOptions{Yes: true}, false, true, false)
	require.EqualError(t, err, "failed to create issues in any repository")
}

func TestRunUpdateForOrgCreatePRReturnsErrorWhenAllUpdatesFail(t *testing.T) {
	origSearch := searchOrgWorkflowReposFn
	origPreview := previewOrgRepoUpdatesFn
	origUpdate := runUpdateForTargetRepoFn
	origWait := waitForOrgRateLimitFn
	searchOrgWorkflowReposFn = func(ctx context.Context, org string, workflowNames []string, verbose bool) ([]string, error) {
		return []string{"octo/a"}, nil
	}
	previewOrgRepoUpdatesFn = func(ctx context.Context, repo string, opts UpdateWorkflowsOptions, verbose bool) (orgRepoPreview, error) {
		return orgRepoPreview{
			Repo:           repo,
			TotalWorkflows: 1,
			Workflows: []orgWorkflowPreview{{
				Name:       "repo-assist",
				CurrentRef: "v1.0.0",
				LatestRef:  "v1.1.0",
			}},
		}, nil
	}
	runUpdateForTargetRepoFn = func(ctx context.Context, targetRepo string, opts UpdateWorkflowsOptions, createPR bool, verbose bool) error {
		return errors.New("boom")
	}
	waitForOrgRateLimitFn = func(ctx context.Context, resource string, verbose bool) error { return nil }
	defer func() {
		searchOrgWorkflowReposFn = origSearch
		previewOrgRepoUpdatesFn = origPreview
		runUpdateForTargetRepoFn = origUpdate
		waitForOrgRateLimitFn = origWait
	}()

	err := runUpdateForOrg(context.Background(), "octo", nil, UpdateWorkflowsOptions{Yes: true}, true, false, false)
	require.EqualError(t, err, "failed to update any repository")
}

func captureUpdateOrgStderr(t *testing.T, fn func()) string {
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

func TestGetLatestWorkflowEditTimeFromCheckout(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = tempDir
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v failed: %s", args, string(output))
	}

	runGit("init")
	runGit("config", "user.name", "Test User")
	runGit("config", "user.email", "test@example.com")

	workflowPath := filepath.Join(tempDir, ".github", "workflows", "example.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(workflowPath), 0o755))
	require.NoError(t, os.WriteFile(workflowPath, []byte("---\nsource: owner/repo/.github/workflows/example.md@v1\n---\n"), 0o644))
	runGit("add", ".")
	runGit("commit", "-m", "add workflow")

	got, err := getLatestWorkflowEditTimeFromCheckout(context.Background(), tempDir, workflowPath)
	require.NoError(t, err)
	assert.False(t, got.IsZero())
}

func TestBuildOrgUpdateIssue(t *testing.T) {
	title, body := buildOrgUpdateIssue(orgRepoPreview{
		Repo: "octo/repo",
		Workflows: []orgWorkflowPreview{
			{Name: "repo-assist", CurrentRef: "1111111", LatestRef: "2222222"},
		},
	}, "v1.2.3", "https://github.com/github/gh-aw/releases/tag/v1.2.3", "<!-- gh-aw-update: v1.2.3 -->")

	assert.Equal(t, "[aw] Updates available", title)
	assert.Contains(t, body, "## Agentic Workflows Update Available")
	assert.Contains(t, body, "- `repo-assist`: `1111111` -> `2222222`")
	assert.Contains(t, body, "Assign this issue to Copilot")
	assert.Contains(t, body, "@copilot update agentic workflows")
	assert.Contains(t, body, "Run `gh aw update`")
	assert.Contains(t, body, "<!-- gh-aw-update: v1.2.3 -->")
	assert.Contains(t, body, "https://github.com/github/gh-aw/releases/tag/v1.2.3")
}

func TestBuildOrgUpdateIssueNoRelease(t *testing.T) {
	_, body := buildOrgUpdateIssue(orgRepoPreview{
		Repo:      "octo/repo",
		Workflows: []orgWorkflowPreview{{Name: "wf", CurrentRef: "aaa", LatestRef: "bbb"}},
	}, "", "", "<!-- gh-aw-update: latest -->")

	assert.Contains(t, body, "<!-- gh-aw-update: latest -->")
	assert.NotContains(t, body, "View gh-aw release")
}
