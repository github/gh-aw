//go:build !integration

package workflow

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandlerManagerGitHubTokenEnvVarForCrossRepo verifies that GITHUB_TOKEN is exposed as
// an environment variable in the consolidated safe outputs handler step when create-pull-request
// or push-to-pull-request-branch is configured with a custom token. This is required so that
// the JavaScript handler's git CLI operations (dynamic checkout in multi-repo scenarios) can
// authenticate with the custom token instead of the default repo-scoped GITHUB_TOKEN.
func TestHandlerManagerGitHubTokenEnvVarForCrossRepo(t *testing.T) {
	tests := []struct {
		name                    string
		frontmatter             map[string]any
		expectedGitHubTokenLine string
		shouldHaveGitHubToken   bool
	}{
		{
			name: "create-pull-request with safe-outputs github-token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"github-token": "${{ secrets.CROSS_REPO_PAT }}",
					"create-pull-request": map[string]any{
						"max":           10,
						"base-branch":   "main",
						"allowed-repos": []any{"Org/repo-a", "Org/repo-b"},
					},
				},
			},
			expectedGitHubTokenLine: "GITHUB_TOKEN: ${{ secrets.CROSS_REPO_PAT }}",
			shouldHaveGitHubToken:   true,
		},
		{
			name: "create-pull-request with per-config github-token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"create-pull-request": map[string]any{
						"github-token":  "${{ secrets.PR_PAT }}",
						"max":           5,
						"allowed-repos": []any{"Org/repo-a"},
					},
				},
			},
			expectedGitHubTokenLine: "GITHUB_TOKEN: ${{ secrets.PR_PAT }}",
			shouldHaveGitHubToken:   true,
		},
		{
			name: "push-to-pull-request-branch with safe-outputs github-token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"github-token": "${{ secrets.PUSH_PAT }}",
					"push-to-pull-request-branch": map[string]any{
						"max": 3,
					},
				},
			},
			expectedGitHubTokenLine: "GITHUB_TOKEN: ${{ secrets.PUSH_PAT }}",
			shouldHaveGitHubToken:   true,
		},
		{
			name: "create-pull-request without custom token - no GITHUB_TOKEN override",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"create-pull-request": map[string]any{
						"max": 1,
					},
				},
			},
			shouldHaveGitHubToken: false,
		},
		{
			name: "push-to-pull-request-branch per-config token takes precedence over safe-outputs token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"github-token": "${{ secrets.SAFE_OUTPUTS_TOKEN }}",
					"push-to-pull-request-branch": map[string]any{
						"github-token": "${{ secrets.PUSH_PAT }}",
						"max":          2,
					},
				},
			},
			expectedGitHubTokenLine: "GITHUB_TOKEN: ${{ secrets.PUSH_PAT }}",
			shouldHaveGitHubToken:   true,
		},
		{
			name: "create-pull-request head-github-token does not override checkout token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"github-token": "${{ secrets.SAFE_OUTPUTS_TOKEN }}",
					"create-pull-request": map[string]any{
						"head-github-token": "${{ secrets.FORK_PAT }}",
						"head-repo":         "fork-owner/test-repo",
						"max":               2,
					},
				},
			},
			// head-github-token is a fork-write credential and must not govern the checkout.
			// The safe-outputs-level github-token should be used instead.
			expectedGitHubTokenLine: "GITHUB_TOKEN: ${{ secrets.SAFE_OUTPUTS_TOKEN }}",
			shouldHaveGitHubToken:   true,
		},
		{
			name: "add-comment without patches - no GITHUB_TOKEN override",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"github-token": "${{ secrets.SOME_PAT }}",
					"add-comment": map[string]any{
						"max": 5,
					},
				},
			},
			shouldHaveGitHubToken: false,
		},
		{
			name: "create-pull-request with github-app - uses minted app token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"github-app": map[string]any{
						"app-id":      "${{ vars.APP_ID }}",
						"private-key": "${{ secrets.APP_PRIVATE_KEY }}",
					},
					"create-pull-request": map[string]any{
						"max":           5,
						"allowed-repos": []any{"Org/repo-a"},
					},
				},
			},
			expectedGitHubTokenLine: "GITHUB_TOKEN: ${{ steps.safe-outputs-app-token.outputs.token }}",
			shouldHaveGitHubToken:   true,
		},
		{
			name: "per-config github-token overrides github-app token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"github-app": map[string]any{
						"app-id":      "${{ vars.APP_ID }}",
						"private-key": "${{ secrets.APP_PRIVATE_KEY }}",
					},
					"create-pull-request": map[string]any{
						"github-token":  "${{ secrets.CREATE_PR_PAT }}",
						"max":           5,
						"allowed-repos": []any{"Org/repo-a"},
					},
				},
			},
			expectedGitHubTokenLine: "GITHUB_TOKEN: ${{ secrets.CREATE_PR_PAT }}",
			shouldHaveGitHubToken:   true,
		},
		{
			name: "create-pull-request head-github-app does not override checkout token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"github-token": "${{ secrets.SAFE_OUTPUTS_TOKEN }}",
					"create-pull-request": map[string]any{
						"head-github-app": map[string]any{
							"client-id":   "${{ vars.HEAD_APP_ID }}",
							"private-key": "${{ secrets.HEAD_APP_PRIVATE_KEY }}",
						},
						"head-repo": "fork-owner/test-repo",
						"max":       2,
					},
				},
			},
			// head-github-app mints a fork-write credential and must not govern the checkout.
			// The safe-outputs-level github-token should be used instead.
			expectedGitHubTokenLine: "GITHUB_TOKEN: ${{ secrets.SAFE_OUTPUTS_TOKEN }}",
			shouldHaveGitHubToken:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			workflowData := &WorkflowData{
				Name:        "test-workflow",
				SafeOutputs: compiler.extractSafeOutputsConfig(tt.frontmatter),
			}

			steps, err := compiler.buildHandlerManagerStep(workflowData)
			require.NoError(t, err)
			yamlStr := strings.Join(steps, "")

			if tt.shouldHaveGitHubToken {
				assert.Contains(t, yamlStr, tt.expectedGitHubTokenLine,
					"Expected GITHUB_TOKEN env var %q to be set in handler manager step for cross-repo git operations",
					tt.expectedGitHubTokenLine)
			} else {
				assert.NotContains(t, yamlStr, "\n          GITHUB_TOKEN:",
					"Expected GITHUB_TOKEN to NOT be explicitly set when no custom checkout token is configured")
			}
		})
	}
}

// TestHandlerManagerProjectGitHubTokenEnvVar verifies that GH_AW_PROJECT_GITHUB_TOKEN
// is exposed as an environment variable in the consolidated safe outputs handler step
// when any project-related safe output is configured
func TestHandlerManagerProjectGitHubTokenEnvVar(t *testing.T) {
	tests := []struct {
		name                string
		frontmatter         map[string]any
		expectedEnvVarValue string
		expectedWithToken   string
		shouldHaveToken     bool
	}{
		{
			name: "update-project with custom github-token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"update-project": map[string]any{
						"github-token": "${{ secrets.PROJECTS_PAT }}",
						"project":      "https://github.com/orgs/myorg/projects/1",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.PROJECTS_PAT }}",
			expectedWithToken:   "github-token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			shouldHaveToken:     true,
		},
		{
			name: "update-project with custom github-token and safe-outputs github-token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"github-token": "${{ secrets.SAFE_OUTPUTS_TOKEN }}",
					"update-project": map[string]any{
						"github-token": "${{ secrets.PROJECTS_PAT }}",
						"project":      "https://github.com/orgs/myorg/projects/1",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.PROJECTS_PAT }}",
			expectedWithToken:   "github-token: ${{ secrets.SAFE_OUTPUTS_TOKEN }}",
			shouldHaveToken:     true,
		},
		{
			name: "update-project without custom github-token (uses GH_AW_PROJECT_GITHUB_TOKEN)",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"update-project": map[string]any{
						"project": "https://github.com/orgs/myorg/projects/1",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.GH_AW_PROJECT_GITHUB_TOKEN }}",
			expectedWithToken:   "github-token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			shouldHaveToken:     true,
		},
		{
			name: "update-project with safe-outputs github-token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"github-token": "${{ secrets.SAFE_OUTPUTS_TOKEN }}",
					"update-project": map[string]any{
						"project": "https://github.com/orgs/myorg/projects/1",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.SAFE_OUTPUTS_TOKEN }}",
			expectedWithToken:   "github-token: ${{ secrets.SAFE_OUTPUTS_TOKEN }}",
			shouldHaveToken:     true,
		},
		{
			name: "create-project-status-update with custom github-token",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"create-project-status-update": map[string]any{
						"github-token": "${{ secrets.STATUS_PAT }}",
						"project":      "https://github.com/orgs/myorg/projects/2",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.STATUS_PAT }}",
			expectedWithToken:   "github-token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			shouldHaveToken:     true,
		},
		{
			name: "create-project with custom github-token (no project URL)",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"create-project": map[string]any{
						"github-token": "${{ secrets.CREATE_PAT }}",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.CREATE_PAT }}",
			expectedWithToken:   "github-token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			shouldHaveToken:     true,
		},
		{
			name: "multiple project configs - update-project takes precedence",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"update-project": map[string]any{
						"github-token": "${{ secrets.UPDATE_PAT }}",
						"project":      "https://github.com/orgs/myorg/projects/1",
					},
					"create-project-status-update": map[string]any{
						"github-token": "${{ secrets.STATUS_PAT }}",
						"project":      "https://github.com/orgs/myorg/projects/2",
					},
					"create-project": map[string]any{
						"github-token": "${{ secrets.CREATE_PAT }}",
					},
				},
			},
			expectedEnvVarValue: "GH_AW_PROJECT_GITHUB_TOKEN: ${{ secrets.UPDATE_PAT }}",
			expectedWithToken:   "github-token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			shouldHaveToken:     true,
		},
		{
			name: "no project configs - no token set",
			frontmatter: map[string]any{
				"name": "Test Workflow",
				"safe-outputs": map[string]any{
					"add-comment": map[string]any{
						"max": 5,
					},
				},
			},
			shouldHaveToken: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			// Parse frontmatter
			workflowData := &WorkflowData{
				Name:        "test-workflow",
				SafeOutputs: compiler.extractSafeOutputsConfig(tt.frontmatter),
			}

			// Build the handler manager step
			steps, err := compiler.buildHandlerManagerStep(workflowData)
			require.NoError(t, err)
			yamlStr := strings.Join(steps, "")

			if tt.shouldHaveToken {
				// Check that the environment variable is present with the expected value
				assert.Contains(t, yamlStr, tt.expectedEnvVarValue,
					"Expected environment variable %q to be set in handler manager step",
					tt.expectedEnvVarValue)

				// Check that the github-script token uses safe-outputs token precedence.
				assert.Contains(t, yamlStr, tt.expectedWithToken,
					"Expected github-script token %q to be set in handler manager step",
					tt.expectedWithToken)
			} else {
				// Check that GH_AW_PROJECT_GITHUB_TOKEN is NOT set
				assert.NotContains(t, yamlStr, "GH_AW_PROJECT_GITHUB_TOKEN",
					"Expected GH_AW_PROJECT_GITHUB_TOKEN to NOT be set when no project configs are present")
			}
		})
	}
}

// TestHandlerManagerGitHubTokenIsolationAcrossSafeOutputHandlers verifies that the shared
// github-script client token remains sourced from safe-outputs.github-token regardless of which
// safe-output handler is configured alongside project-specific token settings.
func TestHandlerManagerGitHubTokenIsolationAcrossSafeOutputHandlers(t *testing.T) {
	const safeOutputsToken = "${{ secrets.SAFE_OUTPUTS_TOKEN }}"
	const handlerToken = "${{ secrets.HANDLER_TOKEN }}"

	projectHandlers := map[string]bool{
		"update_project":               true,
		"create_project_status_update": true,
		"create_project":               true,
	}

	handlerNames := make([]string, 0, len(handlerRegistry))
	for handlerName := range handlerRegistry {
		handlerNames = append(handlerNames, handlerName)
	}
	sort.Strings(handlerNames)

	for _, handlerName := range handlerNames {
		t.Run(handlerName, func(t *testing.T) {
			safeOutputs := &SafeOutputsConfig{
				GitHubToken: safeOutputsToken,
			}
			enableHandlerForIsolationTest(t, safeOutputs, handlerName, handlerToken)

			compiler := NewCompiler()
			workflowData := &WorkflowData{
				Name:        "test-workflow",
				SafeOutputs: safeOutputs,
			}

			steps, err := compiler.buildHandlerManagerStep(workflowData)
			require.NoError(t, err)

			yamlStr := strings.Join(steps, "")
			handlerConfig := extractHandlerManagerConfigFromYAML(t, yamlStr)
			_, ok := handlerConfig[handlerName]
			assert.True(t, ok, "expected handler %q to be present in GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", handlerName)

			assert.Contains(t, yamlStr, "github-token: "+safeOutputsToken)
			assert.NotContains(t, yamlStr, "github-token: "+handlerToken)
			if projectHandlers[handlerName] {
				assert.Contains(t, yamlStr, "GH_AW_PROJECT_GITHUB_TOKEN: "+handlerToken)
			} else {
				assert.NotContains(t, yamlStr, "GH_AW_PROJECT_GITHUB_TOKEN:")
			}
		})
	}
}

func enableHandlerForIsolationTest(t *testing.T, safeOutputs *SafeOutputsConfig, handlerName, token string) {
	t.Helper()

	switch handlerName {
	case "dispatch_repository":
		safeOutputs.DispatchRepository = &DispatchRepositoryConfig{
			Tools: map[string]*DispatchRepositoryToolConfig{
				"default": {
					Workflow:    "safe-outputs-dispatch",
					EventType:   "safe-outputs-dispatch",
					Repository:  "github/gh-aw",
					Max:         strPtr("1"),
					GitHubToken: token,
				},
			},
		}
		return
	case "create_report_incomplete_issue":
		safeOutputs.ReportIncomplete = &ReportIncompleteConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{
				Max:         strPtr("1"),
				GitHubToken: token,
			},
		}
		return
	}

	configValue := reflect.ValueOf(safeOutputs).Elem()
	configType := configValue.Type()

	//nolint:intrange // explicit bounds loop keeps compatibility with reviewers/tools that don't accept range-over-int
	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		yamlTag := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if yamlTag == "" || yamlTag == "-" {
			continue
		}
		if strings.ReplaceAll(yamlTag, "-", "_") != handlerName {
			continue
		}

		require.Equal(t, reflect.Pointer, field.Type.Kind(), "handler field %s must be a pointer type", field.Name)
		handlerValue := reflect.New(field.Type.Elem())
		initializeHandlerConfigForIsolationTest(handlerValue.Elem(), token)
		configValue.Field(i).Set(handlerValue)
		return
	}

	require.Failf(t, "missing handler field", "no SafeOutputsConfig field found for handler %q", handlerName)
}

func initializeHandlerConfigForIsolationTest(handlerStruct reflect.Value, token string) {
	baseField := handlerStruct.FieldByName("BaseSafeOutputConfig")
	if baseField.IsValid() {
		if maxField := baseField.FieldByName("Max"); maxField.IsValid() && maxField.CanSet() {
			maxField.Set(reflect.ValueOf(strPtr("1")))
		}
		if tokenField := baseField.FieldByName("GitHubToken"); tokenField.IsValid() && tokenField.CanSet() {
			tokenField.SetString(token)
		}
	}
	if tokenField := handlerStruct.FieldByName("GitHubToken"); tokenField.IsValid() && tokenField.CanSet() && tokenField.Kind() == reflect.String {
		tokenField.SetString(token)
	}

	// project URL is required for update-project and create-project-status-update
	if projectField := handlerStruct.FieldByName("Project"); projectField.IsValid() && projectField.CanSet() && projectField.Kind() == reflect.String {
		projectField.SetString("https://github.com/orgs/myorg/projects/1")
	}
}

func extractHandlerManagerConfigFromYAML(t *testing.T, yamlStr string) map[string]map[string]any {
	t.Helper()

	const prefix = "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: "
	for line := range strings.SplitSeq(yamlStr, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}

		configJSON := strings.TrimPrefix(trimmed, prefix)
		configJSON = strings.Trim(configJSON, "\"")
		configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")

		var config map[string]map[string]any
		require.NoError(t, json.Unmarshal([]byte(configJSON), &config), "handler config JSON should be valid")
		return config
	}

	require.Fail(t, "missing GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG in compiled YAML")
	return nil
}
