package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var submitPRReviewLog = logger.New("workflow:submit_pr_review")

// SubmitPullRequestReviewConfig holds configuration for submitting a GitHub pull request review
// This works in conjunction with create-pull-request-review-comment: all review comments
// are collected and submitted as a single PR review with the configured event type.
// If this safe output type is not configured, review comments default to event: "COMMENT".
type SubmitPullRequestReviewConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Footer               *bool `yaml:"footer,omitempty"` // Controls whether AI-generated footer is added to the review body. When false, footer is omitted.
}

// parseSubmitPullRequestReviewConfig handles submit-pull-request-review configuration
func (c *Compiler) parseSubmitPullRequestReviewConfig(outputMap map[string]any) *SubmitPullRequestReviewConfig {
	if _, exists := outputMap["submit-pull-request-review"]; !exists {
		submitPRReviewLog.Printf("Configuration not found")
		return nil
	}

	submitPRReviewLog.Printf("Parsing submit PR review configuration")

	configData := outputMap["submit-pull-request-review"]
	config := &SubmitPullRequestReviewConfig{}

	if configMap, ok := configData.(map[string]any); ok {
		// Parse common base fields with default max of 1
		c.parseBaseSafeOutputConfig(configMap, &config.BaseSafeOutputConfig, 1)

		// Parse footer flag
		if footer, exists := configMap["footer"]; exists {
			if footerBool, ok := footer.(bool); ok {
				config.Footer = &footerBool
				submitPRReviewLog.Printf("Footer control: %t", footerBool)
			}
		}
	} else {
		// If configData is nil or not a map, set the default max
		config.Max = 1
	}

	return config
}
