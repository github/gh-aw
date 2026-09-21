package workflow

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var safeOutputsDomainsValidationLog = logger.New("workflow:safe_outputs_domains_validation")

type SafeOutputsURLsPolicy string

const (
	SafeOutputsURLsPolicyAllowedOnly         SafeOutputsURLsPolicy = "allowed-only"
	SafeOutputsURLsPolicyAllowedOrCodeRegion SafeOutputsURLsPolicy = "allowed-or-code-region"
)

// validateSafeOutputsURLs validates the urls policy in safe-outputs.
func validateSafeOutputsURLs(config *SafeOutputsConfig) error {
	if config == nil || config.URLs == "" {
		return nil
	}

	switch config.URLs {
	case SafeOutputsURLsPolicyAllowedOnly, SafeOutputsURLsPolicyAllowedOrCodeRegion:
		return nil
	default:
		return fmt.Errorf(
			"safe-outputs.urls: invalid value %q. Expected one of: %q, %q. Example:\n  safe-outputs:\n    urls: %q",
			config.URLs,
			SafeOutputsURLsPolicyAllowedOnly,
			SafeOutputsURLsPolicyAllowedOrCodeRegion,
			SafeOutputsURLsPolicyAllowedOnly,
		)
	}
}

// validateSafeOutputsAllowedDomains validates the allowed-domains configuration in safe-outputs.
// Supports ecosystem identifiers (e.g., "python", "node", "default-safe-outputs") like network.allowed.
func (c *Compiler) validateSafeOutputsAllowedDomains(config *SafeOutputsConfig) error {
	if config == nil || len(config.AllowedDomains) == 0 {
		return nil
	}

	safeOutputsDomainsValidationLog.Printf("Validating %d allowed domains", len(config.AllowedDomains))

	collector := NewErrorCollector(c.failFast)

	for i, domain := range config.AllowedDomains {
		// Skip ecosystem identifiers - they don't need domain pattern validation
		if isEcosystemIdentifier(domain) {
			safeOutputsDomainsValidationLog.Printf("Skipping ecosystem identifier: %s", domain)
			continue
		}

		if err := validateDomainPattern(domain); err != nil {
			wrappedErr := fmt.Errorf("safe-outputs.allowed-domains[%d]: %w", i, err)
			if returnErr := collector.Add(wrappedErr); returnErr != nil {
				return returnErr // Fail-fast mode
			}
		}
	}

	if err := collector.Error(); err != nil {
		safeOutputsDomainsValidationLog.Printf("Safe outputs allowed domains validation failed: %v", err)
		return err
	}

	safeOutputsDomainsValidationLog.Print("Safe outputs allowed domains validation passed")
	return nil
}

var safeOutputsTargetValidationLog = logger.New("workflow:safe_outputs_target_validation")

type safeOutputTargetConfig struct {
	name   string
	target string
}

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

	configs := collectIssueTargetConfigs(config)
	configs = append(configs, collectPullRequestTargetConfigs(config)...)

	for _, cfg := range configs {
		if err := validateTargetValue(cfg.name, cfg.target); err != nil {
			return err
		}
	}

	safeOutputsTargetValidationLog.Printf("Validated %d target fields", len(configs))
	return nil
}

func collectIssueTargetConfigs(config *SafeOutputsConfig) []safeOutputTargetConfig {
	var configs []safeOutputTargetConfig
	if config.UpdateIssues != nil {
		configs = append(configs, safeOutputTargetConfig{"update-issue", config.UpdateIssues.Target})
	}
	if config.UpdateDiscussions != nil {
		configs = append(configs, safeOutputTargetConfig{"update-discussion", config.UpdateDiscussions.Target})
	}
	if config.CloseIssues != nil {
		configs = append(configs, safeOutputTargetConfig{"close-issue", config.CloseIssues.Target})
	}
	if config.CloseDiscussions != nil {
		configs = append(configs, safeOutputTargetConfig{"close-discussion", config.CloseDiscussions.Target})
	}
	if config.AddLabels != nil {
		configs = append(configs, safeOutputTargetConfig{"add-labels", config.AddLabels.Target})
	}
	if config.RemoveLabels != nil {
		configs = append(configs, safeOutputTargetConfig{"remove-labels", config.RemoveLabels.Target})
	}
	if config.ReplaceLabel != nil {
		configs = append(configs, safeOutputTargetConfig{"replace-label", config.ReplaceLabel.Target})
	}
	if config.AssignMilestone != nil {
		configs = append(configs, safeOutputTargetConfig{"assign-milestone", config.AssignMilestone.Target})
	}
	if config.AssignToAgent != nil {
		configs = append(configs, safeOutputTargetConfig{"assign-to-agent", config.AssignToAgent.Target})
	}
	if config.AssignToUser != nil {
		configs = append(configs, safeOutputTargetConfig{"assign-to-user", config.AssignToUser.Target})
	}
	if config.UnassignFromUser != nil {
		configs = append(configs, safeOutputTargetConfig{"unassign-from-user", config.UnassignFromUser.Target})
	}
	if config.SetIssueType != nil {
		configs = append(configs, safeOutputTargetConfig{"set-issue-type", config.SetIssueType.Target})
	}
	if config.SetIssueField != nil {
		configs = append(configs, safeOutputTargetConfig{"set-issue-field", config.SetIssueField.Target})
	}
	if config.LinkSubIssue != nil {
		configs = append(configs, safeOutputTargetConfig{"link-sub-issue", config.LinkSubIssue.Target})
	}
	if config.HideComment != nil {
		configs = append(configs, safeOutputTargetConfig{"hide-comment", config.HideComment.Target})
	}
	return configs
}

func collectPullRequestTargetConfigs(config *SafeOutputsConfig) []safeOutputTargetConfig {
	var configs []safeOutputTargetConfig
	if config.UpdatePullRequests != nil {
		configs = append(configs, safeOutputTargetConfig{"update-pull-request", config.UpdatePullRequests.Target})
	}
	if config.ClosePullRequests != nil {
		configs = append(configs, safeOutputTargetConfig{"close-pull-request", config.ClosePullRequests.Target})
	}
	if config.AddReviewer != nil {
		configs = append(configs, safeOutputTargetConfig{"add-reviewer", config.AddReviewer.Target})
	}
	if config.MarkPullRequestAsReadyForReview != nil {
		configs = append(configs, safeOutputTargetConfig{"mark-pull-request-as-ready-for-review", config.MarkPullRequestAsReadyForReview.Target})
	}
	if config.DismissPullRequestReview != nil {
		configs = append(configs, safeOutputTargetConfig{"dismiss-pull-request-review", config.DismissPullRequestReview.Target})
	}
	if config.ResolvePullRequestReviewThread != nil {
		configs = append(configs, safeOutputTargetConfig{"resolve-pull-request-review-thread", config.ResolvePullRequestReviewThread.Target})
	}
	if config.AddComments != nil {
		configs = append(configs, safeOutputTargetConfig{"add-comment", config.AddComments.Target})
	}
	if config.CreatePullRequestReviewComments != nil {
		configs = append(configs, safeOutputTargetConfig{"create-pull-request-review-comment", config.CreatePullRequestReviewComments.Target})
	}
	if config.SubmitPullRequestReview != nil {
		configs = append(configs, safeOutputTargetConfig{"submit-pull-request-review", config.SubmitPullRequestReview.Target})
	}
	if config.ReplyToPullRequestReviewComment != nil {
		configs = append(configs, safeOutputTargetConfig{"reply-to-pull-request-review-comment", config.ReplyToPullRequestReviewComment.Target})
	}
	if config.PushToPullRequestBranch != nil {
		configs = append(configs, safeOutputTargetConfig{"push-to-pull-request-branch", config.PushToPullRequestBranch.Target})
	}
	if config.MergePullRequest != nil {
		configs = append(configs, safeOutputTargetConfig{"merge-pull-request", config.MergePullRequest.Target})
	}
	return configs
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
	if containsExpression(target) {
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
	exampleTargetKey := configName
	if idx := strings.LastIndex(exampleTargetKey, "."); idx >= 0 {
		exampleTargetKey = exampleTargetKey[idx+1:]
	}

	// Invalid target value
	return fmt.Errorf(
		"invalid target value for %s: %q. Expected one of: \"triggering\", \"*\", a positive integer like \"123\", or a GitHub Actions expression like \"${{ github.event.issue.number }}\".\n\nExample:\n  safe-outputs:\n    %s:\n      target: \"triggering\"%s",
		configName,
		target,
		exampleTargetKey,
		suggestion,
	)
}

var safeOutputsAllowWorkflowsValidationLog = logger.New("workflow:safe_outputs_allow_workflows_validation")

var safeOutputsMergePullRequestValidationLog = logger.New("workflow:safe_outputs_merge_pull_request_validation")

// validateSafeOutputsMergePullRequest validates merge-pull-request policy configuration.
func validateSafeOutputsMergePullRequest(config *SafeOutputsConfig) error {
	if config == nil || config.MergePullRequest == nil {
		return nil
	}

	c := config.MergePullRequest
	safeOutputsMergePullRequestValidationLog.Print("Validating merge-pull-request policy fields")

	validateNonEmptyStringList := func(field string, values []string) error {
		for i, value := range values {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("safe-outputs.merge-pull-request.%s[%d] cannot be empty. Expected a non-empty string value. Example:\n  safe-outputs:\n    merge-pull-request:\n      %s:\n        - \"safe-to-merge\"", field, i, field)
			}
		}
		return nil
	}

	validateRefGlobList := func(field string, patterns []string) error {
		return validateGlobPatternList(patterns, validateRefGlob, func(i int, pat string, msgs []string) error {
			return fmt.Errorf("invalid glob pattern %q in safe-outputs.merge-pull-request.%s[%d]: %s. Expected a valid ref glob pattern. Example:\n  safe-outputs:\n    merge-pull-request:\n      %s:\n        - \"feature/*\"", pat, field, i, strings.Join(msgs, "; "), field)
		})
	}

	if err := validateNonEmptyStringList("required-labels", c.RequiredLabels); err != nil {
		return err
	}
	if err := validateRefGlobList("allowed-branches", c.AllowedBranches); err != nil {
		return err
	}

	return nil
}

// validateSafeOutputsAllowWorkflows validates that allow-workflows: true requires
// a GitHub App to be configured in safe-outputs.github-app. The workflows permission
// is a GitHub App-only permission and cannot be granted via GITHUB_TOKEN.
func validateSafeOutputsAllowWorkflows(safeOutputs *SafeOutputsConfig) error {
	if safeOutputs == nil {
		return nil
	}

	hasAllowWorkflows := false
	var handlers []string

	if safeOutputs.CreatePullRequests != nil && safeOutputs.CreatePullRequests.AllowWorkflows {
		hasAllowWorkflows = true
		handlers = append(handlers, "create-pull-request")
	}
	if safeOutputs.PushToPullRequestBranch != nil && safeOutputs.PushToPullRequestBranch.AllowWorkflows {
		hasAllowWorkflows = true
		handlers = append(handlers, "push-to-pull-request-branch")
	}

	if !hasAllowWorkflows {
		return nil
	}

	safeOutputsAllowWorkflowsValidationLog.Printf("allow-workflows: true found on: %s", strings.Join(handlers, ", "))

	// Check if GitHub App is configured with required fields
	if safeOutputs.GitHubApp == nil || safeOutputs.GitHubApp.AppID == "" || safeOutputs.GitHubApp.PrivateKey == "" {
		safeOutputsAllowWorkflowsValidationLog.Print("allow-workflows requires github-app but none configured")
		return fmt.Errorf(
			"safe-outputs.%s.allow-workflows: requires a GitHub App to be configured.\n"+
				"The workflows permission is a GitHub App-only permission and cannot be granted via GITHUB_TOKEN.\n\n"+
				"Add a GitHub App configuration to safe-outputs:\n\n"+
				"safe-outputs:\n"+
				"  github-app:\n"+
				"    client-id: ${{ vars.APP_ID }}\n"+
				"    private-key: ${{ secrets.APP_PRIVATE_KEY }}\n"+
				"  %s:\n"+
				"    allow-workflows: true",
			handlers[0], handlers[0],
		)
	}

	safeOutputsAllowWorkflowsValidationLog.Print("allow-workflows validation passed")
	return nil
}

func normalizeApproveWorkflowRunAllowedWorkflowPattern(pattern string) string {
	extension := path.Ext(pattern)
	if strings.EqualFold(extension, ".yaml") {
		return strings.TrimSuffix(pattern, extension) + ".yml"
	}
	return pattern
}

// validateSafeOutputsApproveWorkflowRun requires an explicitly configured external
// token or GitHub App because github.token cannot approve workflow runs for fork PRs.
func validateSafeOutputsApproveWorkflowRun(safeOutputs *SafeOutputsConfig) error {
	if safeOutputs == nil || safeOutputs.ApproveWorkflowRun == nil {
		return nil
	}

	config := safeOutputs.ApproveWorkflowRun
	if len(config.AllowedWorkflows) == 0 {
		return errors.New("safe-outputs.approve-workflow-run: requires a non-empty allowed-workflows list")
	}
	for _, pattern := range config.AllowedWorkflows {
		if path.Base(pattern) != pattern {
			return fmt.Errorf("safe-outputs.approve-workflow-run.allowed-workflows: %q must match a workflow filename, not a path", pattern)
		}
		if _, err := path.Match(normalizeApproveWorkflowRunAllowedWorkflowPattern(pattern), ""); err != nil {
			return fmt.Errorf("safe-outputs.approve-workflow-run.allowed-workflows: invalid wildcard pattern %q: %w", pattern, err)
		}
	}
	if templatableBoolIsTrue(safeOutputs.Staged) || templatableBoolIsTrue(config.Staged) {
		return nil
	}
	if config.GitHubToken != "" || config.GitHubApp != nil || safeOutputs.GitHubToken != "" || safeOutputs.GitHubApp != nil {
		return nil
	}

	return errors.New(
		"safe-outputs.approve-workflow-run: requires an external github-token or github-app because github.token cannot approve workflow runs requiring approval.\n\n" +
			"Example:\n  safe-outputs:\n    approve-workflow-run:\n      github-token: ${{ secrets.APPROVE_WORKFLOW_RUN_TOKEN }}",
	)
}
