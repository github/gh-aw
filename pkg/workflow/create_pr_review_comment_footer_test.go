//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPRReviewCommentFooterConfig(t *testing.T) {
	t.Run("parses footer string values", func(t *testing.T) {
		testCases := []struct {
			name     string
			value    string
			expected string
		}{
			{name: "always", value: "always", expected: "always"},
			{name: "none", value: "none", expected: "none"},
			{name: "if-body", value: "if-body", expected: "if-body"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				compiler := NewCompiler()
				outputMap := map[string]any{
					"create-pull-request-review-comment": map[string]any{
						"footer": tc.value,
					},
				}

				config := compiler.parsePullRequestReviewCommentsConfig(outputMap)
				require.NotNil(t, config, "Config should be parsed")
				require.NotNil(t, config.Footer, "Footer should be set")
				assert.Equal(t, tc.expected, *config.Footer, "Footer value should match")
			})
		}
	})

	t.Run("parses footer boolean values", func(t *testing.T) {
		testCases := []struct {
			name     string
			value    bool
			expected string
		}{
			{name: "true maps to always", value: true, expected: "always"},
			{name: "false maps to none", value: false, expected: "none"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				compiler := NewCompiler()
				outputMap := map[string]any{
					"create-pull-request-review-comment": map[string]any{
						"footer": tc.value,
					},
				}

				config := compiler.parsePullRequestReviewCommentsConfig(outputMap)
				require.NotNil(t, config, "Config should be parsed")
				require.NotNil(t, config.Footer, "Footer should be set")
				assert.Equal(t, tc.expected, *config.Footer, "Footer value should be mapped from boolean")
			})
		}
	})

	t.Run("ignores invalid footer values", func(t *testing.T) {
		compiler := NewCompiler()
		outputMap := map[string]any{
			"create-pull-request-review-comment": map[string]any{
				"footer": "invalid-value",
			},
		}

		config := compiler.parsePullRequestReviewCommentsConfig(outputMap)
		require.NotNil(t, config, "Config should be parsed")
		assert.Nil(t, config.Footer, "Invalid footer value should be ignored")
	})

	t.Run("footer not set when omitted", func(t *testing.T) {
		compiler := NewCompiler()
		outputMap := map[string]any{
			"create-pull-request-review-comment": map[string]any{
				"side": "RIGHT",
			},
		}

		config := compiler.parsePullRequestReviewCommentsConfig(outputMap)
		require.NotNil(t, config, "Config should be parsed")
		assert.Nil(t, config.Footer, "Footer should be nil when not configured")
	})
}

func TestPRReviewCommentFooterInHandlerConfig(t *testing.T) {
	t.Run("footer included in handler config", func(t *testing.T) {
		compiler := NewCompiler()
		footerValue := "if-body"
		workflowData := &WorkflowData{
			Name: "Test",
			SafeOutputs: &SafeOutputsConfig{
				CreatePullRequestReviewComments: &CreatePullRequestReviewCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 10},
					Side:                 "RIGHT",
					Footer:               &footerValue,
				},
			},
		}

		var steps []string
		compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
		require.NotEmpty(t, steps, "Steps should not be empty")

		stepsContent := strings.Join(steps, "")
		require.Contains(t, stepsContent, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG")

		for _, step := range steps {
			if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
				parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
				if len(parts) == 2 {
					jsonStr := strings.TrimSpace(parts[1])
					jsonStr = strings.Trim(jsonStr, "\"")
					jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")
					var handlerConfig map[string]any
					err := json.Unmarshal([]byte(jsonStr), &handlerConfig)
					require.NoError(t, err, "Should unmarshal handler config")

					reviewCommentConfig, ok := handlerConfig["create_pull_request_review_comment"].(map[string]any)
					require.True(t, ok, "create_pull_request_review_comment config should exist")
					assert.Equal(t, "if-body", reviewCommentConfig["footer"], "Footer should be in handler config")
				}
			}
		}
	})

	t.Run("footer not in handler config when not set", func(t *testing.T) {
		compiler := NewCompiler()
		workflowData := &WorkflowData{
			Name: "Test",
			SafeOutputs: &SafeOutputsConfig{
				CreatePullRequestReviewComments: &CreatePullRequestReviewCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 10},
					Side:                 "RIGHT",
				},
			},
		}

		var steps []string
		compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)
		require.NotEmpty(t, steps, "Steps should not be empty")

		stepsContent := strings.Join(steps, "")
		require.Contains(t, stepsContent, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG")

		for _, step := range steps {
			if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
				parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
				if len(parts) == 2 {
					jsonStr := strings.TrimSpace(parts[1])
					jsonStr = strings.Trim(jsonStr, "\"")
					jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")
					var handlerConfig map[string]any
					err := json.Unmarshal([]byte(jsonStr), &handlerConfig)
					require.NoError(t, err, "Should unmarshal handler config")

					reviewCommentConfig, ok := handlerConfig["create_pull_request_review_comment"].(map[string]any)
					require.True(t, ok, "create_pull_request_review_comment config should exist")
					_, hasFooter := reviewCommentConfig["footer"]
					assert.False(t, hasFooter, "Footer should not be in handler config when not set")
				}
			}
		}
	})
}
