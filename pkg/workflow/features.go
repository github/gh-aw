package workflow

import (
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var featuresLog = logger.New("workflow:features")

// isFeatureEnabled checks if a feature flag is enabled by merging information from
// the frontmatter features field and the GH_AW_FEATURES environment variable.
// Features from frontmatter take precedence over environment variables.
//
// If workflowData is nil or has no features, it falls back to checking the environment variable only.
func isFeatureEnabled(flag constants.FeatureFlag, workflowData *WorkflowData) bool {
	flagLower := strings.ToLower(strings.TrimSpace(string(flag)))
	featuresLog.Printf("Checking if feature is enabled: %s", flagLower)

	// First, check if the feature is explicitly set in frontmatter
	if workflowData != nil && workflowData.Features != nil {
		if value, exists := workflowData.Features[flagLower]; exists {
			// Convert value to boolean if it is one
			if enabled, ok := value.(bool); ok {
				featuresLog.Printf("Feature found in frontmatter: %s=%v", flagLower, enabled)
				return enabled
			}
			// If the value is not a boolean, treat non-empty strings as true
			if strVal, ok := value.(string); ok {
				enabled := strVal != ""
				featuresLog.Printf("Feature found in frontmatter (string): %s=%v", flagLower, enabled)
				return enabled
			}
		}
		// Also check case-insensitive match
		for key, value := range workflowData.Features {
			if strings.ToLower(key) == flagLower {
				// Convert value to boolean if it is one
				if enabled, ok := value.(bool); ok {
					featuresLog.Printf("Feature found in frontmatter (case-insensitive): %s=%v", flagLower, enabled)
					return enabled
				}
				// If the value is not a boolean, treat non-empty strings as true
				if strVal, ok := value.(string); ok {
					enabled := strVal != ""
					featuresLog.Printf("Feature found in frontmatter (case-insensitive, string): %s=%v", flagLower, enabled)
					return enabled
				}
			}
		}
	}

	// Fall back to checking the environment variable
	features := os.Getenv("GH_AW_FEATURES")
	if features == "" {
		featuresLog.Printf("Feature not found, GH_AW_FEATURES empty: %s=false", flagLower)
		return false
	}

	featuresLog.Printf("Checking GH_AW_FEATURES environment variable: %s", features)

	// Split by comma and check each feature
	featureList := strings.Split(features, ",")

	for _, feature := range featureList {
		if strings.ToLower(strings.TrimSpace(feature)) == flagLower {
			featuresLog.Printf("Feature found in GH_AW_FEATURES: %s=true", flagLower)
			return true
		}
	}

	featuresLog.Printf("Feature not found: %s=false", flagLower)
	return false
}
