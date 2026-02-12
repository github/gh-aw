package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var unassignFromUserLog = logger.New("workflow:unassign_from_user")

// UnassignFromUserConfig holds configuration for removing assignees from issues
type UnassignFromUserConfig struct {
	BaseSafeOutputConfig   `yaml:",inline"`
	SafeOutputTargetConfig `yaml:",inline"`
	Allowed                []string `yaml:"allowed,omitempty"` // Optional list of allowed usernames. If omitted, any users can be unassigned.
}

// parseUnassignFromUserConfig handles unassign-from-user configuration
func (c *Compiler) parseUnassignFromUserConfig(outputMap map[string]any) *UnassignFromUserConfig {
	// Check if the key exists
	if _, exists := outputMap["unassign-from-user"]; !exists {
		return nil
	}

	unassignFromUserLog.Print("Parsing unassign-from-user configuration")

	// Unmarshal into typed config struct
	var config UnassignFromUserConfig
	if err := unmarshalConfig(outputMap, "unassign-from-user", &config, unassignFromUserLog); err != nil {
		unassignFromUserLog.Printf("Failed to unmarshal config: %v", err)
		// For backward compatibility, use defaults
		unassignFromUserLog.Print("Using default configuration")
		config = UnassignFromUserConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
		}
	}

	// Set default max if not specified
	if config.Max == 0 {
		config.Max = 1
	}

	unassignFromUserLog.Printf("Parsed configuration: allowed_count=%d, target=%s", len(config.Allowed), config.Target)

	return &config
}
