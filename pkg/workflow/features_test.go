//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestIsFeatureEnabled(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		flag     constants.FeatureFlag
		expected bool
	}{
		{
			name:     "feature enabled - single flag",
			envValue: "firewall",
			flag:     "firewall",
			expected: true,
		},
		{
			name:     "feature enabled - case insensitive",
			envValue: "FIREWALL",
			flag:     "firewall",
			expected: true,
		},
		{
			name:     "feature enabled - mixed case",
			envValue: "Firewall",
			flag:     "FIREWALL",
			expected: true,
		},
		{
			name:     "feature enabled - multiple flags",
			envValue: "feature1,firewall,feature2",
			flag:     "firewall",
			expected: true,
		},
		{
			name:     "feature enabled - with spaces",
			envValue: "feature1, firewall , feature2",
			flag:     "firewall",
			expected: true,
		},
		{
			name:     "feature disabled - empty env",
			envValue: "",
			flag:     "firewall",
			expected: false,
		},
		{
			name:     "feature disabled - not in list",
			envValue: "feature1,feature2",
			flag:     "firewall",
			expected: false,
		},
		{
			name:     "feature disabled - partial match",
			envValue: "firewall-extra",
			flag:     "firewall",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			t.Setenv("GH_AW_FEATURES", tt.envValue)

			result := isFeatureEnabled(tt.flag, nil)
			if result != tt.expected {
				t.Errorf("isFeatureEnabled(%q, nil) with env=%q = %v, want %v",
					tt.flag, tt.envValue, result, tt.expected)
			}
		})
	}
}

func TestIsFeatureEnabledNoEnv(t *testing.T) {
	result := isFeatureEnabled(constants.FeatureFlag("firewall"), nil)
	if result != false {
		t.Errorf("isFeatureEnabled(\"firewall\", nil) with no env = %v, want false", result)
	}
}

func TestGHAWDetectionFeatureDefaultsToEnabled(t *testing.T) {
	t.Setenv("GH_AW_FEATURES", "")

	if !isFeatureEnabled(constants.GHAWDetectionFeatureFlag, nil) {
		t.Fatal("gh-aw-detection should be enabled by default")
	}

	if isFeatureEnabled(constants.GHAWDetectionFeatureFlag, &WorkflowData{
		Features: map[string]any{
			string(constants.GHAWDetectionFeatureFlag): false,
		},
	}) {
		t.Fatal("explicit gh-aw-detection: false should disable the external detector")
	}
}

func TestIsFeatureEnabledWithData(t *testing.T) {
	tests := []struct {
		name        string
		envValue    string
		frontmatter map[string]any
		engineID    string
		workflowAI  string
		flag        constants.FeatureFlag
		expected    bool
		description string
	}{
		{
			name:        "frontmatter takes precedence - enabled in frontmatter, disabled in env",
			envValue:    "",
			frontmatter: map[string]any{"firewall": true},
			engineID:    string(constants.CopilotEngine),
			flag:        "firewall",
			expected:    true,
			description: "When feature is in frontmatter, it should be enabled regardless of env",
		},
		{
			name:        "frontmatter takes precedence - disabled in frontmatter, enabled in env",
			envValue:    "firewall",
			frontmatter: map[string]any{"firewall": false},
			engineID:    string(constants.CopilotEngine),
			flag:        "firewall",
			expected:    false,
			description: "When feature is explicitly disabled in frontmatter, env should be ignored",
		},
		{
			name:        "fallback to env when not in frontmatter",
			envValue:    "firewall",
			frontmatter: map[string]any{"other-feature": true},
			engineID:    string(constants.CopilotEngine),
			flag:        "firewall",
			expected:    true,
			description: "When feature is not in frontmatter, should check env",
		},
		{
			name:        "disabled when not in frontmatter or env",
			envValue:    "",
			frontmatter: map[string]any{"other-feature": true},
			engineID:    string(constants.CopilotEngine),
			flag:        "firewall",
			expected:    false,
			description: "When feature is in neither frontmatter nor env, should be disabled",
		},
		{
			name:        "case insensitive frontmatter check",
			envValue:    "",
			frontmatter: map[string]any{"FIREWALL": true},
			engineID:    string(constants.CopilotEngine),
			flag:        "firewall",
			expected:    true,
			description: "Frontmatter feature check should be case insensitive",
		},
		{
			name:        "nil frontmatter falls back to env",
			envValue:    "firewall",
			frontmatter: nil,
			engineID:    string(constants.CopilotEngine),
			flag:        "firewall",
			expected:    true,
			description: "When frontmatter is nil, should check env",
		},
		{
			name:        "empty frontmatter falls back to env",
			envValue:    "firewall",
			frontmatter: map[string]any{},
			engineID:    string(constants.CopilotEngine),
			flag:        "firewall",
			expected:    true,
			description: "When frontmatter is empty, should check env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Always set environment variable (including empty string) to prevent
			// flakiness from inherited outer process environment.
			t.Setenv("GH_AW_FEATURES", tt.envValue)

			// Create WorkflowData with features
			var workflowData *WorkflowData
			if tt.frontmatter != nil {
				workflowData = &WorkflowData{
					Features: tt.frontmatter,
					AI:       tt.workflowAI,
				}
				if tt.engineID != "" {
					workflowData.EngineConfig = &EngineConfig{
						ID: tt.engineID,
					}
				}
			}

			result := isFeatureEnabled(tt.flag, workflowData)
			if result != tt.expected {
				t.Errorf("%s: isFeatureEnabled(%q, %+v) with env=%q = %v, want %v",
					tt.description, tt.flag, tt.frontmatter, tt.envValue, result, tt.expected)
			}
		})
	}
}

func TestIsFeatureEnabledWithDataNilWorkflow(t *testing.T) {
	// Set environment variable
	t.Setenv("GH_AW_FEATURES", "firewall")

	// When workflowData is nil, should fall back to env
	result := isFeatureEnabled(constants.FeatureFlag("firewall"), nil)
	if result != true {
		t.Errorf("isFeatureEnabled(\"firewall\", nil) with env=firewall = %v, want true", result)
	}
}

// TestMergedFeaturesAreUsedByIsFeatureEnabled verifies that features merged from imports
// are accessible via isFeatureEnabled function
func TestMergedFeaturesAreUsedByIsFeatureEnabled(t *testing.T) {
	// Create workflow data with merged features (simulating the result of merging imports)
	workflowData := &WorkflowData{
		Features: map[string]any{
			"imported-feature":  true,
			"another-feature":   false,
			"string-feature":    "enabled",
			"top-level-feature": true,
		},
	}

	// Test that imported features are accessible via isFeatureEnabled
	tests := []struct {
		name     string
		flag     constants.FeatureFlag
		expected bool
	}{
		{
			name:     "imported feature enabled",
			flag:     "imported-feature",
			expected: true,
		},
		{
			name:     "imported feature disabled",
			flag:     "another-feature",
			expected: false,
		},
		{
			name:     "string feature treated as enabled",
			flag:     "string-feature",
			expected: true,
		},
		{
			name:     "top-level feature enabled",
			flag:     "top-level-feature",
			expected: true,
		},
		{
			name:     "non-existent feature",
			flag:     "non-existent",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFeatureEnabled(tt.flag, workflowData)
			if result != tt.expected {
				t.Errorf("isFeatureEnabled(%q) = %v, want %v", tt.flag, result, tt.expected)
			}
		})
	}
}

// TestMergedFeaturesTopLevelPrecedence verifies that top-level features take precedence
// over imported features in the merged features map
func TestMergedFeaturesTopLevelPrecedence(t *testing.T) {
	// This test verifies that when features are merged, top-level features override imports
	// The actual merging happens in MergeFeatures function, but we test the end result here

	// Simulate a workflow where top-level feature overrides an imported one
	workflowData := &WorkflowData{
		Features: map[string]any{
			"override-feature": false, // Top-level value (overriding import that had true)
			"import-only":      true,  // Only from import
		},
	}

	// Verify that the overridden value is what isFeatureEnabled sees
	overrideResult := isFeatureEnabled(constants.FeatureFlag("override-feature"), workflowData)
	if overrideResult != false {
		t.Errorf("isFeatureEnabled(\"override-feature\") = %v, want false (top-level override)", overrideResult)
	}

	// Verify that import-only feature is still accessible
	importOnlyResult := isFeatureEnabled(constants.FeatureFlag("import-only"), workflowData)
	if importOnlyResult != true {
		t.Errorf("isFeatureEnabled(\"import-only\") = %v, want true (from import)", importOnlyResult)
	}
}

func TestInlineAgentsFeatureAlwaysEnabled(t *testing.T) {
	t.Setenv("GH_AW_FEATURES", "")

	tests := []struct {
		name     string
		features map[string]any
	}{
		{
			name:     "enabled when feature absent",
			features: map[string]any{},
		},
		{
			name: "enabled when explicitly true",
			features: map[string]any{
				"inline-agents": true,
			},
		},
		{
			name: "enabled when explicitly false",
			features: map[string]any{
				"inline-agents": false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{Features: tt.features}
			result := isFeatureEnabled("inline-agents", workflowData)
			if !result {
				t.Errorf("isFeatureEnabled(%q, %+v) = %v, want true", "inline-agents", tt.features, result)
			}
		})
	}
}
