package workflow

import (
	"errors"

	"github.com/github/gh-aw/pkg/logger"
)

var addLabelsLog = logger.New("workflow:add_labels")

// AddLabelsConfig holds configuration for adding labels to issues/PRs from agent output
type AddLabelsConfig struct {
	BaseSafeOutputConfig       `yaml:",inline"`
	SafeOutputTargetConfig     `yaml:",inline"`
	SafeOutputFilterConfig     `yaml:",inline"`
	SafeOutputAllowBlockConfig `yaml:",inline"`
	Issues                     *bool   `yaml:"issues,omitempty"`            // When false, excludes issues:write permission. Default (nil or true) includes issues:write.
	PullRequests               *bool   `yaml:"pull-requests,omitempty"`     // When false, excludes pull-requests:write permission. Default (nil or true) includes pull-requests:write.
	CreateIfMissing            *bool   `yaml:"create-if-missing,omitempty"` // When true, automatically creates labels that don't already exist in the target repository. Default (nil or false) does not create missing labels.
	MaxAddedLabels             *string `yaml:"max-added-labels,omitempty"`
}

// parseAddLabelsConfig handles add-labels configuration
func (c *Compiler) parseAddLabelsConfig(outputMap map[string]any) *AddLabelsConfig {
	return parseConfigScaffoldWithPostProcess(outputMap, "add-labels", addLabelsLog,
		func(err error) *AddLabelsConfig {
			addLabelsLog.Printf("Failed to unmarshal config: %v", err)
			// Handle null case: create empty config (allows any labels)
			addLabelsLog.Print("Using empty configuration (allows any labels)")
			return &AddLabelsConfig{}
		},
		func(config *AddLabelsConfig) {
			addLabelsLog.Printf("Parsed configuration: allowed_count=%d, blocked_count=%d, target=%s", len(config.Allowed), len(config.Blocked), config.Target)
		})
}

// buildAddLabelsPermissions computes the permissions for add_labels based on config.
// Issues: nil or true → issues:write (default: true)
// PullRequests: nil or true → pull-requests:write (default: true)
func buildAddLabelsPermissions(config *AddLabelsConfig) *Permissions {
	permMap := map[PermissionScope]PermissionLevel{}
	if config == nil || config.Issues == nil || *config.Issues {
		permMap[PermissionIssues] = PermissionWrite
	}
	if config == nil || config.PullRequests == nil || *config.PullRequests {
		permMap[PermissionPullRequests] = PermissionWrite
	}
	return NewPermissionsFromMap(permMap)
}

// validateAddLabelsPermissions returns an error when both issues and pull-requests
// are explicitly set to false, which would produce an empty permission set and
// cause every label operation to fail at runtime with a token scope error.
func validateAddLabelsPermissions(config *SafeOutputsConfig) error {
	if config == nil || config.AddLabels == nil {
		return nil
	}
	c := config.AddLabels
	if c.Issues != nil && !*c.Issues && c.PullRequests != nil && !*c.PullRequests {
		return errors.New("safe-outputs.add-labels: at least one of 'issues' or 'pull-requests' must be enabled")
	}
	return nil
}
