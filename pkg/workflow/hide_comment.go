package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var hideCommentLog = logger.New("workflow:hide_comment")

// HideCommentConfig holds configuration for hiding comments from agent output
type HideCommentConfig struct {
	BaseSafeOutputConfig   `yaml:",inline"`
	SafeOutputTargetConfig `yaml:",inline"`
	AllowedReasons         []string `yaml:"allowed-reasons,omitempty"` // List of allowed reasons for hiding comments (default: all reasons allowed)
}

// parseHideCommentConfig handles hide-comment configuration
func (c *Compiler) parseHideCommentConfig(outputMap map[string]any) *HideCommentConfig {
	hideCommentLog.Print("Parsing hide-comment configuration")
	if configData, exists := outputMap["hide-comment"]; exists {
		hideCommentConfig := &HideCommentConfig{}

		if configMap, ok := configData.(map[string]any); ok {
			hideCommentLog.Print("Found hide-comment config map")

			// Parse target config (target-repo) with validation
			targetConfig, isInvalid := ParseTargetConfig(configMap)
			if isInvalid {
				return nil // Invalid configuration (e.g., wildcard target-repo), return nil to cause validation error
			}
			hideCommentConfig.SafeOutputTargetConfig = targetConfig

			// Parse allowed-reasons
			if allowedReasons, exists := configMap["allowed-reasons"]; exists {
				if reasonsArray, ok := allowedReasons.([]any); ok {
					for _, reason := range reasonsArray {
						if reasonStr, ok := reason.(string); ok {
							hideCommentConfig.AllowedReasons = append(hideCommentConfig.AllowedReasons, reasonStr)
						}
					}
				}
			}

			// Parse common base fields with default max of 5
			c.parseBaseSafeOutputConfig(configMap, &hideCommentConfig.BaseSafeOutputConfig, 5)

			hideCommentLog.Printf("Parsed hide-comment config: max=%d, target_repo=%s",
				hideCommentConfig.Max, hideCommentConfig.TargetRepoSlug)
		} else {
			// If configData is nil or not a map, still set the default max
			hideCommentConfig.Max = 5
		}

		return hideCommentConfig
	}

	return nil
}
