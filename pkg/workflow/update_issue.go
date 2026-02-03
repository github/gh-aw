package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var updateIssueLog = logger.New("workflow:update_issue")

// UpdateIssuesConfig holds configuration for updating GitHub issues from agent output
type UpdateIssuesConfig struct {
	UpdateEntityConfig `yaml:",inline"`
	Status             *bool `yaml:"status,omitempty"` // Allow updating issue status (open/closed) - presence indicates field can be updated
	Title              *bool `yaml:"title,omitempty"`  // Allow updating issue title - presence indicates field can be updated
	Body               *bool `yaml:"body,omitempty"`   // Allow updating issue body - presence indicates field can be updated
}

// parseUpdateIssuesConfig handles update-issue configuration
func (c *Compiler) parseUpdateIssuesConfig(outputMap map[string]any) *UpdateIssuesConfig {
	return parseUpdateEntityConfigTyped(c, outputMap,
		UpdateEntityIssue, "update-issue", updateIssueLog,
		func(cfg *UpdateIssuesConfig) []UpdateEntityFieldSpec {
			return []UpdateEntityFieldSpec{
				{Name: "status", Mode: FieldParsingKeyExistence, Dest: &cfg.Status},
				{Name: "title", Mode: FieldParsingKeyExistence, Dest: &cfg.Title},
				{Name: "body", Mode: FieldParsingKeyExistence, Dest: &cfg.Body},
			}
		}, nil)
}
