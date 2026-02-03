// This file provides validation for GitHub Actions event filter mutual exclusivity.
//
// # Filter Validation
//
// This file validates that event filters follow GitHub Actions requirements for mutual exclusivity.
// GitHub Actions rejects workflows that specify both:
//   - branches and branches-ignore in the same event
//   - paths and paths-ignore in the same event
//
// # Validation Functions
//
//   - ValidateEventFilters() - Main entry point for filter validation
//   - validateFilterExclusivity() - Validates a single event's filter configuration
//
// # GitHub Actions Requirements
//
// From GitHub Actions documentation:
//   - You cannot use both branches and branches-ignore filters for the same event
//   - You cannot use both paths and paths-ignore filters for the same event
//
// These restrictions apply to push and pull_request event filters.
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates event filter configurations
//   - It checks for GitHub Actions filter requirements
//   - It validates mutual exclusivity of filter options
//
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var filterValidationLog = logger.New("workflow:filter_validation")

// ValidateEventFilters checks for GitHub Actions filter mutual exclusivity rules
func ValidateEventFilters(frontmatter map[string]any) error {
	filterValidationLog.Print("Validating event filter mutual exclusivity")

	on, exists := frontmatter["on"]
	if !exists {
		filterValidationLog.Print("No 'on' section found, skipping filter validation")
		return nil
	}

	onMap, ok := on.(map[string]any)
	if !ok {
		filterValidationLog.Print("'on' section is not a map, skipping filter validation")
		return nil
	}

	// Check push event
	if pushVal, exists := onMap["push"]; exists {
		filterValidationLog.Print("Validating push event filters")
		if err := validateFilterExclusivity(pushVal, "push"); err != nil {
			return err
		}
	}

	// Check pull_request event
	if prVal, exists := onMap["pull_request"]; exists {
		filterValidationLog.Print("Validating pull_request event filters")
		if err := validateFilterExclusivity(prVal, "pull_request"); err != nil {
			return err
		}
	}

	filterValidationLog.Print("Event filter validation completed successfully")
	return nil
}

// validateFilterExclusivity validates that a single event doesn't use mutually exclusive filters
func validateFilterExclusivity(eventVal any, eventName string) error {
	eventMap, ok := eventVal.(map[string]any)
	if !ok {
		filterValidationLog.Printf("Event '%s' is not a map, skipping filter validation", eventName)
		return nil
	}

	// Check branches/branches-ignore
	_, hasBranches := eventMap["branches"]
	_, hasBranchesIgnore := eventMap["branches-ignore"]

	if hasBranches && hasBranchesIgnore {
		filterValidationLog.Printf("ERROR: Event '%s' has both 'branches' and 'branches-ignore' filters", eventName)
		return fmt.Errorf("%s event cannot specify both 'branches' and 'branches-ignore' - they are mutually exclusive per GitHub Actions requirements. Use either 'branches' to include specific branches, or 'branches-ignore' to exclude specific branches, but not both", eventName)
	}

	// Check paths/paths-ignore
	_, hasPaths := eventMap["paths"]
	_, hasPathsIgnore := eventMap["paths-ignore"]

	if hasPaths && hasPathsIgnore {
		filterValidationLog.Printf("ERROR: Event '%s' has both 'paths' and 'paths-ignore' filters", eventName)
		return fmt.Errorf("%s event cannot specify both 'paths' and 'paths-ignore' - they are mutually exclusive per GitHub Actions requirements. Use either 'paths' to include specific paths, or 'paths-ignore' to exclude specific paths, but not both", eventName)
	}

	filterValidationLog.Printf("Event '%s' filters are valid", eventName)
	return nil
}
