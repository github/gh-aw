package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/syncutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var orgIPLog = logger.New("cli:org_issue_pr")

var (
	ghawReleaseTagCache syncutil.OnceLoader[string]

	getLatestOrgReleaseFunc = func(ctx context.Context, includePrereleases bool) (string, error) {
		return getLatestRelease(ctx, includePrereleases)
	}
)

var errEmptyGhawReleaseTag = errors.New("latest gh-aw release tag was empty")

const (
	// ghawUpgradeMarkerPrefix is the XML marker prefix embedded in upgrade org PRs/issues.
	// Full marker format: <!-- gh-aw-upgrade: vX.Y.Z -->
	ghawUpgradeMarkerPrefix = "<!-- gh-aw-upgrade:"

	// ghawUpdateMarkerPrefix is the XML marker prefix embedded in update org PRs/issues.
	// Full marker format: <!-- gh-aw-update: vX.Y.Z -->
	ghawUpdateMarkerPrefix = "<!-- gh-aw-update:"

	// ghawReleaseRepo is the GitHub repository for gh-aw releases.
	ghawReleaseRepo = "github/gh-aw"

	// agenticWorkflowsLabel is the GitHub label applied to org runner PRs and issues.
	agenticWorkflowsLabel = "agentic-workflows"
)

// buildOrgXMLMarker builds a full XML comment marker string for a given prefix and release tag.
// Example: buildOrgXMLMarker("<!-- gh-aw-upgrade:", "v1.2.3") → "<!-- gh-aw-upgrade: v1.2.3 -->"
// If tag is empty the marker is still written with a placeholder so it remains searchable.
func buildOrgXMLMarker(prefix, tag string) string {
	if tag == "" {
		return prefix + " latest -->"
	}
	return fmt.Sprintf("%s %s -->", prefix, tag)
}

// getGhawReleaseInfo returns the latest stable gh-aw release tag and its HTML URL.
// Both values are empty strings when the release cannot be determined; callers must
// handle this gracefully (e.g. omit the release link rather than failing).
func getGhawReleaseInfo(ctx context.Context) (tag, releaseURL string) {
	tag, err := ghawReleaseTagCache.Get(func() (string, error) {
		tag, err := getLatestOrgReleaseFunc(ctx, false)
		if err != nil {
			return "", err
		}
		if tag == "" {
			return "", errEmptyGhawReleaseTag
		}
		return tag, nil
	})
	if err != nil {
		orgIPLog.Printf("Could not resolve latest gh-aw release: %v", err)
		ghawReleaseTagCache.Reset()
		return "", ""
	}
	return tag, fmt.Sprintf("https://github.com/%s/releases/tag/%s", ghawReleaseRepo, tag)
}

type orgListItem struct {
	Number      int             `json:"number"`
	Body        string          `json:"body"`
	PullRequest *orgPullRequest `json:"pull_request,omitempty"`
}

// orgPullRequest is a presence marker for the GitHub issues API, which returns
// pull requests and issues in the same collection.
type orgPullRequest struct{}

func runOrgAPI(ctx context.Context, remoteHost, spinnerMessage string, args ...string) ([]byte, error) {
	ghArgs := append([]string{"api", "--hostname", remoteHost}, args...)
	return workflow.RunGHContext(ctx, spinnerMessage, ghArgs...)
}

func runOrgAPICombined(ctx context.Context, remoteHost, spinnerMessage string, args ...string) ([]byte, error) {
	ghArgs := append([]string{"api", "--hostname", remoteHost}, args...)
	return workflow.RunGHCombinedContext(ctx, spinnerMessage, ghArgs...)
}

func listOpenOrgItems(ctx context.Context, repo, collection string) ([]orgListItem, error) {
	remoteHost := getHostFromOriginRemote()
	items := make([]orgListItem, 0)
	spinnerMessage := "Checking for existing items..."
	switch collection {
	case "issues":
		spinnerMessage = "Checking for existing issues..."
	case "pulls":
		spinnerMessage = "Checking for existing PRs..."
	}
	for page := 1; ; page++ {
		output, err := runOrgAPI(ctx, remoteHost, spinnerMessage,
			fmt.Sprintf("/repos/%s/%s?state=open&per_page=100&page=%d", repo, collection, page),
		)
		if err != nil {
			return nil, err
		}

		var pageItems []orgListItem
		if err := json.Unmarshal(output, &pageItems); err != nil {
			return nil, err
		}
		items = append(items, pageItems...)
		if len(pageItems) < 100 {
			return items, nil
		}
	}
}

func isLabelValidationError(output []byte, err error) bool {
	if err == nil {
		return false
	}
	errText := strings.ToLower(err.Error())
	outText := strings.ToLower(string(output))
	has422 := strings.Contains(errText, "http 422") || strings.Contains(outText, "http 422")
	hasValidationFailure := strings.Contains(errText, "validation failed") || strings.Contains(outText, "validation failed")
	return has422 && hasValidationFailure &&
		(strings.Contains(errText, "label") || strings.Contains(outText, "label"))
}

// closeExistingOrgIssuesByMarker finds all open issues in repo whose body contains
// the given marker prefix string and closes them as not_planned. Errors are
// non-fatal: a warning is logged and the function continues.
func closeExistingOrgIssuesByMarker(ctx context.Context, repo, markerPrefix string, verbose bool) {
	issues, err := listOpenOrgItems(ctx, repo, "issues")
	if err != nil {
		orgIPLog.Printf("Failed to list open issues in %s: %v", repo, err)
		return
	}

	for _, issue := range issues {
		if issue.PullRequest != nil {
			continue
		}
		if !strings.Contains(issue.Body, markerPrefix) {
			continue
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(
				fmt.Sprintf("Closing outdated issue #%d in %s", issue.Number, repo),
			))
		}
		if _, closeErr := runOrgAPI(ctx, getHostFromOriginRemote(), "Closing outdated issue...",
			"--method", "PATCH",
			fmt.Sprintf("/repos/%s/issues/%d", repo, issue.Number),
			"-f", "state=closed",
			"-f", "state_reason=not_planned",
		); closeErr != nil {
			orgIPLog.Printf("Failed to close issue #%d in %s: %v", issue.Number, repo, closeErr)
		}
	}
}

// closeExistingOrgPRsByMarker finds all open PRs in repo whose body contains the
// given marker prefix string and closes them. Errors are non-fatal.
func closeExistingOrgPRsByMarker(ctx context.Context, repo, markerPrefix string, verbose bool) {
	prs, err := listOpenOrgItems(ctx, repo, "pulls")
	if err != nil {
		orgIPLog.Printf("Failed to list open PRs in %s: %v", repo, err)
		return
	}

	for _, pr := range prs {
		if !strings.Contains(pr.Body, markerPrefix) {
			continue
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(
				fmt.Sprintf("Closing outdated PR #%d in %s", pr.Number, repo),
			))
		}
		if _, closeErr := runOrgAPI(ctx, getHostFromOriginRemote(), "Closing outdated PR...",
			"--method", "PATCH",
			fmt.Sprintf("/repos/%s/pulls/%d", repo, pr.Number),
			"-f", "state=closed",
		); closeErr != nil {
			orgIPLog.Printf("Failed to close PR #%d in %s: %v", pr.Number, repo, closeErr)
		}
	}
}

// addLabelToOrgPR adds a label to a PR identified by URL using gh pr edit.
// Errors are non-fatal and emitted as warnings.
func addLabelToOrgPR(ctx context.Context, prURL, label string, verbose bool) {
	remoteHost := getHostFromOriginRemote()
	if _, err := workflow.RunGHContextWithHost(ctx, "Adding label to PR...", remoteHost, "pr", "edit", prURL, "--add-label", label); err != nil {
		orgIPLog.Printf("Failed to add label %q to PR %s: %v", label, prURL, err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
				fmt.Sprintf("Failed to add label %q to PR (non-fatal): %v", label, err),
			))
		}
	}
}

// createOrgIssue creates a GitHub issue with the given title, body, and label. If
// creating with the label fails (e.g. label does not exist), it retries once without
// the label so the issue is always created.
func createOrgIssue(ctx context.Context, repo, title, body, label string) error {
	endpoint := fmt.Sprintf("/repos/%s/issues", repo)
	remoteHost := getHostFromOriginRemote()
	output, err := runOrgAPICombined(ctx, remoteHost, "Creating issue...",
		"--method", "POST",
		endpoint,
		"-f", "title="+title,
		"-f", "body="+body,
		"-f", "labels[]="+label,
	)
	if err == nil {
		return nil
	}
	if !isLabelValidationError(output, err) {
		return err
	}
	// Label may not exist; retry without it so the issue is always created.
	orgIPLog.Printf("Failed to create issue with label %q, retrying without: %v", label, err)
	_, err = runOrgAPI(ctx, remoteHost, "Creating issue...",
		"--method", "POST",
		endpoint,
		"-f", "title="+title,
		"-f", "body="+body,
	)
	return err
}
