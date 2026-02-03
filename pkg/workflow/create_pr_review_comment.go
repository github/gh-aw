package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var createPRReviewCommentLog = logger.New("workflow:create_pr_review_comment")

// CreatePullRequestReviewCommentsConfig holds configuration for creating GitHub pull request review comments from agent output
type CreatePullRequestReviewCommentsConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Side                 string   `yaml:"side,omitempty"`          // Side of the diff: "LEFT" or "RIGHT" (default: "RIGHT")
	Target               string   `yaml:"target,omitempty"`        // Target for comments: "triggering" (default), "*" (any PR), or explicit PR number
	TargetRepoSlug       string   `yaml:"target-repo,omitempty"`   // Target repository in format "owner/repo" for cross-repository PR review comments
	AllowedRepos         []string `yaml:"allowed-repos,omitempty"` // List of additional repositories that PR review comments can be added to (additionally to the target-repo)
}

// buildCreateOutputPullRequestReviewCommentJob creates the create_pr_review_comment job
//
//nolint:unused // Only used in integration tests
func (c *Compiler) buildCreateOutputPullRequestReviewCommentJob(data *WorkflowData, mainJobName string) (*Job, error) {
	createPRReviewCommentLog.Printf("Building PR review comment job: main_job=%s", mainJobName)

	if data.SafeOutputs == nil || data.SafeOutputs.CreatePullRequestReviewComments == nil {
		return nil, fmt.Errorf("safe-outputs.create-pull-request-review-comment configuration is required")
	}

	// Log configuration details
	side := data.SafeOutputs.CreatePullRequestReviewComments.Side
	target := data.SafeOutputs.CreatePullRequestReviewComments.Target
	createPRReviewCommentLog.Printf("Configuration: side=%s, target=%s", side, target)

	// Build custom environment variables specific to create-pull-request-review-comment
	var customEnvVars []string

	// Pass the side configuration
	if data.SafeOutputs.CreatePullRequestReviewComments.Side != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_PR_REVIEW_COMMENT_SIDE: %q\n", data.SafeOutputs.CreatePullRequestReviewComments.Side))
	}
	// Pass the target configuration
	if data.SafeOutputs.CreatePullRequestReviewComments.Target != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_PR_REVIEW_COMMENT_TARGET: %q\n", data.SafeOutputs.CreatePullRequestReviewComments.Target))
	}

	// Add standard environment variables (metadata + staged/target repo)
	customEnvVars = append(customEnvVars, c.buildStandardSafeOutputEnvVars(data, data.SafeOutputs.CreatePullRequestReviewComments.TargetRepoSlug)...)

	// Create outputs for the job
	outputs := map[string]string{
		"review_comment_id":  "${{ steps.create_pr_review_comment.outputs.review_comment_id }}",
		"review_comment_url": "${{ steps.create_pr_review_comment.outputs.review_comment_url }}",
	}

	var jobCondition = BuildSafeOutputType("create_pull_request_review_comment")
	if data.SafeOutputs.CreatePullRequestReviewComments != nil && data.SafeOutputs.CreatePullRequestReviewComments.Target == "" {
		issueWithPR := &AndNode{
			Left:  &ExpressionNode{Expression: "github.event.issue.number"},
			Right: &ExpressionNode{Expression: "github.event.issue.pull_request"},
		}
		eventCondition := BuildOr(
			issueWithPR,
			BuildPropertyAccess("github.event.pull_request"),
		)
		jobCondition = BuildAnd(jobCondition, eventCondition)
	}

	// Use the shared builder function to create the job
	return c.buildSafeOutputJob(data, SafeOutputJobConfig{
		JobName:        "create_pr_review_comment",
		StepName:       "Create PR Review Comment",
		StepID:         "create_pr_review_comment",
		MainJobName:    mainJobName,
		CustomEnvVars:  customEnvVars,
		Script:         getCreatePRReviewCommentScript(),
		Permissions:    NewPermissionsContentsReadPRWrite(),
		Outputs:        outputs,
		Condition:      jobCondition,
		Token:          data.SafeOutputs.CreatePullRequestReviewComments.GitHubToken,
		TargetRepoSlug: data.SafeOutputs.CreatePullRequestReviewComments.TargetRepoSlug,
	})
}

// parsePullRequestReviewCommentsConfig handles create-pull-request-review-comment configuration
func (c *Compiler) parsePullRequestReviewCommentsConfig(outputMap map[string]any) *CreatePullRequestReviewCommentsConfig {
	if _, exists := outputMap["create-pull-request-review-comment"]; !exists {
		createPRReviewCommentLog.Printf("Configuration not found")
		return nil
	}

	createPRReviewCommentLog.Printf("Parsing PR review comment configuration")

	configData := outputMap["create-pull-request-review-comment"]
	prReviewCommentsConfig := &CreatePullRequestReviewCommentsConfig{Side: "RIGHT"} // Default side is RIGHT

	if configMap, ok := configData.(map[string]any); ok {
		// Parse side
		if side, exists := configMap["side"]; exists {
			if sideStr, ok := side.(string); ok {
				// Validate side value
				if sideStr == "LEFT" || sideStr == "RIGHT" {
					prReviewCommentsConfig.Side = sideStr
				}
			}
		}

		// Parse target
		if target, exists := configMap["target"]; exists {
			if targetStr, ok := target.(string); ok {
				prReviewCommentsConfig.Target = targetStr
			}
		}

		// Parse target-repo using shared helper with validation
		targetRepoSlug, isInvalid := parseTargetRepoWithValidation(configMap)
		if isInvalid {
			return nil // Invalid configuration, return nil to cause validation error
		}
		prReviewCommentsConfig.TargetRepoSlug = targetRepoSlug

		// Parse common base fields with default max of 10
		c.parseBaseSafeOutputConfig(configMap, &prReviewCommentsConfig.BaseSafeOutputConfig, 10)
	} else {
		// If configData is nil or not a map (e.g., "create-pull-request-review-comment:" with no value),
		// still set the default max
		prReviewCommentsConfig.Max = 10
	}

	return prReviewCommentsConfig
}
