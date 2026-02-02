package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var assignToUserLog = logger.New("workflow:assign_to_user")

// AssignToUserConfig holds configuration for assigning users to issues from agent output
type AssignToUserConfig struct {
	BaseSafeOutputConfig   `yaml:",inline"`
	SafeOutputTargetConfig `yaml:",inline"`
	Allowed                []string `yaml:"allowed,omitempty"` // Optional list of allowed usernames. If omitted, any users are allowed.
}

// parseAssignToUserConfig handles assign-to-user configuration
func (c *Compiler) parseAssignToUserConfig(outputMap map[string]any) *AssignToUserConfig {
	// Check if the key exists
	if _, exists := outputMap["assign-to-user"]; !exists {
		return nil
	}

	assignToUserLog.Print("Parsing assign-to-user configuration")

	// Unmarshal into typed config struct
	var config AssignToUserConfig
	if err := unmarshalConfig(outputMap, "assign-to-user", &config, assignToUserLog); err != nil {
		assignToUserLog.Printf("Failed to unmarshal config: %v", err)
		// For backward compatibility, use defaults
		assignToUserLog.Print("Using default configuration")
		config = AssignToUserConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
		}
	}

	// Set default max if not specified
	if config.Max == 0 {
		config.Max = 1
	}

	assignToUserLog.Printf("Parsed configuration: allowed_count=%d, target=%s", len(config.Allowed), config.Target)

	return &config
}
