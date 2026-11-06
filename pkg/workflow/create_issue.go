package workflow

import (
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var createIssueLog = logger.New("workflow:create_issue")

const defaultMaxIssueArtifacts = 10
const defaultMaxIssueArtifactSize = 1024 * 1024

var issueArtifactSizePattern = regexp.MustCompile(`(?i)^\s*([0-9]+(?:\.[0-9]+)?)\s*(b|k(?:i?b)?|m(?:i?b)?|g(?:i?b)?|t(?:i?b)?)?\s*$`)

// CreateIssuesConfig holds configuration for creating GitHub issues from agent output
type CreateIssuesConfig struct {
	BaseSafeOutputConfig          `yaml:",inline"`
	SafeOutputAllowedLabelsConfig `yaml:",inline"`
	TitlePrefix                   string                `yaml:"title-prefix,omitempty"`
	RequireTemporaryID            bool                  `yaml:"require-temporary-id,omitempty"` // When true, create_issue tool calls must include temporary_id.
	Labels                        []string              `yaml:"labels,omitempty"`
	AllowedFields                 []string              `yaml:"allowed-fields,omitempty"`          // Optional list of allowed issue field names. If omitted or empty, any issue fields are allowed. Use ["*"] to explicitly allow all.
	MaxIssueArtifacts             *int                  `yaml:"max-issue-artifacts,omitempty"`     // Maximum artifacts per issue; non-positive values disable attachments.
	MaxIssueArtifactSize          *int                  `yaml:"max-issue-artifact-size,omitempty"` // Maximum UTF-8 bytes per artifact body.
	Assignees                     []string              `yaml:"assignees,omitempty"`               // List of users/bots to assign the issue to
	DeduplicateByTitle            *TemplatableBoolOrInt `yaml:"deduplicate-by-title,omitempty"`    // When true or 0, deduplicate by exact title match. When set to a positive integer N, also allow fuzzy matches up to edit distance N. When false or omitted, disable title-based deduplication. Accepts GitHub Actions expressions.
	TargetRepoSlug                string                `yaml:"target-repo,omitempty"`             // Target repository in format "owner/repo" for cross-repository issues
	AllowedRepos                  []string              `yaml:"allowed-repos,omitempty"`           // List of additional repositories that issues can be created in
	CloseOlderConfig              `yaml:",inline"`      // Shared close-older settings; Enabled is sourced from close-older-issues.
	GroupByDay                    *string               `yaml:"group-by-day,omitempty"` // When true, if an open issue was already created today (UTC), post new content as a comment on it instead of creating a duplicate. Works best with close-older-issues: true.
	Expires                       int                   `yaml:"expires,omitempty"`      // Hours until the issue expires and should be automatically closed
	Group                         *string               `yaml:"group,omitempty"`        // If true, group issues as sub-issues under a parent issue (workflow ID is used as group identifier)
	artifactSizeParseError        error
}

// parseCreateIssuesConfig handles create-issue configuration
func (c *Compiler) parseCreateIssuesConfig(outputMap map[string]any) *CreateIssuesConfig {
	if enabled, ok := outputMap["create-issue"].(bool); ok && !enabled {
		return nil
	}

	if configMap, ok := outputMap["create-issue"].(map[string]any); ok {
		if size, ok := configMap["max-issue-artifact-size"].(string); ok {
			bytes, err := parseIssueArtifactSize(size)
			if err != nil {
				return &CreateIssuesConfig{artifactSizeParseError: fmt.Errorf("safe-outputs.create-issue.max-issue-artifact-size: %w", err)}
			}
			configMap["max-issue-artifact-size"] = bytes
		}
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

func (c *CreateIssuesConfig) maxIssueArtifactSize() int {
	if c.MaxIssueArtifactSize == nil {
		return defaultMaxIssueArtifactSize
	}
	return *c.MaxIssueArtifactSize
}

// parseIssueArtifactSize uses exact decimal arithmetic so fractional bytes and
// oversized values cannot be silently rounded into a different limit.
func parseIssueArtifactSize(size string) (int, error) {
	matches := issueArtifactSizePattern.FindStringSubmatch(size)
	if len(size) > 64 || len(matches) != 3 {
		return 0, fmt.Errorf("invalid size %q; use a positive byte count or a size such as 512kb, 1mb, or 1.5mb", size)
	}
	amount, ok := new(big.Rat).SetString(matches[1]) //nolint:uncheckedsliceindex // Match length is checked above.
	if !ok {
		return 0, fmt.Errorf("invalid size %q", size)
	}
	multiplier := int64(1)
	switch strings.ToUpper(matches[2]) { //nolint:uncheckedsliceindex // Match length is checked above.
	case "K", "KB", "KIB":
		multiplier = 1 << 10
	case "M", "MB", "MIB":
		multiplier = 1 << 20
	case "G", "GB", "GIB":
		multiplier = 1 << 30
	case "T", "TB", "TIB":
		multiplier = 1 << 40
	}
	amount.Mul(amount, new(big.Rat).SetInt64(multiplier))
	maxBytes := min(int64(^uint(0)>>1), int64(1<<53-1))
	if !amount.IsInt() || amount.Sign() <= 0 || amount.Cmp(new(big.Rat).SetInt64(maxBytes)) > 0 {
		return 0, fmt.Errorf("size %q must resolve to a positive whole byte count no greater than %d", size, maxBytes)
	}
	return int(amount.Num().Int64()), nil
}

// hasCopilotAssignee checks if "copilot" is in the assignees list
func hasCopilotAssignee(assignees []string) bool {
	return slices.Contains(assignees, "copilot")
}
