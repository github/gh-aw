package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/github/gh-aw/pkg/workflow/lint"
)

var pullRequestTargetGuidedErrorCodemodLog = logger.New("cli:codemod_pull_request_target_guided_error")

func getPullRequestTargetGuidedErrorCodemod() Codemod {
	return Codemod{
		ID:           "pull-request-target-guided-error",
		Name:         "Detect remaining unsafe pull_request_target configurations (manual fix required)",
		Description:  "Reports unsafe pull_request_target checkouts or read/all roles that remain after automatic fixes, with manual remediation options; never acknowledges risk automatically.",
		IntroducedIn: "1.5.0",
		Guided:       true,
		Apply:        applyPullRequestTargetGuidedCheck,
	}
}

func applyPullRequestTargetGuidedCheck(content string, frontmatter map[string]any) (string, bool, error) {
	if isFrontmatterStrictFalse(frontmatter) || !hasPullRequestTargetTrigger(frontmatter) {
		return content, false, nil
	}
	document, err := aw.Parse(content)
	if err != nil {
		return content, false, fmt.Errorf("parsing pull_request_target workflow: %w", err)
	}
	runner, err := lint.New(lint.PullRequestTargetCheckoutRule(), lint.PullRequestTargetRolesRule())
	if err != nil {
		return content, false, err
	}
	diagnostics, err := runner.Run(context.Background(), lint.Input{Document: document})
	if err != nil {
		return content, false, fmt.Errorf("checking pull_request_target configuration: %w", err)
	}
	var problems []string
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity != lint.Error {
			continue
		}
		switch diagnostic.RuleID {
		case "pull-request-target-checkout":
			problems = append(problems, "Checkout is not restricted to trusted code. Set 'checkout: false' if repository checkout is unnecessary, "+
				"or use a trusted base checkout such as 'checkout: {ref: '${{ github.event.pull_request.base.sha }}'}'. "+
				"For a fixed external checkout, manually review an exact repository/ref pair in on.pull_request_target.allowed-checkouts.")
		case "pull-request-target-roles":
			problems = append(problems, "Untrusted contributors can trigger the agent with access to repository secrets. "+
				"Restrict on.roles to trusted repository roles, for example 'on.roles: [admin, maintainer, write]'.")
		}
	}
	if len(problems) == 0 {
		return content, false, nil
	}
	pullRequestTargetGuidedErrorCodemodLog.Print("Remaining pull_request_target configuration requires a manual fix")
	return content, false, fmt.Errorf("unsafe pull_request_target configuration. Manual fix required: %s "+
		"Alternatively, switch to pull_request after reviewing the workflow's permission and secret requirements. "+
		"on.pull_request_target.acknowledge-risk: true suppresses only the trigger-risk warning; "+
		"it does not bypass checkout or role restrictions and is never added automatically. "+
		"No changes to this workflow are written until manual blockers are resolved. "+
		"See: https://securitylab.github.com/resources/github-actions-preventing-pwn-requests/",
		strings.Join(problems, " "))
}
