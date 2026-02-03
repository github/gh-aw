//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/stretchr/testify/assert"
)

func TestValidateDiscussionCategory(t *testing.T) {
	tests := []struct {
		name          string
		category      string
		expectInvalid bool
	}{
		{
			name:          "empty category is valid",
			category:      "",
			expectInvalid: false,
		},
		{
			name:          "lowercase category is valid",
			category:      "audits",
			expectInvalid: false,
		},
		{
			name:          "lowercase plural is valid",
			category:      "reports",
			expectInvalid: false,
		},
		{
			name:          "lowercase research is valid",
			category:      "research",
			expectInvalid: false,
		},
		{
			name:          "general lowercase is valid",
			category:      "general",
			expectInvalid: false,
		},
		{
			name:          "capitalized Audits fails",
			category:      "Audits",
			expectInvalid: true,
		},
		{
			name:          "capitalized General fails",
			category:      "General",
			expectInvalid: true,
		},
		{
			name:          "capitalized Reports fails",
			category:      "Reports",
			expectInvalid: true,
		},
		{
			name:          "capitalized Research fails",
			category:      "Research",
			expectInvalid: true,
		},
		{
			name:          "unknown capitalized category fails",
			category:      "MyCategory",
			expectInvalid: true,
		},
		{
			name:          "mixed case fails",
			category:      "AuDiTs",
			expectInvalid: true,
		},
		{
			name:          "singular audit is valid but logged as warning",
			category:      "audit",
			expectInvalid: false,
			// Note: This will log a warning, but not fail
		},
		{
			name:          "singular report is valid but logged as warning",
			category:      "report",
			expectInvalid: false,
			// Note: This will log a warning, but not fail
		},
		{
			name:          "category ID is valid",
			category:      "DIC_kwDOGFsHUM4BsUn3",
			expectInvalid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := logger.New("test:discussion_validation")
			isInvalid := validateDiscussionCategory(tt.category, log, "test.md")

			if tt.expectInvalid {
				assert.True(t, isInvalid, "Expected validation to fail for category %q", tt.category)
			} else {
				assert.False(t, isInvalid, "Expected validation to pass for category %q", tt.category)
			}
		})
	}
}

func TestParseDiscussionsConfigValidation(t *testing.T) {
	tests := []struct {
		name            string
		category        string
		expectNilResult bool
	}{
		{
			name:            "valid lowercase category returns config",
			category:        "audits",
			expectNilResult: false,
		},
		{
			name:            "capitalized category returns nil",
			category:        "Audits",
			expectNilResult: true,
		},
		{
			name:            "General category returns nil",
			category:        "General",
			expectNilResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler(WithFailFast(true))
			outputMap := map[string]any{
				"create-discussion": map[string]any{
					"category": tt.category,
				},
			}

			result := compiler.parseDiscussionsConfig(outputMap)

			if tt.expectNilResult {
				assert.Nil(t, result, "Expected nil result for invalid category %q", tt.category)
			} else {
				assert.NotNil(t, result, "Expected non-nil result for valid category %q", tt.category)
			}
		})
	}
}

func TestParseDiscussionsConfigFallbackToIssue(t *testing.T) {
	tests := []struct {
		name             string
		config           map[string]any
		expectedFallback *bool
	}{
		{
			name: "default fallback-to-issue is true",
			config: map[string]any{
				"category": "general",
			},
			expectedFallback: boolPtr(true),
		},
		{
			name: "explicit fallback-to-issue true",
			config: map[string]any{
				"category":          "general",
				"fallback-to-issue": true,
			},
			expectedFallback: boolPtr(true),
		},
		{
			name: "explicit fallback-to-issue false",
			config: map[string]any{
				"category":          "general",
				"fallback-to-issue": false,
			},
			expectedFallback: boolPtr(false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler(WithFailFast(true))
			outputMap := map[string]any{
				"create-discussion": tt.config,
			}

			result := compiler.parseDiscussionsConfig(outputMap)

			assert.NotNil(t, result, "Expected non-nil result")
			if tt.expectedFallback != nil {
				assert.NotNil(t, result.FallbackToIssue, "Expected FallbackToIssue to be set")
				assert.Equal(t, *tt.expectedFallback, *result.FallbackToIssue, "FallbackToIssue value mismatch")
			}
		})
	}
}
