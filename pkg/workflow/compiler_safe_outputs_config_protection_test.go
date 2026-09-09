//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========================================
// Protected Files and Checkout Mapping Tests
// ========================================

// TestProtectedFilesExclude verifies that the _protected_files_exclude sentinel key is
// used at compile time to filter manifest files and is NOT forwarded to the runtime config.
func TestProtectedFilesExclude(t *testing.T) {
	tests := []struct {
		name               string
		excludeFiles       []string
		wantExcludedFromPF []string // files that must NOT be in the final protected_files list
		wantPresentInPF    []string // files that must still be in the protected_files list
	}{
		{
			name:               "exclude AGENTS.md from create-pull-request",
			excludeFiles:       []string{"AGENTS.md"},
			wantExcludedFromPF: []string{"AGENTS.md", "CHANGELOG.md"},
			wantPresentInPF:    []string{"package.json", "go.mod", "CODEOWNERS", "DESIGN.md"},
		},
		{
			name:               "exclude multiple files",
			excludeFiles:       []string{"AGENTS.md", "CLAUDE.md"},
			wantExcludedFromPF: []string{"AGENTS.md", "CLAUDE.md", "CHANGELOG.md"},
			wantPresentInPF:    []string{"package.json", "go.mod"},
		},
		{
			name:               "empty exclude list leaves defaults intact",
			excludeFiles:       nil,
			wantExcludedFromPF: []string{"CHANGELOG.md"},
			wantPresentInPF:    []string{"package.json", "go.mod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			workflowData := &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					CreatePullRequests: &CreatePullRequestsConfig{
						BaseSafeOutputConfig:  BaseSafeOutputConfig{Max: strPtr("1")},
						ProtectedFilesExclude: tt.excludeFiles,
					},
				},
			}

			var steps []string
			compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
			require.NotEmpty(t, steps, "should produce config steps")

			stepsContent := strings.Join(steps, "")
			require.Contains(t, stepsContent, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", "should produce handler config")

			// Extract JSON
			var configJSON string
			for _, step := range steps {
				if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
					parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
					require.Len(t, parts, 2, "should be able to split env var line")
					configJSON = strings.TrimSpace(parts[1])
					configJSON = strings.Trim(configJSON, "\"")
					configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
				}
			}
			require.NotEmpty(t, configJSON, "should have extracted JSON")

			var config map[string]map[string]any
			require.NoError(t, json.Unmarshal([]byte(configJSON), &config), "config JSON should be valid")

			prConfig, ok := config["create_pull_request"]
			require.True(t, ok, "should have create_pull_request config")

			// Sentinel key must NOT appear in the final runtime config
			_, hasSentinel := prConfig["_protected_files_exclude"]
			assert.False(t, hasSentinel, "_protected_files_exclude sentinel must not appear in runtime config")

			// Check protected_files list
			pfRaw, ok := prConfig["protected_files"]
			require.True(t, ok, "should have protected_files field")
			pfAny, ok := pfRaw.([]any)
			require.True(t, ok, "protected_files should be a slice")
			pfStrings := make([]string, 0, len(pfAny))
			for _, v := range pfAny {
				if s, ok := v.(string); ok {
					pfStrings = append(pfStrings, s)
				}
			}

			for _, excluded := range tt.wantExcludedFromPF {
				assert.NotContains(t, pfStrings, excluded,
					"excluded file %q should not appear in protected_files", excluded)
			}
			for _, present := range tt.wantPresentInPF {
				assert.Contains(t, pfStrings, present,
					"non-excluded file %q should still appear in protected_files", present)
			}
		})
	}
}

// TestProtectedFilesExcludePushToPRBranch verifies the same exclusion logic for
// the push_to_pull_request_branch handler.
func TestProtectedFilesExcludePushToPRBranch(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			PushToPullRequestBranch: &PushToPullRequestBranchConfig{
				ProtectedFilesExclude: []string{"AGENTS.md"},
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
	require.NotEmpty(t, steps, "should produce config steps")

	var configJSON string
	for _, step := range steps {
		if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
			require.Len(t, parts, 2, "should split env var line")
			configJSON = strings.TrimSpace(parts[1])
			configJSON = strings.Trim(configJSON, "\"")
			configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
		}
	}
	require.NotEmpty(t, configJSON, "should have extracted JSON")

	var config map[string]map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config), "config JSON should be valid")

	pushConfig, ok := config["push_to_pull_request_branch"]
	require.True(t, ok, "should have push_to_pull_request_branch config")

	_, hasSentinel := pushConfig["_protected_files_exclude"]
	assert.False(t, hasSentinel, "_protected_files_exclude sentinel must not appear in runtime config")

	pfRaw, ok := pushConfig["protected_files"]
	require.True(t, ok, "should have protected_files field")
	pfAny, ok := pfRaw.([]any)
	require.True(t, ok, "protected_files should be a slice")
	pfStrings := make([]string, 0, len(pfAny))
	for _, v := range pfAny {
		if s, ok := v.(string); ok {
			pfStrings = append(pfStrings, s)
		}
	}
	assert.NotContains(t, pfStrings, "AGENTS.md", "AGENTS.md should be excluded from protected_files")
	assert.NotContains(t, pfStrings, "CHANGELOG.md", "CHANGELOG.md should be excluded by default from protected_files")
	assert.Contains(t, pfStrings, "package.json", "package.json should still be in protected_files")

	// Dot-folder prefixes are no longer in protected_path_prefixes — they are
	// covered by the general protect_top_level_dot_folders rule.
	_, hasProtectedPathPrefixes := pushConfig["protected_path_prefixes"]
	assert.False(t, hasProtectedPathPrefixes, "protected_path_prefixes should be absent: dot-folders are covered by protect_top_level_dot_folders")
}

// TestGetDotFolderExcludes verifies that getDotFolderExcludes correctly identifies
// top-level dot-folder path prefixes from an exclusion list.
func TestGetDotFolderExcludes(t *testing.T) {
	tests := []struct {
		name         string
		excludeFiles []string
		want         []string
	}{
		{
			name:         "empty input returns nil",
			excludeFiles: nil,
			want:         nil,
		},
		{
			name:         "no dot-folder entries",
			excludeFiles: []string{"AGENTS.md", "CLAUDE.md", "go.mod"},
			want:         nil,
		},
		{
			name:         "single dot-folder prefix",
			excludeFiles: []string{".agents/"},
			want:         []string{".agents/"},
		},
		{
			name:         "mixed files and dot-folder prefixes",
			excludeFiles: []string{"AGENTS.md", ".agents/", "go.mod", ".cursor/"},
			want:         []string{".agents/", ".cursor/"},
		},
		{
			name:         "dot-file without trailing slash is not a dot-folder",
			excludeFiles: []string{".env"},
			want:         nil,
		},
		{
			name:         "dot alone is not a valid dot-folder",
			excludeFiles: []string{"./"},
			want:         nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getDotFolderExcludes(tt.excludeFiles)
			if len(tt.want) == 0 {
				assert.Empty(t, got, "expected no dot-folder excludes")
			} else {
				assert.Equal(t, tt.want, got, "dot-folder excludes should match expected list")
			}
		})
	}
}

// extractHandlerManagerConfigJSON compiles a minimal workflow with both
// create_pull_request and push_to_pull_request_branch handlers and returns the
// decoded handler-config map, ready for per-handler assertions.
func extractHandlerManagerConfigJSON(t *testing.T) map[string]map[string]any {
	t.Helper()
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
			},
			PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
	require.NotEmpty(t, steps, "should produce config steps")

	var configJSON string
	for _, step := range steps {
		if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
			require.Len(t, parts, 2, "should split env var line")
			configJSON = strings.TrimSpace(parts[1])
			configJSON = strings.Trim(configJSON, "\"")
			configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
		}
	}
	require.NotEmpty(t, configJSON, "should have extracted JSON")

	var config map[string]map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config), "config JSON should be valid")
	return config
}

// TestProtectTopLevelDotFolders verifies that protect_top_level_dot_folders is always
// set to true in both create_pull_request and push_to_pull_request_branch handler configs.
func TestProtectTopLevelDotFolders(t *testing.T) {
	config := extractHandlerManagerConfigJSON(t)

	for _, handlerName := range []string{"create_pull_request", "push_to_pull_request_branch"} {
		handlerCfg, ok := config[handlerName]
		require.True(t, ok, "%s handler should be present", handlerName)
		val, exists := handlerCfg["protect_top_level_dot_folders"]
		assert.True(t, exists, "%s: protect_top_level_dot_folders should be present", handlerName)
		boolVal, ok := val.(bool)
		require.True(t, ok, "%s: protect_top_level_dot_folders should be a bool", handlerName)
		assert.True(t, boolVal, "%s: protect_top_level_dot_folders should be true", handlerName)
	}
}

func TestInjectCheckoutMappingForWildcardTargetRepo(t *testing.T) {
	t.Run("injects mapping when target-repo is wildcard", func(t *testing.T) {
		data := &WorkflowData{
			CheckoutConfigs: []*CheckoutConfig{
				{Repository: "octocat/Hello-World", Path: "./hello-world"},
				{Repository: "octocat/Spoon-Knife", Path: "./spoon-knife"},
			},
		}
		handlerCfg := map[string]any{"target-repo": "*"}
		injectCheckoutMapping("create_pull_request", handlerCfg, data)
		mapping, ok := handlerCfg["checkout_mapping"].(map[string]string)
		require.True(t, ok, "checkout_mapping should be a map[string]string")
		assert.Equal(t, "hello-world", mapping["octocat/hello-world"])
		assert.Equal(t, "spoon-knife", mapping["octocat/spoon-knife"])
	})

	t.Run("skips when target-repo is not wildcard", func(t *testing.T) {
		data := &WorkflowData{
			CheckoutConfigs: []*CheckoutConfig{
				{Repository: "octocat/Hello-World", Path: "./hello-world"},
			},
		}
		handlerCfg := map[string]any{"target-repo": "octocat/Hello-World"}
		injectCheckoutMapping("create_pull_request", handlerCfg, data)
		_, ok := handlerCfg["checkout_mapping"]
		assert.False(t, ok, "checkout_mapping should not be injected for non-wildcard")
	})

	t.Run("skips wiki checkouts", func(t *testing.T) {
		data := &WorkflowData{
			CheckoutConfigs: []*CheckoutConfig{
				{Repository: "octocat/Hello-World", Path: "./hello-world", Wiki: true},
			},
		}
		handlerCfg := map[string]any{"target-repo": "*"}
		injectCheckoutMapping("create_pull_request", handlerCfg, data)
		_, ok := handlerCfg["checkout_mapping"]
		assert.False(t, ok, "checkout_mapping should not include wiki checkouts")
	})

	t.Run("skips unrelated handlers", func(t *testing.T) {
		data := &WorkflowData{
			CheckoutConfigs: []*CheckoutConfig{
				{Repository: "octocat/Hello-World", Path: "./hello-world"},
			},
		}
		handlerCfg := map[string]any{"target-repo": "*"}
		injectCheckoutMapping("create_issue", handlerCfg, data)
		_, ok := handlerCfg["checkout_mapping"]
		assert.False(t, ok, "checkout_mapping should not be injected for unrelated handlers")
	})
}

func TestHandlerConfigInjectsCurrentCheckoutPatchWorkspacePath(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		CheckoutConfigs: []*CheckoutConfig{
			{
				Repository: "caido/proxy-frontend",
				Path:       "./proxy-frontend",
				Current:    true,
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				TargetRepoSlug:       "caido/proxy-frontend",
			},
			PushToPullRequestBranch: &PushToPullRequestBranchConfig{
				TargetRepoSlug: "caido/proxy-frontend",
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
	require.NotEmpty(t, steps, "should produce config steps")

	var configJSON string
	for _, step := range steps {
		if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
			require.Len(t, parts, 2, "should split env var line")
			configJSON = strings.TrimSpace(parts[1])
			configJSON = strings.Trim(configJSON, "\"")
			configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
		}
	}
	require.NotEmpty(t, configJSON, "should have extracted JSON")

	var config map[string]map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config), "config JSON should be valid")

	for _, handlerName := range []string{"create_pull_request", "push_to_pull_request_branch"} {
		handlerCfg, ok := config[handlerName]
		require.True(t, ok, "%s handler should be present", handlerName)
		assert.Equal(t, "proxy-frontend", handlerCfg["patch_workspace_path"])
		assert.Equal(t, "caido/proxy-frontend", handlerCfg["current_checkout_repo"])
	}
}

// TestProtectTopLevelMdFiles verifies that well-known top-level Markdown files
// (README.md, CONTRIBUTING.md, SECURITY.md, CODE_OF_CONDUCT.md) are always
// included in the protected_files list in both handler configs, while CHANGELOG.md
// is intentionally excluded by default to allow routine changelog updates.
func TestProtectTopLevelMdFiles(t *testing.T) {
	config := extractHandlerManagerConfigJSON(t)

	expectedFiles := []string{"README.md", "CONTRIBUTING.md", "SECURITY.md", "CODE_OF_CONDUCT.md"}
	for _, handlerName := range []string{"create_pull_request", "push_to_pull_request_branch"} {
		handlerCfg, ok := config[handlerName]
		require.True(t, ok, "%s handler should be present", handlerName)
		rawFiles, exists := handlerCfg["protected_files"]
		require.True(t, exists, "%s: protected_files should be present", handlerName)
		filesSlice, ok := rawFiles.([]any)
		require.True(t, ok, "%s: protected_files should be a slice", handlerName)
		fileSet := make(map[string]bool, len(filesSlice))
		for _, f := range filesSlice {
			if s, ok := f.(string); ok {
				fileSet[s] = true
			}
		}
		for _, expectedFile := range expectedFiles {
			assert.True(t, fileSet[expectedFile], "%s: protected_files should contain %s", handlerName, expectedFile)
		}
	}
}

// TestProtectedDotFolderExcludes verifies that when a dot-folder prefix is excluded via
// ProtectedFilesExclude, the runtime config receives a protected_dot_folder_excludes list.
func TestProtectedDotFolderExcludes(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig:  BaseSafeOutputConfig{Max: strPtr("1")},
				ProtectedFilesExclude: []string{"AGENTS.md", ".agents/", ".cursor/"},
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
	require.NotEmpty(t, steps, "should produce config steps")

	var configJSON string
	for _, step := range steps {
		if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
			require.Len(t, parts, 2, "should split env var line")
			configJSON = strings.TrimSpace(parts[1])
			configJSON = strings.Trim(configJSON, "\"")
			configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
		}
	}
	require.NotEmpty(t, configJSON, "should have extracted JSON")

	var config map[string]map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config), "config JSON should be valid")

	prConfig, ok := config["create_pull_request"]
	require.True(t, ok, "should have create_pull_request config")

	// Sentinel key must not leak into runtime config
	_, hasSentinel := prConfig["_protected_files_exclude"]
	assert.False(t, hasSentinel, "_protected_files_exclude sentinel must not appear in runtime config")

	// Non-dot-folder excludes must not be in protected_dot_folder_excludes
	raw, exists := prConfig["protected_dot_folder_excludes"]
	require.True(t, exists, "protected_dot_folder_excludes should be present")
	excludesAny, ok := raw.([]any)
	require.True(t, ok, "protected_dot_folder_excludes should be a slice")
	excludes := make([]string, 0, len(excludesAny))
	for _, v := range excludesAny {
		if s, ok := v.(string); ok {
			excludes = append(excludes, s)
		}
	}
	assert.Contains(t, excludes, ".agents/", ".agents/ should be in protected_dot_folder_excludes")
	assert.Contains(t, excludes, ".cursor/", ".cursor/ should be in protected_dot_folder_excludes")
	assert.NotContains(t, excludes, "AGENTS.md", "non-dot-folder files must not be in protected_dot_folder_excludes")
}

// TestNoProtectedDotFolderExcludesWhenNoneDotFolderExcluded verifies that
// protected_dot_folder_excludes is absent when the exclusion list has no dot-folder prefixes.
func TestNoProtectedDotFolderExcludesWhenNoneDotFolderExcluded(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig:  BaseSafeOutputConfig{Max: strPtr("1")},
				ProtectedFilesExclude: []string{"AGENTS.md", "CLAUDE.md"},
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
	require.NotEmpty(t, steps, "should produce config steps")

	var configJSON string
	for _, step := range steps {
		if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
			require.Len(t, parts, 2, "should split env var line")
			configJSON = strings.TrimSpace(parts[1])
			configJSON = strings.Trim(configJSON, "\"")
			configJSON = strings.ReplaceAll(configJSON, "\\\"", "\"")
		}
	}
	require.NotEmpty(t, configJSON, "should have extracted JSON")

	var config map[string]map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config), "config JSON should be valid")

	prConfig, ok := config["create_pull_request"]
	require.True(t, ok, "should have create_pull_request config")

	_, exists := prConfig["protected_dot_folder_excludes"]
	assert.False(t, exists, "protected_dot_folder_excludes should be absent when no dot-folders excluded")
}
