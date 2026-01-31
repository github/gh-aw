//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddHandlerManagerConfigEnvVar tests handler config JSON generation
func TestAddHandlerManagerConfigEnvVar(t *testing.T) {
	tests := []struct {
		name          string
		safeOutputs   *SafeOutputsConfig
		checkContains []string
		checkJSON     bool
		expectedKeys  []string
	}{
		{
			name: "create issue config",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 5,
					},
					AllowedLabels: []string{"bug", "feature"},
					Labels:        []string{"ai-generated"},
					TitlePrefix:   "[AI] ",
					Assignees:     []string{"user1"},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_issue"},
		},
		{
			name: "add comment config",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 3,
					},
					Target:            "issue",
					HideOlderComments: true,
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"add_comment"},
		},
		{
			name: "create discussion config",
			safeOutputs: &SafeOutputsConfig{
				CreateDiscussions: &CreateDiscussionsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 2,
					},
					Category:              "general",
					TitlePrefix:           "[Discussion] ",
					Labels:                []string{"ai"},
					CloseOlderDiscussions: true,
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_discussion"},
		},
		{
			name: "close issue config",
			safeOutputs: &SafeOutputsConfig{
				CloseIssues: &CloseEntityConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 10,
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"close_issue"},
		},
		{
			name: "add labels config",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{
					Allowed: []string{"bug", "enhancement", "documentation"},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"add_labels"},
		},
		{
			name: "update issue config",
			safeOutputs: &SafeOutputsConfig{
				UpdateIssues: &UpdateIssuesConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: 5,
						},
					},
					Status: testBoolPtr(true),
					Title:  testBoolPtr(true),
					Body:   testBoolPtr(true),
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"update_issue"},
		},
		{
			name: "create pull request config",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 3,
					},
					TitlePrefix: "[PR] ",
					Labels:      []string{"automated"},
					Draft:       testBoolPtr(true),
					IfNoChanges: "skip",
					AllowEmpty:  true,
					Expires:     7,
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_pull_request"},
		},
		{
			name: "push to PR branch config",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 5,
					},
					Target:            "pull_request",
					TitlePrefix:       "[Update] ",
					Labels:            []string{"update"},
					IfNoChanges:       "skip",
					CommitTitleSuffix: " - Auto Update",
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"push_to_pull_request_branch"},
		},
		{
			name: "multiple safe output types",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					TitlePrefix: "[Issue] ",
				},
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 3,
					},
				},
				AddLabels: &AddLabelsConfig{
					Allowed: []string{"bug"},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_issue", "add_comment", "add_labels"},
		},
		{
			name: "config with target-repo",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					TargetRepoSlug: "org/repo",
					TitlePrefix:    "[Test] ",
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_issue"},
		},
		{
			name: "config with allowed repos",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					AllowedRepos: []string{"org/repo1", "org/repo2"},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_issue"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			workflowData := &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: tt.safeOutputs,
			}

			var steps []string
			compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

			require.NotEmpty(t, steps)

			stepsContent := strings.Join(steps, "")

			for _, expected := range tt.checkContains {
				assert.Contains(t, stepsContent, expected, "Expected to find: "+expected)
			}

			// Extract and validate JSON if requested
			if tt.checkJSON {
				// Extract JSON from the env var line
				for _, step := range steps {
					if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
						// Extract the JSON value
						parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
						if len(parts) == 2 {
							jsonStr := strings.TrimSpace(parts[1])
							jsonStr = strings.Trim(jsonStr, "\"")
							jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")

							var config map[string]map[string]any
							err := json.Unmarshal([]byte(jsonStr), &config)
							require.NoError(t, err, "Config JSON should be valid")

							// Check expected keys
							for _, key := range tt.expectedKeys {
								assert.Contains(t, config, key, "Expected config key: "+key)
							}
						}
					}
				}
			}
		})
	}
}

// TestHandlerConfigMaxValues tests max value configuration
func TestHandlerConfigMaxValues(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 10,
				},
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	// Extract and validate JSON
	for _, step := range steps {
		if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
			if len(parts) == 2 {
				jsonStr := strings.TrimSpace(parts[1])
				jsonStr = strings.Trim(jsonStr, "\"")
				jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")

				var config map[string]map[string]any
				err := json.Unmarshal([]byte(jsonStr), &config)
				require.NoError(t, err)

				issueConfig, ok := config["create_issue"]
				require.True(t, ok)

				maxVal, ok := issueConfig["max"]
				require.True(t, ok)
				assert.InDelta(t, float64(10), maxVal, 0.0001)
			}
		}
	}
}

// TestHandlerConfigAllowedLabels tests allowed labels configuration
func TestHandlerConfigAllowedLabels(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				AllowedLabels: []string{"bug", "enhancement", "documentation"},
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	// Extract and validate JSON
	for _, step := range steps {
		if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
			if len(parts) == 2 {
				jsonStr := strings.TrimSpace(parts[1])
				jsonStr = strings.Trim(jsonStr, "\"")
				jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")

				var config map[string]map[string]any
				err := json.Unmarshal([]byte(jsonStr), &config)
				require.NoError(t, err)

				issueConfig, ok := config["create_issue"]
				require.True(t, ok)

				labels, ok := issueConfig["allowed_labels"]
				require.True(t, ok)

				labelSlice, ok := labels.([]any)
				require.True(t, ok)
				assert.Len(t, labelSlice, 3)
			}
		}
	}
}

// TestHandlerConfigBooleanFields tests boolean field configuration
func TestHandlerConfigBooleanFields(t *testing.T) {
	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		checkField  string
		checkKey    string
	}{
		{
			name: "hide older comments",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					HideOlderComments: true,
				},
			},
			checkField: "add_comment",
			checkKey:   "hide_older_comments",
		},
		{
			name: "close older discussions",
			safeOutputs: &SafeOutputsConfig{
				CreateDiscussions: &CreateDiscussionsConfig{
					CloseOlderDiscussions: true,
				},
			},
			checkField: "create_discussion",
			checkKey:   "close_older_discussions",
		},
		{
			name: "allow empty PR",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					AllowEmpty: true,
				},
			},
			checkField: "create_pull_request",
			checkKey:   "allow_empty",
		},
		{
			name: "draft PR",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					Draft: testBoolPtr(true),
				},
			},
			checkField: "create_pull_request",
			checkKey:   "draft",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			workflowData := &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: tt.safeOutputs,
			}

			var steps []string
			compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

			// Extract and validate JSON
			for _, step := range steps {
				if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
					parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
					if len(parts) == 2 {
						jsonStr := strings.TrimSpace(parts[1])
						jsonStr = strings.Trim(jsonStr, "\"")
						jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")

						var config map[string]map[string]any
						err := json.Unmarshal([]byte(jsonStr), &config)
						require.NoError(t, err)

						fieldConfig, ok := config[tt.checkField]
						require.True(t, ok, "Expected config for: "+tt.checkField)

						val, ok := fieldConfig[tt.checkKey]
						require.True(t, ok, "Expected key: "+tt.checkKey)
						assert.Equal(t, true, val)
					}
				}
			}
		})
	}
}

// TestHandlerConfigUpdateFields tests update field configurations
func TestHandlerConfigUpdateFields(t *testing.T) {
	tests := []struct {
		name         string
		config       *UpdateIssuesConfig
		expectedKeys []string
	}{
		{
			name: "all fields enabled",
			config: &UpdateIssuesConfig{
				Status: testBoolPtr(true),
				Title:  testBoolPtr(true),
				Body:   testBoolPtr(true),
			},
			expectedKeys: []string{"allow_status", "allow_title", "allow_body"},
		},
		{
			name: "only status",
			config: &UpdateIssuesConfig{
				Status: testBoolPtr(true),
			},
			expectedKeys: []string{"allow_status"},
		},
		{
			name: "title and body",
			config: &UpdateIssuesConfig{
				Title: testBoolPtr(true),
				Body:  testBoolPtr(true),
			},
			expectedKeys: []string{"allow_title", "allow_body"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			workflowData := &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					UpdateIssues: tt.config,
				},
			}

			var steps []string
			compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

			// Extract and validate JSON
			for _, step := range steps {
				if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
					parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
					if len(parts) == 2 {
						jsonStr := strings.TrimSpace(parts[1])
						jsonStr = strings.Trim(jsonStr, "\"")
						jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")

						var config map[string]map[string]any
						err := json.Unmarshal([]byte(jsonStr), &config)
						require.NoError(t, err)

						updateConfig, ok := config["update_issue"]
						require.True(t, ok)

						for _, key := range tt.expectedKeys {
							_, ok := updateConfig[key]
							assert.True(t, ok, "Expected key: "+key)
						}
					}
				}
			}
		})
	}
}

// TestEmptySafeOutputsConfig tests behavior with no safe outputs
func TestEmptySafeOutputsConfig(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		SafeOutputs: nil,
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	// Should not add any steps when safe outputs is nil
	assert.Empty(t, steps)
}

// TestHandlerConfigTargetRepo tests target-repo configuration
func TestHandlerConfigTargetRepo(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				TargetRepoSlug: "org/target-repo",
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	// Extract and validate JSON
	for _, step := range steps {
		if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
			if len(parts) == 2 {
				jsonStr := strings.TrimSpace(parts[1])
				jsonStr = strings.Trim(jsonStr, "\"")
				jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")

				var config map[string]map[string]any
				err := json.Unmarshal([]byte(jsonStr), &config)
				require.NoError(t, err)

				issueConfig, ok := config["create_issue"]
				require.True(t, ok)

				targetRepo, ok := issueConfig["target-repo"]
				require.True(t, ok)
				assert.Equal(t, "org/target-repo", targetRepo)
			}
		}
	}
}

// TestHandlerConfigPatchSize tests max patch size configuration
func TestHandlerConfigPatchSize(t *testing.T) {
	tests := []struct {
		name         string
		maxPatchSize int
		expectedSize int
	}{
		{
			name:         "default patch size",
			maxPatchSize: 0,
			expectedSize: 1024,
		},
		{
			name:         "custom patch size",
			maxPatchSize: 2048,
			expectedSize: 2048,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			workflowData := &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					MaximumPatchSize: tt.maxPatchSize,
					CreatePullRequests: &CreatePullRequestsConfig{
						TitlePrefix: "[PR] ",
					},
				},
			}

			var steps []string
			compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

			// Extract and validate JSON
			for _, step := range steps {
				if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
					parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
					if len(parts) == 2 {
						jsonStr := strings.TrimSpace(parts[1])
						jsonStr = strings.Trim(jsonStr, "\"")
						jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")

						var config map[string]map[string]any
						err := json.Unmarshal([]byte(jsonStr), &config)
						require.NoError(t, err)

						prConfig, ok := config["create_pull_request"]
						require.True(t, ok)

						maxSize, ok := prConfig["max_patch_size"]
						require.True(t, ok)
						assert.InDelta(t, float64(tt.expectedSize), maxSize, 0.0001)
					}
				}
			}
		})
	}
}

// testBoolPtr is a helper function for bool pointers in config tests
func testBoolPtr(b bool) *bool {
	return &b
}

// TestAutoEnabledHandlers tests that missing_tool and missing_data
// are automatically enabled even when not explicitly configured.
// Note: noop is NOT included here because it is always processed by a dedicated
// standalone step (see notify_comment.go) and should never be in the handler manager config.
func TestAutoEnabledHandlers(t *testing.T) {
	tests := []struct {
		name         string
		safeOutputs  *SafeOutputsConfig
		expectedKeys []string
	}{
		{
			name: "missing_tool auto-enabled",
			safeOutputs: &SafeOutputsConfig{
				MissingTool: &MissingToolConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 5,
					},
				},
			},
			expectedKeys: []string{"missing_tool"},
		},
		{
			name: "missing_data auto-enabled",
			safeOutputs: &SafeOutputsConfig{
				MissingData: &MissingDataConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 5,
					},
				},
			},
			expectedKeys: []string{"missing_data"},
		},
		{
			name: "all auto-enabled handlers together",
			safeOutputs: &SafeOutputsConfig{
				MissingTool: &MissingToolConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 5,
					},
				},
				MissingData: &MissingDataConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 5,
					},
				},
			},
			expectedKeys: []string{"missing_tool", "missing_data"},
		},
		{
			name: "auto-enabled with other handlers",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					TitlePrefix: "[Test] ",
				},
				MissingTool: &MissingToolConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 5,
					},
				},
			},
			expectedKeys: []string{"create_issue", "missing_tool"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			workflowData := &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: tt.safeOutputs,
			}

			var steps []string
			compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

			require.NotEmpty(t, steps, "Steps should be generated")

			// Extract and validate JSON
			for _, step := range steps {
				if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
					parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
					if len(parts) == 2 {
						jsonStr := strings.TrimSpace(parts[1])
						jsonStr = strings.Trim(jsonStr, "\"")
						jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")

						var config map[string]map[string]any
						err := json.Unmarshal([]byte(jsonStr), &config)
						require.NoError(t, err, "Config JSON should be valid")

						// Check that all expected keys are present
						for _, key := range tt.expectedKeys {
							_, ok := config[key]
							assert.True(t, ok, "Expected auto-enabled handler: "+key)
						}
					}
				}
			}
		})
	}
}
