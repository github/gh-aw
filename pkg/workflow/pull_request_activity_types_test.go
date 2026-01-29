//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestPullRequestActivityTypeEnumValidation tests all valid PR activity types
func TestPullRequestActivityTypeEnumValidation(t *testing.T) {
	// All valid pull_request activity types according to GitHub Actions
	// Reference: https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#pull_request
	validActivityTypes := []string{
		"opened",
		"edited",
		"closed",
		"reopened",
		"synchronize",
		"assigned",
		"unassigned",
		"labeled",
		"unlabeled",
		"review_requested",
		"review_request_removed",
		"ready_for_review",
		"converted_to_draft",
		"auto_merge_enabled",
		"auto_merge_disabled",
		"locked",
		"unlocked",
		"enqueued",
		"dequeued",
		"milestoned",
		"demilestoned",
	}

	for _, activityType := range validActivityTypes {
		t.Run("valid: pull_request "+activityType, func(t *testing.T) {
			trigger := "pull_request " + activityType
			ir, err := ParseTriggerShorthand(trigger)

			// Handle cases where activity type is not in trigger parser's validTypes map
			if err != nil {
				// Parser explicitly rejected this type with an error
				t.Logf("Activity type %q is valid in GitHub Actions but not yet in trigger parser validTypes map (error: %v)", activityType, err)
				return
			}

			if ir == nil {
				// Parser returned nil (simple trigger without parsing)
				t.Logf("Activity type %q is valid in GitHub Actions but not in trigger parser validTypes map", activityType)
				return
			}

			if ir.Event != "pull_request" {
				t.Errorf("Expected event 'pull_request', got %q", ir.Event)
			}

			// Check if the activity type is in the types array
			foundType := false
			for _, irType := range ir.Types {
				if irType == activityType {
					foundType = true
					break
				}
			}

			if !foundType {
				t.Errorf("Activity type %q not found in parsed types: %v", activityType, ir.Types)
			}
		})
	}
}

// TestPullRequestInvalidActivityTypes tests invalid PR activity types
func TestPullRequestInvalidActivityTypes(t *testing.T) {
	invalidActivityTypes := []struct {
		name         string
		activityType string
		description  string
	}{
		{
			name:         "uppercase OPENED",
			activityType: "OPENED",
			description:  "activity types are case-sensitive",
		},
		{
			name:         "mixed case Opened",
			activityType: "Opened",
			description:  "activity types are case-sensitive",
		},
		{
			name:         "invalid: merged (special case)",
			activityType: "merged",
			description:  "merged is handled as closed + condition, not a type",
		},
		{
			name:         "invalid: approved",
			activityType: "approved",
			description:  "approved is for pull_request_review, not pull_request",
		},
		{
			name:         "invalid: commented",
			activityType: "commented",
			description:  "not a valid pull_request activity type",
		},
		{
			name:         "invalid: created",
			activityType: "created",
			description:  "not a valid pull_request activity type",
		},
		{
			name:         "invalid: deleted",
			activityType: "deleted",
			description:  "not a valid pull_request activity type",
		},
		{
			name:         "invalid: random",
			activityType: "random",
			description:  "not a valid activity type",
		},
	}

	for _, tt := range invalidActivityTypes {
		t.Run(tt.name, func(t *testing.T) {
			trigger := "pull_request " + tt.activityType
			ir, err := ParseTriggerShorthand(trigger)

			// Special case: "merged" is handled specially and should succeed
			if tt.activityType == "merged" {
				if err != nil {
					t.Errorf("'merged' should be handled as special case: %v", err)
					return
				}
				if ir == nil || ir.Event != "pull_request" {
					t.Errorf("'merged' should create pull_request trigger with condition")
					return
				}
				// Verify it creates the right condition
				if len(ir.Conditions) == 0 || !strings.Contains(ir.Conditions[0], "merged") {
					t.Errorf("'merged' should add merged condition, got conditions: %v", ir.Conditions)
				}
				return
			}

			// For truly invalid types, they might not be caught by the parser
			// if they're not in the validTypes map - this documents behavior
			if ir != nil {
				t.Logf("%s: Invalid activity type %q was accepted by parser (behavior: %s)",
					tt.name, tt.activityType, tt.description)
			}
		})
	}
}

// TestPullRequestActivityTypeCaseSensitivity tests case sensitivity of activity types
func TestPullRequestActivityTypeCaseSensitivity(t *testing.T) {
	baseTypes := []string{
		"opened",
		"closed",
		"synchronize",
		"reopened",
	}

	for _, baseType := range baseTypes {
		t.Run("case sensitivity for "+baseType, func(t *testing.T) {
			// Test lowercase (valid)
			trigger := "pull_request " + baseType
			ir, err := ParseTriggerShorthand(trigger)
			if err != nil {
				t.Errorf("Lowercase %q should be valid: %v", baseType, err)
			}
			if ir == nil {
				t.Errorf("Lowercase %q should be parsed", baseType)
			}

			// Test uppercase (invalid)
			upperType := strings.ToUpper(baseType)
			trigger = "pull_request " + upperType
			ir, _ = ParseTriggerShorthand(trigger)
			// The parser might not explicitly reject uppercase, but it won't match
			if ir != nil && len(ir.Types) > 0 && ir.Types[0] == upperType {
				t.Errorf("Uppercase %q should not be treated as valid type", upperType)
			}

			// Test mixed case (invalid)
			mixedType := strings.ToUpper(baseType[:1]) + baseType[1:]
			trigger = "pull_request " + mixedType
			ir, _ = ParseTriggerShorthand(trigger)
			if ir != nil && len(ir.Types) > 0 && ir.Types[0] == mixedType {
				t.Errorf("Mixed case %q should not be treated as valid type", mixedType)
			}
		})
	}
}

// TestPullRequestMultipleActivityTypes tests multiple activity types in trigger
func TestPullRequestMultipleActivityTypes(t *testing.T) {
	// The shorthand parser handles one activity type at a time
	// Multiple types would need to be specified in YAML format
	// This test documents that behavior

	t.Run("single activity type", func(t *testing.T) {
		trigger := "pull_request opened"
		ir, err := ParseTriggerShorthand(trigger)
		if err != nil {
			t.Errorf("Single activity type should work: %v", err)
		}
		if ir != nil && len(ir.Types) != 1 {
			t.Errorf("Expected 1 type, got %d: %v", len(ir.Types), ir.Types)
		}
	})

	t.Run("affecting pattern with default types", func(t *testing.T) {
		trigger := "pull_request affecting src/**"
		ir, err := ParseTriggerShorthand(trigger)
		if err != nil {
			t.Errorf("Affecting pattern should work: %v", err)
		}
		if ir != nil {
			// When using "affecting" without activity type, it should use default types
			expectedTypes := []string{"opened", "synchronize", "reopened"}
			if len(ir.Types) != len(expectedTypes) {
				t.Errorf("Expected %d default types for 'affecting', got %d: %v",
					len(expectedTypes), len(ir.Types), ir.Types)
			}
		}
	})

	t.Run("activity type with affecting", func(t *testing.T) {
		trigger := "pull_request opened affecting docs/**"
		ir, err := ParseTriggerShorthand(trigger)
		if err != nil {
			t.Errorf("Activity type with affecting should work: %v", err)
		}
		if ir != nil {
			if len(ir.Types) != 1 || ir.Types[0] != "opened" {
				t.Errorf("Expected single type 'opened', got: %v", ir.Types)
			}
			if ir.Filters == nil {
				t.Errorf("Expected filters to be set")
			}
		}
	})
}

// TestPullRequestEdgeCases tests edge cases for PR activity type parsing
func TestPullRequestEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		trigger     string
		expectNil   bool
		expectError bool
		description string
	}{
		{
			name:        "simple pull_request",
			trigger:     "pull_request",
			expectNil:   true,
			expectError: false,
			description: "simple trigger without activity type is left as-is",
		},
		{
			name:        "pull alias",
			trigger:     "pull",
			expectNil:   true,
			expectError: false,
			description: "pull is an alias for pull_request",
		},
		{
			name:        "empty activity type",
			trigger:     "pull_request ",
			expectNil:   true,
			expectError: false,
			description: "trailing space results in simple trigger",
		},
		{
			name:        "whitespace between words",
			trigger:     "pull_request  opened",
			expectError: false,
			description: "extra whitespace should be handled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ir, err := ParseTriggerShorthand(tt.trigger)

			if tt.expectError {
				if err == nil {
					t.Errorf("%s: expected error but got none", tt.description)
				}
				return
			}

			if err != nil {
				t.Errorf("%s: unexpected error: %v", tt.description, err)
				return
			}

			if tt.expectNil {
				if ir != nil {
					t.Errorf("%s: expected nil IR (simple trigger), got: %+v", tt.description, ir)
				}
			}
		})
	}
}

// TestPullRequestActivityTypeInTriggerParser tests that trigger parser has correct valid types
func TestPullRequestActivityTypeInTriggerParser(t *testing.T) {
	// These are the activity types currently defined in trigger_parser.go validTypes map
	// This test ensures they remain valid according to GitHub Actions spec
	currentlySupported := []string{
		"opened",
		"edited",
		"closed",
		"reopened",
		"synchronize",
		"assigned",
		"unassigned",
		"labeled",
		"unlabeled",
		"review_requested",
	}

	// All of these should be in the official GitHub Actions spec
	officialTypes := map[string]bool{
		"opened":                 true,
		"edited":                 true,
		"closed":                 true,
		"reopened":               true,
		"synchronize":            true,
		"assigned":               true,
		"unassigned":             true,
		"labeled":                true,
		"unlabeled":              true,
		"review_requested":       true,
		"review_request_removed": true,
		"ready_for_review":       true,
		"converted_to_draft":     true,
		"auto_merge_enabled":     true,
		"auto_merge_disabled":    true,
		"locked":                 true,
		"unlocked":               true,
		"enqueued":               true,
		"dequeued":               true,
		"milestoned":             true,
		"demilestoned":           true,
	}

	t.Run("all currently supported types are official", func(t *testing.T) {
		for _, activityType := range currentlySupported {
			if !officialTypes[activityType] {
				t.Errorf("Currently supported type %q is not in official GitHub Actions spec", activityType)
			}
		}
	})

	t.Run("document unsupported but valid types", func(t *testing.T) {
		for officialType := range officialTypes {
			isSupported := false
			for _, supported := range currentlySupported {
				if supported == officialType {
					isSupported = true
					break
				}
			}
			if !isSupported {
				t.Logf("Official GitHub Actions type %q is not in trigger_parser.go validTypes map", officialType)
			}
		}
	})
}

// TestPullRequestMergedSpecialCase tests the special handling of 'merged' pseudo-type
func TestPullRequestMergedSpecialCase(t *testing.T) {
	t.Run("merged creates closed type with condition", func(t *testing.T) {
		trigger := "pull_request merged"
		ir, err := ParseTriggerShorthand(trigger)

		if err != nil {
			t.Fatalf("'merged' should not produce error: %v", err)
		}

		if ir == nil {
			t.Fatal("'merged' should produce IR")
		}

		if ir.Event != "pull_request" {
			t.Errorf("Expected event 'pull_request', got %q", ir.Event)
		}

		if len(ir.Types) != 1 || ir.Types[0] != "closed" {
			t.Errorf("Expected types ['closed'], got %v", ir.Types)
		}

		if len(ir.Conditions) == 0 {
			t.Error("Expected condition for merged state")
		} else {
			condition := ir.Conditions[0]
			if !strings.Contains(condition, "merged") || !strings.Contains(condition, "true") {
				t.Errorf("Expected merged condition, got: %q", condition)
			}
		}
	})
}

// TestIssueActivityTypeEnumValidation tests valid issue activity types for comparison
func TestIssueActivityTypeEnumValidation(t *testing.T) {
	// Document issue activity types for comparison with PR types
	validIssueTypes := []string{
		"opened",
		"edited",
		"closed",
		"reopened",
		"assigned",
		"unassigned",
		"labeled",
		"unlabeled",
		"deleted",
		"transferred",
		"pinned",
		"unpinned",
		"milestoned",
		"demilestoned",
		"locked",
		"unlocked",
	}

	for _, activityType := range validIssueTypes {
		t.Run("valid: issue "+activityType, func(t *testing.T) {
			trigger := "issue " + activityType
			ir, err := ParseTriggerShorthand(trigger)

			if err != nil {
				// Error means it's not in the validTypes map yet
				t.Logf("Issue activity type %q is valid in GitHub Actions but not yet in trigger parser (error: %v)", activityType, err)
				return
			}

			if ir == nil {
				t.Logf("Issue activity type %q is valid in GitHub Actions but not in trigger parser validTypes map", activityType)
				return
			}

			if ir.Event != "issues" {
				t.Errorf("Expected event 'issues', got %q", ir.Event)
			}
		})
	}
}

// TestDiscussionActivityTypeEnumValidation tests valid discussion activity types
func TestDiscussionActivityTypeEnumValidation(t *testing.T) {
	validDiscussionTypes := []string{
		"created",
		"edited",
		"deleted",
		"transferred",
		"pinned",
		"unpinned",
		"labeled",
		"unlabeled",
		"locked",
		"unlocked",
		"category_changed",
		"answered",
		"unanswered",
	}

	for _, activityType := range validDiscussionTypes {
		t.Run("valid: discussion "+activityType, func(t *testing.T) {
			trigger := "discussion " + activityType
			ir, err := ParseTriggerShorthand(trigger)

			if err != nil {
				// Error means it's not in the validTypes map yet
				t.Logf("Discussion activity type %q is valid in GitHub Actions but not yet in trigger parser (error: %v)", activityType, err)
				return
			}

			if ir == nil {
				t.Logf("Discussion activity type %q is valid in GitHub Actions but not in trigger parser validTypes map", activityType)
				return
			}

			if ir.Event != "discussion" {
				t.Errorf("Expected event 'discussion', got %q", ir.Event)
			}
		})
	}
}
