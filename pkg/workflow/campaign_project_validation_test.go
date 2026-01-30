//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateCampaignProject(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		expectError bool
		errorMsg    string
	}{
		{
			name: "campaign with agentic-campaign label and project URL - valid",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"add-labels": map[string]any{
						"allowed": []any{"agentic-campaign", "z_campaign_test"},
					},
				},
				"project": "https://github.com/orgs/test/projects/123",
			},
			expectError: false,
		},
		{
			name: "campaign with z_campaign_ label and project URL - valid",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-issue": map[string]any{
						"labels": []any{"z_campaign_security"},
					},
				},
				"project": "https://github.com/orgs/test/projects/456",
			},
			expectError: false,
		},
		{
			name: "campaign with campaign-id in repo-memory and project URL - valid",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"repo-memory": map[string]any{
						"campaign-id": "security-alert-burndown",
					},
				},
				"project": "https://github.com/orgs/test/projects/789",
			},
			expectError: false,
		},
		{
			name: "campaign with agentic-campaign label but no project - error",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"add-labels": map[string]any{
						"allowed": []any{"agentic-campaign"},
					},
				},
			},
			expectError: true,
			errorMsg:    "campaign orchestrator requires a GitHub Project URL",
		},
		{
			name: "campaign with z_campaign_ label but empty project URL - error",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-pull-request": map[string]any{
						"labels": []any{"z_campaign_test"},
					},
				},
				"project": "",
			},
			expectError: true,
			errorMsg:    "requires a non-empty GitHub Project URL",
		},
		{
			name: "campaign with campaign-id but nil project - error",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"repo-memory": map[string]any{
						"campaign-id": "test-campaign",
					},
				},
				"project": nil,
			},
			expectError: true,
			errorMsg:    "campaign orchestrator requires a GitHub Project URL",
		},
		{
			name: "campaign with project config object - valid",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"add-labels": map[string]any{
						"allowed": []any{"agentic-campaign"},
					},
				},
				"project": map[string]any{
					"url":         "https://github.com/orgs/test/projects/123",
					"max-updates": 100,
				},
			},
			expectError: false,
		},
		{
			name: "campaign with project config but missing URL - error",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"repo-memory": []any{
						map[string]any{
							"campaign-id": "test",
						},
					},
				},
				"project": map[string]any{
					"max-updates": 100,
				},
			},
			expectError: true,
			errorMsg:    "project configuration must include a 'url' field",
		},
		{
			name: "non-campaign workflow without project - valid",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-issue": map[string]any{
						"labels": []any{"bug", "enhancement"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "workflow with regular labels - valid",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"add-labels": map[string]any{
						"allowed": []any{"bug", "feature", "documentation"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "campaign with multiple repo-memory entries - valid",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"repo-memory": []any{
						map[string]any{
							"id": "state",
						},
						map[string]any{
							"campaign-id": "test-campaign",
						},
					},
				},
				"project": "https://github.com/orgs/test/projects/999",
			},
			expectError: false,
		},
		{
			name: "campaign via create-discussion labels - valid",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-discussion": map[string]any{
						"labels": []any{"agentic-campaign"},
					},
				},
				"project": "https://github.com/orgs/test/projects/111",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			err := compiler.validateCampaignProject(tt.frontmatter)

			if tt.expectError {
				require.Error(t, err, "Expected error but got none")
				assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
			} else {
				assert.NoError(t, err, "Expected no error but got: %v", err)
			}
		})
	}
}

func TestDetectCampaignWorkflow(t *testing.T) {
	tests := []struct {
		name             string
		frontmatter      map[string]any
		expectedCampaign bool
		expectedSource   string
	}{
		{
			name: "detect via agentic-campaign label",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"add-labels": map[string]any{
						"allowed": []any{"agentic-campaign"},
					},
				},
			},
			expectedCampaign: true,
			expectedSource:   "campaign labels",
		},
		{
			name: "detect via z_campaign_ prefix",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-issue": map[string]any{
						"labels": []any{"z_campaign_test"},
					},
				},
			},
			expectedCampaign: true,
			expectedSource:   "campaign labels",
		},
		{
			name: "detect via campaign-id in single repo-memory",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"repo-memory": map[string]any{
						"campaign-id": "test",
					},
				},
			},
			expectedCampaign: true,
			expectedSource:   "campaign-id",
		},
		{
			name: "detect via campaign-id in array repo-memory",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"repo-memory": []any{
						map[string]any{
							"campaign-id": "test",
						},
					},
				},
			},
			expectedCampaign: true,
			expectedSource:   "campaign-id",
		},
		{
			name: "no campaign characteristics",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-issue": map[string]any{
						"labels": []any{"bug"},
					},
				},
			},
			expectedCampaign: false,
			expectedSource:   "",
		},
		{
			name: "empty repo-memory campaign-id",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"repo-memory": map[string]any{
						"campaign-id": "",
					},
				},
			},
			expectedCampaign: false,
			expectedSource:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isCampaign, source := detectCampaignWorkflow(tt.frontmatter)
			assert.Equal(t, tt.expectedCampaign, isCampaign, "Campaign detection mismatch")
			if tt.expectedCampaign {
				assert.Contains(t, source, tt.expectedSource, "Source should contain expected text")
			}
		})
	}
}

func TestIsCampaignLabel(t *testing.T) {
	tests := []struct {
		name     string
		label    string
		expected bool
	}{
		{"agentic-campaign exact match", "agentic-campaign", true},
		{"z_campaign_ prefix", "z_campaign_security", true},
		{"z_campaign_ prefix with dashes", "z_campaign_go-size-reduction", true},
		{"regular label", "bug", false},
		{"feature label", "feature", false},
		{"partial match", "my-agentic-campaign", false},
		{"empty string", "", false},
		{"z_ without campaign", "z_test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCampaignLabel(tt.label)
			assert.Equal(t, tt.expected, result, "Label %q should %v be a campaign label", tt.label, map[bool]string{true: "", false: "not"}[tt.expected])
		})
	}
}

func TestHasCampaignID(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		expected    bool
	}{
		{
			name: "single repo-memory with campaign-id",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"repo-memory": map[string]any{
						"campaign-id": "test",
					},
				},
			},
			expected: true,
		},
		{
			name: "array repo-memory with campaign-id",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"repo-memory": []any{
						map[string]any{
							"campaign-id": "test",
						},
					},
				},
			},
			expected: true,
		},
		{
			name:        "no tools",
			frontmatter: map[string]any{},
			expected:    false,
		},
		{
			name: "no repo-memory",
			frontmatter: map[string]any{
				"tools": map[string]any{},
			},
			expected: false,
		},
		{
			name: "empty campaign-id",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"repo-memory": map[string]any{
						"campaign-id": "",
					},
				},
			},
			expected: false,
		},
		{
			name: "nil campaign-id",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"repo-memory": map[string]any{
						"campaign-id": nil,
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasCampaignID(tt.frontmatter)
			assert.Equal(t, tt.expected, result, "Campaign ID detection mismatch")
		})
	}
}

func TestHasCampaignLabels(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		expected    bool
	}{
		{
			name: "add-labels with agentic-campaign",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"add-labels": map[string]any{
						"allowed": []any{"agentic-campaign"},
					},
				},
			},
			expected: true,
		},
		{
			name: "create-issue with z_campaign_ label",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-issue": map[string]any{
						"labels": []any{"z_campaign_test"},
					},
				},
			},
			expected: true,
		},
		{
			name: "create-pull-request with campaign label",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-pull-request": map[string]any{
						"labels": []any{"dependency", "agentic-campaign"},
					},
				},
			},
			expected: true,
		},
		{
			name: "create-discussion with campaign label",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-discussion": map[string]any{
						"labels": []any{"agentic-campaign"},
					},
				},
			},
			expected: true,
		},
		{
			name:        "no safe-outputs",
			frontmatter: map[string]any{},
			expected:    false,
		},
		{
			name: "regular labels only",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"add-labels": map[string]any{
						"allowed": []any{"bug", "feature"},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasCampaignLabels(tt.frontmatter)
			assert.Equal(t, tt.expected, result, "Campaign labels detection mismatch")
		})
	}
}

func TestCampaignValidationIntegration(t *testing.T) {
	// Test with actual dependabot-bundler.md style frontmatter (missing project)
	frontmatter := map[string]any{
		"name": "Test Campaign",
		"on":   "workflow_dispatch",
		"safe-outputs": map[string]any{
			"add-labels": map[string]any{
				"allowed": []any{
					"agentic-campaign",
					"z_campaign_security-alert-burndown",
				},
			},
			"create-pull-request": map[string]any{
				"labels": []any{"security", "dependencies", "agentic-campaign"},
			},
		},
		"tools": map[string]any{
			"repo-memory": []any{
				map[string]any{
					"id":          "campaigns",
					"branch-name": "memory/campaigns",
					"campaign-id": "security-alert-burndown",
				},
			},
		},
	}

	compiler := NewCompiler()
	err := compiler.validateCampaignProject(frontmatter)
	require.Error(t, err, "Should fail validation without project URL")
	assert.Contains(t, err.Error(), "campaign orchestrator requires a GitHub Project URL")

	// Add project URL and verify it passes
	frontmatter["project"] = "https://github.com/orgs/test/projects/144"
	err = compiler.validateCampaignProject(frontmatter)
	assert.NoError(t, err, "Should pass validation with project URL")
}

func TestCampaignValidationErrorMessages(t *testing.T) {
	tests := []struct {
		name            string
		frontmatter     map[string]any
		expectedInError []string
	}{
		{
			name: "error message explains campaign source - labels",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"add-labels": map[string]any{
						"allowed": []any{"agentic-campaign"},
					},
				},
			},
			expectedInError: []string{
				"campaign orchestrator",
				"GitHub Project URL",
				"campaign labels",
			},
		},
		{
			name: "error message explains campaign source - campaign-id",
			frontmatter: map[string]any{
				"tools": map[string]any{
					"repo-memory": map[string]any{
						"campaign-id": "test",
					},
				},
			},
			expectedInError: []string{
				"campaign orchestrator",
				"GitHub Project URL",
				"campaign-id",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			err := compiler.validateCampaignProject(tt.frontmatter)
			require.Error(t, err, "Should fail validation")

			errMsg := err.Error()
			for _, expected := range tt.expectedInError {
				assert.Contains(t, strings.ToLower(errMsg), strings.ToLower(expected),
					"Error message should contain %q", expected)
			}
		})
	}
}
