package workflow

import "github.com/github/gh-aw/pkg/logger"

var safeOutputsConfigGenLog = logger.New("workflow:safe_outputs_config_generation_helpers")

// ========================================
// Safe Output Configuration Generation Helpers
// ========================================
//
// This file contains helper functions to reduce duplication in safe output
// configuration generation. These helpers extract common patterns for:
// - Generating max value configs with defaults
// - Generating configs with allowed fields (labels, repos, etc.)
// - Generating configs with optional target fields
//
// The goal is to make generateSafeOutputsConfig more maintainable by
// extracting repetitive code patterns into reusable functions.

// generateMaxConfig creates a simple config map with just a max value
func generateMaxConfig(max int, defaultMax int) map[string]any {
	config := make(map[string]any)
	maxValue := defaultMax
	if max > 0 {
		maxValue = max
	}
	config["max"] = maxValue
	return config
}

// generateMaxWithAllowedLabelsConfig creates a config with max and optional allowed_labels
func generateMaxWithAllowedLabelsConfig(max int, defaultMax int, allowedLabels []string) map[string]any {
	config := generateMaxConfig(max, defaultMax)
	if len(allowedLabels) > 0 {
		config["allowed_labels"] = allowedLabels
	}
	return config
}

// generateMaxWithTargetConfig creates a config with max and optional target field
func generateMaxWithTargetConfig(max int, defaultMax int, target string) map[string]any {
	config := make(map[string]any)
	if target != "" {
		config["target"] = target
	}
	maxValue := defaultMax
	if max > 0 {
		maxValue = max
	}
	config["max"] = maxValue
	return config
}

// generateMaxWithAllowedConfig creates a config with max and optional allowed list
func generateMaxWithAllowedConfig(max int, defaultMax int, allowed []string) map[string]any {
	config := generateMaxConfig(max, defaultMax)
	if len(allowed) > 0 {
		config["allowed"] = allowed
	}
	return config
}

// generateMaxWithRequiredFieldsConfig creates a config with max and optional required filter fields
func generateMaxWithRequiredFieldsConfig(max int, defaultMax int, requiredLabels []string, requiredTitlePrefix string) map[string]any {
	config := generateMaxConfig(max, defaultMax)
	if len(requiredLabels) > 0 {
		config["required_labels"] = requiredLabels
	}
	if requiredTitlePrefix != "" {
		config["required_title_prefix"] = requiredTitlePrefix
	}
	return config
}

// generateMaxWithDiscussionFieldsConfig creates a config with discussion-specific filter fields
func generateMaxWithDiscussionFieldsConfig(max int, defaultMax int, requiredCategory string, requiredLabels []string, requiredTitlePrefix string) map[string]any {
	config := generateMaxConfig(max, defaultMax)
	if requiredCategory != "" {
		config["required_category"] = requiredCategory
	}
	if len(requiredLabels) > 0 {
		config["required_labels"] = requiredLabels
	}
	if requiredTitlePrefix != "" {
		config["required_title_prefix"] = requiredTitlePrefix
	}
	return config
}

// generateMaxWithReviewersConfig creates a config with max and optional reviewers list
func generateMaxWithReviewersConfig(max int, defaultMax int, reviewers []string) map[string]any {
	config := generateMaxConfig(max, defaultMax)
	if len(reviewers) > 0 {
		config["reviewers"] = reviewers
	}
	return config
}

// generateAssignToAgentConfig creates a config with optional max, default_agent, target, and allowed
func generateAssignToAgentConfig(max int, defaultMax int, defaultAgent string, target string, allowed []string) map[string]any {
	if safeOutputsConfigGenLog.Enabled() {
		safeOutputsConfigGenLog.Printf("Generating assign-to-agent config: max=%d, defaultMax=%d, defaultAgent=%s, target=%s, allowed_count=%d",
			max, defaultMax, defaultAgent, target, len(allowed))
	}
	config := make(map[string]any)

	// Apply default max if max is not specified
	maxValue := defaultMax
	if max > 0 {
		maxValue = max
	}
	config["max"] = maxValue

	if defaultAgent != "" {
		config["default_agent"] = defaultAgent
	}
	if target != "" {
		config["target"] = target
	}
	if len(allowed) > 0 {
		config["allowed"] = allowed
	}
	return config
}

// generatePullRequestConfig creates a config with allowed_labels, allow_empty, auto_merge, and expires
func generatePullRequestConfig(allowedLabels []string, allowEmpty bool, autoMerge bool, expires int) map[string]any {
	safeOutputsConfigGenLog.Printf("Generating pull request config: allowEmpty=%t, autoMerge=%t, expires=%d, labels_count=%d",
		allowEmpty, autoMerge, expires, len(allowedLabels))
	config := make(map[string]any)
	// Note: max is always 1 for pull requests, not configurable
	if len(allowedLabels) > 0 {
		config["allowed_labels"] = allowedLabels
	}
	// Pass allow_empty flag to MCP server so it can skip patch generation
	if allowEmpty {
		config["allow_empty"] = true
	}
	// Pass auto_merge flag to enable auto-merge for the pull request
	if autoMerge {
		config["auto_merge"] = true
	}
	// Pass expires to configure pull request expiration
	if expires > 0 {
		config["expires"] = expires
	}
	return config
}

// generateHideCommentConfig creates a config with max and optional allowed_reasons
func generateHideCommentConfig(max int, defaultMax int, allowedReasons []string) map[string]any {
	config := generateMaxConfig(max, defaultMax)
	if len(allowedReasons) > 0 {
		config["allowed_reasons"] = allowedReasons
	}
	return config
}
