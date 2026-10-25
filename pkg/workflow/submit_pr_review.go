package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var submitPRReviewLog = logger.New("workflow:submit_pr_review")

// SubmitPullRequestReviewConfig holds configuration for submitting a GitHub pull request review
// This works in conjunction with create-pull-request-review-comment: all review comments
// are collected and submitted as a single PR review with the configured event type.
// If this safe output type is not configured, review comments default to event: "COMMENT".
type SubmitPullRequestReviewConfig struct {
	BaseSafeOutputConfig   `yaml:",inline"`
	SafeOutputTargetConfig `yaml:",inline"`
	SafeOutputFilterConfig `yaml:",inline"`
	AllowedEvents          []string `yaml:"allowed-events,omitempty"`          // Optional list of allowed review event types: APPROVE, COMMENT, REQUEST_CHANGES. If omitted, all event types are allowed.
	SupersedeOlderReviews  bool     `yaml:"supersede-older-reviews,omitempty"` // When true, dismisses older same-workflow REQUEST_CHANGES reviews after a replacement review is posted.
	CommitId               string   `yaml:"commit-id,omitempty"`               // When set, pins the review to this commit SHA instead of the current PR head.
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

		// Parse target config (target, target-repo, allowed-repos)
		// Uses parseTargetRepoWithValidation to disallow wildcard "*" for target-repo
		if targetStr := extractTargetFromMap(configMap, submitPRReviewLog); targetStr != "" {
			config.Target = targetStr
		}

		targetRepoSlug, isInvalid := parseTargetRepoWithValidation(configMap)
		if isInvalid {
			return nil // Invalid configuration, return nil to cause validation error
		}
		config.TargetRepoSlug = targetRepoSlug
		config.AllowedRepos = ParseStringArrayFromConfig(configMap, "allowed-repos", submitPRReviewLog)

		parseSubmitReviewFooter(configMap, config)
		if !parseSubmitReviewAllowedEvents(configMap, config) {
			return nil
		}
		parseSubmitReviewOptions(configMap, config)
		submitPRReviewLog.Printf("Parsed submit-pull-request-review config: max=%d, target=%s, target_repo=%s, allowed_events=%v, supersede_older_reviews=%t", templatableIntValue(config.Max), config.Target, config.TargetRepoSlug, config.AllowedEvents, config.SupersedeOlderReviews)
	} else {
		// If configData is nil or not a map, set the default max.
		config.Max = defaultIntStr(1)
	}
	return config
}

func parseSubmitReviewFooter(configMap map[string]any, config *SubmitPullRequestReviewConfig) {
	// Parse footer configuration (string: "always"/"none"/"if-body", or bool for backward compat)
	if footer, exists := configMap["footer"]; exists {
		switch f := footer.(type) {
		case string:
			// Validate string values: "always", "none", "if-body"
			if f == "always" || f == "none" || f == "if-body" {
				config.Footer = &f
				submitPRReviewLog.Printf("Footer control: %s", f)
			} else {
				submitPRReviewLog.Printf("Invalid footer value: %s (must be 'always', 'none', or 'if-body')", f)
			}
		case bool:
			// Map boolean to string: true -> "always", false -> "none"
			var footerStr string
			if f {
				footerStr = "always"
			} else {
				footerStr = "none"
			}
			config.Footer = &footerStr
			submitPRReviewLog.Printf("Footer control (mapped from bool): %s", footerStr)
		}
	}
}

func parseSubmitReviewAllowedEvents(configMap map[string]any, config *SubmitPullRequestReviewConfig) bool {
	// Parse allowed-events configuration
	if allowedEvents, exists := configMap["allowed-events"]; exists {
		eventsSlice, ok := allowedEvents.([]any)
		if !ok {
			submitPRReviewLog.Printf("Invalid allowed-events configuration: must be a list of review event types")
			return false
		}

		validEvents := map[string]struct {
		}{"APPROVE": {}, "COMMENT": {}, "REQUEST_CHANGES": {}}
		for _, e := range eventsSlice {
			if eventStr, ok := e.(string); ok {
				upper := strings.ToUpper(eventStr)
				if setutil.Contains(validEvents, upper) {
					config.AllowedEvents = append(config.AllowedEvents, upper)
				} else {
					submitPRReviewLog.Printf("Ignoring invalid allowed-events value: %s", eventStr)
				}
			}
		}

		if len(config.AllowedEvents) == 0 {
			submitPRReviewLog.Printf("Invalid allowed-events configuration: at least one valid event type is required when allowed-events is specified")
			return false
		}
	}
	return true
}

func parseSubmitReviewOptions(configMap map[string]any, config *SubmitPullRequestReviewConfig) {
	if supersedeOlderReviews, exists := configMap["supersede-older-reviews"]; exists {
		if supersedeEnabled, ok := supersedeOlderReviews.(bool); ok {
			config.SupersedeOlderReviews = supersedeEnabled
		} else {
			submitPRReviewLog.Printf("Invalid supersede-older-reviews value: must be a boolean")
		}
	}

	if commitId, exists := configMap["commit-id"]; exists {
		if commitIdStr, ok := commitId.(string); ok {
			config.CommitId = commitIdStr
		}
	}

}
