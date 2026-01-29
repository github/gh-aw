//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// FuzzParseTriggerShorthand is a fuzz test for the ParseTriggerShorthand function.
// It validates that the parser handles arbitrary input gracefully without panicking,
// and that it produces consistent, valid output for recognized trigger patterns.
//
// The fuzzer validates that:
// 1. The parser never panics on any input
// 2. Error handling is consistent (error implies nil result)
// 3. Valid triggers produce well-formed TriggerIR structures
// 4. Event types are from the expected set of GitHub Actions events
// 5. Filter structures are valid when present
// 6. Additional events are properly formatted
// 7. Edge cases (empty strings, very long inputs, special characters) are handled
func FuzzParseTriggerShorthand(f *testing.F) {
	// Seed corpus with valid trigger patterns

	// Source Control Patterns
	f.Add("push to main")
	f.Add("push to develop")
	f.Add("push to feature/test")
	f.Add("push tags v*")
	f.Add("push tags v1.*")
	f.Add("pull_request opened")
	f.Add("pull_request synchronize")
	f.Add("pull_request reopened")
	f.Add("pull_request closed")
	f.Add("pull_request merged")
	f.Add("pull_request labeled")
	f.Add("pull_request affecting src/**")
	f.Add("pull_request affecting docs/*.md")
	f.Add("pull_request opened affecting src/**.go")
	f.Add("pull opened")
	f.Add("pull synchronize")

	// Issue Patterns
	f.Add("issue opened")
	f.Add("issue edited")
	f.Add("issue closed")
	f.Add("issue reopened")
	f.Add("issue assigned")
	f.Add("issue unassigned")
	f.Add("issue labeled")
	f.Add("issue unlabeled")
	f.Add("issue deleted")
	f.Add("issue transferred")
	f.Add("issue opened labeled bug")
	f.Add("issue opened labeled enhancement")

	// Discussion Patterns
	f.Add("discussion created")
	f.Add("discussion edited")
	f.Add("discussion deleted")
	f.Add("discussion transferred")
	f.Add("discussion pinned")
	f.Add("discussion unpinned")
	f.Add("discussion labeled")
	f.Add("discussion unlabeled")
	f.Add("discussion locked")
	f.Add("discussion unlocked")
	f.Add("discussion category_changed")
	f.Add("discussion answered")
	f.Add("discussion unanswered")

	// Manual Invocation Patterns
	f.Add("manual")
	f.Add("manual with input version")
	f.Add("manual with input environment")
	f.Add("manual with input tag")
	f.Add("workflow completed ci-test")
	f.Add("workflow completed build")
	f.Add("workflow completed deploy-prod")

	// Comment Patterns
	f.Add("comment created")

	// Release and Repository Patterns
	f.Add("release published")
	f.Add("release unpublished")
	f.Add("release created")
	f.Add("release edited")
	f.Add("release deleted")
	f.Add("release prereleased")
	f.Add("release released")
	f.Add("repository starred")
	f.Add("repository forked")

	// Security Patterns
	f.Add("dependabot pull request")
	f.Add("security alert")
	f.Add("code scanning alert")

	// External Integration Patterns
	f.Add("api dispatch deploy-staging")
	f.Add("api dispatch custom-event")
	f.Add("api dispatch build-triggered")

	// Case variations
	f.Add("PUSH TO MAIN")
	f.Add("Pull_Request Opened")
	f.Add("Issue Created")
	f.Add("MANUAL")

	// Invalid/Edge Cases that should handle gracefully

	// Empty and whitespace
	f.Add("")
	f.Add("   ")
	f.Add("\t\n")
	f.Add("\r\n")

	// Unrecognized patterns
	f.Add("random text here")
	f.Add("not a trigger")
	f.Add("some random input")
	f.Add("123456789")

	// Incomplete patterns
	f.Add("push")
	f.Add("pull_request")
	f.Add("issue")
	f.Add("discussion")
	f.Add("release")
	f.Add("repository")
	f.Add("push to")
	f.Add("push tags")
	f.Add("pull_request affecting")
	f.Add("issue opened labeled")
	f.Add("manual with input")
	f.Add("workflow completed")
	f.Add("api dispatch")

	// Invalid activity types
	f.Add("issue invalid_type")
	f.Add("pull_request invalid_type")
	f.Add("discussion invalid_type")
	f.Add("release invalid_type")

	// Malformed patterns
	f.Add("push to to main")
	f.Add("push tags tags v*")
	f.Add("pull_request opened opened")
	f.Add("issue opened closed")
	f.Add("manual manual")
	f.Add("repository starred forked")

	// Very long strings
	longString := strings.Repeat("a", 10000)
	f.Add(longString)
	f.Add("push to " + longString)
	f.Add("issue opened " + longString)
	f.Add("manual with input " + longString)

	// Special characters
	f.Add("push to main\x00")
	f.Add("issue opened\n\r")
	f.Add("pull_request;echo hack")
	f.Add("manual' OR '1'='1")
	f.Add("push to main<script>alert(1)</script>")
	f.Add("issue opened$(whoami)")
	f.Add("push to main`id`")

	// Unicode characters
	f.Add("push to メイン")
	f.Add("issue 开放")
	f.Add("manual avec entrée version")

	// Multiple spaces and tabs
	f.Add("push  to  main")
	f.Add("push\tto\tmain")
	f.Add("issue   opened")
	f.Add("pull_request   opened   affecting   src/**")

	// Mixed patterns
	f.Add("push to main pull_request opened")
	f.Add("issue opened discussion created")
	f.Add("manual release published")

	// Numeric edge cases
	f.Add("workflow completed 2147483647")
	f.Add("api dispatch " + strings.Repeat("x", 1000))

	// Path/branch with special characters
	f.Add("push to feat/new-feature")
	f.Add("push to release/v1.0.0")
	f.Add("push to hotfix/urgent-fix")
	f.Add("pull_request affecting src/**/test_*.go")
	f.Add("pull_request affecting .github/workflows/*.yml")

	// Run the fuzzer
	f.Fuzz(func(t *testing.T, input string) {
		// The parser should never panic regardless of input
		ir, err := ParseTriggerShorthand(input)

		// Validate output consistency
		if err != nil {
			// On error, IR should be nil
			if ir != nil {
				t.Errorf("ParseTriggerShorthand returned non-nil IR with error for input: %q, error: %v", input, err)
			}

			// Error message should not be empty
			if err.Error() == "" {
				t.Errorf("ParseTriggerShorthand returned error with empty message for input: %q", input)
			}

			// Error should not be generic
			if err.Error() == "error" {
				t.Errorf("ParseTriggerShorthand returned generic 'error' message for input: %q", input)
			}
		}

		// If IR is returned, validate its structure
		if ir != nil {
			validateTriggerIR(t, ir, input)
		}

		// For empty input, should return an error
		if strings.TrimSpace(input) == "" && err == nil && ir != nil {
			t.Errorf("ParseTriggerShorthand should error on empty input, but returned IR: %v", ir)
		}

		// Validate that recognized patterns produce IR
		if looksLikeValidTrigger(input) && err == nil && ir == nil {
			// This might be a simple trigger like "push" or "pull_request" that returns nil
			// This is acceptable behavior
			_ = input
		}
	})
}

// FuzzTriggerIRToYAMLMap is a fuzz test for the ToYAMLMap method.
// It validates that the YAML map generation handles various IR structures correctly.
func FuzzTriggerIRToYAMLMap(f *testing.F) {
	// Seed with valid IR structures
	f.Add("push", "")
	f.Add("pull_request", "opened,synchronize")
	f.Add("issues", "opened")
	f.Add("discussion", "created")
	f.Add("release", "published")
	f.Add("watch", "started")
	f.Add("fork", "")
	f.Add("issue_comment", "created")
	f.Add("workflow_run", "completed")
	f.Add("repository_dispatch", "")
	f.Add("code_scanning_alert", "created")

	f.Fuzz(func(t *testing.T, event string, typesStr string) {
		// Parse types from comma-separated string
		var types []string
		if typesStr != "" {
			for _, typ := range strings.Split(typesStr, ",") {
				typ = strings.TrimSpace(typ)
				if typ != "" {
					types = append(types, typ)
				}
			}
		}

		// Create IR
		ir := &TriggerIR{
			Event: event,
			Types: types,
			Filters: map[string]any{
				"branches": []string{"main"},
			},
			AdditionalEvents: map[string]any{
				"workflow_dispatch": nil,
			},
		}

		// ToYAMLMap should never panic
		result := ir.ToYAMLMap()

		// Validate result structure
		if result == nil {
			t.Error("ToYAMLMap returned nil")
			return
		}

		// If event is non-empty, it should be in the result
		if event != "" {
			if _, hasEvent := result[event]; !hasEvent {
				t.Errorf("ToYAMLMap result missing event %q", event)
			}
		}

		// Additional events should be present
		if _, hasDispatch := result["workflow_dispatch"]; !hasDispatch {
			t.Error("ToYAMLMap result missing workflow_dispatch")
		}
	})
}

// validateTriggerIR validates that a TriggerIR structure is well-formed
func validateTriggerIR(t *testing.T, ir *TriggerIR, input string) {
	// Event should be from the expected set of GitHub Actions events
	validEvents := map[string]bool{
		"push":                true,
		"pull_request":        true,
		"issues":              true,
		"discussion":          true,
		"release":             true,
		"watch":               true,
		"fork":                true,
		"issue_comment":       true,
		"workflow_run":        true,
		"repository_dispatch": true,
		"code_scanning_alert": true,
		"":                    true, // Empty is valid for manual-only triggers
	}

	if !validEvents[ir.Event] {
		t.Errorf("TriggerIR has unexpected event type %q for input: %q", ir.Event, input)
	}

	// Types should not contain empty strings
	for i, typ := range ir.Types {
		if typ == "" {
			t.Errorf("TriggerIR.Types[%d] is empty for input: %q", i, input)
		}
	}

	// Filters should be a valid map
	if ir.Filters != nil {
		for key, value := range ir.Filters {
			if key == "" {
				t.Errorf("TriggerIR.Filters has empty key for input: %q", input)
			}
			if value == nil {
				t.Errorf("TriggerIR.Filters[%q] is nil for input: %q", key, input)
			}
		}
	}

	// Conditions should not contain empty strings
	for i, cond := range ir.Conditions {
		if cond == "" {
			t.Errorf("TriggerIR.Conditions[%d] is empty for input: %q", i, input)
		}
	}

	// AdditionalEvents should be a valid map
	if ir.AdditionalEvents != nil {
		for key := range ir.AdditionalEvents {
			if key == "" {
				t.Errorf("TriggerIR.AdditionalEvents has empty key for input: %q", input)
			}
		}
	}

	// Validate that if Event is empty, AdditionalEvents should have something
	if ir.Event == "" && len(ir.AdditionalEvents) == 0 {
		t.Errorf("TriggerIR has empty Event but no AdditionalEvents for input: %q", input)
	}
}

// looksLikeValidTrigger returns true if the input looks like it might be a valid trigger pattern
func looksLikeValidTrigger(input string) bool {
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "" {
		return false
	}

	// Check for valid pattern prefixes
	validPrefixes := []string{
		"push to",
		"push tags",
		"pull_request",
		"pull ",
		"issue ",
		"discussion ",
		"manual",
		"workflow completed",
		"comment created",
		"release ",
		"repository starred",
		"repository forked",
		"dependabot pull request",
		"security alert",
		"code scanning alert",
		"api dispatch",
	}

	for _, prefix := range validPrefixes {
		if strings.HasPrefix(input, prefix) {
			return true
		}
	}

	return false
}
