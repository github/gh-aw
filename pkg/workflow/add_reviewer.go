package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var addReviewerLog = logger.New("workflow:add_reviewer")

// AddReviewerConfig holds configuration for adding reviewers to PRs from agent output
type AddReviewerConfig struct {
	BaseSafeOutputConfig   `yaml:",inline"`
	SafeOutputTargetConfig `yaml:",inline"`
	SafeOutputFilterConfig `yaml:",inline"`
	AllowedReviewers       []string `yaml:"allowed-reviewers,omitempty"`      // Allowed reviewer usernames (preferred)
	AllowedTeamReviewers   []string `yaml:"allowed-team-reviewers,omitempty"` // Allowed team reviewer slugs (preferred)
	Reviewers              []string `yaml:"reviewers,omitempty"`              // Deprecated: use allowed-reviewers
	TeamReviewers          []string `yaml:"team-reviewers,omitempty"`         // Deprecated: use allowed-team-reviewers
}

// parseAddReviewerConfig handles add-reviewer configuration
func (c *Compiler) parseAddReviewerConfig(outputMap map[string]any) *AddReviewerConfig {
	// Check if the key exists
	if _, exists := outputMap["add-reviewer"]; !exists {
		return nil
	}

	// Get config data for pre-processing before YAML unmarshaling
	configData, _ := typeutil.LookupMap(outputMap, "add-reviewer")

	// Pre-process reviewers fields to convert single string to array BEFORE unmarshaling
	if configData != nil {
		for _, key := range []string{"allowed-reviewers", "reviewers"} {
			if val, exists := configData[key]; exists {
				if str, ok := val.(string); ok {
					configData[key] = []string{str}
				}
			}
		}
		for _, key := range []string{"allowed-team-reviewers", "team-reviewers"} {
			if val, exists := configData[key]; exists {
				if str, ok := val.(string); ok {
					configData[key] = []string{str}
				}
			}
		}
	}

	// Pre-process templatable int fields
	if err := preprocessIntFieldAsString(configData, "max", addReviewerLog); err != nil {
		addReviewerLog.Printf("Invalid max value: %v", err)
		return nil
	}

	config := parseConfigScaffoldWithPostProcess(outputMap, "add-reviewer", addReviewerLog,
		func(err error) *AddReviewerConfig {
			addReviewerLog.Printf("Failed to unmarshal config: %v", err)
			// For backward compatibility, handle nil/empty config
			return &AddReviewerConfig{}
		},
		func(config *AddReviewerConfig) {
			// Set default max if not specified
			if config.Max == nil {
				config.Max = defaultIntStr(3)
			}

			// Fallback from deprecated field names to preferred names
			if len(config.AllowedReviewers) == 0 {
				config.AllowedReviewers = config.Reviewers
			}
			if len(config.AllowedTeamReviewers) == 0 {
				config.AllowedTeamReviewers = config.TeamReviewers
			}

			addReviewerLog.Printf("Parsed add-reviewer config: allowed_reviewers=%d, target=%s", len(config.AllowedReviewers), config.Target)
		})

	return config
}
