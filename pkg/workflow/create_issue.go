package workflow

import (
	"slices"

	"github.com/github/gh-aw/pkg/logger"
)

var createIssueLog = logger.New("workflow:create_issue")

const defaultMaxIssueArtifacts = 10

// CreateIssuesConfig holds configuration for creating GitHub issues from agent output
type CreateIssuesConfig struct {
	BaseSafeOutputConfig          `yaml:",inline"`
	SafeOutputAllowedLabelsConfig `yaml:",inline"`
	TitlePrefix                   string                `yaml:"title-prefix,omitempty"`
	RequireTemporaryID            bool                  `yaml:"require-temporary-id,omitempty"` // When true, create_issue tool calls must include temporary_id.
	Labels                        []string              `yaml:"labels,omitempty"`
	AllowedFields                 []string              `yaml:"allowed-fields,omitempty"`       // Optional list of allowed issue field names. If omitted or empty, any issue fields are allowed. Use ["*"] to explicitly allow all.
	MaxIssueArtifacts             *int                  `yaml:"max-issue-artifacts,omitempty"`  // Maximum artifacts per issue; non-positive values disable attachments.
	Assignees                     []string              `yaml:"assignees,omitempty"`            // List of users/bots to assign the issue to
	DeduplicateByTitle            *TemplatableBoolOrInt `yaml:"deduplicate-by-title,omitempty"` // When true or 0, deduplicate by exact title match. When set to a positive integer N, also allow fuzzy matches up to edit distance N. When false or omitted, disable title-based deduplication. Accepts GitHub Actions expressions.
	TargetRepoSlug                string                `yaml:"target-repo,omitempty"`          // Target repository in format "owner/repo" for cross-repository issues
	AllowedRepos                  []string              `yaml:"allowed-repos,omitempty"`        // List of additional repositories that issues can be created in
	CloseOlderConfig              `yaml:",inline"`      // Shared close-older settings; Enabled is sourced from close-older-issues.
	GroupByDay                    *string               `yaml:"group-by-day,omitempty"` // When true, if an open issue was already created today (UTC), post new content as a comment on it instead of creating a duplicate. Works best with close-older-issues: true.
	Expires                       int                   `yaml:"expires,omitempty"`      // Hours until the issue expires and should be automatically closed
	Group                         *string               `yaml:"group,omitempty"`        // If true, group issues as sub-issues under a parent issue (workflow ID is used as group identifier)
}

// parseCreateIssuesConfig handles create-issue configuration
func (c *Compiler) parseCreateIssuesConfig(outputMap map[string]any) *CreateIssuesConfig {
	if enabled, ok := outputMap["create-issue"].(bool); ok && !enabled {
		return nil
	}

	return parseCreateEntityConfig(
		outputMap,
		"create-issue",
		CreateParseOptions{
			BoolFields:    []string{"close-older-issues", "group", "footer", "group-by-day"},
			IntFields:     []string{"max"},
			HandleExpires: true,
		},
		createIssueLog,
		func(err error) *CreateIssuesConfig {
			createIssueLog.Printf("Failed to unmarshal config: %v", err)
			// For backward compatibility, handle nil/empty config
			return &CreateIssuesConfig{}
		},
		func(configData map[string]any) bool {
			if alias, exists := configData["max-isssue-artifacts"]; exists {
				if _, canonicalExists := configData["max-issue-artifacts"]; !canonicalExists {
					configData["max-issue-artifacts"] = alias
				}
				delete(configData, "max-isssue-artifacts")
			}
			coerceStringOrArrayFields(configData, []string{"assignees"}, createIssueLog)
			return true
		},
		func(configData map[string]any, config *CreateIssuesConfig, expiresDisabled bool) {
			config.Enabled = closeOlderEnabledFromConfigData(configData, "close-older-issues")

			// Set default max if not specified
			if config.Max == nil {
				config.Max = defaultIntStr(1)
			}

			// Log expires if configured or explicitly disabled
			if expiresDisabled {
				createIssueLog.Print("Issue expiration explicitly disabled")
			} else if config.Expires > 0 {
				createIssueLog.Printf("Issue expiration configured: %d hours", config.Expires)
			}
		},
	)
}

func (c *CreateIssuesConfig) maxIssueArtifacts() int {
	if c.MaxIssueArtifacts == nil {
		return defaultMaxIssueArtifacts
	}
	return *c.MaxIssueArtifacts
}

// hasCopilotAssignee checks if "copilot" is in the assignees list
func hasCopilotAssignee(assignees []string) bool {
	return slices.Contains(assignees, "copilot")
}
