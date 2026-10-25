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
	BaseSafeOutputConfig          `yaml:",inline"`
	SafeOutputAllowedLabelsConfig `yaml:",inline"`
	TitlePrefix                   string           `yaml:"title-prefix,omitempty"`
	Category                      string           `yaml:"category,omitempty"`        // Discussion category ID or name
	MinBodyLength                 int              `yaml:"min-body-length,omitempty"` // Minimum required discussion body length before footer/markers
	Labels                        []string         `yaml:"labels,omitempty"`          // Labels to attach to discussions and match when closing older ones
	TargetRepoSlug                string           `yaml:"target-repo,omitempty"`     // Target repository in format "owner/repo" for cross-repository discussions
	AllowedRepos                  []string         `yaml:"allowed-repos,omitempty"`   // List of additional repositories that discussions can be created in
	CloseOlderConfig              `yaml:",inline"` // Shared close-older settings; Enabled is sourced from close-older-discussions.
	RequiredCategory              string           `yaml:"required-category,omitempty"` // Required category for matching when close-older-discussions is enabled
	Expires                       int              `yaml:"expires,omitempty"`           // Hours until the discussion expires and should be automatically closed
	FallbackToIssue               *bool            `yaml:"fallback-to-issue,omitempty"` // When true (default), fallback to create-issue if discussion creation fails due to permissions.
}

// parseCreateDiscussionsConfig handles create-discussion configuration
func (c *Compiler) parseCreateDiscussionsConfig(outputMap map[string]any) *CreateDiscussionsConfig {
	config := parseCreateEntityConfig(
		outputMap,
		"create-discussion",
		CreateParseOptions{
			BoolFields:    []string{"close-older-discussions", "footer"},
			IntFields:     []string{"max"},
			HandleExpires: true,
		},
		discussionLog,
		func(err error) *CreateDiscussionsConfig {
			discussionLog.Printf("Failed to unmarshal config: %v", err)
			// For backward compatibility, handle nil/empty config
			return &CreateDiscussionsConfig{}
		},
		nil,
		applyCreateDiscussionDefaults,
	)
	if config == nil {
		return nil
	}

	// Normalize and validate category naming convention
	config.Category = normalizeDiscussionCategory(config.Category, discussionLog, c.markdownPath)
	logCreateDiscussionConfig(config)
	return config
}

func applyCreateDiscussionDefaults(configData map[string]any, config *CreateDiscussionsConfig, expiresDisabled bool) {
	config.Enabled = closeOlderEnabledFromConfigData(configData, "close-older-discussions")
	// Set default max if not specified
	if config.Max == nil {
		config.Max = defaultIntStr(1)
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
		trueVal := true
		config.FallbackToIssue = &trueVal
		discussionLog.Print("Using default fallback-to-issue: true")
	}
}

func logCreateDiscussionConfig(config *CreateDiscussionsConfig) {
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
	if config.Enabled != nil {
		discussionLog.Print("Close older discussions flag set")
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

}

// Returns normalized category (or original if it's a category ID)
func normalizeDiscussionCategory(category string, debugLog *logger.Logger, markdownPath string) string {
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
			if debugLog != nil {
				debugLog.Printf("Normalized discussion category %q to lowercase: %q", category, corrected)
			}
		} else {
			message = fmt.Sprintf("Discussion category %q normalized to lowercase: %q", category, normalizedCategory)
			if debugLog != nil {
				debugLog.Printf("Normalized discussion category %q to lowercase: %q", category, normalizedCategory)
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
		if debugLog != nil {
			debugLog.Printf("⚠ Discussion category %q is singular; consider using plural form %q for consistency", normalizedCategory, plural)
		}
	}

	return normalizedCategory
}
