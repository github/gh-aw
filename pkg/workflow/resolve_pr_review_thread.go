package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var resolvePRReviewThreadLog = logger.New("workflow:resolve_pr_review_thread")

// ResolvePullRequestReviewThreadConfig holds configuration for resolving PR review threads.
// Resolution is scoped to the triggering PR only — the JavaScript handler validates
// that each thread belongs to the triggering pull request before resolving it.
type ResolvePullRequestReviewThreadConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
}

// parseResolvePullRequestReviewThreadConfig handles resolve-pull-request-review-thread configuration
func (c *Compiler) parseResolvePullRequestReviewThreadConfig(outputMap map[string]any) *ResolvePullRequestReviewThreadConfig {
	if configData, exists := outputMap["resolve-pull-request-review-thread"]; exists {
		resolvePRReviewThreadLog.Print("Parsing resolve-pull-request-review-thread configuration")
		config := &ResolvePullRequestReviewThreadConfig{}

		if configMap, ok := configData.(map[string]any); ok {
			resolvePRReviewThreadLog.Print("Found resolve-pull-request-review-thread config map")

			// Parse common base fields with default max of 10
			c.parseBaseSafeOutputConfig(configMap, &config.BaseSafeOutputConfig, 10)

			resolvePRReviewThreadLog.Printf("Parsed resolve-pull-request-review-thread config: max=%d", config.Max)
		} else {
			// If configData is nil or not a map, still set the default max
			config.Max = 10
		}

		return config
	}

	return nil
}
