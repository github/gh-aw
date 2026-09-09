//go:build !integration

package workflow

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========================================
// Target Repo and Patch Limit Tests
// ========================================

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

func TestHandlerConfigClosePullRequestTargetRepo(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			ClosePullRequests: &ClosePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: strPtr("1"),
				},
				SafeOutputTargetConfig: SafeOutputTargetConfig{
					Target:         "*",
					TargetRepoSlug: "${{ needs.input_parser.outputs.repo }}",
				},
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

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

				closePRConfig, ok := config["close_pull_request"]
				require.True(t, ok)

				targetRepo, ok := closePRConfig["target-repo"]
				require.True(t, ok)
				assert.Equal(t, "${{ needs.input_parser.outputs.repo }}", targetRepo)

				target, ok := closePRConfig["target"]
				require.True(t, ok)
				assert.Equal(t, "*", target)
			}
		}
	}
}

func TestMarshalSafeOutputsConfigPreservesExpressionOperators(t *testing.T) {
	configJSON, err := marshalSafeOutputsConfig(map[string]any{
		"create_issue": map[string]any{
			"target-repo": "${{ condition && value || fallback }}",
		},
	})

	require.NoError(t, err)
	assert.Contains(t, string(configJSON), "${{ condition && value || fallback }}")
	assert.NotContains(t, string(configJSON), `\u0026`)
}

func TestMarshalSafeOutputsConfigPreservesTemplatableExpressionOperators(t *testing.T) {
	builder := newHandlerConfigBuilder()
	builder.AddTemplatableJSONSlice("items", []string{"${{ condition && value || fallback }}"})

	configJSON, err := marshalSafeOutputsConfig(builder.config)

	require.NoError(t, err)
	assert.Contains(t, string(configJSON), "${{ toJSON(condition && value || fallback) }}")
	assert.NotContains(t, string(configJSON), `\u0026`)
}

func TestHandlerConfigCreateCheckRunTarget(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreateCheckRun: &CreateCheckRunConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: strPtr("1"),
				},
				Target: "*",
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	foundHandlerConfig := false
	for _, step := range steps {
		if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			foundHandlerConfig = true
			parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
			if len(parts) == 2 {
				jsonStr := strings.TrimSpace(parts[1])
				jsonStr = strings.Trim(jsonStr, "\"")
				jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")

				var config map[string]map[string]any
				err := json.Unmarshal([]byte(jsonStr), &config)
				require.NoError(t, err)

				checkRunConfig, ok := config["create_check_run"]
				require.True(t, ok)

				target, ok := checkRunConfig["target"]
				require.True(t, ok)
				assert.Equal(t, "*", target)
			}
		}
	}
	require.True(t, foundHandlerConfig, "Expected GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG in generated steps")
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
			expectedSize: 4096,
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

// TestHandlerConfigPatchFiles tests that the max-patch-files configuration is
// propagated into the create_pull_request handler config (regression for the
// hardcoded 100-file limit for long-running branches with multi-commit patches).
func TestHandlerConfigPatchFiles(t *testing.T) {
	tests := []struct {
		name              string
		maxPatchFiles     int
		expectedFileLimit int
	}{
		{
			name:              "default file limit",
			maxPatchFiles:     0,
			expectedFileLimit: 100,
		},
		{
			name:              "custom file limit",
			maxPatchFiles:     500,
			expectedFileLimit: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			workflowData := &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					MaximumPatchFiles: tt.maxPatchFiles,
					CreatePullRequests: &CreatePullRequestsConfig{
						TitlePrefix: "[PR] ",
					},
				},
			}

			var steps []string
			compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

			found := false
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
						require.True(t, ok, "create_pull_request handler config should exist")

						maxFiles, ok := prConfig["max_patch_files"]
						require.True(t, ok, "max_patch_files should be present in handler config")
						assert.InDelta(t, float64(tt.expectedFileLimit), maxFiles, 0.0001, "max_patch_files should match expected value")
						found = true
					}
				}
			}
			assert.True(t, found, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG step should be present")
		})
	}
}

// TestParseSafeOutputsMaxPatchFiles tests that the top-level safe-outputs
// `max-patch-files` config option is parsed into MaximumPatchFiles.
func TestParseSafeOutputsMaxPatchFiles(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected int
	}{
		{name: "int value", value: 250, expected: 250},
		{name: "uint64 value", value: uint64(300), expected: 300},
		{name: "float value", value: 150.0, expected: 150},
		{name: "zero falls back to default", value: 0, expected: 100},
		{name: "negative falls back to default", value: -5, expected: 100},
		// Overflow / out-of-range guards: values that would wrap or produce
		// undefined results when narrowed to int must be clamped or rejected,
		// not silently treated as 0 (which would fall back to the default).
		{name: "uint64 max clamps to MaxInt", value: uint64(math.MaxUint64), expected: math.MaxInt},
		{name: "huge float ignored (out of int range)", value: 1e30, expected: 100},
		{name: "negative huge float ignored", value: -1e30, expected: 100},
		{name: "NaN ignored", value: math.NaN(), expected: 100},
		{name: "+Inf ignored", value: math.Inf(1), expected: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			frontmatter := map[string]any{
				"safe-outputs": map[string]any{
					"max-patch-files":     tt.value,
					"create-pull-request": map[string]any{},
				},
			}
			cfg := compiler.extractSafeOutputsConfig(frontmatter)
			require.NotNil(t, cfg, "safe outputs config should be parsed")
			assert.Equal(t, tt.expected, cfg.MaximumPatchFiles, "MaximumPatchFiles should match expected value")
		})
	}
}

func TestHandlerConfigCreatePullRequestPatchLimitsOverrideGlobals(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			MaximumPatchSize:  4096,
			MaximumPatchFiles: 800,
			CreatePullRequests: &CreatePullRequestsConfig{
				MaxPatchSize:  2048,
				MaxPatchFiles: 300,
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	found := false
	for _, step := range steps {
		if !strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			continue
		}
		parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
		if len(parts) != 2 {
			continue
		}
		jsonStr := strings.TrimSpace(parts[1])
		jsonStr = strings.Trim(jsonStr, "\"")
		jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")

		var config map[string]map[string]any
		err := json.Unmarshal([]byte(jsonStr), &config)
		require.NoError(t, err)

		prConfig, ok := config["create_pull_request"]
		require.True(t, ok, "create_pull_request handler config should exist")

		maxSize, ok := prConfig["max_patch_size"]
		require.True(t, ok, "max_patch_size should be present in handler config")
		assert.InDelta(t, float64(2048), maxSize, 0.0001, "create-pull-request max_patch_size should use per-handler override")

		maxFiles, ok := prConfig["max_patch_files"]
		require.True(t, ok, "max_patch_files should be present in handler config")
		assert.InDelta(t, float64(300), maxFiles, 0.0001, "create-pull-request max_patch_files should use per-handler override")
		found = true
	}

	assert.True(t, found, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG step should be present")
}

func TestHandlerConfigPushToPullRequestBranchPatchSizeOverridesGlobal(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			MaximumPatchSize: 4096,
			PushToPullRequestBranch: &PushToPullRequestBranchConfig{
				MaxPatchSize: 2048,
			},
		},
	}

	var steps []string
	compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

	found := false
	for _, step := range steps {
		if !strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
			continue
		}
		parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
		if len(parts) != 2 {
			continue
		}
		jsonStr := strings.TrimSpace(parts[1])
		jsonStr = strings.Trim(jsonStr, "\"")
		jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")

		var config map[string]map[string]any
		err := json.Unmarshal([]byte(jsonStr), &config)
		require.NoError(t, err)

		pushConfig, ok := config["push_to_pull_request_branch"]
		require.True(t, ok, "push_to_pull_request_branch handler config should exist")

		maxSize, ok := pushConfig["max_patch_size"]
		require.True(t, ok, "max_patch_size should be present in handler config")
		assert.InDelta(t, float64(2048), maxSize, 0.0001, "push-to-pull-request-branch max_patch_size should use per-handler override")
		found = true
	}

	assert.True(t, found, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG step should be present")
}
