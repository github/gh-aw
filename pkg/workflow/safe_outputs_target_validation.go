package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var safeOutputsTargetValidationLog = logger.New("workflow:safe_outputs_target_validation")

// validateSafeOutputsTarget validates target fields in all safe-outputs configurations
// Valid target values:
//   - "" (empty/default) - uses "triggering" behavior
//   - "triggering" - targets the triggering issue/PR/discussion
//   - "*" - targets any item specified in the output
//   - A positive integer as a string (e.g., "123")
//   - A GitHub Actions expression (e.g., "${{ github.event.issue.number }}")
func validateSafeOutputsTarget(config *SafeOutputsConfig) error {
	if config == nil {
		return nil
	}

	safeOutputsTargetValidationLog.Print("Validating safe-outputs target fields")

	// List of configs to validate - each with a name for error messages
	type targetConfig struct {
		name   string
		target string
	}

	var configs []targetConfig

	// Collect all target fields from various safe-output configurations
	if config.UpdateIssues != nil {
		configs = append(configs, targetConfig{"update-issue", config.UpdateIssues.Target})
	}
	if config.UpdateDiscussions != nil {
		configs = append(configs, targetConfig{"update-discussion", config.UpdateDiscussions.Target})
	}
	if config.UpdatePullRequests != nil {
		configs = append(configs, targetConfig{"update-pull-request", config.UpdatePullRequests.Target})
	}
	if config.CloseIssues != nil {
		configs = append(configs, targetConfig{"close-issue", config.CloseIssues.Target})
	}
	if config.CloseDiscussions != nil {
		configs = append(configs, targetConfig{"close-discussion", config.CloseDiscussions.Target})
	}
	if config.ClosePullRequests != nil {
		configs = append(configs, targetConfig{"close-pull-request", config.ClosePullRequests.Target})
	}
	if config.AddLabels != nil {
		configs = append(configs, targetConfig{"add-labels", config.AddLabels.Target})
	}
	if config.RemoveLabels != nil {
		configs = append(configs, targetConfig{"remove-labels", config.RemoveLabels.Target})
	}
	if config.AddReviewer != nil {
		configs = append(configs, targetConfig{"add-reviewer", config.AddReviewer.Target})
	}
	if config.AssignMilestone != nil {
		configs = append(configs, targetConfig{"assign-milestone", config.AssignMilestone.Target})
	}
	if config.AssignToAgent != nil {
		configs = append(configs, targetConfig{"assign-to-agent", config.AssignToAgent.Target})
	}
	if config.AssignToUser != nil {
		configs = append(configs, targetConfig{"assign-to-user", config.AssignToUser.Target})
	}
	if config.LinkSubIssue != nil {
		configs = append(configs, targetConfig{"link-sub-issue", config.LinkSubIssue.Target})
	}
	if config.HideComment != nil {
		configs = append(configs, targetConfig{"hide-comment", config.HideComment.Target})
	}
	if config.MarkPullRequestAsReadyForReview != nil {
		configs = append(configs, targetConfig{"mark-pull-request-as-ready-for-review", config.MarkPullRequestAsReadyForReview.Target})
	}
	if config.AddComments != nil {
		configs = append(configs, targetConfig{"add-comment", config.AddComments.Target})
	}
	if config.CreatePullRequestReviewComments != nil {
		configs = append(configs, targetConfig{"create-pull-request-review-comment", config.CreatePullRequestReviewComments.Target})
	}
	if config.PushToPullRequestBranch != nil {
		configs = append(configs, targetConfig{"push-to-pull-request-branch", config.PushToPullRequestBranch.Target})
	}

	// Validate each target field
	for _, cfg := range configs {
		if err := validateTargetValue(cfg.name, cfg.target); err != nil {
			return err
		}
	}

	safeOutputsTargetValidationLog.Printf("Validated %d target fields", len(configs))
	return nil
}

// validateTargetValue validates a single target value
func validateTargetValue(configName, target string) error {
	// Empty or "triggering" are always valid
	if target == "" || target == "triggering" {
		return nil
	}

	// "*" is valid (any item)
	if target == "*" {
		return nil
	}

	// Check if it's a GitHub Actions expression
	if isGitHubExpression(target) {
		safeOutputsTargetValidationLog.Printf("Target for %s is a GitHub Actions expression", configName)
		return nil
	}

	// Check if it's a positive integer
	if stringutil.IsPositiveInteger(target) {
		safeOutputsTargetValidationLog.Printf("Target for %s is a valid number: %s", configName, target)
		return nil
	}

	// Build a helpful suggestion based on the invalid value
	suggestion := ""
	if target == "event" || strings.Contains(target, "github.event") {
		suggestion = "\n\nDid you mean to use \"${{ github.event.issue.number }}\" instead of \"" + target + "\"?"
	}

	// Invalid target value
	return fmt.Errorf(
		"invalid target value for %s: %q\n\nValid target values are:\n  - \"triggering\" (default) - targets the triggering issue/PR/discussion\n  - \"*\" - targets any item specified in the output\n  - A positive integer (e.g., \"123\")\n  - A GitHub Actions expression (e.g., \"${{ github.event.issue.number }}\")%s",
		configName,
		target,
		suggestion,
	)
}

// isGitHubExpression checks if a string is a valid GitHub Actions expression
// A valid expression must have properly balanced ${{ and }} markers
func isGitHubExpression(s string) bool {
	// Must contain both opening and closing markers
	if !strings.Contains(s, "${{") || !strings.Contains(s, "}}") {
		return false
	}

	// Basic validation: opening marker must come before closing marker
	openIndex := strings.Index(s, "${{")
	closeIndex := strings.Index(s, "}}")

	// The closing marker must come after the opening marker
	// and there must be something between them
	return openIndex >= 0 && closeIndex > openIndex+3
}
