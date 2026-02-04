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
// This follows the helper file conventions documented in the developer instructions.
// See skills/developer/SKILL.md#helper-file-conventions for details.
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
		logger.Printf("No configuration found for %s", params.ConfigKey)
		return nil
	}

	logger.Printf("Parsing %s configuration", params.ConfigKey)

	// Unmarshal into typed config struct
	var config CloseEntityConfig
	if err := unmarshalConfig(outputMap, params.ConfigKey, &config, logger); err != nil {
		logger.Printf("Failed to unmarshal config: %v", err)
		// For backward compatibility, handle nil/empty config
		config = CloseEntityConfig{}
	}

	// Set default max if not specified
	if config.Max == 0 {
		config.Max = 1
		logger.Printf("Set default max to 1 for %s", params.ConfigKey)
	}

	// Validate target-repo (wildcard "*" is not allowed)
	if validateTargetRepoSlug(config.TargetRepoSlug, logger) {
		logger.Printf("Invalid target-repo slug for %s", params.ConfigKey)
		return nil
	}

	logger.Printf("Parsed %s configuration: max=%d, target=%s", params.ConfigKey, config.Max, config.Target)

	return &config
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
		Logger:           logger.New("workflow:close_issue"),
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
		Logger:           logger.New("workflow:close_pull_request"),
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
		Logger:           logger.New("workflow:close_discussion"),
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
