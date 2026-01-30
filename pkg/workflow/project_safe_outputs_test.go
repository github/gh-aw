//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyProjectSafeOutputs(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name                string
		frontmatter         map[string]any
		existingSafeOutputs *SafeOutputsConfig
		expectUpdateProject bool
		expectStatusUpdate  bool
		expectedMaxUpdates  int
		expectedMaxStatus   int
	}{
		{
			name: "project with URL string - creates safe-outputs",
			frontmatter: map[string]any{
				"project": "https://github.com/orgs/<ORG>/projects/<NUMBER>",
			},
			existingSafeOutputs: nil,
			expectUpdateProject: true,
			expectStatusUpdate:  true,
			expectedMaxUpdates:  100,
			expectedMaxStatus:   1,
		},
		{
			name: "project with existing safe-outputs preserves existing",
			frontmatter: map[string]any{
				"project": "https://github.com/orgs/<ORG>/projects/<NUMBER>",
			},
			existingSafeOutputs: &SafeOutputsConfig{
				UpdateProjects: &UpdateProjectConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 25},
				},
				CreateProjectStatusUpdates: &CreateProjectStatusUpdateConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 3},
				},
			},
			expectUpdateProject: true,
			expectStatusUpdate:  true,
			expectedMaxUpdates:  25,
			expectedMaxStatus:   3,
		},
		{
			name: "no project field - returns existing",
			frontmatter: map[string]any{
				"name": "test-workflow",
			},
			existingSafeOutputs: nil,
			expectUpdateProject: false,
			expectStatusUpdate:  false,
		},
		{
			name: "project with blank URL string is ignored",
			frontmatter: map[string]any{
				"project": "   ",
			},
			existingSafeOutputs: nil,
			expectUpdateProject: false,
			expectStatusUpdate:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.applyProjectSafeOutputs(tt.frontmatter, tt.existingSafeOutputs)

			if tt.expectUpdateProject {
				require.NotNil(t, result, "Safe outputs should be created")
				require.NotNil(t, result.UpdateProjects, "UpdateProjects should be configured")
				assert.Equal(t, tt.expectedMaxUpdates, result.UpdateProjects.Max, "UpdateProjects max should match expected")
			} else if result != nil && result.UpdateProjects != nil {
				// Only check if update-project wasn't expected but was present in existing config
				if tt.existingSafeOutputs != nil && tt.existingSafeOutputs.UpdateProjects != nil {
					assert.NotNil(t, result.UpdateProjects, "Existing UpdateProjects should be preserved")
				}
			}

			if tt.expectStatusUpdate {
				require.NotNil(t, result, "Safe outputs should be created")
				require.NotNil(t, result.CreateProjectStatusUpdates, "CreateProjectStatusUpdates should be configured")
				assert.Equal(t, tt.expectedMaxStatus, result.CreateProjectStatusUpdates.Max, "CreateProjectStatusUpdates max should match expected")
			} else if result != nil && result.CreateProjectStatusUpdates != nil {
				// Only check if status-update wasn't expected but was present in existing config
				if tt.existingSafeOutputs != nil && tt.existingSafeOutputs.CreateProjectStatusUpdates != nil {
					assert.NotNil(t, result.CreateProjectStatusUpdates, "Existing CreateProjectStatusUpdates should be preserved")
				}
			}
		})
	}
}

func TestProjectConfigIntegration(t *testing.T) {
	compiler := NewCompiler()

	// Test integration: project string -> safe-outputs defaults
	frontmatter := map[string]any{
		"project": "https://github.com/orgs/<ORG>/projects/<NUMBER>",
	}

	result := compiler.applyProjectSafeOutputs(frontmatter, nil)

	require.NotNil(t, result, "Safe outputs should be created")
	require.NotNil(t, result.UpdateProjects, "UpdateProjects should be configured")
	require.NotNil(t, result.CreateProjectStatusUpdates, "CreateProjectStatusUpdates should be configured")

	// Check update-project configuration
	assert.Equal(t, 100, result.UpdateProjects.Max, "UpdateProjects max should match")

	// Check create-project-status-update configuration
	assert.Equal(t, 1, result.CreateProjectStatusUpdates.Max, "CreateProjectStatusUpdates max should match")
}
