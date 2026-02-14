package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var resolvePRReviewThreadLog = logger.New("workflow:resolve_pr_review_thread")

// ResolvePullRequestReviewThreadConfig holds configuration for resolving PR review threads
type ResolvePullRequestReviewThreadConfig struct {
	BaseSafeOutputConfig   `yaml:",inline"`
	SafeOutputTargetConfig `yaml:",inline"`
}

// parseResolvePullRequestReviewThreadConfig handles resolve-pull-request-review-thread configuration
func (c *Compiler) parseResolvePullRequestReviewThreadConfig(outputMap map[string]any) *ResolvePullRequestReviewThreadConfig {
	if configData, exists := outputMap["resolve-pull-request-review-thread"]; exists {
		resolvePRReviewThreadLog.Print("Parsing resolve-pull-request-review-thread configuration")
		config := &ResolvePullRequestReviewThreadConfig{}

		if configMap, ok := configData.(map[string]any); ok {
			resolvePRReviewThreadLog.Print("Found resolve-pull-request-review-thread config map")

			// Parse target config (target, target-repo, allowed-repos) with validation
			targetConfig, isInvalid := ParseTargetConfig(configMap)
			if isInvalid {
				return nil // Invalid configuration (e.g., wildcard target-repo), return nil to cause validation error
			}
			config.SafeOutputTargetConfig = targetConfig

			// Parse common base fields with default max of 10
			c.parseBaseSafeOutputConfig(configMap, &config.BaseSafeOutputConfig, 10)

			resolvePRReviewThreadLog.Printf("Parsed resolve-pull-request-review-thread config: max=%d, target_repo=%s",
				config.Max, config.TargetRepoSlug)
		} else {
			// If configData is nil or not a map, still set the default max
			config.Max = 10
		}

		return config
	}

	return nil
}
