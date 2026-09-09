package workflow

import (
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var jiraSafeOutputsLog = logger.New("workflow:jira_safe_outputs")

// JiraSafeOutputConfig holds the common configuration for Jira safe outputs.
type JiraSafeOutputConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
}

func (c *Compiler) parseJiraSafeOutputConfig(outputMap map[string]any, key string) *JiraSafeOutputConfig {
	return parseConfigScaffoldWithPostProcess(outputMap, key, jiraSafeOutputsLog,
		func(err error) *JiraSafeOutputConfig {
			jiraSafeOutputsLog.Printf("Failed to unmarshal %s config: %v", key, err)
			return &JiraSafeOutputConfig{}
		},
		func(config *JiraSafeOutputConfig) {
			if config.Max == nil {
				config.Max = defaultIntStr(1)
			}
		})
}

func (c *Compiler) extractJiraSafeOutputConfigs(outputMap map[string]any, config *SafeOutputsConfig) {
	config.JiraCreateIssue = c.parseJiraSafeOutputConfig(outputMap, "jira-create-issue")
	config.JiraUpdateIssue = c.parseJiraSafeOutputConfig(outputMap, "jira-update-issue")
	config.JiraAddComment = c.parseJiraSafeOutputConfig(outputMap, "jira-add-comment")
	config.JiraAddLabel = c.parseJiraSafeOutputConfig(outputMap, "jira-add-label")
}

func hasAnyJiraSafeOutputEnabled(config *SafeOutputsConfig) bool {
	return config.JiraCreateIssue != nil ||
		config.JiraUpdateIssue != nil ||
		config.JiraAddComment != nil ||
		config.JiraAddLabel != nil
}

var jiraSafeOutputDefaultEnv = map[string]string{
	"JIRA_BASE_URL":   constants.JiraBaseURLExpr,
	"JIRA_USER_EMAIL": constants.JiraUserEmailExpr,
	"JIRA_API_TOKEN":  constants.JiraAPITokenExpr,
}

func injectJiraCredentialsIntoProcessorStep(steps []string, config *SafeOutputsConfig) []string {
	if config == nil || !hasAnyJiraSafeOutputEnabled(config) {
		return steps
	}

	env := make(map[string]string, len(jiraSafeOutputDefaultEnv))
	for name, defaultValue := range jiraSafeOutputDefaultEnv {
		env[name] = defaultValue
		if value := config.Env[name]; value != "" {
			env[name] = value
		}
	}
	return injectProcessorStepEnv(steps, env)
}
