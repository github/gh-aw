//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProjectConfig(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name           string
		projectMap     map[string]any
		expectedConfig *ProjectConfig
	}{
		{
			name: "complete configuration",
			projectMap: map[string]any{
				"url":                         "https://github.com/orgs/github/projects/123",
				"scope":                       []any{"owner/repo1", "owner/repo2", "org:github"},
				"max-updates":                 50,
				"max-status-updates":          2,
				"github-token":                "${{ secrets.PROJECT_TOKEN }}",
				"do-not-downgrade-done-items": true,
			},
			expectedConfig: &ProjectConfig{
				URL:                     "https://github.com/orgs/github/projects/123",
				Scope:                   []string{"owner/repo1", "owner/repo2", "org:github"},
				MaxUpdates:              50,
				MaxStatusUpdates:        2,
				GitHubToken:             "${{ secrets.PROJECT_TOKEN }}",
				DoNotDowngradeDoneItems: boolPtr(true),
			},
		},
		{
			name: "configuration with scope only",
			projectMap: map[string]any{
				"url":   "https://github.com/orgs/github/projects/999",
				"scope": []any{"org:myorg", "owner/special-repo"},
			},
			expectedConfig: &ProjectConfig{
				URL:   "https://github.com/orgs/github/projects/999",
				Scope: []string{"org:myorg", "owner/special-repo"},
			},
		},
		{
			name: "minimal configuration with URL only",
			projectMap: map[string]any{
				"url": "https://github.com/users/username/projects/456",
			},
			expectedConfig: &ProjectConfig{
				URL: "https://github.com/users/username/projects/456",
			},
		},
		{
			name: "URL with max-updates",
			projectMap: map[string]any{
				"url":         "https://github.com/orgs/github/projects/789",
				"max-updates": 100,
			},
			expectedConfig: &ProjectConfig{
				URL:        "https://github.com/orgs/github/projects/789",
				MaxUpdates: 100,
			},
		},
		{
			name: "URL with custom token",
			projectMap: map[string]any{
				"url":          "https://github.com/orgs/github/projects/123",
				"github-token": "${{ secrets.CUSTOM_TOKEN }}",
			},
			expectedConfig: &ProjectConfig{
				URL:         "https://github.com/orgs/github/projects/123",
				GitHubToken: "${{ secrets.CUSTOM_TOKEN }}",
			},
		},
		{
			name: "numeric max-updates as float64",
			projectMap: map[string]any{
				"url":         "https://github.com/orgs/github/projects/123",
				"max-updates": 75.0,
			},
			expectedConfig: &ProjectConfig{
				URL:        "https://github.com/orgs/github/projects/123",
				MaxUpdates: 75,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := compiler.parseProjectConfig(tt.projectMap)

			require.NotNil(t, config, "parseProjectConfig() should not return nil")
			assert.Equal(t, tt.expectedConfig.URL, config.URL, "URL should match")
			assert.Equal(t, tt.expectedConfig.Scope, config.Scope, "Scope should match")
			assert.Equal(t, tt.expectedConfig.MaxUpdates, config.MaxUpdates, "MaxUpdates should match")
			assert.Equal(t, tt.expectedConfig.MaxStatusUpdates, config.MaxStatusUpdates, "MaxStatusUpdates should match")
			assert.Equal(t, tt.expectedConfig.GitHubToken, config.GitHubToken, "GitHubToken should match")

			if tt.expectedConfig.DoNotDowngradeDoneItems != nil {
				require.NotNil(t, config.DoNotDowngradeDoneItems, "DoNotDowngradeDoneItems should not be nil")
				assert.Equal(t, *tt.expectedConfig.DoNotDowngradeDoneItems, *config.DoNotDowngradeDoneItems, "DoNotDowngradeDoneItems should match")
			} else {
				assert.Nil(t, config.DoNotDowngradeDoneItems, "DoNotDowngradeDoneItems should be nil")
			}
		})
	}
}

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
				"project": "https://github.com/orgs/github/projects/123",
			},
			existingSafeOutputs: nil,
			expectUpdateProject: true,
			expectStatusUpdate:  true,
			expectedMaxUpdates:  100, // default
			expectedMaxStatus:   1,   // default
		},
		{
			name: "project with full config object",
			frontmatter: map[string]any{
				"project": map[string]any{
					"url":                "https://github.com/orgs/github/projects/456",
					"max-updates":        50,
					"max-status-updates": 2,
					"github-token":       "${{ secrets.PROJECT_TOKEN }}",
				},
			},
			existingSafeOutputs: nil,
			expectUpdateProject: true,
			expectStatusUpdate:  true,
			expectedMaxUpdates:  50,
			expectedMaxStatus:   2,
		},
		{
			name: "project with existing safe-outputs preserves existing",
			frontmatter: map[string]any{
				"project": "https://github.com/orgs/github/projects/789",
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
			expectedMaxUpdates:  25, // preserved from existing
			expectedMaxStatus:   3,  // preserved from existing
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
			name: "empty project map - returns existing",
			frontmatter: map[string]any{
				"project": map[string]any{},
			},
			existingSafeOutputs: nil,
			expectUpdateProject: false,
			expectStatusUpdate:  false,
		},
		{
			name: "project with no URL - returns existing",
			frontmatter: map[string]any{
				"project": map[string]any{
					"max-updates": 50,
				},
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

	// Test full integration: frontmatter -> safe-outputs config
	frontmatter := map[string]any{
		"project": map[string]any{
			"url":                "https://github.com/orgs/test/projects/100",
			"max-updates":        75,
			"max-status-updates": 2,
			"github-token":       "${{ secrets.TEST_TOKEN }}",
		},
	}

	result := compiler.applyProjectSafeOutputs(frontmatter, nil)

	require.NotNil(t, result, "Safe outputs should be created")
	require.NotNil(t, result.UpdateProjects, "UpdateProjects should be configured")
	require.NotNil(t, result.CreateProjectStatusUpdates, "CreateProjectStatusUpdates should be configured")

	// Check update-project configuration
	assert.Equal(t, 75, result.UpdateProjects.Max, "UpdateProjects max should match")
	assert.Equal(t, "${{ secrets.TEST_TOKEN }}", result.UpdateProjects.GitHubToken, "UpdateProjects token should match")

	// Check create-project-status-update configuration
	assert.Equal(t, 2, result.CreateProjectStatusUpdates.Max, "CreateProjectStatusUpdates max should match")
	assert.Equal(t, "${{ secrets.TEST_TOKEN }}", result.CreateProjectStatusUpdates.GitHubToken, "CreateProjectStatusUpdates token should match")
}
