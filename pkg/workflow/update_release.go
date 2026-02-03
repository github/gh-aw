package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var updateReleaseLog = logger.New("workflow:update_release")

// UpdateReleaseConfig holds configuration for updating GitHub releases from agent output
type UpdateReleaseConfig struct {
	UpdateEntityConfig `yaml:",inline"`
}

// parseUpdateReleaseConfig handles update-release configuration
func (c *Compiler) parseUpdateReleaseConfig(outputMap map[string]any) *UpdateReleaseConfig {
	return parseUpdateEntityConfigTyped(c, outputMap,
		UpdateEntityRelease, "update-release", updateReleaseLog,
		func(cfg *UpdateReleaseConfig) []UpdateEntityFieldSpec {
			return nil // No entity-specific fields for releases
		}, nil)
}
