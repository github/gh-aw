package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var removeLabelsLog = logger.New("workflow:remove_labels")

// RemoveLabelsConfig holds configuration for removing labels from issues/PRs from agent output
type RemoveLabelsConfig struct {
	BaseSafeOutputConfig   `yaml:",inline"`
	SafeOutputTargetConfig `yaml:",inline"`
	Allowed                []string `yaml:"allowed,omitempty"` // Optional list of allowed labels to remove. If omitted, any labels can be removed.
}

// parseRemoveLabelsConfig handles remove-labels configuration
func (c *Compiler) parseRemoveLabelsConfig(outputMap map[string]any) *RemoveLabelsConfig {
	// Check if the key exists
	if _, exists := outputMap["remove-labels"]; !exists {
		return nil
	}

	removeLabelsLog.Print("Parsing remove-labels configuration")

	// Unmarshal into typed config struct
	var config RemoveLabelsConfig
	if err := unmarshalConfig(outputMap, "remove-labels", &config, removeLabelsLog); err != nil {
		removeLabelsLog.Printf("Failed to unmarshal config: %v", err)
		// Handle null case: create empty config (allows any labels)
		removeLabelsLog.Print("Using empty configuration (allows any labels)")
		return &RemoveLabelsConfig{}
	}

	removeLabelsLog.Printf("Parsed configuration: allowed_count=%d, target=%s", len(config.Allowed), config.Target)

	return &config
}
