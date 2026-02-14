package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var replyToPRReviewCommentLog = logger.New("workflow:reply_to_pr_review_comment")

// ReplyToPullRequestReviewCommentConfig holds configuration for replying to PR review comments.
// Uses the GitHub REST API to create reply comments on existing review comment threads.
type ReplyToPullRequestReviewCommentConfig struct {
	BaseSafeOutputConfig   `yaml:",inline"`
	SafeOutputTargetConfig `yaml:",inline"`
	Footer                 *bool `yaml:"footer,omitempty"` // Whether to add AI-generated footer to replies
}

// parseReplyToPullRequestReviewCommentConfig handles reply-to-pull-request-review-comment configuration
func (c *Compiler) parseReplyToPullRequestReviewCommentConfig(outputMap map[string]any) *ReplyToPullRequestReviewCommentConfig {
	if configData, exists := outputMap["reply-to-pull-request-review-comment"]; exists {
		replyToPRReviewCommentLog.Print("Parsing reply-to-pull-request-review-comment configuration")
		config := &ReplyToPullRequestReviewCommentConfig{}

		if configMap, ok := configData.(map[string]any); ok {
			replyToPRReviewCommentLog.Print("Found reply-to-pull-request-review-comment config map")

			// Parse common base fields with default max of 10
			c.parseBaseSafeOutputConfig(configMap, &config.BaseSafeOutputConfig, 10)

			// Parse target
			if target, exists := configMap["target"]; exists {
				if targetStr, ok := target.(string); ok {
					config.Target = targetStr
				}
			}

			// Parse target-repo using shared helper with validation
			targetRepoSlug, isInvalid := parseTargetRepoWithValidation(configMap)
			if isInvalid {
				return nil // Invalid configuration, return nil to cause validation error
			}
			config.TargetRepoSlug = targetRepoSlug

			// Parse allowed-repos
			if allowedRepos, exists := configMap["allowed-repos"]; exists {
				if repos, ok := allowedRepos.([]any); ok {
					for _, repo := range repos {
						if repoStr, ok := repo.(string); ok {
							config.AllowedRepos = append(config.AllowedRepos, repoStr)
						}
					}
				}
			}

			// Parse footer
			if footer, ok := configMap["footer"].(bool); ok {
				config.Footer = &footer
			}

			replyToPRReviewCommentLog.Printf("Parsed reply-to-pull-request-review-comment config: max=%d", config.Max)
		} else {
			// If configData is nil or not a map, still set the default max
			config.Max = 10
		}

		return config
	}

	return nil
}
