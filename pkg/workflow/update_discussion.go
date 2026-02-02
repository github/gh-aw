package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var updateDiscussionLog = logger.New("workflow:update_discussion")

// UpdateDiscussionsConfig holds configuration for updating GitHub discussions from agent output
type UpdateDiscussionsConfig struct {
	UpdateEntityConfig `yaml:",inline"`
	Title              *bool    `yaml:"title,omitempty"`          // Allow updating discussion title - presence indicates field can be updated
	Body               *bool    `yaml:"body,omitempty"`           // Allow updating discussion body - presence indicates field can be updated
	Labels             *bool    `yaml:"labels,omitempty"`         // Allow updating discussion labels - presence indicates field can be updated
	AllowedLabels      []string `yaml:"allowed-labels,omitempty"` // Optional list of allowed labels. If omitted, any labels are allowed (including creating new ones).
}

// parseUpdateDiscussionsConfig handles update-discussion configuration
func (c *Compiler) parseUpdateDiscussionsConfig(outputMap map[string]any) *UpdateDiscussionsConfig {
	return parseUpdateEntityConfigTyped(c, outputMap,
		UpdateEntityDiscussion, "update-discussion", updateDiscussionLog,
		func(cfg *UpdateDiscussionsConfig) []UpdateEntityFieldSpec {
			return []UpdateEntityFieldSpec{
				{Name: "title", Mode: FieldParsingKeyExistence, Dest: &cfg.Title},
				{Name: "body", Mode: FieldParsingKeyExistence, Dest: &cfg.Body},
				{Name: "labels", Mode: FieldParsingKeyExistence, Dest: &cfg.Labels},
			}
		},
		func(cm map[string]any, cfg *UpdateDiscussionsConfig) {
			// Parse allowed-labels using shared helper
			cfg.AllowedLabels = parseAllowedLabelsFromConfig(cm)
			if len(cfg.AllowedLabels) > 0 {
				updateDiscussionLog.Printf("Allowed labels configured: %v", cfg.AllowedLabels)
				// If allowed-labels is specified, implicitly enable labels
				if cfg.Labels == nil {
					cfg.Labels = new(bool)
				}
			}
		})
}
