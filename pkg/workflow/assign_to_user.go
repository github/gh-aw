package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var assignToUserLog = logger.New("workflow:assign_to_user")

// AssignToUserConfig holds configuration for assigning users to issues from agent output
type AssignToUserConfig struct {
	BaseSafeOutputConfig       `yaml:",inline"`
	SafeOutputTargetConfig     `yaml:",inline"`
	SafeOutputFilterConfig     `yaml:",inline"`
	SafeOutputAllowBlockConfig `yaml:",inline"`
	UnassignFirst              *string `yaml:"unassign-first,omitempty"` // If true, unassign all current assignees before assigning new ones
}

// parseAssignToUserConfig handles assign-to-user configuration
func (c *Compiler) parseAssignToUserConfig(outputMap map[string]any) *AssignToUserConfig {
	// Check key existence first so we can preprocess templatable fields before YAML unmarshaling
	if _, exists := outputMap["assign-to-user"]; !exists {
		return nil
	}

	// Get config data for pre-processing before YAML unmarshaling
	configData, _ := typeutil.LookupMap(outputMap, "assign-to-user")

	// Pre-process templatable bool fields
	if err := preprocessBoolFieldAsString(configData, "unassign-first", assignToUserLog); err != nil {
		assignToUserLog.Printf("Invalid unassign-first value: %v", err)
		return nil
	}

	// Pre-process templatable int fields
	if err := preprocessIntFieldAsString(configData, "max", assignToUserLog); err != nil {
		assignToUserLog.Printf("Invalid max value: %v", err)
		return nil
	}

	config := parseConfigScaffoldWithPostProcess(outputMap, "assign-to-user", assignToUserLog,
		func(err error) *AssignToUserConfig {
			assignToUserLog.Printf("Failed to unmarshal config: %v", err)
			// For backward compatibility, use defaults
			assignToUserLog.Print("Using default configuration")
			return &AssignToUserConfig{
				Max: defaultIntStr(1),
			}
		},
		func(config *AssignToUserConfig) {
			// Set default max if not specified
			if config.Max == nil {
				config.Max = defaultIntStr(1)
			}

			assignToUserLog.Printf("Parsed configuration: allowed_count=%d, target=%s", len(config.Allowed), config.Target)
		})

	return config
}
