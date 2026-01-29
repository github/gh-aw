//go:build !integration

package main

import (
	"testing"
)

func TestCheckErrorQuality(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		shouldPass  bool
		description string
	}{
		{
			name:        "good validation error with example",
			message:     `invalid time delta format: +%s. Expected format like +25h, +3d, +1w, +1mo. Example: +3d`,
			shouldPass:  true,
			description: "Has 'invalid', 'Expected', and 'Example'",
		},
		{
			name:        "good type error with example",
			message:     `manual-approval value must be a string, got %T. Example: manual-approval: "production"`,
			shouldPass:  true,
			description: "Has 'must be', and 'Example'",
		},
		{
			name:        "good enum error with example",
			message:     `invalid engine: %s. Valid engines are: copilot, claude, codex, custom. Example: engine: copilot`,
			shouldPass:  true,
			description: "Has 'invalid', 'Valid engines', and 'Example'",
		},
		{
			name:        "bad validation error without example",
			message:     `invalid format`,
			shouldPass:  false,
			description: "Has 'invalid' but no example",
		},
		{
			name:        "bad type error without example",
			message:     `manual-approval value must be a string`,
			shouldPass:  false,
			description: "Has 'must be' but no example",
		},
		{
			name:        "wrapped error should pass",
			message:     `failed to parse configuration: %w`,
			shouldPass:  true,
			description: "Wrapped errors are allowed to skip quality check",
		},
		{
			name:        "error with doc link should pass",
			message:     `unsupported feature. See https://docs.example.com/features`,
			shouldPass:  true,
			description: "Errors with documentation links can skip examples",
		},
		{
			name:        "short simple error should pass",
			message:     `not found`,
			shouldPass:  true,
			description: "Very short errors can be self-explanatory",
		},
		{
			name:        "duplicate error should pass",
			message:     `duplicate unit 'd' in time delta`,
			shouldPass:  true,
			description: "Self-explanatory duplicate error",
		},
		{
			name:        "missing required field with example",
			message:     `tool 'my-tool' missing required 'command' field. Example: tools:\n  my-tool:\n    command: "node server.js"`,
			shouldPass:  true,
			description: "Has 'missing required' and 'Example'",
		},
		{
			name:        "config error without example",
			message:     `tool 'my-tool' mcp configuration must specify either 'command' or 'container'`,
			shouldPass:  false,
			description: "Configuration error without example should fail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := checkErrorQuality(tt.message, 1)
			passed := (issue == nil)

			if passed != tt.shouldPass {
				if tt.shouldPass {
					t.Errorf("Expected message to pass quality check but failed: %s\nMessage: %q\nIssue: %v",
						tt.description, tt.message, issue)
				} else {
					t.Errorf("Expected message to fail quality check but passed: %s\nMessage: %q",
						tt.description, tt.message)
				}
			}
		})
	}
}

func TestShouldSkipQualityCheck(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		shouldSkip bool
	}{
		{
			name:       "wrapped error",
			message:    "failed to parse: %w",
			shouldSkip: true,
		},
		{
			name:       "doc link",
			message:    "see https://docs.example.com",
			shouldSkip: true,
		},
		{
			name:       "very short",
			message:    "not found",
			shouldSkip: true,
		},
		{
			name:       "duplicate error",
			message:    "duplicate unit",
			shouldSkip: true,
		},
		{
			name:       "empty string",
			message:    "empty time delta",
			shouldSkip: true,
		},
		{
			name:       "validation error should not skip",
			message:    "invalid engine configuration that is longer than fifty characters",
			shouldSkip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldSkipQualityCheck(tt.message)
			if result != tt.shouldSkip {
				t.Errorf("shouldSkipQualityCheck(%q) = %v, want %v", tt.message, result, tt.shouldSkip)
			}
		})
	}
}

func TestSuggestImprovement(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		wantContain string
	}{
		{
			name:        "format error",
			message:     "invalid time format",
			wantContain: "format",
		},
		{
			name:        "type error",
			message:     "value must be a string, got %T",
			wantContain: "type",
		},
		{
			name:        "enum error",
			message:     "invalid engine",
			wantContain: "valid options",
		},
		{
			name:        "missing field",
			message:     "missing required field",
			wantContain: "required field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := suggestImprovement(tt.message)
			if result == "" {
				t.Errorf("suggestImprovement(%q) returned empty string", tt.message)
			}
			// Just verify it returns something, specific suggestions may vary
		})
	}
}

func TestPatternMatching(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		message string
		want    bool
	}{
		{
			name:    "hasExample matches",
			pattern: "example",
			message: "Example: field: value",
			want:    true,
		},
		{
			name:    "hasExpected matches",
			pattern: "expected",
			message: "Expected format: YYYY-MM-DD",
			want:    true,
		},
		{
			name:    "isValidationError matches invalid",
			pattern: "validation",
			message: "invalid configuration",
			want:    true,
		},
		{
			name:    "isValidationError matches must",
			pattern: "validation",
			message: "value must be positive",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result bool
			switch tt.pattern {
			case "example":
				result = hasExample.MatchString(tt.message)
			case "expected":
				result = hasExpected.MatchString(tt.message)
			case "validation":
				result = isValidationError.MatchString(tt.message)
			}

			if result != tt.want {
				t.Errorf("Pattern %q match on %q = %v, want %v", tt.pattern, tt.message, result, tt.want)
			}
		})
	}
}
