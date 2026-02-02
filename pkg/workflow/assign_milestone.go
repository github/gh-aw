package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var assignMilestoneLog = logger.New("workflow:assign_milestone")

// AssignMilestoneConfig holds configuration for assigning milestones to issues from agent output
type AssignMilestoneConfig struct {
	BaseSafeOutputConfig   `yaml:",inline"`
	SafeOutputTargetConfig `yaml:",inline"`
	Allowed                []string `yaml:"allowed,omitempty"` // Optional list of allowed milestone titles or IDs
}

// parseAssignMilestoneConfig handles assign-milestone configuration
func (c *Compiler) parseAssignMilestoneConfig(outputMap map[string]any) *AssignMilestoneConfig {
	// Check if the key exists
	if _, exists := outputMap["assign-milestone"]; !exists {
		assignMilestoneLog.Print("No assign-milestone configuration found")
		return nil
	}

	assignMilestoneLog.Print("Parsing assign-milestone configuration")

	// Unmarshal into typed config struct
	var config AssignMilestoneConfig
	if err := unmarshalConfig(outputMap, "assign-milestone", &config, assignMilestoneLog); err != nil {
		assignMilestoneLog.Printf("Failed to unmarshal config: %v", err)
		// Handle null case: create empty config (allows any milestones)
		assignMilestoneLog.Print("Null milestone config, allowing any milestones")
		return &AssignMilestoneConfig{}
	}

	assignMilestoneLog.Printf("Parsed milestone config: target=%s, allowed_count=%d",
		config.Target, len(config.Allowed))

	return &config
}
