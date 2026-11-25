package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var createProjectStatusUpdateLog = logger.New("workflow:create_project_status_update")

// CreateProjectStatusUpdateConfig holds configuration for creating GitHub project status updates
type CreateProjectStatusUpdateConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Project              string `yaml:"project,omitempty"` // Required by the schema; used for project/token setup, not as a handler fallback.
}

// parseCreateProjectStatusUpdateConfig handles create-project-status-update configuration
func (c *Compiler) parseCreateProjectStatusUpdateConfig(outputMap map[string]any) *CreateProjectStatusUpdateConfig {
	if configData, exists := outputMap["create-project-status-update"]; exists {
		createProjectStatusUpdateLog.Print("Parsing create-project-status-update configuration")
		config := &CreateProjectStatusUpdateConfig{}
		config.Max = defaultIntStr(1) // Default max is 1

		if configMap, ok := configData.(map[string]any); ok {
			c.parseBaseSafeOutputConfig(configMap, &config.BaseSafeOutputConfig, 1)

			// Parse project URL override if specified
			if project, exists := configMap["project"]; exists {
				if projectStr, ok := project.(string); ok {
					config.Project = projectStr
					createProjectStatusUpdateLog.Printf("Using custom project URL for create-project-status-update: %s", projectStr)
				}
			}
		}

		createProjectStatusUpdateLog.Printf("Parsed create-project-status-update config: max=%v, hasCustomToken=%v, hasCustomProject=%v",
			config.Max, config.GitHubToken != "", config.Project != "")
		return config
	}
	createProjectStatusUpdateLog.Print("No create-project-status-update configuration found")
	return nil
}
