package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var deployOrgLog = logger.New("cli:deploy_org")

var searchOrgDeployReposFn = searchOrgLockWorkflowRepos
var runDeployForTargetRepoFn = runDeploy

func runDeployForOrg(ctx context.Context, org string, repoGlobs []string, workflows []string, addOpts AddOptions, coolDown time.Duration, yes bool, verbose bool) error {
	deployOrgLog.Printf("Running org deploy: org=%s, globs=%d, workflows=%d, coolDown=%s", org, len(repoGlobs), len(workflows), coolDown)
	const createPR = true
	const createIssue = false
	return runCommandForOrg(ctx, org, repoGlobs, orgRunCallbacks{
		AutoYes: yes,
		SearchFn: func(ctx context.Context, org string, verbose bool) ([]string, error) {
			return searchOrgDeployReposFn(ctx, org, verbose)
		},
		ReportFn: renderOrgDeployReport,
		ApplyFn: func(ctx context.Context, preview orgRepoPreview, v bool) error {
			return runDeployForTargetRepoFn(ctx, preview.Repo, workflows, addOpts, coolDown)
		},
		DiscoveringMsg:  "Discovering repositories in " + org + " with agentic workflows...",
		NoReposMsg:      "No repositories found with agentic workflows",
		ApplyLabel:      "Deploying",
		AllFailApplyMsg: "failed to deploy workflows to any repository",
	}, createPR, createIssue, verbose)
}

func renderOrgDeployReport(results []orgRepoPreview, applying bool) {
	deployOrgLog.Printf("Rendering deploy report: repos=%d, applying=%v", len(results), applying)
	if applying {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Repositories selected for deploy (%d):", len(results))))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Dry-run preview of deploy pull requests:"))
	}
	for _, result := range results {
		fmt.Fprintf(os.Stderr, "- %s\n", result.Repo)
	}
}
