package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var assignToAgentLog = logger.New("workflow:assign_to_agent")

// AssignToAgentConfig holds configuration for assigning agents to issues from agent output
type AssignToAgentConfig struct {
	BaseSafeOutputConfig      `yaml:",inline"`
	SafeOutputTargetConfig    `yaml:",inline"`
	DefaultAgent              string   `yaml:"name,omitempty"`                       // Default agent to assign (e.g., "copilot")
	DefaultModel              string   `yaml:"model,omitempty"`                      // Default AI model to use (e.g., "claude-sonnet-5")
	ReasoningEffort           string   `yaml:"reasoning-effort,omitempty"`           // Optional reasoning effort
	DefaultCustomAgent        string   `yaml:"custom-agent,omitempty"`               // Default custom agent ID for custom agents
	DefaultCustomInstructions string   `yaml:"custom-instructions,omitempty"`        // Default custom instructions for the agent
	Allowed                   []string `yaml:"allowed,omitempty"`                    // Optional list of allowed agent names. If omitted, any agents are allowed.
	IgnoreIfError             bool     `yaml:"ignore-if-error,omitempty"`            // If true, workflow continues when agent assignment fails
	PullRequestRepoSlug       string   `yaml:"pull-request-repo,omitempty"`          // Target repository for PR creation in format "owner/repo" (where the issue lives may differ)
	AllowedPullRequestRepos   []string `yaml:"allowed-pull-request-repos,omitempty"` // List of additional repositories that PRs can be created in (beyond pull-request-repo which is automatically allowed)
	BaseBranch                string   `yaml:"base-branch,omitempty"`                // Base branch for PR creation in target repo (defaults to target repo's default branch)
}

// parseAssignToAgentConfig handles assign-to-agent configuration
func (c *Compiler) parseAssignToAgentConfig(outputMap map[string]any) *AssignToAgentConfig {
	// Check key existence first so we can preprocess templatable fields before YAML unmarshaling
	if _, exists := outputMap["assign-to-agent"]; !exists {
		return nil
	}

	// Get config data for pre-processing before YAML unmarshaling
	configData, _ := outputMap["assign-to-agent"].(map[string]any)

	// Pre-process templatable int fields
	if err := preprocessIntFieldAsString(configData, "max", assignToAgentLog); err != nil {
		assignToAgentLog.Printf("Invalid max value: %v", err)
		return nil
	}

	config := parseConfigScaffoldWithPostProcess(outputMap, "assign-to-agent", assignToAgentLog,
		func(err error) *AssignToAgentConfig {
			assignToAgentLog.Printf("Failed to unmarshal config: %v", err)
			// Handle null case: create empty config
			return &AssignToAgentConfig{}
		},
		func(config *AssignToAgentConfig) {
			// Set default max if not specified
			if config.Max == nil {
				config.Max = defaultIntStr(1)
			}

			assignToAgentLog.Printf("Parsed assign-to-agent config: default_agent=%s, default_model=%s, default_custom_agent=%s, allowed_count=%d, target=%s, max=%s, pull_request_repo=%s, base_branch=%s",
				config.DefaultAgent, config.DefaultModel, config.DefaultCustomAgent, len(config.Allowed), config.Target, *config.Max, config.PullRequestRepoSlug, config.BaseBranch)
		})

	return config
}
