package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var safeOutputBuilderLog = logger.New("workflow:safe_output_builder")

// SafeOutputTargetConfig contains common target-related fields for safe output configurations.
// Embed this in safe output config structs that support targeting specific items.
type SafeOutputTargetConfig struct {
	Target         string   `yaml:"target,omitempty"`        // Target for the operation: "triggering" (default), "*" (any item), or explicit number
	TargetRepoSlug string   `yaml:"target-repo,omitempty"`   // Target repository in format "owner/repo" for cross-repository operations
	AllowedRepos   []string `yaml:"allowed-repos,omitempty"` // List of additional repositories that operations can target (additionally to the target-repo)
}

// SafeOutputFilterConfig contains common filtering fields for safe output configurations.
// Embed this in safe output config structs that support filtering by labels or title prefix.
type SafeOutputFilterConfig struct {
	RequiredLabels      []string `yaml:"required-labels,omitempty"`       // Required labels for the operation
	RequiredTitlePrefix string   `yaml:"required-title-prefix,omitempty"` // Required title prefix for the operation
}

// SafeOutputDiscussionFilterConfig extends SafeOutputFilterConfig with discussion-specific fields.
type SafeOutputDiscussionFilterConfig struct {
	SafeOutputFilterConfig `yaml:",inline"`
	RequiredCategory       string `yaml:"required-category,omitempty"` // Required category for discussion operations
}

// ======================================
// Generic Config Field Parsers
// ======================================

// ParseTargetConfig parses target and target-repo fields from a config map.
// Returns the parsed SafeOutputTargetConfig and a boolean indicating if there was a validation error.
// If target-repo is "*" (wildcard), it returns an error (second return value is true).
func ParseTargetConfig(configMap map[string]any) (SafeOutputTargetConfig, bool) {
	safeOutputBuilderLog.Print("Parsing target config from map")
	config := SafeOutputTargetConfig{}

	// Parse target
	if target, exists := configMap["target"]; exists {
		if targetStr, ok := target.(string); ok {
			config.Target = targetStr
			safeOutputBuilderLog.Printf("Target set to: %s", targetStr)
		}
	}

	// Parse target-repo using shared helper with validation
	targetRepoSlug, isInvalid := parseTargetRepoWithValidation(configMap)
	if isInvalid {
		safeOutputBuilderLog.Print("Target repo validation failed")
		return config, true // Return true to indicate validation error
	}
	config.TargetRepoSlug = targetRepoSlug

	return config, false
}

// ParseFilterConfig parses required-labels and required-title-prefix fields from a config map.
func ParseFilterConfig(configMap map[string]any) SafeOutputFilterConfig {
	safeOutputBuilderLog.Print("Parsing filter config from map")
	config := SafeOutputFilterConfig{}

	// Parse required-labels
	config.RequiredLabels = parseRequiredLabelsFromConfig(configMap)
	if len(config.RequiredLabels) > 0 {
		safeOutputBuilderLog.Printf("Parsed %d required labels", len(config.RequiredLabels))
	}

	// Parse required-title-prefix
	config.RequiredTitlePrefix = parseRequiredTitlePrefixFromConfig(configMap)

	return config
}

// ParseDiscussionFilterConfig parses filter config plus required-category for discussion operations.
func ParseDiscussionFilterConfig(configMap map[string]any) SafeOutputDiscussionFilterConfig {
	config := SafeOutputDiscussionFilterConfig{
		SafeOutputFilterConfig: ParseFilterConfig(configMap),
	}

	// Parse required-category
	if requiredCategory, exists := configMap["required-category"]; exists {
		if categoryStr, ok := requiredCategory.(string); ok {
			config.RequiredCategory = categoryStr
		}
	}

	return config
}

// parseRequiredLabelsFromConfig extracts and validates required-labels from a config map.
// Returns a slice of label strings, or nil if not present or invalid.
func parseRequiredLabelsFromConfig(configMap map[string]any) []string {
	return ParseStringArrayFromConfig(configMap, "required-labels", safeOutputBuilderLog)
}

// parseRequiredTitlePrefixFromConfig extracts required-title-prefix from a config map.
// Returns the prefix string, or empty string if not present or invalid.
func parseRequiredTitlePrefixFromConfig(configMap map[string]any) string {
	return extractStringFromMap(configMap, "required-title-prefix", safeOutputBuilderLog)
}

// ======================================
// Generic Env Var Builders
// ======================================

// BuildTargetEnvVar builds a target environment variable line for safe-output jobs.
// envVarName should be the full env var name like "GH_AW_CLOSE_ISSUE_TARGET".
// Returns an empty slice if target is empty.
func BuildTargetEnvVar(envVarName string, target string) []string {
	if target == "" {
		return nil
	}
	return []string{fmt.Sprintf("          %s: %q\n", envVarName, target)}
}

// BuildRequiredLabelsEnvVar builds a required-labels environment variable line for safe-output jobs.
// envVarName should be the full env var name like "GH_AW_CLOSE_ISSUE_REQUIRED_LABELS".
// Returns an empty slice if requiredLabels is empty.
func BuildRequiredLabelsEnvVar(envVarName string, requiredLabels []string) []string {
	if len(requiredLabels) == 0 {
		return nil
	}
	labelsStr := strings.Join(requiredLabels, ",")
	return []string{fmt.Sprintf("          %s: %q\n", envVarName, labelsStr)}
}

// BuildRequiredTitlePrefixEnvVar builds a required-title-prefix environment variable line for safe-output jobs.
// envVarName should be the full env var name like "GH_AW_CLOSE_ISSUE_REQUIRED_TITLE_PREFIX".
// Returns an empty slice if requiredTitlePrefix is empty.
func BuildRequiredTitlePrefixEnvVar(envVarName string, requiredTitlePrefix string) []string {
	if requiredTitlePrefix == "" {
		return nil
	}
	return []string{fmt.Sprintf("          %s: %q\n", envVarName, requiredTitlePrefix)}
}

// BuildRequiredCategoryEnvVar builds a required-category environment variable line for discussion safe-output jobs.
// envVarName should be the full env var name like "GH_AW_CLOSE_DISCUSSION_REQUIRED_CATEGORY".
// Returns an empty slice if requiredCategory is empty.
func BuildRequiredCategoryEnvVar(envVarName string, requiredCategory string) []string {
	if requiredCategory == "" {
		return nil
	}
	return []string{fmt.Sprintf("          %s: %q\n", envVarName, requiredCategory)}
}

// BuildMaxCountEnvVar builds a max count environment variable line for safe-output jobs.
// envVarName should be the full env var name like "GH_AW_CLOSE_ISSUE_MAX_COUNT".
func BuildMaxCountEnvVar(envVarName string, maxCount int) []string {
	return []string{fmt.Sprintf("          %s: %d\n", envVarName, maxCount)}
}

// BuildAllowedListEnvVar builds an allowed list environment variable line for safe-output jobs.
// envVarName should be the full env var name like "GH_AW_LABELS_ALLOWED".
// Always outputs the env var, even when empty (empty string means "allow all").
func BuildAllowedListEnvVar(envVarName string, allowed []string) []string {
	allowedStr := strings.Join(allowed, ",")
	return []string{fmt.Sprintf("          %s: %q\n", envVarName, allowedStr)}
}

// ======================================
// Close Job Config Helpers
// ======================================

// CloseJobConfig represents common configuration for close operations (close-issue, close-discussion, close-pull-request)
type CloseJobConfig struct {
	SafeOutputTargetConfig `yaml:",inline"`
	SafeOutputFilterConfig `yaml:",inline"`
}

// ParseCloseJobConfig parses common close job fields from a config map.
// Returns the parsed CloseJobConfig and a boolean indicating if there was a validation error.
func ParseCloseJobConfig(configMap map[string]any) (CloseJobConfig, bool) {
	config := CloseJobConfig{}

	// Parse target config
	targetConfig, isInvalid := ParseTargetConfig(configMap)
	if isInvalid {
		return config, true
	}
	config.SafeOutputTargetConfig = targetConfig

	// Parse filter config
	config.SafeOutputFilterConfig = ParseFilterConfig(configMap)

	return config, false
}

// BuildCloseJobEnvVars builds common environment variables for close operations.
// prefix should be like "GH_AW_CLOSE_ISSUE" or "GH_AW_CLOSE_PR".
// Returns a slice of environment variable lines.
func BuildCloseJobEnvVars(prefix string, config CloseJobConfig) []string {
	var envVars []string

	// Add target
	envVars = append(envVars, BuildTargetEnvVar(prefix+"_TARGET", config.Target)...)

	// Add required labels
	envVars = append(envVars, BuildRequiredLabelsEnvVar(prefix+"_REQUIRED_LABELS", config.RequiredLabels)...)

	// Add required title prefix
	envVars = append(envVars, BuildRequiredTitlePrefixEnvVar(prefix+"_REQUIRED_TITLE_PREFIX", config.RequiredTitlePrefix)...)

	return envVars
}

// ======================================
// List-based Job Config Helpers
// ======================================

// ListJobConfig represents common configuration for list-based operations (add-labels, add-reviewer, assign-milestone)
type ListJobConfig struct {
	SafeOutputTargetConfig `yaml:",inline"`
	Allowed                []string `yaml:"allowed,omitempty"` // Optional list of allowed values
}

// ParseListJobConfig parses common list job fields from a config map.
// Returns the parsed ListJobConfig and a boolean indicating if there was a validation error.
func ParseListJobConfig(configMap map[string]any, allowedKey string) (ListJobConfig, bool) {
	config := ListJobConfig{}

	// Parse target config
	targetConfig, isInvalid := ParseTargetConfig(configMap)
	if isInvalid {
		return config, true
	}
	config.SafeOutputTargetConfig = targetConfig

	// Parse allowed list (using the specified key like "allowed", "reviewers", etc.)
	if allowed, exists := configMap[allowedKey]; exists {
		// Handle single string format
		if allowedStr, ok := allowed.(string); ok {
			config.Allowed = []string{allowedStr}
		} else if allowedArray, ok := allowed.([]any); ok {
			// Handle array format
			for _, item := range allowedArray {
				if itemStr, ok := item.(string); ok {
					config.Allowed = append(config.Allowed, itemStr)
				}
			}
		}
	}

	return config, false
}

// BuildListJobEnvVars builds common environment variables for list-based operations.
// prefix should be like "GH_AW_LABELS" or "GH_AW_REVIEWERS".
// Returns a slice of environment variable lines.
func BuildListJobEnvVars(prefix string, config ListJobConfig, maxCount int) []string {
	var envVars []string

	// Add allowed list
	envVars = append(envVars, BuildAllowedListEnvVar(prefix+"_ALLOWED", config.Allowed)...)

	// Add max count
	envVars = append(envVars, BuildMaxCountEnvVar(prefix+"_MAX_COUNT", maxCount)...)

	// Add target
	envVars = append(envVars, BuildTargetEnvVar(prefix+"_TARGET", config.Target)...)

	return envVars
}

// ======================================
// List Job Builder Helpers
// ======================================

// ListJobBuilderConfig contains parameters for building list-based safe-output jobs
type ListJobBuilderConfig struct {
	JobName        string        // e.g., "add_labels", "assign_milestone"
	StepName       string        // e.g., "Add Labels", "Assign Milestone"
	StepID         string        // e.g., "add_labels", "assign_milestone"
	EnvPrefix      string        // e.g., "GH_AW_LABELS", "GH_AW_MILESTONE"
	OutputName     string        // e.g., "labels_added", "assigned_milestones"
	Script         string        // JavaScript script for the operation
	Permissions    *Permissions  // Job permissions
	DefaultMax     int           // Default max count if not specified in config
	ExtraCondition ConditionNode // Additional condition to append (optional)
}

// BuildListSafeOutputJob builds a list-based safe-output job using shared logic.
// This consolidates the common builder pattern used by add-labels, assign-milestone, and assign-to-user.
func (c *Compiler) BuildListSafeOutputJob(data *WorkflowData, mainJobName string, listJobConfig ListJobConfig, baseSafeOutputConfig BaseSafeOutputConfig, builderConfig ListJobBuilderConfig) (*Job, error) {
	safeOutputBuilderLog.Printf("Building list safe-output job: %s", builderConfig.JobName)

	// Handle max count with default
	maxCount := builderConfig.DefaultMax
	if baseSafeOutputConfig.Max > 0 {
		maxCount = baseSafeOutputConfig.Max
	}
	safeOutputBuilderLog.Printf("Max count set to: %d", maxCount)

	// Build custom environment variables using shared helpers
	customEnvVars := BuildListJobEnvVars(builderConfig.EnvPrefix, listJobConfig, maxCount)

	// Add standard environment variables (metadata + staged/target repo)
	customEnvVars = append(customEnvVars, c.buildStandardSafeOutputEnvVars(data, listJobConfig.TargetRepoSlug)...)

	// Create outputs for the job
	outputs := map[string]string{
		builderConfig.OutputName: fmt.Sprintf("${{ steps.%s.outputs.%s }}", builderConfig.StepID, builderConfig.OutputName),
	}

	// Build base job condition
	jobCondition := BuildSafeOutputType(builderConfig.JobName)

	// Add extra condition if provided
	if builderConfig.ExtraCondition != nil {
		jobCondition = BuildAnd(jobCondition, builderConfig.ExtraCondition)
	}

	// Use the shared builder function to create the job
	return c.buildSafeOutputJob(data, SafeOutputJobConfig{
		JobName:        builderConfig.JobName,
		StepName:       builderConfig.StepName,
		StepID:         builderConfig.StepID,
		MainJobName:    mainJobName,
		CustomEnvVars:  customEnvVars,
		Script:         builderConfig.Script,
		Permissions:    builderConfig.Permissions,
		Outputs:        outputs,
		Condition:      jobCondition,
		Token:          baseSafeOutputConfig.GitHubToken,
		TargetRepoSlug: listJobConfig.TargetRepoSlug,
	})
}
