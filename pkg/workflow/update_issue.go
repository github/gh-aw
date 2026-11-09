// This file provides configuration types and parsing for the update-issue safe output.
//
// For shared update entity infrastructure (types, generic parsers, field parsing modes),
// see update_entity_helpers.go.

package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var updateIssueLog = logger.New("workflow:update_issue")

// UpdateIssuesConfig holds configuration for updating GitHub issues from agent output
type UpdateIssuesConfig struct {
	UpdateEntityConfig     `yaml:",inline"`
	Status                 *bool    `yaml:"status,omitempty"`                  // Allow updating issue status (open/closed) - presence indicates field can be updated
	Title                  *bool    `yaml:"title,omitempty"`                   // Allow updating issue title - presence indicates field can be updated
	Body                   *bool    `yaml:"body,omitempty"`                    // Allow updating issue body - boolean value controls permission (defaults to true)
	TitlePrefix            string   `yaml:"title-prefix,omitempty"`            // Required title prefix for issue validation - only issues with this prefix can be updated (deprecated: use required-title-prefix)
	RequiredTitlePrefix    string   `yaml:"required-title-prefix,omitempty"`   // Title prefix the issue must have (preferred over title-prefix)
	RequiredLabels         []string `yaml:"required-labels,omitempty"`         // Labels that must ALL be present on the issue
	MaxIssueArtifacts      *int     `yaml:"max-issue-artifacts,omitempty"`     // Maximum artifacts per issue; non-positive values disable attachments.
	MaxIssueArtifactSize   *int     `yaml:"max-issue-artifact-size,omitempty"` // Maximum UTF-8 bytes per artifact body.
	artifactSizeParseError error
}

// parseUpdateIssuesConfig handles update-issue configuration
func (c *Compiler) parseUpdateIssuesConfig(outputMap map[string]any) *UpdateIssuesConfig {
	updateIssueLog.Print("Parsing update-issue config")
	return parseUpdateEntityConfigTyped(c, outputMap,
		UpdateEntityIssue, "update-issue", updateIssueLog,
		func(cfg *UpdateIssuesConfig) []UpdateEntityFieldSpec {
			return []UpdateEntityFieldSpec{
				{Name: "status", Mode: FieldParsingKeyExistence, Dest: &cfg.Status},
				{Name: "title", Mode: FieldParsingKeyExistence, Dest: &cfg.Title},
				{Name: "body", Mode: FieldParsingBoolValue, Dest: &cfg.Body},
				updateEntityFooterField(&cfg.Footer),
			}
		}, func(configMap map[string]any, cfg *UpdateIssuesConfig) {
			cfg.MaxIssueArtifacts = parseOptionalIssueArtifactLimit(configMap["max-issue-artifacts"])
			if size, ok := configMap["max-issue-artifact-size"].(string); ok {
				bytes, err := parseIssueArtifactSize(size)
				if err != nil {
					cfg.artifactSizeParseError = fmt.Errorf("safe-outputs.update-issue.max-issue-artifact-size: %w", err)
				} else {
					cfg.MaxIssueArtifactSize = &bytes
				}
			} else {
				cfg.MaxIssueArtifactSize = parseOptionalIssueArtifactLimit(configMap["max-issue-artifact-size"])
			}
			cfg.TitlePrefix = extractStringFromMap(configMap, "title-prefix", updateIssueLog)
			// Parse required-labels and required-title-prefix directly to avoid the
			// deprecated title-prefix fallback in ParseFilterConfig double-populating
			// RequiredTitlePrefix when only title-prefix is set.
			cfg.RequiredLabels = ParseStringArrayFromConfig(configMap, "required-labels", updateIssueLog)
			cfg.RequiredTitlePrefix = extractStringFromMap(configMap, "required-title-prefix", updateIssueLog)
		})
}

func parseOptionalIssueArtifactLimit(value any) *int {
	if value, ok := value.(int64); ok {
		limit := int(value)
		if int64(limit) == value {
			return &limit
		}
		return nil
	}
	return parseOptionalInt(value)
}

func (c *UpdateIssuesConfig) maxIssueArtifacts() int {
	if c.MaxIssueArtifacts == nil {
		return defaultMaxIssueArtifacts
	}
	return *c.MaxIssueArtifacts
}

func (c *UpdateIssuesConfig) maxIssueArtifactSize() int {
	if c.MaxIssueArtifactSize == nil {
		return defaultMaxIssueArtifactSize
	}
	return *c.MaxIssueArtifactSize
}
