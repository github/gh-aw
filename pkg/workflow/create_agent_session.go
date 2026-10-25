package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var createAgentSessionLog = logger.New("workflow:create_agent_session")

// CreateAgentSessionConfig holds configuration for creating GitHub Copilot coding agent sessions from agent output
type CreateAgentSessionConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Base                 string   `yaml:"base,omitempty"`          // Base branch for the pull request
	TargetRepoSlug       string   `yaml:"target-repo,omitempty"`   // Target repository in format "owner/repo" for cross-repository agent sessions
	AllowedRepos         []string `yaml:"allowed-repos,omitempty"` // List of additional repositories that agent sessions can be created in (additionally to the target-repo)
}

// parseAgentSessionConfig handles create-agent-session configuration
func (c *Compiler) parseAgentSessionConfig(outputMap map[string]any) *CreateAgentSessionConfig {
	// Try new key first
	if configData, exists := outputMap["create-agent-session"]; exists {
		createAgentSessionLog.Print("Parsing create-agent-session configuration")
		return c.parseAgentSessionValue(configData)
	}

	// Fall back to deprecated key for backward compatibility
	if configData, exists := outputMap["create-agent-task"]; exists {
		createAgentSessionLog.Print("WARNING: Using deprecated 'create-agent-task' configuration. Please migrate to 'create-agent-session' using 'gh aw fix'")
		return c.parseAgentSessionValue(configData)
	}

	return nil
}

func (c *Compiler) parseAgentSessionValue(configData any) *CreateAgentSessionConfig {
	agentSessionConfig := &CreateAgentSessionConfig{}
	if configMap, ok := configData.(map[string]any); ok {
		if base, exists := configMap["base"]; exists {
			if baseStr, ok := base.(string); ok {
				agentSessionConfig.Base = baseStr
			}
		}
		targetRepoSlug, isInvalid := parseTargetRepoWithValidation(configMap)
		if isInvalid {
			return nil
		}
		agentSessionConfig.TargetRepoSlug = targetRepoSlug
		c.parseBaseSafeOutputConfig(configMap, &agentSessionConfig.BaseSafeOutputConfig, 1)
	} else {
		agentSessionConfig.Max = defaultIntStr(1)
	}
	return agentSessionConfig
}
