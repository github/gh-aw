//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCreateProjectsConfig(t *testing.T) {
	tests := []struct {
		name           string
		outputMap      map[string]any
		expectedConfig *CreateProjectsConfig
		expectedNil    bool
	}{
		{
			name: "basic config with max",
			outputMap: map[string]any{
				"create-project": map[string]any{
					"max": 2,
				},
			},
			expectedConfig: &CreateProjectsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 2,
				},
			},
		},
		{
			name: "config with all fields",
			outputMap: map[string]any{
				"create-project": map[string]any{
					"max":          1,
					"github-token": "${{ secrets.PROJECTS_PAT }}",
					"target-owner": "myorg",
					"title-prefix": "Project",
				},
			},
			expectedConfig: &CreateProjectsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
				GitHubToken: "${{ secrets.PROJECTS_PAT }}",
				TargetOwner: "myorg",
				TitlePrefix: "Project",
			},
		},
		{
			name: "config with views",
			outputMap: map[string]any{
				"create-project": map[string]any{
					"max": 1,
					"views": []any{
						map[string]any{
							"name":   "Roadmap",
							"layout": "roadmap",
							"filter": "is:issue is:pr",
						},
						map[string]any{
							"name":   "Task Tracker",
							"layout": "table",
							"filter": "is:open",
						},
					},
				},
			},
			expectedConfig: &CreateProjectsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
				Views: []ProjectView{
					{
						Name:   "Roadmap",
						Layout: "roadmap",
						Filter: "is:issue is:pr",
					},
					{
						Name:   "Task Tracker",
						Layout: "table",
						Filter: "is:open",
					},
				},
			},
		},
		{
			name: "config with views including visible-fields",
			outputMap: map[string]any{
				"create-project": map[string]any{
					"max": 1,
					"views": []any{
						map[string]any{
							"name":           "Task Board",
							"layout":         "board",
							"filter":         "is:issue",
							"visible-fields": []any{1, 2, 3},
							"description":    "Main task board",
						},
					},
				},
			},
			expectedConfig: &CreateProjectsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
				Views: []ProjectView{
					{
						Name:          "Task Board",
						Layout:        "board",
						Filter:        "is:issue",
						VisibleFields: []int{1, 2, 3},
						Description:   "Main task board",
					},
				},
			},
		},
		{
			name: "config with default max when not specified",
			outputMap: map[string]any{
				"create-project": map[string]any{
					"target-owner": "testorg",
				},
			},
			expectedConfig: &CreateProjectsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
				TargetOwner: "testorg",
			},
		},
		{
			name: "no create-project config",
			outputMap: map[string]any{
				"create-issue": map[string]any{},
			},
			expectedNil: true,
		},
		{
			name:        "empty outputMap",
			outputMap:   map[string]any{},
			expectedNil: true,
		},
		{
			name: "views with missing required fields are skipped",
			outputMap: map[string]any{
				"create-project": map[string]any{
					"max": 1,
					"views": []any{
						map[string]any{
							"name":   "Valid View",
							"layout": "table",
						},
						map[string]any{
							// Missing layout - should be skipped
							"name": "Invalid View",
						},
						map[string]any{
							// Missing name - should be skipped
							"layout": "board",
						},
					},
				},
			},
			expectedConfig: &CreateProjectsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
				Views: []ProjectView{
					{
						Name:   "Valid View",
						Layout: "table",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			config := compiler.parseCreateProjectsConfig(tt.outputMap)

			if tt.expectedNil {
				assert.Nil(t, config, "Expected nil config")
			} else {
				require.NotNil(t, config, "Expected non-nil config")
				assert.Equal(t, tt.expectedConfig.Max, config.Max, "Max should match")
				assert.Equal(t, tt.expectedConfig.GitHubToken, config.GitHubToken, "GitHubToken should match")
				assert.Equal(t, tt.expectedConfig.TargetOwner, config.TargetOwner, "TargetOwner should match")
				assert.Equal(t, tt.expectedConfig.TitlePrefix, config.TitlePrefix, "TitlePrefix should match")
				assert.Len(t, config.Views, len(tt.expectedConfig.Views), "Views count should match")

				// Check views details
				for i, expectedView := range tt.expectedConfig.Views {
					assert.Equal(t, expectedView.Name, config.Views[i].Name, "View name should match")
					assert.Equal(t, expectedView.Layout, config.Views[i].Layout, "View layout should match")
					assert.Equal(t, expectedView.Filter, config.Views[i].Filter, "View filter should match")
					assert.Equal(t, expectedView.VisibleFields, config.Views[i].VisibleFields, "View visible fields should match")
					assert.Equal(t, expectedView.Description, config.Views[i].Description, "View description should match")
				}

				// Check field definitions
				assert.Len(t, config.FieldDefinitions, len(tt.expectedConfig.FieldDefinitions), "Field definitions count should match")
				for i, expectedField := range tt.expectedConfig.FieldDefinitions {
					assert.Equal(t, expectedField.Name, config.FieldDefinitions[i].Name, "Field name should match")
					assert.Equal(t, expectedField.DataType, config.FieldDefinitions[i].DataType, "Field data type should match")
					assert.Equal(t, expectedField.Options, config.FieldDefinitions[i].Options, "Field options should match")
				}
			}
		})
	}
}

func TestCreateProjectsConfig_DefaultMax(t *testing.T) {
	compiler := NewCompiler()

	outputMap := map[string]any{
		"create-project": map[string]any{
			"target-owner": "myorg",
		},
	}

	config := compiler.parseCreateProjectsConfig(outputMap)
	require.NotNil(t, config)

	// Default max should be 1 when not specified
	assert.Equal(t, 1, config.Max, "Default max should be 1")
}

func TestCreateProjectsConfig_ViewsParsing(t *testing.T) {
	compiler := NewCompiler()

	outputMap := map[string]any{
		"create-project": map[string]any{
			"max": 1,
			"views": []any{
				map[string]any{
					"name":   "Sprint Board",
					"layout": "board",
					"filter": "is:open label:sprint",
				},
				map[string]any{
					"name":   "Timeline",
					"layout": "roadmap",
				},
			},
		},
	}

	config := compiler.parseCreateProjectsConfig(outputMap)
	require.NotNil(t, config)
	require.Len(t, config.Views, 2, "Should parse 2 views")

	// Check first view
	assert.Equal(t, "Sprint Board", config.Views[0].Name)
	assert.Equal(t, "board", config.Views[0].Layout)
	assert.Equal(t, "is:open label:sprint", config.Views[0].Filter)

	// Check second view
	assert.Equal(t, "Timeline", config.Views[1].Name)
	assert.Equal(t, "roadmap", config.Views[1].Layout)
	assert.Empty(t, config.Views[1].Filter) // No filter specified
}

func TestCreateProjectsConfig_FieldDefinitionsParsing(t *testing.T) {
	compiler := NewCompiler()

	outputMap := map[string]any{
		"create-project": map[string]any{
			"max": 1,
			"field-definitions": []any{
				map[string]any{
					"name":      "Tracking Id",
					"data-type": "TEXT",
				},
				map[string]any{
					"name":      "Priority",
					"data-type": "SINGLE_SELECT",
					"options":   []any{"High", "Medium", "Low"},
				},
				map[string]any{
					"name":      "Start Date",
					"data-type": "DATE",
				},
			},
		},
	}

	config := compiler.parseCreateProjectsConfig(outputMap)
	require.NotNil(t, config, "Config should not be nil")
	require.Len(t, config.FieldDefinitions, 3, "Should parse 3 field definitions")

	// Check first field
	assert.Equal(t, "Tracking Id", config.FieldDefinitions[0].Name)
	assert.Equal(t, "TEXT", config.FieldDefinitions[0].DataType)
	assert.Empty(t, config.FieldDefinitions[0].Options)

	// Check second field
	assert.Equal(t, "Priority", config.FieldDefinitions[1].Name)
	assert.Equal(t, "SINGLE_SELECT", config.FieldDefinitions[1].DataType)
	assert.Equal(t, []string{"High", "Medium", "Low"}, config.FieldDefinitions[1].Options)

	// Check third field
	assert.Equal(t, "Start Date", config.FieldDefinitions[2].Name)
	assert.Equal(t, "DATE", config.FieldDefinitions[2].DataType)
	assert.Empty(t, config.FieldDefinitions[2].Options)
}

func TestCreateProjectsConfig_FieldDefinitionsWithUnderscores(t *testing.T) {
	compiler := NewCompiler()

	// Test underscore variant of field-definitions and data-type
	outputMap := map[string]any{
		"create-project": map[string]any{
			"max": 1,
			"field_definitions": []any{
				map[string]any{
					"name":      "Worker Workflow",
					"data_type": "TEXT",
				},
			},
		},
	}

	config := compiler.parseCreateProjectsConfig(outputMap)
	require.NotNil(t, config, "Config should not be nil")
	require.Len(t, config.FieldDefinitions, 1, "Should parse 1 field definition")

	assert.Equal(t, "Worker Workflow", config.FieldDefinitions[0].Name)
	assert.Equal(t, "TEXT", config.FieldDefinitions[0].DataType)
}

func TestCreateProjectsConfig_ViewsAndFieldDefinitions(t *testing.T) {
	compiler := NewCompiler()

	outputMap := map[string]any{
		"create-project": map[string]any{
			"max":          1,
			"target-owner": "myorg",
			"views": []any{
				map[string]any{
					"name":   "Task Board",
					"layout": "board",
				},
			},
			"field-definitions": []any{
				map[string]any{
					"name":      "Tracking Id",
					"data-type": "TEXT",
				},
				map[string]any{
					"name":      "Size",
					"data-type": "SINGLE_SELECT",
					"options":   []any{"Small", "Medium", "Large"},
				},
			},
		},
	}

	config := compiler.parseCreateProjectsConfig(outputMap)
	require.NotNil(t, config, "Config should not be nil")

	// Check views
	require.Len(t, config.Views, 1, "Should have 1 view")
	assert.Equal(t, "Task Board", config.Views[0].Name)
	assert.Equal(t, "board", config.Views[0].Layout)

	// Check field definitions
	require.Len(t, config.FieldDefinitions, 2, "Should have 2 field definitions")
	assert.Equal(t, "Tracking Id", config.FieldDefinitions[0].Name)
	assert.Equal(t, "TEXT", config.FieldDefinitions[0].DataType)
	assert.Equal(t, "Size", config.FieldDefinitions[1].Name)
	assert.Equal(t, "SINGLE_SELECT", config.FieldDefinitions[1].DataType)
	assert.Equal(t, []string{"Small", "Medium", "Large"}, config.FieldDefinitions[1].Options)
}
