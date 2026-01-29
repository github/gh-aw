//go:build !integration

package campaign

import (
	"testing"

	"github.com/githubnext/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectOrchestratorFeatures_ProjectIDTriggersCampaignAndDefaults(t *testing.T) {
	data := &workflow.WorkflowData{
		Name:            "Security Alert Burndown",
		WorkflowID:      "security-alert-burndown",
		MarkdownContent: "# Test",
		ParsedFrontmatter: &workflow.FrontmatterConfig{
			Project: &workflow.ProjectConfig{
				URL: "https://github.com/orgs/githubnext/projects/144",
			},
		},
	}

	err := InjectOrchestratorFeatures(data)
	require.NoError(t, err, "campaign injection should succeed")
	require.NotNil(t, data.ParsedFrontmatter, "ParsedFrontmatter should remain")
	require.NotNil(t, data.ParsedFrontmatter.Project, "Project should remain")

	project := data.ParsedFrontmatter.Project
	assert.Equal(t, "security-alert-burndown", project.ID, "Campaign ID should be inferred and normalized")
	assert.Equal(t, "z_campaign_security-alert-burndown", project.TrackerLabel, "TrackerLabel should be defaulted")
	assert.Equal(t, []string{"memory/campaigns/security-alert-burndown/**"}, project.MemoryPaths, "MemoryPaths should be defaulted")
	assert.Equal(t, "memory/campaigns/security-alert-burndown/metrics/*.json", project.MetricsGlob, "MetricsGlob should be defaulted")
	assert.Equal(t, "memory/campaigns/security-alert-burndown/cursor.json", project.CursorGlob, "CursorGlob should be defaulted")

	assert.Contains(t, data.MarkdownContent, "ORCHESTRATOR INSTRUCTIONS", "Markdown should have orchestrator instructions injected")
}

func TestInjectOrchestratorFeatures_ProjectTrackingOnly_DoesNotInject(t *testing.T) {
	data := &workflow.WorkflowData{
		Name:            "Project Tracking Only",
		FrontmatterName: "project-tracking-only",
		MarkdownContent: "# Test",
		ParsedFrontmatter: &workflow.FrontmatterConfig{
			Project: &workflow.ProjectConfig{
				URL:   "https://github.com/orgs/githubnext/projects/144",
				Scope: []string{"githubnext/gh-aw"},
			},
		},
	}

	err := InjectOrchestratorFeatures(data)
	require.NoError(t, err, "non-campaign project tracking should be a no-op")
	assert.NotContains(t, data.MarkdownContent, "ORCHESTRATOR INSTRUCTIONS", "Should not inject campaign sections")
}

func TestInjectOrchestratorFeatures_EdgeCases(t *testing.T) {
	tests := []struct {
		name               string
		workflowData       *workflow.WorkflowData
		shouldErr          bool
		expectInjection    bool
		expectedCampaignID string
	}{
		{
			name: "nil frontmatter - should skip",
			workflowData: &workflow.WorkflowData{
				Name:              "Test",
				WorkflowID:        "test",
				MarkdownContent:   "# Test",
				ParsedFrontmatter: nil,
			},
			shouldErr:       false,
			expectInjection: false,
		},
		{
			name: "nil project - should skip",
			workflowData: &workflow.WorkflowData{
				Name:            "Test",
				WorkflowID:      "test",
				MarkdownContent: "# Test",
				ParsedFrontmatter: &workflow.FrontmatterConfig{
					Project: nil,
				},
			},
			shouldErr:       false,
			expectInjection: false,
		},
		{
			name: "infer campaign ID from WorkflowID",
			workflowData: &workflow.WorkflowData{
				Name:            "Security Alert Burndown",
				WorkflowID:      "security-alert-burndown",
				MarkdownContent: "# Test",
				ParsedFrontmatter: &workflow.FrontmatterConfig{
					Project: &workflow.ProjectConfig{
						URL: "https://github.com/orgs/githubnext/projects/144",
					},
				},
			},
			shouldErr:          false,
			expectInjection:    true,
			expectedCampaignID: "security-alert-burndown",
		},
		{
			name: "explicit campaign ID with WorkflowID",
			workflowData: &workflow.WorkflowData{
				Name:            "Security Alert Burndown",
				WorkflowID:      "security-alert-burndown",
				MarkdownContent: "# Test",
				ParsedFrontmatter: &workflow.FrontmatterConfig{
					Project: &workflow.ProjectConfig{
						URL: "https://github.com/orgs/githubnext/projects/144",
						ID:  "custom-campaign-id",
					},
				},
			},
			shouldErr:          false,
			expectInjection:    true,
			expectedCampaignID: "custom-campaign-id",
		},
		{
			name: "campaign ID inferred from Name when WorkflowID empty but campaign indicator present",
			workflowData: &workflow.WorkflowData{
				Name:            "Security Alert Burndown",
				WorkflowID:      "",
				MarkdownContent: "# Test",
				ParsedFrontmatter: &workflow.FrontmatterConfig{
					Project: &workflow.ProjectConfig{
						URL:       "https://github.com/orgs/githubnext/projects/144",
						Workflows: []string{"worker.md"}, // Campaign indicator
					},
				},
			},
			shouldErr:          false,
			expectInjection:    true,
			expectedCampaignID: "security-alert-burndown",
		},
		{
			name: "fallback to default campaign ID when all identifiers empty",
			workflowData: &workflow.WorkflowData{
				Name:            "",
				WorkflowID:      "",
				MarkdownContent: "# Test",
				ParsedFrontmatter: &workflow.FrontmatterConfig{
					Project: &workflow.ProjectConfig{
						URL:       "https://github.com/orgs/githubnext/projects/144",
						Workflows: []string{"worker.md"}, // Campaign indicator
					},
				},
			},
			shouldErr:          false,
			expectInjection:    true,
			expectedCampaignID: "campaign",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := InjectOrchestratorFeatures(tt.workflowData)

			if tt.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				if tt.expectInjection {
					assert.Contains(t, tt.workflowData.MarkdownContent, "ORCHESTRATOR INSTRUCTIONS", "should inject campaign sections")
					if tt.expectedCampaignID != "" {
						assert.Equal(t, tt.expectedCampaignID, tt.workflowData.ParsedFrontmatter.Project.ID, "campaign ID should match expected")
					}
				} else {
					assert.NotContains(t, tt.workflowData.MarkdownContent, "ORCHESTRATOR INSTRUCTIONS", "should not inject campaign sections")
				}
			}
		})
	}
}

func TestInjectOrchestratorFeatures_GovernanceConfiguration(t *testing.T) {
	data := &workflow.WorkflowData{
		Name:            "Campaign with Governance",
		WorkflowID:      "test-campaign",
		MarkdownContent: "# Test",
		ParsedFrontmatter: &workflow.FrontmatterConfig{
			Project: &workflow.ProjectConfig{
				URL: "https://github.com/orgs/githubnext/projects/144",
				Governance: &workflow.CampaignGovernanceConfig{
					MaxDiscoveryItemsPerRun: 10,
					MaxDiscoveryPagesPerRun: 5,
					MaxProjectUpdatesPerRun: 20,
					MaxCommentsPerRun:       15,
				},
			},
		},
		SafeOutputs: &workflow.SafeOutputsConfig{
			UpdateProjects: &workflow.UpdateProjectConfig{
				BaseSafeOutputConfig: workflow.BaseSafeOutputConfig{Max: 5},
			},
		},
	}

	err := InjectOrchestratorFeatures(data)
	require.NoError(t, err, "should handle governance configuration")

	// Verify governance max is applied to safe-outputs
	assert.Equal(t, 20, data.SafeOutputs.UpdateProjects.Max, "governance max should override safe-outputs max")
}

func TestInjectOrchestratorFeatures_BootstrapConfiguration(t *testing.T) {
	data := &workflow.WorkflowData{
		Name:            "Campaign with Bootstrap",
		WorkflowID:      "test-campaign",
		MarkdownContent: "# Test",
		ParsedFrontmatter: &workflow.FrontmatterConfig{
			Project: &workflow.ProjectConfig{
				URL: "https://github.com/orgs/githubnext/projects/144",
				Bootstrap: &workflow.CampaignBootstrapConfig{
					Mode: "seeder",
					SeederWorker: &workflow.SeederWorkerConfig{
						WorkflowID: "seeder-workflow",
						MaxItems:   50,
					},
				},
			},
		},
	}

	err := InjectOrchestratorFeatures(data)
	require.NoError(t, err, "should handle bootstrap configuration")
	assert.Contains(t, data.MarkdownContent, "BOOTSTRAP INSTRUCTIONS", "should include bootstrap instructions")
}

func TestInjectOrchestratorFeatures_WorkersConfiguration(t *testing.T) {
	data := &workflow.WorkflowData{
		Name:            "Campaign with Workers",
		WorkflowID:      "test-campaign",
		MarkdownContent: "# Test",
		ParsedFrontmatter: &workflow.FrontmatterConfig{
			Project: &workflow.ProjectConfig{
				URL: "https://github.com/orgs/githubnext/projects/144",
				Workers: []workflow.WorkerMetadata{
					{
						ID:          "worker-1",
						Name:        "Test Worker",
						Description: "A test worker",
						PayloadSchema: map[string]workflow.WorkerPayloadField{
							"field1": {
								Type:        "string",
								Description: "Test field",
								Required:    true,
								Example:     "example",
							},
						},
						OutputLabeling: workflow.WorkerOutputLabeling{
							Labels:     []string{"test-label"},
							KeyInTitle: true,
						},
					},
				},
			},
		},
	}

	err := InjectOrchestratorFeatures(data)
	require.NoError(t, err, "should handle workers configuration")
	assert.Contains(t, data.MarkdownContent, "ORCHESTRATOR INSTRUCTIONS", "should inject orchestrator instructions")
}

func TestInjectOrchestratorFeatures_SafeOutputsDispatchWorkflow(t *testing.T) {
	data := &workflow.WorkflowData{
		Name:            "Campaign with Workflows",
		WorkflowID:      "test-campaign",
		MarkdownContent: "# Test",
		ParsedFrontmatter: &workflow.FrontmatterConfig{
			Project: &workflow.ProjectConfig{
				URL:       "https://github.com/orgs/githubnext/projects/144",
				Workflows: []string{"worker-1.md", "worker-2.md"},
			},
		},
	}

	err := InjectOrchestratorFeatures(data)
	require.NoError(t, err, "should configure dispatch-workflow")
	require.NotNil(t, data.SafeOutputs, "SafeOutputs should be initialized")
	require.NotNil(t, data.SafeOutputs.DispatchWorkflow, "DispatchWorkflow should be configured")
	assert.Equal(t, 3, data.SafeOutputs.DispatchWorkflow.Max, "should have max of 3")
	assert.Equal(t, []string{"worker-1.md", "worker-2.md"}, data.SafeOutputs.DispatchWorkflow.Workflows, "should have configured workflows")
}

func TestInjectOrchestratorFeatures_ConcurrencyControl(t *testing.T) {
	data := &workflow.WorkflowData{
		Name:            "Campaign",
		WorkflowID:      "test-campaign",
		MarkdownContent: "# Test",
		Concurrency:     "", // Empty - should be injected
		ParsedFrontmatter: &workflow.FrontmatterConfig{
			Project: &workflow.ProjectConfig{
				URL: "https://github.com/orgs/githubnext/projects/144",
			},
		},
	}

	err := InjectOrchestratorFeatures(data)
	require.NoError(t, err, "should add concurrency control")
	assert.Contains(t, data.Concurrency, "campaign-test-campaign-orchestrator", "should include campaign ID in concurrency group")
	assert.Contains(t, data.Concurrency, "cancel-in-progress: false", "should not cancel in progress")
}

func TestInjectOrchestratorFeatures_ConcurrencyAlreadySet(t *testing.T) {
	existingConcurrency := "concurrency:\n  group: custom-group\n  cancel-in-progress: true"
	data := &workflow.WorkflowData{
		Name:            "Campaign",
		WorkflowID:      "test-campaign",
		MarkdownContent: "# Test",
		Concurrency:     existingConcurrency,
		ParsedFrontmatter: &workflow.FrontmatterConfig{
			Project: &workflow.ProjectConfig{
				URL: "https://github.com/orgs/githubnext/projects/144",
			},
		},
	}

	err := InjectOrchestratorFeatures(data)
	require.NoError(t, err, "should not override existing concurrency")
	assert.Equal(t, existingConcurrency, data.Concurrency, "should preserve existing concurrency")
}

func TestInjectOrchestratorFeatures_DefaultsApplication(t *testing.T) {
	tests := []struct {
		name                 string
		campaignID           string
		initialTrackerLabel  string
		initialMemoryPaths   []string
		initialMetricsGlob   string
		initialCursorGlob    string
		expectedTrackerLabel string
		expectedMemoryPaths  []string
		expectedMetricsGlob  string
		expectedCursorGlob   string
	}{
		{
			name:                 "all defaults applied",
			campaignID:           "test-campaign",
			initialTrackerLabel:  "",
			initialMemoryPaths:   nil,
			initialMetricsGlob:   "",
			initialCursorGlob:    "",
			expectedTrackerLabel: "z_campaign_test-campaign",
			expectedMemoryPaths:  []string{"memory/campaigns/test-campaign/**"},
			expectedMetricsGlob:  "memory/campaigns/test-campaign/metrics/*.json",
			expectedCursorGlob:   "memory/campaigns/test-campaign/cursor.json",
		},
		{
			name:                 "explicit values preserved",
			campaignID:           "test-campaign",
			initialTrackerLabel:  "custom-label",
			initialMemoryPaths:   []string{"custom/path/**"},
			initialMetricsGlob:   "custom/metrics/*.json",
			initialCursorGlob:    "custom/cursor.json",
			expectedTrackerLabel: "custom-label",
			expectedMemoryPaths:  []string{"custom/path/**"},
			expectedMetricsGlob:  "custom/metrics/*.json",
			expectedCursorGlob:   "custom/cursor.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &workflow.WorkflowData{
				Name:            "Test Campaign",
				WorkflowID:      tt.campaignID,
				MarkdownContent: "# Test",
				ParsedFrontmatter: &workflow.FrontmatterConfig{
					Project: &workflow.ProjectConfig{
						URL:          "https://github.com/orgs/githubnext/projects/144",
						TrackerLabel: tt.initialTrackerLabel,
						MemoryPaths:  tt.initialMemoryPaths,
						MetricsGlob:  tt.initialMetricsGlob,
						CursorGlob:   tt.initialCursorGlob,
					},
				},
			}

			err := InjectOrchestratorFeatures(data)
			require.NoError(t, err, "should apply defaults")

			project := data.ParsedFrontmatter.Project
			assert.Equal(t, tt.expectedTrackerLabel, project.TrackerLabel, "TrackerLabel should match")
			assert.Equal(t, tt.expectedMemoryPaths, project.MemoryPaths, "MemoryPaths should match")
			assert.Equal(t, tt.expectedMetricsGlob, project.MetricsGlob, "MetricsGlob should match")
			assert.Equal(t, tt.expectedCursorGlob, project.CursorGlob, "CursorGlob should match")
		})
	}
}

func TestNormalizeCampaignID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "mixed case with spaces",
			input:    "Security Alert Burndown",
			expected: "security-alert-burndown",
		},
		{
			name:     "underscores to hyphens",
			input:    "security_alert_burndown",
			expected: "security-alert-burndown",
		},
		{
			name:     "special characters and extra hyphens",
			input:    " security---alert@@burndown ",
			expected: "security-alert-burndown",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: "",
		},
		{
			name:     "only special characters",
			input:    "@@##$$",
			expected: "",
		},
		{
			name:     "already normalized",
			input:    "campaign-name",
			expected: "campaign-name",
		},
		{
			name:     "numbers preserved",
			input:    "Campaign 123 Test",
			expected: "campaign-123-test",
		},
		{
			name:     "leading and trailing hyphens",
			input:    "---campaign---",
			expected: "campaign",
		},
		{
			name:     "unicode characters",
			input:    "Campaña Tëst",
			expected: "campa-a-t-st",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeCampaignID(tt.input)
			assert.Equal(t, tt.expected, result, "normalization should match expected output")
		})
	}
}
