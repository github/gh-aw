package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var linkSubIssueLog = logger.New("workflow:link_sub_issue")

// LinkSubIssueConfig holds configuration for linking issues as sub-issues from agent output
type LinkSubIssueConfig struct {
	BaseSafeOutputConfig   `yaml:",inline"`
	SafeOutputTargetConfig `yaml:",inline"`
	ParentRequiredLabels   []string `yaml:"parent-required-labels,omitempty"` // Required labels the parent issue must have
	ParentTitlePrefix      string   `yaml:"parent-title-prefix,omitempty"`    // Required title prefix for parent issue
	SubRequiredLabels      []string `yaml:"sub-required-labels,omitempty"`    // Required labels the sub-issue must have
	SubTitlePrefix         string   `yaml:"sub-title-prefix,omitempty"`       // Required title prefix for sub-issue
}

// parseLinkSubIssueConfig handles link-sub-issue configuration
func (c *Compiler) parseLinkSubIssueConfig(outputMap map[string]any) *LinkSubIssueConfig {
	linkSubIssueLog.Print("Parsing link-sub-issue configuration")
	if configData, exists := outputMap["link-sub-issue"]; exists {
		linkSubIssueConfig := &LinkSubIssueConfig{}

		if configMap, ok := configData.(map[string]any); ok {
			linkSubIssueLog.Print("Found link-sub-issue config map")

			// Parse target config (target-repo) with validation
			targetConfig, isInvalid := ParseTargetConfig(configMap)
			if isInvalid {
				return nil // Invalid configuration (e.g., wildcard target-repo), return nil to cause validation error
			}
			linkSubIssueConfig.SafeOutputTargetConfig = targetConfig

			// Parse common base fields with default max of 5
			c.parseBaseSafeOutputConfig(configMap, &linkSubIssueConfig.BaseSafeOutputConfig, 5)

			// Parse parent-required-labels
			linkSubIssueConfig.ParentRequiredLabels = ParseStringArrayFromConfig(configMap, "parent-required-labels", linkSubIssueLog)

			// Parse parent-title-prefix
			linkSubIssueConfig.ParentTitlePrefix = extractStringFromMap(configMap, "parent-title-prefix", linkSubIssueLog)

			// Parse sub-required-labels
			linkSubIssueConfig.SubRequiredLabels = ParseStringArrayFromConfig(configMap, "sub-required-labels", linkSubIssueLog)

			// Parse sub-title-prefix
			linkSubIssueConfig.SubTitlePrefix = extractStringFromMap(configMap, "sub-title-prefix", linkSubIssueLog)

			linkSubIssueLog.Printf("Parsed link-sub-issue config: max=%d, parent_labels=%d, sub_labels=%d, target_repo=%s",
				linkSubIssueConfig.Max, len(linkSubIssueConfig.ParentRequiredLabels),
				len(linkSubIssueConfig.SubRequiredLabels), linkSubIssueConfig.TargetRepoSlug)
		} else {
			// If configData is nil or not a map, still set the default max
			linkSubIssueConfig.Max = 5
		}

		return linkSubIssueConfig
	}

	return nil
}
