package workflow

import (
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
	logEnabled := featuresLog.Enabled()
	if logEnabled {
		featuresLog.Printf("Checking if feature is enabled: %s", flagLower)
	}

	// Inline sub-agents are now enabled by default and the corresponding
	// frontmatter flag is deprecated/no-op.
	if strings.EqualFold(flagLower, "inline-agents") {
		if logEnabled {
			featuresLog.Printf("Feature %s is deprecated and always enabled", flagLower)
		}
		return true
	}

	// The bundled threat detector is the default. The feature flag is kept
	// as an opt-out for workflows that need the legacy inline detection path.
	if strings.EqualFold(flagLower, string(constants.GHAWDetectionFeatureFlag)) {
		if enabled, found := getFeatureValueFromFrontmatter(flagLower, workflowData, logEnabled); found {
			return enabled
		}
		if isFeatureInEnvironment(flagLower, logEnabled) {
			return true
		}
		return true
	}

	// First, check if the feature is explicitly set in frontmatter.
	// Frontmatter values always take precedence.
	if enabled, found := getFeatureValueFromFrontmatter(flagLower, workflowData, logEnabled); found {
		return enabled
	}

	// Fall back to checking the environment variable
	if isFeatureInEnvironment(flagLower, logEnabled) {
		if logEnabled {
			featuresLog.Printf("Feature found in GH_AW_FEATURES: %s=true", flagLower)
		}
		return true
	}

	if logEnabled {
		featuresLog.Printf("Feature not found: %s=false", flagLower)
	}
	return false
}

func getFeatureValueFromFrontmatter(flagLower string, workflowData *WorkflowData, logEnabled bool) (bool, bool) {
	if workflowData == nil || workflowData.Features == nil {
		return false, false
	}

	if value, exists := workflowData.Features[flagLower]; exists {
		if enabled, found := parseFeatureValue(value); found {
			if logEnabled {
				featuresLog.Printf("Feature found in frontmatter: %s=%v", flagLower, enabled)
			}
			return enabled, true
		}
	}

	for key, value := range workflowData.Features {
		if strings.EqualFold(key, flagLower) {
			if enabled, found := parseFeatureValue(value); found {
				if logEnabled {
					featuresLog.Printf("Feature found in frontmatter (case-insensitive): %s=%v", flagLower, enabled)
				}
				return enabled, true
			}
		}
	}

	return false, false
}

func parseFeatureValue(value any) (bool, bool) {
	if enabled, ok := value.(bool); ok {
		return enabled, true
	}
	if strVal, ok := value.(string); ok {
		return strVal != "", true
	}
	return false, false
}

func isFeatureInEnvironment(flagLower string, logEnabled bool) bool {
	features := lookupProcessEnv("GH_AW_FEATURES")
	if features == "" {
		if logEnabled {
			featuresLog.Printf("Feature not found, GH_AW_FEATURES empty: %s=false", flagLower)
		}
		return false
	}

	if logEnabled {
		featuresLog.Printf("Checking GH_AW_FEATURES environment variable: %s", features)
	}
	for feature := range strings.SplitSeq(features, ",") {
		if strings.EqualFold(strings.TrimSpace(feature), flagLower) {
			return true
		}
	}
	return false
}
