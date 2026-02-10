//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeDiscussionCategory(t *testing.T) {
	tests := []struct {
		name             string
		category         string
		expectedCategory string
	}{
		{
			name:             "empty category unchanged",
			category:         "",
			expectedCategory: "",
		},
		{
			name:             "lowercase category unchanged",
			category:         "audits",
			expectedCategory: "audits",
		},
		{
			name:             "lowercase plural unchanged",
			category:         "reports",
			expectedCategory: "reports",
		},
		{
			name:             "lowercase research unchanged",
			category:         "research",
			expectedCategory: "research",
		},
		{
			name:             "general lowercase unchanged",
			category:         "general",
			expectedCategory: "general",
		},
		{
			name:             "capitalized Audits normalized to lowercase",
			category:         "Audits",
			expectedCategory: "audits",
		},
		{
			name:             "capitalized General normalized to lowercase",
			category:         "General",
			expectedCategory: "general",
		},
		{
			name:             "capitalized Reports normalized to lowercase",
			category:         "Reports",
			expectedCategory: "reports",
		},
		{
			name:             "capitalized Research normalized to lowercase",
			category:         "Research",
			expectedCategory: "research",
		},
		{
			name:             "unknown capitalized category normalized",
			category:         "MyCategory",
			expectedCategory: "mycategory",
		},
		{
			name:             "mixed case normalized",
			category:         "AuDiTs",
			expectedCategory: "audits",
		},
		{
			name:             "singular audit unchanged (but warns)",
			category:         "audit",
			expectedCategory: "audit",
		},
		{
			name:             "singular report unchanged (but warns)",
			category:         "report",
			expectedCategory: "report",
		},
		{
			name:             "category ID unchanged",
			category:         "DIC_kwDOGFsHUM4BsUn3",
			expectedCategory: "DIC_kwDOGFsHUM4BsUn3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := logger.New("test:discussion_validation")
			normalized := normalizeDiscussionCategory(tt.category, log, "test.md")
			assert.Equal(t, tt.expectedCategory, normalized, "Expected category %q to be normalized to %q", tt.category, tt.expectedCategory)
		})
	}
}

func TestParseDiscussionsConfigNormalization(t *testing.T) {
	tests := []struct {
		name               string
		category           string
		expectedCategory   string
		expectNonNilResult bool
	}{
		{
			name:               "valid lowercase category returns config",
			category:           "audits",
			expectedCategory:   "audits",
			expectNonNilResult: true,
		},
		{
			name:               "capitalized category normalized and returns config",
			category:           "Audits",
			expectedCategory:   "audits",
			expectNonNilResult: true,
		},
		{
			name:               "General category normalized and returns config",
			category:           "General",
			expectedCategory:   "general",
			expectNonNilResult: true,
		},
		{
			name:               "mixed case category normalized",
			category:           "MyCategory",
			expectedCategory:   "mycategory",
			expectNonNilResult: true,
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

			if tt.expectNonNilResult {
				assert.NotNil(t, result, "Expected non-nil result for category %q", tt.category)
				assert.Equal(t, tt.expectedCategory, result.Category, "Expected category to be normalized to %q", tt.expectedCategory)
			} else {
				assert.Nil(t, result, "Expected nil result for invalid category %q", tt.category)
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
