package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var discussionLog = logger.New("workflow:create_discussion")

// CreateDiscussionsConfig holds configuration for creating GitHub discussions from agent output
type CreateDiscussionsConfig struct {
	BaseSafeOutputConfig  `yaml:",inline"`
	TitlePrefix           string   `yaml:"title-prefix,omitempty"`
	Category              string   `yaml:"category,omitempty"`                // Discussion category ID or name
	Labels                []string `yaml:"labels,omitempty"`                  // Labels to attach to discussions and match when closing older ones
	AllowedLabels         []string `yaml:"allowed-labels,omitempty"`          // Optional list of allowed labels. If omitted, any labels are allowed (including creating new ones).
	TargetRepoSlug        string   `yaml:"target-repo,omitempty"`             // Target repository in format "owner/repo" for cross-repository discussions
	AllowedRepos          []string `yaml:"allowed-repos,omitempty"`           // List of additional repositories that discussions can be created in
	CloseOlderDiscussions bool     `yaml:"close-older-discussions,omitempty"` // When true, close older discussions with same title prefix or labels as outdated
	RequiredCategory      string   `yaml:"required-category,omitempty"`       // Required category for matching when close-older-discussions is enabled
	Expires               int      `yaml:"expires,omitempty"`                 // Hours until the discussion expires and should be automatically closed
	FallbackToIssue       *bool    `yaml:"fallback-to-issue,omitempty"`       // When true (default), fallback to create-issue if discussion creation fails due to permissions
}

// parseDiscussionsConfig handles create-discussion configuration
func (c *Compiler) parseDiscussionsConfig(outputMap map[string]any) *CreateDiscussionsConfig {
	// Check if the key exists
	if _, exists := outputMap["create-discussion"]; !exists {
		return nil
	}

	discussionLog.Print("Parsing create-discussion configuration")

	// Get the config data to check for special cases before unmarshaling
	configData, _ := outputMap["create-discussion"].(map[string]any)

	// Pre-process the expires field (convert to hours before unmarshaling)
	expiresDisabled := false
	if configData != nil {
		if expires, exists := configData["expires"]; exists {
			// Always parse the expires value through parseExpiresFromConfig
			// This handles: integers (days), strings (time specs like "48h"), and boolean false
			expiresInt := parseExpiresFromConfig(configData)
			if expiresInt == -1 {
				// Explicitly disabled with false
				expiresDisabled = true
				configData["expires"] = 0
			} else if expiresInt > 0 {
				configData["expires"] = expiresInt
			} else {
				// Invalid or missing - set to 0
				configData["expires"] = 0
			}
			discussionLog.Printf("Parsed expires value %v to %d hours (disabled=%t)", expires, expiresInt, expiresDisabled)
		}
	}

	// Unmarshal into typed config struct
	var config CreateDiscussionsConfig
	if err := unmarshalConfig(outputMap, "create-discussion", &config, discussionLog); err != nil {
		discussionLog.Printf("Failed to unmarshal config: %v", err)
		// For backward compatibility, handle nil/empty config
		config = CreateDiscussionsConfig{}
	}

	// Set default max if not specified
	if config.Max == 0 {
		config.Max = 1
	}

	// Set default expires to 7 days (168 hours) if not specified and not explicitly disabled
	if config.Expires == 0 && !expiresDisabled {
		config.Expires = 168 // 7 days = 168 hours
		discussionLog.Print("Using default expiration: 7 days (168 hours)")
	} else if expiresDisabled {
		config.Expires = 0
		discussionLog.Print("Expiration explicitly disabled")
	}

	// Set default fallback-to-issue to true if not specified
	if config.FallbackToIssue == nil {
		trueValue := true
		config.FallbackToIssue = &trueValue
		discussionLog.Print("Using default fallback-to-issue: true")
	}

	// Validate target-repo (wildcard "*" is not allowed)
	if validateTargetRepoSlug(config.TargetRepoSlug, discussionLog) {
		return nil // Invalid configuration, return nil to cause validation error
	}

	// Normalize and validate category naming convention
	config.Category = normalizeDiscussionCategory(config.Category, discussionLog, c.markdownPath)

	// Log configured values
	if config.TitlePrefix != "" {
		discussionLog.Printf("Title prefix configured: %q", config.TitlePrefix)
	}
	if config.Category != "" {
		discussionLog.Printf("Discussion category configured: %q", config.Category)
	}
	if len(config.Labels) > 0 {
		discussionLog.Printf("Labels configured: %v", config.Labels)
	}
	if len(config.AllowedLabels) > 0 {
		discussionLog.Printf("Allowed labels configured: %v", config.AllowedLabels)
	}
	if config.TargetRepoSlug != "" {
		discussionLog.Printf("Target repository configured: %s", config.TargetRepoSlug)
	}
	if len(config.AllowedRepos) > 0 {
		discussionLog.Printf("Allowed repos configured: %v", config.AllowedRepos)
	}
	if config.CloseOlderDiscussions {
		discussionLog.Print("Close older discussions enabled")
		if config.RequiredCategory != "" {
			discussionLog.Printf("Required category for close older discussions: %q", config.RequiredCategory)
		}
	}
	if config.Expires > 0 {
		discussionLog.Printf("Discussion expiration configured: %d hours", config.Expires)
	}
	if config.FallbackToIssue != nil {
		discussionLog.Printf("Fallback to issue configured: %t", *config.FallbackToIssue)
	}

	return &config
}

// buildCreateOutputDiscussionJob creates the create_discussion job
func (c *Compiler) buildCreateOutputDiscussionJob(data *WorkflowData, mainJobName string, createIssueJobName string) (*Job, error) {
	discussionLog.Printf("Building create_discussion job for workflow: %s", data.Name)

	if data.SafeOutputs == nil || data.SafeOutputs.CreateDiscussions == nil {
		return nil, fmt.Errorf("safe-outputs.create-discussion configuration is required")
	}

	// Build custom environment variables specific to create-discussion using shared helpers
	var customEnvVars []string
	customEnvVars = append(customEnvVars, buildTitlePrefixEnvVar("GH_AW_DISCUSSION_TITLE_PREFIX", data.SafeOutputs.CreateDiscussions.TitlePrefix)...)
	customEnvVars = append(customEnvVars, buildCategoryEnvVar("GH_AW_DISCUSSION_CATEGORY", data.SafeOutputs.CreateDiscussions.Category)...)
	customEnvVars = append(customEnvVars, buildLabelsEnvVar("GH_AW_DISCUSSION_LABELS", data.SafeOutputs.CreateDiscussions.Labels)...)
	customEnvVars = append(customEnvVars, buildLabelsEnvVar("GH_AW_DISCUSSION_ALLOWED_LABELS", data.SafeOutputs.CreateDiscussions.AllowedLabels)...)
	customEnvVars = append(customEnvVars, buildAllowedReposEnvVar("GH_AW_ALLOWED_REPOS", data.SafeOutputs.CreateDiscussions.AllowedRepos)...)

	// Add close-older-discussions flag if enabled
	if data.SafeOutputs.CreateDiscussions.CloseOlderDiscussions {
		customEnvVars = append(customEnvVars, "          GH_AW_CLOSE_OLDER_DISCUSSIONS: \"true\"\n")
	}

	// Add expires value if set
	if data.SafeOutputs.CreateDiscussions.Expires > 0 {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_DISCUSSION_EXPIRES: \"%d\"\n", data.SafeOutputs.CreateDiscussions.Expires))
	}

	// Add fallback-to-issue flag
	if data.SafeOutputs.CreateDiscussions.FallbackToIssue != nil && *data.SafeOutputs.CreateDiscussions.FallbackToIssue {
		customEnvVars = append(customEnvVars, "          GH_AW_DISCUSSION_FALLBACK_TO_ISSUE: \"true\"\n")
	}

	// Add environment variable for temporary ID map from create_issue job
	if createIssueJobName != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_TEMPORARY_ID_MAP: ${{ needs.%s.outputs.temporary_id_map }}\n", createIssueJobName))
	}

	discussionLog.Printf("Configured %d custom environment variables for discussion creation", len(customEnvVars))

	// Add standard environment variables (metadata + staged/target repo)
	customEnvVars = append(customEnvVars, c.buildStandardSafeOutputEnvVars(data, data.SafeOutputs.CreateDiscussions.TargetRepoSlug)...)

	// Create outputs for the job
	outputs := map[string]string{
		"discussion_number": "${{ steps.create_discussion.outputs.discussion_number }}",
		"discussion_url":    "${{ steps.create_discussion.outputs.discussion_url }}",
	}

	// Build the needs list - always depend on mainJobName, and conditionally on create_issue
	needs := []string{mainJobName}
	if createIssueJobName != "" {
		needs = append(needs, createIssueJobName)
	}

	// Use the shared builder function to create the job
	return c.buildSafeOutputJob(data, SafeOutputJobConfig{
		JobName:        "create_discussion",
		StepName:       "Create Output Discussion",
		StepID:         "create_discussion",
		MainJobName:    mainJobName,
		CustomEnvVars:  customEnvVars,
		Script:         getCreateDiscussionScript(),
		Permissions:    NewPermissionsContentsReadIssuesWriteDiscussionsWrite(),
		Outputs:        outputs,
		Needs:          needs,
		Token:          data.SafeOutputs.CreateDiscussions.GitHubToken,
		TargetRepoSlug: data.SafeOutputs.CreateDiscussions.TargetRepoSlug,
	})
}

// normalizeDiscussionCategory normalizes discussion category to lowercase
// and provides warnings about naming conventions
// Returns normalized category (or original if it's a category ID)
func normalizeDiscussionCategory(category string, log *logger.Logger, markdownPath string) string {
	// Empty category is allowed (GitHub Discussions will use default)
	if category == "" {
		return category
	}

	// GitHub Discussion category IDs start with "DIC_" - don't normalize these
	if strings.HasPrefix(category, "DIC_") {
		return category
	}

	// List of known category naming issues and their corrections
	categoryCorrections := map[string]string{
		"Audits":   "audits",
		"General":  "general",
		"Reports":  "reports",
		"Research": "research",
	}

	// Check if category has uppercase letters and normalize
	normalizedCategory := strings.ToLower(category)
	if category != normalizedCategory {
		var message string
		// Check if we have a known correction
		if corrected, exists := categoryCorrections[category]; exists {
			message = fmt.Sprintf("Discussion category %q normalized to lowercase: %q", category, corrected)
			if log != nil {
				log.Printf("Normalized discussion category %q to lowercase: %q", category, corrected)
			}
		} else {
			message = fmt.Sprintf("Discussion category %q normalized to lowercase: %q", category, normalizedCategory)
			if log != nil {
				log.Printf("Normalized discussion category %q to lowercase: %q", category, normalizedCategory)
			}
		}

		// Print formatted info message to stderr
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "info", message))
	}

	// Warn about singular forms of common categories
	singularToPlural := map[string]string{
		"audit":  "audits",
		"report": "reports",
	}

	if plural, isSingular := singularToPlural[normalizedCategory]; isSingular {
		if log != nil {
			log.Printf("⚠ Discussion category %q is singular; consider using plural form %q for consistency", normalizedCategory, plural)
		}
	}

	return normalizedCategory
}
