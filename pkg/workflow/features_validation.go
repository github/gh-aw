// This file provides validation for feature flags.
//
// # Features Validation
//
// This file validates feature flag values to ensure they meet requirements
// before being used in workflow compilation. It ensures that:
//   - action-tag uses a full 40-character SHA or a version tag when specified
//   - Other feature-specific constraints are met
//
// # Validation Functions
//
//   - validateFeatures() - Validates all feature flags in WorkflowData
//   - validateActionTag() - Validates action-tag is a full SHA or version tag
//   - semverutil.IsActionVersionTag() - Checks if a string is a valid version tag (in pkg/semverutil)
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - Adding new feature flags that require specific value formats
//   - Feature flags need cross-validation with other workflow settings
//   - Feature flag values need format or constraint checking

package workflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/semverutil"
)

var featuresValidationLog = logger.New("workflow:features_validation")

func validateDryRunFeatures(features map[string]any) error {
	var prohibited []string
	for name, value := range features {
		enabled, _ := parseFeatureValue(value)
		if enabled && strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "dangerously-") {
			prohibited = append(prohibited, "features."+name)
		}
	}
	if len(prohibited) == 0 {
		return nil
	}
	slices.Sort(prohibited)
	return fmt.Errorf("strict dry-run validation: enabled dangerously-* workflow features are not allowed: %s. Remove these entries from the workflow Markdown configuration", strings.Join(prohibited, ", "))
}

// validateFeatures validates all feature flags in the workflow data
func validateFeatures(data *WorkflowData) error {
	if data == nil || data.Features == nil {
		featuresValidationLog.Print("No features to validate")
		return nil
	}

	featuresValidationLog.Printf("Validating features: count=%d", len(data.Features))

	// Validate action-tag if present
	if actionTagVal, exists := data.Features["action-tag"]; exists {
		featuresValidationLog.Print("Validating action-tag feature")
		if err := validateActionTag(actionTagVal); err != nil {
			featuresValidationLog.Printf("Action-tag validation failed: %v", err)
			return err
		}
		featuresValidationLog.Print("Action-tag validation passed")
	}

	featuresValidationLog.Print("Features validation completed successfully")
	return nil
}

// validateActionTag validates that action-tag is a full 40-character SHA or a version tag when specified
func validateActionTag(value any) error {
	// Allow empty or nil values
	if value == nil {
		return nil
	}

	// Convert to string
	strVal, ok := value.(string)
	if !ok {
		return NewValidationError(
			"features.action-tag",
			fmt.Sprintf("%T", value),
			fmt.Sprintf("action-tag must be a string, got %T", value),
			"Provide a string value for action-tag. Example:\nfeatures:\n  action-tag: \"v0\"",
		)
	}

	// Allow empty string (falls back to version)
	if strVal == "" {
		return nil
	}

	// Accept full 40-character commit SHA
	if gitutil.IsValidFullSHA(strVal) {
		return nil
	}

	// Accept version tags like "v0", "v1", "v1.0.0"
	if semverutil.IsActionVersionTag(strVal) {
		return nil
	}

	return NewValidationError(
		"features.action-tag",
		strVal,
		fmt.Sprintf("action-tag must be a full 40-character commit SHA or a version tag (e.g. v0, v1.0.0). Got: %q", strVal),
		"Use a version tag or a full commit SHA. Examples:\nfeatures:\n  action-tag: \"v0\"\n\nOr with a full SHA:\nfeatures:\n  action-tag: \"a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0\"",
	)
}
