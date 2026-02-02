// This file provides helper functions for updating GitHub entities.
//
// This file contains shared utilities for building update entity jobs (issues,
// pull requests, discussions, releases). These helpers extract common patterns
// used across the four update entity implementations to reduce code duplication
// and ensure consistency in configuration parsing and job generation.
//
// # Organization Rationale
//
// These update entity helpers are grouped here because they:
//   - Provide generic update entity functionality used by 4 entity types
//   - Share common configuration patterns (target, max, field updates)
//   - Support two field parsing modes (key existence vs. bool value)
//   - Enable DRY principles for update operations
//
// This follows the helper file conventions documented in the developer instructions.
// See skills/developer/SKILL.md#helper-file-conventions for details.
//
// # Key Functions
//
// Configuration Parsing:
//   - parseUpdateEntityConfig() - Generic update entity configuration parser
//   - parseUpdateEntityBase() - Parse base configuration (max, target, target-repo)
//   - parseUpdateEntityConfigWithFields() - Parse config with entity-specific fields
//   - parseUpdateEntityBoolField() - Generic boolean field parser with mode support
//
// Field Parsing Modes:
//   - FieldParsingKeyExistence - Field presence indicates it can be updated (issues, discussions)
//   - FieldParsingBoolValue - Field's boolean value determines update permission (pull requests)
//
// # Usage Patterns
//
// The update entity helpers support two parsing strategies:
//
//  1. **Key Existence Mode** (for issues and discussions):
//     Fields are enabled if the key exists in config, regardless of value:
//     ```yaml
//     update-issue:
//     title: null    # Can update title (key exists)
//     body: null     # Can update body (key exists)
//     ```
//
//  2. **Bool Value Mode** (for pull requests):
//     Fields are enabled based on explicit boolean values:
//     ```yaml
//     update-pull-request:
//     title: true    # Can update title
//     body: false    # Cannot update body
//     ```
//
// # When to Use vs Alternatives
//
// Use these helpers when:
//   - Implementing update operations for GitHub entities
//   - Parsing update entity configurations from workflow YAML
//   - Building update entity jobs with consistent patterns
//
// For create/close operations, see:
//   - create_*.go files for entity creation logic
//   - close_entity_helpers.go for entity close logic

package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var updateEntityHelpersLog = logger.New("workflow:update_entity_helpers")

// UpdateEntityType represents the type of entity being updated
type UpdateEntityType string

const (
	UpdateEntityIssue       UpdateEntityType = "issue"
	UpdateEntityPullRequest UpdateEntityType = "pull_request"
	UpdateEntityDiscussion  UpdateEntityType = "discussion"
	UpdateEntityRelease     UpdateEntityType = "release"
)

// UpdateEntityConfig holds the configuration for an update entity operation
type UpdateEntityConfig struct {
	BaseSafeOutputConfig   `yaml:",inline"`
	SafeOutputTargetConfig `yaml:",inline"`
	// Type-specific fields are stored in the concrete config structs
}

// UpdateEntityJobParams holds the parameters needed to build an update entity job
type UpdateEntityJobParams struct {
	EntityType      UpdateEntityType
	ConfigKey       string // e.g., "update-issue", "update-pull-request"
	JobName         string // e.g., "update_issue", "update_pull_request"
	StepName        string // e.g., "Update Issue", "Update Pull Request"
	ScriptGetter    func() string
	PermissionsFunc func() *Permissions
	CustomEnvVars   []string          // Type-specific environment variables
	Outputs         map[string]string // Type-specific outputs
	Condition       ConditionNode     // Job condition expression
}

// UpdateEntityJobBuilder encapsulates entity-specific configuration for building update jobs
type UpdateEntityJobBuilder struct {
	EntityType          UpdateEntityType
	ConfigKey           string
	JobName             string
	StepName            string
	ScriptGetter        func() string
	PermissionsFunc     func() *Permissions
	BuildCustomEnvVars  func(*UpdateEntityConfig) []string
	BuildOutputs        func() map[string]string
	BuildEventCondition func(string) ConditionNode // Optional: builds event condition if target is empty
}

// parseUpdateEntityConfig is a generic function to parse update entity configurations
func (c *Compiler) parseUpdateEntityConfig(outputMap map[string]any, params UpdateEntityJobParams, logger *logger.Logger, parseSpecificFields func(map[string]any, *UpdateEntityConfig)) *UpdateEntityConfig {
	if configData, exists := outputMap[params.ConfigKey]; exists {
		updateEntityHelpersLog.Printf("Parsing %s configuration", params.ConfigKey)
		logger.Printf("Parsing %s configuration", params.ConfigKey)
		config := &UpdateEntityConfig{}

		if configMap, ok := configData.(map[string]any); ok {
			// Parse target config (target, target-repo) with validation
			targetConfig, isInvalid := ParseTargetConfig(configMap)
			if isInvalid {
				updateEntityHelpersLog.Printf("Invalid target-repo configuration for %s", params.ConfigKey)
				logger.Print("Invalid target-repo configuration")
				return nil
			}
			config.SafeOutputTargetConfig = targetConfig
			updateEntityHelpersLog.Printf("Parsed target config for %s: target=%s", params.ConfigKey, targetConfig.Target)

			// Parse type-specific fields if provided
			if parseSpecificFields != nil {
				parseSpecificFields(configMap, config)
			}

			// Parse common base fields with default max of 1
			c.parseBaseSafeOutputConfig(configMap, &config.BaseSafeOutputConfig, 1)
		} else {
			// If configData is nil or not a map, still set the default max
			config.Max = 1
		}

		return config
	}

	return nil
}

// parseUpdateEntityBase is a helper that reduces scaffolding duplication across update entity parsers.
// It handles the common pattern of:
//  1. Building UpdateEntityJobParams
//  2. Calling parseUpdateEntityConfig
//  3. Checking for nil result
//  4. Returning the base config and config map for entity-specific field parsing
//
// Returns:
//   - baseConfig: The parsed base configuration (nil if parsing failed)
//   - configMap: The entity-specific config map for additional field parsing (nil if not present)
//
// Callers should check if baseConfig is nil before proceeding with entity-specific parsing.
func (c *Compiler) parseUpdateEntityBase(
	outputMap map[string]any,
	entityType UpdateEntityType,
	configKey string,
	logger *logger.Logger,
) (*UpdateEntityConfig, map[string]any) {
	// Build params for base config parsing
	params := UpdateEntityJobParams{
		EntityType: entityType,
		ConfigKey:  configKey,
	}

	// Parse the base config (common fields like max, target, target-repo)
	baseConfig := c.parseUpdateEntityConfig(outputMap, params, logger, nil)
	if baseConfig == nil {
		return nil, nil
	}

	// Extract the config map for entity-specific field parsing
	var configMap map[string]any
	if configData, exists := outputMap[configKey]; exists {
		if cm, ok := configData.(map[string]any); ok {
			configMap = cm
		}
	}

	return baseConfig, configMap
}

// FieldParsingMode determines how boolean fields are parsed from the config
type FieldParsingMode int

const (
	// FieldParsingKeyExistence mode: Field presence (even if nil) indicates it can be updated
	// Used by update-issue and update-discussion
	FieldParsingKeyExistence FieldParsingMode = iota
	// FieldParsingBoolValue mode: Field's boolean value determines if it can be updated
	// Used by update-pull-request (defaults to true if nil)
	FieldParsingBoolValue
)

// parseUpdateEntityBoolField is a generic helper that parses boolean fields from config maps
// based on the specified parsing mode.
//
// Parameters:
//   - configMap: The entity-specific configuration map
//   - fieldName: The name of the field to parse (e.g., "title", "body", "status")
//   - mode: The parsing mode (FieldParsingKeyExistence or FieldParsingBoolValue)
//
// Returns:
//   - *bool: A pointer to bool if the field should be enabled, nil if disabled
//
// Behavior by mode:
//   - FieldParsingKeyExistence: Returns new(bool) if key exists, nil otherwise
//   - FieldParsingBoolValue: Returns &boolValue if key exists and is bool, nil otherwise
func parseUpdateEntityBoolField(configMap map[string]any, fieldName string, mode FieldParsingMode) *bool {
	if configMap == nil {
		return nil
	}

	val, exists := configMap[fieldName]
	if !exists {
		return nil
	}

	switch mode {
	case FieldParsingKeyExistence:
		// Key presence (even if nil) indicates field can be updated
		return new(bool) // Allocate a new bool pointer (defaults to false)

	case FieldParsingBoolValue:
		// Parse actual boolean value from config
		if boolVal, ok := val.(bool); ok {
			return &boolVal
		}
		// If present but not a bool (e.g., null), return nil (no explicit setting)
		return nil

	default:
		return nil
	}
}

// UpdateEntityFieldSpec defines a boolean field to be parsed from config
type UpdateEntityFieldSpec struct {
	Name string           // Field name in config (e.g., "title", "body", "status")
	Mode FieldParsingMode // Parsing mode for this field
	Dest **bool           // Pointer to the destination field in the config struct
}

// UpdateEntityParseOptions holds options for parsing entity-specific configuration
type UpdateEntityParseOptions struct {
	EntityType   UpdateEntityType        // Type of entity being parsed
	ConfigKey    string                  // Config key (e.g., "update-issue")
	Logger       *logger.Logger          // Logger for this entity type
	Fields       []UpdateEntityFieldSpec // Field specifications to parse
	CustomParser func(map[string]any)    // Optional custom field parser
}

// parseUpdateEntityConfigWithFields is a generic helper that reduces scaffolding duplication
// across update entity parsers by handling:
// 1. Calling parseUpdateEntityBase to get base config and config map
// 2. Parsing entity-specific bool fields according to field specs
// 3. Calling optional custom parser for special fields
//
// This eliminates the repetitive pattern of:
//
//	baseConfig, configMap := c.parseUpdateEntityBase(...)
//	if baseConfig == nil { return nil }
//	cfg := &SpecificConfig{UpdateEntityConfig: *baseConfig}
//	cfg.Field1 = parseUpdateEntityBoolField(configMap, "field1", mode)
//	cfg.Field2 = parseUpdateEntityBoolField(configMap, "field2", mode)
//	...
//
// Returns nil if parsing fails, otherwise parsing is done in-place via field specs.
func (c *Compiler) parseUpdateEntityConfigWithFields(
	outputMap map[string]any,
	opts UpdateEntityParseOptions,
) (*UpdateEntityConfig, map[string]any) {
	// Parse base configuration using helper
	baseConfig, configMap := c.parseUpdateEntityBase(
		outputMap,
		opts.EntityType,
		opts.ConfigKey,
		opts.Logger,
	)
	if baseConfig == nil {
		return nil, nil
	}

	// Parse entity-specific bool fields according to specs
	for _, field := range opts.Fields {
		*field.Dest = parseUpdateEntityBoolField(configMap, field.Name, field.Mode)
	}

	// Call custom parser if provided (e.g., for AllowedLabels in discussions)
	if opts.CustomParser != nil && configMap != nil {
		opts.CustomParser(configMap)
	}

	return baseConfig, configMap
}

// parseUpdateEntityConfigTyped is a generic helper that eliminates the final
// scaffolding duplication in update entity parsers.
//
// It handles the complete parsing flow:
//  1. Creates entity-specific config struct
//  2. Builds field specs with pointers to config fields
//  3. Calls parseUpdateEntityConfigWithFields
//  4. Checks for nil result (early return)
//  5. Copies base config into entity-specific struct
//  6. Returns typed config
//
// Type parameter:
//   - T: The entity-specific config type (must embed UpdateEntityConfig)
//
// Parameters:
//   - c: Compiler instance
//   - outputMap: The safe-outputs configuration map
//   - entityType: Type of entity (issue, pull request, discussion, release)
//   - configKey: Config key in YAML (e.g., "update-issue")
//   - logger: Logger for this entity type
//   - buildFields: Function that receives the config struct and returns field specs
//   - customParser: Optional custom parser for special fields (can be nil)
//
// Returns:
//   - *T: Pointer to the parsed and populated config struct, or nil if parsing failed
//
// Usage example:
//
//	func (c *Compiler) parseUpdateIssuesConfig(outputMap map[string]any) *UpdateIssuesConfig {
//	    return parseUpdateEntityConfigTyped(c, outputMap,
//	        UpdateEntityIssue, "update-issue", updateIssueLog,
//	        func(cfg *UpdateIssuesConfig) []UpdateEntityFieldSpec {
//	            return []UpdateEntityFieldSpec{
//	                {Name: "status", Mode: FieldParsingKeyExistence, Dest: &cfg.Status},
//	                {Name: "title", Mode: FieldParsingKeyExistence, Dest: &cfg.Title},
//	                {Name: "body", Mode: FieldParsingKeyExistence, Dest: &cfg.Body},
//	            }
//	        }, nil)
//	}
func parseUpdateEntityConfigTyped[T any](
	c *Compiler,
	outputMap map[string]any,
	entityType UpdateEntityType,
	configKey string,
	logger *logger.Logger,
	buildFields func(*T) []UpdateEntityFieldSpec,
	customParser func(map[string]any, *T),
) *T {
	// Create entity-specific config struct
	cfg := new(T)

	// Build field specs with pointers to config fields
	fields := buildFields(cfg)

	// Build parsing options
	opts := UpdateEntityParseOptions{
		EntityType: entityType,
		ConfigKey:  configKey,
		Logger:     logger,
		Fields:     fields,
	}

	// Add custom parser wrapper if provided
	if customParser != nil {
		opts.CustomParser = func(cm map[string]any) {
			customParser(cm, cfg)
		}
	}

	// Parse base config and entity-specific fields
	baseConfig, _ := c.parseUpdateEntityConfigWithFields(outputMap, opts)
	if baseConfig == nil {
		return nil
	}

	// Use type assertion to set base config
	// Since we can't use interface assertion with generics directly,
	// we use type switch via any to assign the base config
	cfgAny := any(cfg)
	switch v := cfgAny.(type) {
	case *UpdateIssuesConfig:
		v.UpdateEntityConfig = *baseConfig
	case *UpdateDiscussionsConfig:
		v.UpdateEntityConfig = *baseConfig
	case *UpdatePullRequestsConfig:
		v.UpdateEntityConfig = *baseConfig
	case *UpdateReleaseConfig:
		v.UpdateEntityConfig = *baseConfig
	}

	return cfg
}
