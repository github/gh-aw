// This file provides helper functions for closing GitHub entities.
//
// This file contains shared utilities for building close entity jobs (issues,
// pull requests, discussions). These helpers extract common patterns used across
// the three close entity implementations to reduce code duplication and ensure
// consistency in configuration parsing and job generation.
//
// # Organization Rationale
//
// These close entity helpers are grouped here because they:
//   - Provide generic close entity functionality used by 3 entity types
//   - Share common configuration patterns (target, filters, max)
//   - Follow a consistent entity registry pattern
//   - Enable DRY principles for close operations
//
// # Why Grouped Here vs. Split Like Update-Entity Files
//
// The update-entity operations (update_issue.go,
// update_discussion.go, update_pull_request.go) are split
// into one file per entity type because each file owns a distinct type
// definition (UpdateIssuesConfig, UpdateDiscussionsConfig,
// UpdatePullRequestsConfig) with different fields per entity.
//
// Close-entity operations share a single CloseEntityConfig struct and use
// a registry pattern (closeEntityDefinition / closeEntityRegistry) to
// express per-entity variation via data rather than per-entity functions.
// Grouping all three entity parsers in one file therefore keeps the registry
// and its consumers together, reducing indirection without sacrificing
// clarity. If a future close-entity type requires a distinct config struct,
// follow the update-entity convention and extract it to its own file.
//
// # Key Functions
//
// Configuration Parsing:
//   - parseCloseEntityConfig() - Generic close entity configuration parser
//   - parseCloseIssuesConfig() - Parse close-issue configuration
//   - parseClosePullRequestsConfig() - Parse close-pull-request configuration
//   - parseCloseDiscussionsConfig() - Parse close-discussion configuration
//
// Entity Registry:
//   - closeEntityRegistry - Central registry of all close entity definitions
//   - closeEntityDefinition - Definition structure for close entity types
//
// # Usage Patterns
//
// The close entity helpers follow a registry pattern where each entity type
// (issue, pull request, discussion) is defined with its specific parameters
// (config keys, environment variables, permissions, scripts). This allows:
//   - Consistent configuration parsing across entity types
//   - Easy addition of new close entity types
//   - Centralized entity type definitions
//
// # When to Use vs Alternatives
//
// Use these helpers when:
//   - Implementing close operations for GitHub entities
//   - Parsing close entity configurations from workflow YAML
//   - Building close entity jobs with consistent patterns
//
// For create/update operations, see:
//   - create_*.go files for entity creation logic
//   - update_entity_helpers.go for entity update logic

package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

// CloseEntityType represents the type of entity being closed
type CloseEntityType string

const (
	CloseEntityIssue       CloseEntityType = "issue"
	CloseEntityPullRequest CloseEntityType = "pull_request"
	CloseEntityDiscussion  CloseEntityType = "discussion"
)

// CloseEntityConfig holds the configuration for a close entity operation
type CloseEntityConfig struct {
	BaseSafeOutputConfig             `yaml:",inline"`
	SafeOutputTargetConfig           `yaml:",inline"`
	SafeOutputFilterConfig           `yaml:",inline"`
	SafeOutputDiscussionFilterConfig `yaml:",inline"` // Only used for discussions
	StateReason                      string           `yaml:"state-reason,omitempty"`         // Only used for issues. Scalar: fixed reason. Mutually exclusive with AllowedStateReason.
	AllowedStateReason               []string         `yaml:"allowed-state-reason,omitempty"` // Only used for issues. List: agent selects from this subset.
	AllowBody                        *bool            `yaml:"allow-body,omitempty"`           // If false, any body provided by the agent is dropped with a warning; close proceeds without a comment
}

// CloseEntityJobParams holds the parameters needed to build a close entity job
type CloseEntityJobParams struct {
	EntityType       CloseEntityType
	ConfigKey        string // e.g., "close-issue", "close-pull-request"
	EnvVarPrefix     string // e.g., "GH_AW_CLOSE_ISSUE", "GH_AW_CLOSE_PR"
	JobName          string // e.g., "close_issue", "close_pull_request"
	StepName         string // e.g., "Close Issue", "Close Pull Request"
	OutputNumberKey  string // e.g., "issue_number", "pull_request_number"
	OutputURLKey     string // e.g., "issue_url", "pull_request_url"
	EventNumberPath1 string // e.g., "github.event.issue.number"
	EventNumberPath2 string // e.g., "github.event.comment.issue.number"
	PermissionsFunc  func() *Permissions
}

// parseCloseEntityConfig is a generic function to parse close entity configurations
func (c *Compiler) parseCloseEntityConfig(outputMap map[string]any, params CloseEntityJobParams, logger *logger.Logger) *CloseEntityConfig {
	// Check if the key exists
	if _, exists := outputMap[params.ConfigKey]; !exists {
		return nil
	}

	// Get config data for pre-processing before YAML unmarshaling
	configData, _ := typeutil.LookupMap(outputMap, params.ConfigKey)

	// Pre-process templatable int fields
	if err := preprocessIntFieldAsString(configData, "max", logger); err != nil {
		logger.Printf("Invalid max value for %s: %v", params.ConfigKey, err)
		return nil
	}

	// Pre-process state-reason: when the value is a sequence (list) rather than a scalar,
	// move it to "allowed-state-reason" so it unmarshals into AllowedStateReason []string
	// instead of the scalar StateReason string field. This supports the list form:
	//   state-reason: [not_planned, duplicate]
	if configData != nil {
		if raw, exists := configData["state-reason"]; exists {
			if preprocessStateReasonList(configData, raw, logger) {
				logger.Printf("state-reason list form detected for %s; moved to allowed-state-reason", params.ConfigKey)
			}
		}
	}

	config := parseConfigScaffoldWithPostProcess(outputMap, params.ConfigKey, logger,
		func(err error) *CloseEntityConfig {
			logger.Printf("Failed to unmarshal config: %v", err)
			// For backward compatibility, handle nil/empty config
			return &CloseEntityConfig{}
		},
		func(config *CloseEntityConfig) {
			// Set default max if not specified
			if config.Max == nil {
				config.Max = defaultIntStr(1)
				logger.Printf("Set default max to 1 for %s", params.ConfigKey)
			}

			// Backward compatibility: map deprecated title-prefix to required-title-prefix.
			if config.RequiredTitlePrefix == "" && config.TitlePrefix != "" {
				config.RequiredTitlePrefix = config.TitlePrefix
			}

			logger.Printf("Parsed %s configuration: max=%s, target=%s", params.ConfigKey, *config.Max, config.Target)
		})

	return config
}

// preprocessStateReasonList converts a list-form state-reason value into the allowed-state-reason key.
// Returns true if the value was a non-empty list and was successfully converted.
// Returns false and leaves configData unchanged when the value is not a list or when no valid
// string elements are found — the latter prevents a silent escalation to unrestricted (omitted) mode.
func preprocessStateReasonList(configData map[string]any, raw any, logger *logger.Logger) bool {
	switch v := raw.(type) {
	case []any:
		reasons := make([]string, 0, len(v))
		for _, elem := range v {
			s, ok := elem.(string)
			if !ok {
				if logger != nil {
					logger.Printf("state-reason list contains non-string element %T; ignoring", elem)
				}
				continue
			}
			reasons = append(reasons, s)
		}
		if len(reasons) == 0 {
			// No usable strings found; leave configData unchanged so downstream validation
			// reports an error rather than silently granting unrestricted reason selection.
			if logger != nil {
				logger.Printf("state-reason list has no valid string elements; treating as invalid")
			}
			return false
		}
		configData["allowed-state-reason"] = reasons
		delete(configData, "state-reason")
		return true
	case []string:
		if len(v) == 0 {
			// Empty explicit slice; leave configData unchanged for the same reason as above.
			if logger != nil {
				logger.Printf("state-reason list is empty; treating as invalid")
			}
			return false
		}
		configData["allowed-state-reason"] = v
		delete(configData, "state-reason")
		return true
	}
	return false
}

// closeEntityDefinition holds all parameters for a close entity type
type closeEntityDefinition struct {
	EntityType       CloseEntityType
	ConfigKey        string
	EnvVarPrefix     string
	JobName          string
	StepName         string
	OutputNumberKey  string
	OutputURLKey     string
	EventNumberPath1 string
	EventNumberPath2 string
	PermissionsFunc  func() *Permissions
	Logger           *logger.Logger
}

var logCloseIssue = logger.New("workflow:close_issue")
var logClosePullRequest = logger.New("workflow:close_pull_request")
var logCloseDiscussion = logger.New("workflow:close_discussion")

// closeEntityRegistry holds all close entity definitions
var closeEntityRegistry = []closeEntityDefinition{
	{
		EntityType:       CloseEntityIssue,
		ConfigKey:        "close-issue",
		EnvVarPrefix:     "GH_AW_CLOSE_ISSUE",
		JobName:          "close_issue",
		StepName:         "Close Issue",
		OutputNumberKey:  "issue_number",
		OutputURLKey:     "issue_url",
		EventNumberPath1: "github.event.issue.number",
		EventNumberPath2: "github.event.comment.issue.number",
		PermissionsFunc:  NewPermissionsContentsReadIssuesWrite,
		Logger:           logCloseIssue,
	},
	{
		EntityType:       CloseEntityPullRequest,
		ConfigKey:        "close-pull-request",
		EnvVarPrefix:     "GH_AW_CLOSE_PR",
		JobName:          "close_pull_request",
		StepName:         "Close Pull Request",
		OutputNumberKey:  "pull_request_number",
		OutputURLKey:     "pull_request_url",
		EventNumberPath1: "github.event.pull_request.number",
		EventNumberPath2: "github.event.comment.pull_request.number",
		PermissionsFunc:  NewPermissionsContentsReadPRWrite,
		Logger:           logClosePullRequest,
	},
	{
		EntityType:       CloseEntityDiscussion,
		ConfigKey:        "close-discussion",
		EnvVarPrefix:     "GH_AW_CLOSE_DISCUSSION",
		JobName:          "close_discussion",
		StepName:         "Close Discussion",
		OutputNumberKey:  "discussion_number",
		OutputURLKey:     "discussion_url",
		EventNumberPath1: "github.event.discussion.number",
		EventNumberPath2: "github.event.comment.discussion.number",
		PermissionsFunc:  NewPermissionsContentsReadDiscussionsWrite,
		Logger:           logCloseDiscussion,
	},
}

// Type aliases for backward compatibility
type CloseIssuesConfig = CloseEntityConfig
type ClosePullRequestsConfig = CloseEntityConfig
type CloseDiscussionsConfig = CloseEntityConfig

// parseCloseIssuesConfig handles close-issue configuration
func (c *Compiler) parseCloseIssuesConfig(outputMap map[string]any) *CloseIssuesConfig {
	def := closeEntityRegistry[0] // issue
	params := CloseEntityJobParams{
		EntityType: def.EntityType,
		ConfigKey:  def.ConfigKey,
	}
	return c.parseCloseEntityConfig(outputMap, params, def.Logger)
}

// parseClosePullRequestsConfig handles close-pull-request configuration
func (c *Compiler) parseClosePullRequestsConfig(outputMap map[string]any) *ClosePullRequestsConfig {
	def := closeEntityRegistry[1] // pull request
	params := CloseEntityJobParams{
		EntityType: def.EntityType,
		ConfigKey:  def.ConfigKey,
	}
	return c.parseCloseEntityConfig(outputMap, params, def.Logger)
}

// parseCloseDiscussionsConfig handles close-discussion configuration
func (c *Compiler) parseCloseDiscussionsConfig(outputMap map[string]any) *CloseDiscussionsConfig {
	def := closeEntityRegistry[2] // discussion
	params := CloseEntityJobParams{
		EntityType: def.EntityType,
		ConfigKey:  def.ConfigKey,
	}
	return c.parseCloseEntityConfig(outputMap, params, def.Logger)
}
