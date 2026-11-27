// This file provides configuration types and parsing for the update-pull-request safe output.
//
// For shared update entity infrastructure (types, generic parsers, field parsing modes),
// see update_entity_helpers.go.

package workflow

import "github.com/github/gh-aw/pkg/logger"

var updatePullRequestLog = logger.New("workflow:update_pull_request")

// UpdatePullRequestsConfig holds configuration for updating GitHub pull requests from agent output
type UpdatePullRequestsConfig struct {
	UpdateEntityConfig     `yaml:",inline"`
	SafeOutputFilterConfig `yaml:",inline"`
	Title                  *bool   `yaml:"title,omitempty"`         // Allow updating PR title - defaults to true, set to false to disable
	Body                   *bool   `yaml:"body,omitempty"`          // Allow updating PR body - defaults to true, set to false to disable
	UpdateBranch           *bool   `yaml:"update-branch,omitempty"` // When true, update PR branch with latest base branch changes before applying other updates. Defaults to false.
	UpdateBranchStacks     *bool   `yaml:"sync-stack,omitempty"`    // When true, allow stacked-PR stack-sync fallback if update-branch endpoint is unsupported. Defaults to true.
	Operation              *string `yaml:"operation,omitempty"`     // Default operation for body updates: "append", "prepend", or "replace" (defaults to "replace")
	RequiredAuthor         string  `yaml:"required-author,omitempty"`
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
				{Name: "update-branch", Mode: FieldParsingBoolValue, Dest: &cfg.UpdateBranch},
				{Name: "sync-stack", Mode: FieldParsingBoolValue, Dest: &cfg.UpdateBranchStacks},
				updateEntityFooterField(&cfg.Footer),
			}
		}, func(configMap map[string]any, cfg *UpdatePullRequestsConfig) {
			// Parse operation field
			if operationVal, exists := configMap["operation"]; exists {
				if operationStr, ok := operationVal.(string); ok {
					cfg.Operation = &operationStr
				}
			}
			// Parse required-labels and required-title-prefix filter fields
			cfg.SafeOutputFilterConfig = ParseFilterConfig(configMap)
			cfg.RequiredAuthor = extractStringFromMap(configMap, "required-author", updatePullRequestLog)
		})
}
