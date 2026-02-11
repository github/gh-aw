package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var assignToAgentLog = logger.New("workflow:assign_to_agent")

// AssignToAgentConfig holds configuration for assigning agents to issues from agent output
type AssignToAgentConfig struct {
	BaseSafeOutputConfig   `yaml:",inline"`
	SafeOutputTargetConfig `yaml:",inline"`
	DefaultAgent           string   `yaml:"name,omitempty"`            // Default agent to assign (e.g., "copilot")
	Allowed                []string `yaml:"allowed,omitempty"`         // Optional list of allowed agent names. If omitted, any agents are allowed.
	IgnoreIfError          bool     `yaml:"ignore-if-error,omitempty"` // If true, workflow continues when agent assignment fails
}

// parseAssignToAgentConfig handles assign-to-agent configuration
func (c *Compiler) parseAssignToAgentConfig(outputMap map[string]any) *AssignToAgentConfig {
	// Check if the key exists
	if _, exists := outputMap["assign-to-agent"]; !exists {
		return nil
	}

	assignToAgentLog.Print("Parsing assign-to-agent configuration")

	// Unmarshal into typed config struct
	var config AssignToAgentConfig
	if err := unmarshalConfig(outputMap, "assign-to-agent", &config, assignToAgentLog); err != nil {
		assignToAgentLog.Printf("Failed to unmarshal config: %v", err)
		// Handle null case: create empty config
		return &AssignToAgentConfig{}
	}

	// Set default max if not specified
	if config.Max == 0 {
		config.Max = 1
	}

	assignToAgentLog.Printf("Parsed assign-to-agent config: default_agent=%s, allowed_count=%d, target=%s, max=%d", config.DefaultAgent, len(config.Allowed), config.Target, config.Max)

	return &config
}
