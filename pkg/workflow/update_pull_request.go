package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var updatePullRequestLog = logger.New("workflow:update_pull_request")

// UpdatePullRequestsConfig holds configuration for updating GitHub pull requests from agent output
type UpdatePullRequestsConfig struct {
	UpdateEntityConfig `yaml:",inline"`
	Title              *bool   `yaml:"title,omitempty"`     // Allow updating PR title - defaults to true, set to false to disable
	Body               *bool   `yaml:"body,omitempty"`      // Allow updating PR body - defaults to true, set to false to disable
	Operation          *string `yaml:"operation,omitempty"` // Default operation for body updates: "append", "prepend", or "replace" (defaults to "replace")
}

// parseUpdatePullRequestsConfig handles update-pull-request configuration
func (c *Compiler) parseUpdatePullRequestsConfig(outputMap map[string]any) *UpdatePullRequestsConfig {
	updatePullRequestLog.Print("Parsing update pull request configuration")

	return parseUpdateEntityConfigTyped(c, outputMap,
		UpdateEntityPullRequest, "update-pull-request", updatePullRequestLog,
		func(cfg *UpdatePullRequestsConfig) []UpdateEntityFieldSpec {
			return []UpdateEntityFieldSpec{
				{Name: "title", Mode: FieldParsingBoolValue, Dest: &cfg.Title},
				{Name: "body", Mode: FieldParsingBoolValue, Dest: &cfg.Body},
			}
		}, func(configMap map[string]any, cfg *UpdatePullRequestsConfig) {
			// Parse operation field
			if operationVal, exists := configMap["operation"]; exists {
				if operationStr, ok := operationVal.(string); ok {
					cfg.Operation = &operationStr
				}
			}
		})
}
