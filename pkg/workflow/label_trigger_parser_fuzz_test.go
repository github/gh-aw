//go:build !integration

package workflow

import (
	"testing"
)

// FuzzParseLabelTriggerShorthand is a fuzz test for the parseLabelTriggerShorthand function
// It tests that the parser handles arbitrary input gracefully without panicking
func FuzzParseLabelTriggerShorthand(f *testing.F) {
	// Seed corpus with known valid and edge case inputs
	f.Add("issue labeled bug")
	f.Add("pull_request labeled needs-review")
	f.Add("pull-request labeled approved")
	f.Add("discussion labeled question")
	f.Add("labeled bug") // implicit syntax (should not parse)
	f.Add("issue labeled")
	f.Add("issue bug labeled")
	f.Add("")
	f.Add("   ")
	f.Add("issue labeled bug enhancement priority-high")
	f.Add("pull_request labeled needs-review approved ready-to-merge")
	f.Add("discussion labeled question announcement help-wanted")
	f.Add("random text here")
	f.Add("issue\nlabeled\nbug")
	f.Add("issue\tlabeled\tbug")
	f.Add("issue  labeled  bug  enhancement")
	f.Add("ISSUE LABELED BUG")
	f.Add("Issue Labeled Bug")

	f.Fuzz(func(t *testing.T, input string) {
		// The function should never panic regardless of input
		entityType, labelNames, isLabelTrigger, err := parseLabelTriggerShorthand(input)

		// Validate the output is consistent
		if isLabelTrigger {
			// If it's recognized as a label trigger, must have entity type
			if entityType == "" {
				t.Errorf("isLabelTrigger=true but entityType is empty for input: %q", input)
			}

			// If no error, must have at least one label name
			if err == nil && len(labelNames) == 0 {
				t.Errorf("isLabelTrigger=true and err=nil but no label names for input: %q", input)
			}

			// If error is present, label names should be nil or empty
			if err != nil && len(labelNames) > 0 {
				t.Errorf("isLabelTrigger=true with error but labelNames is not empty for input: %q", input)
			}
		} else {
			// If not a label trigger, entity type should be empty
			if entityType != "" {
				t.Errorf("isLabelTrigger=false but entityType=%q for input: %q", entityType, input)
			}

			// If not a label trigger, label names should be nil
			if labelNames != nil {
				t.Errorf("isLabelTrigger=false but labelNames=%v for input: %q", labelNames, input)
			}

			// If not a label trigger, should not have an error
			if err != nil {
				t.Errorf("isLabelTrigger=false but has error for input: %q, error: %v", input, err)
			}
		}

		// Validate entity types are only the expected ones
		if entityType != "" && entityType != "issues" && entityType != "pull_request" && entityType != "discussion" {
			t.Errorf("unexpected entityType=%q for input: %q", entityType, input)
		}

		// Validate label names don't contain empty strings
		for _, label := range labelNames {
			if label == "" {
				t.Errorf("labelNames contains empty string for input: %q", input)
			}
		}
	})
}

// FuzzExpandLabelTriggerShorthand is a fuzz test for the expandLabelTriggerShorthand function
// It tests that the expansion handles various entity types and label combinations
func FuzzExpandLabelTriggerShorthand(f *testing.F) {
	// Seed corpus
	f.Add("issues", "bug")
	f.Add("pull_request", "needs-review")
	f.Add("discussion", "question")
	f.Add("issues", "bug,enhancement,priority-high")
	f.Add("unknown", "test")
	f.Add("", "bug")

	f.Fuzz(func(t *testing.T, entityType string, labelsStr string) {
		// Parse labels string into array
		var labelNames []string
		if labelsStr != "" {
			for _, label := range splitLabels(labelsStr) {
				if label != "" {
					labelNames = append(labelNames, label)
				}
			}
		}

		if len(labelNames) == 0 {
			// Skip if no labels
			return
		}

		// The function should never panic
		result := expandLabelTriggerShorthand(entityType, labelNames)

		// Validate result structure
		if result == nil {
			t.Errorf("expandLabelTriggerShorthand returned nil for entityType=%q, labels=%v", entityType, labelNames)
			return
		}

		// Check for workflow_dispatch
		if _, hasDispatch := result["workflow_dispatch"]; !hasDispatch {
			t.Errorf("result missing workflow_dispatch for entityType=%q", entityType)
		}

		// Check for trigger key (issues, pull_request, or discussion)
		hasTrigger := false
		for key := range result {
			if key == "issues" || key == "pull_request" || key == "discussion" {
				hasTrigger = true

				// Validate trigger structure
				if triggerMap, ok := result[key].(map[string]any); ok {
					// Check for types field
					if types, hasTypes := triggerMap["types"]; !hasTypes {
						t.Errorf("trigger missing types field for entityType=%q", entityType)
					} else if typeArray, ok := types.([]any); !ok {
						t.Errorf("types is not an array for entityType=%q", entityType)
					} else if len(typeArray) == 0 {
						t.Errorf("types array is empty for entityType=%q", entityType)
					}

					// Check for names field (only for issues and pull_request, not discussion)
					switch entityType {
					case "issues", "pull_request":
						if names, hasNames := triggerMap["names"]; !hasNames {
							t.Errorf("trigger missing names field for entityType=%q", entityType)
						} else if namesArray, ok := names.([]string); !ok {
							t.Errorf("names is not a string array for entityType=%q", entityType)
						} else if len(namesArray) != len(labelNames) {
							t.Errorf("names array length mismatch: got %d, want %d for entityType=%q", len(namesArray), len(labelNames), entityType)
						}
					case "discussion":
						// Discussion should not have names field (GitHub Actions doesn't support it)
						if _, hasNames := triggerMap["names"]; hasNames {
							t.Errorf("discussion trigger should not have names field, but it does")
						}
					}
				}
			}
		}

		if !hasTrigger {
			t.Errorf("result missing trigger key (issues/pull_request/discussion) for entityType=%q", entityType)
		}
	})
}

// splitLabels is a helper to split label strings for fuzzing
func splitLabels(s string) []string {
	var result []string
	for _, part := range []byte(s) {
		if part == ',' {
			result = append(result, "")
		} else if len(result) == 0 {
			result = append(result, string(part))
		} else {
			result[len(result)-1] += string(part)
		}
	}
	return result
}
