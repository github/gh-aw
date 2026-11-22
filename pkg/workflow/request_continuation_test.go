//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestRequestContinuationCompiledLock(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "continuation.md")
	require.NoError(t, os.WriteFile(source, []byte(`---
on: push
engine: copilot
network: defaults
safe-outputs:
  request-continuation:
    max: 2
  report-incomplete:
    create-issue: false
  noop:
    report-as-issue: false
  report-missing-tool:
    create-issue: false
  report-missing-data:
    create-issue: false
  threat-detection: false
---
Continue the analysis if more work remains.
`), 0600))
	require.NoError(t, NewCompiler().CompileWorkflow(source))
	lock, err := os.ReadFile(filepath.Join(dir, "continuation.lock.yml"))
	require.NoError(t, err)
	text := string(lock)
	require.Contains(t, text, `"request_continuation"`)
	toolsStart := strings.Index(text, "<safe-output-tools>")
	require.NotEqual(t, -1, toolsStart)
	toolsEnd := strings.Index(text[toolsStart:], "</safe-output-tools>")
	require.GreaterOrEqual(t, toolsEnd, 0)
	require.Contains(t, text[toolsStart:toolsStart+toolsEnd], "request_continuation(max:2)")
	require.Contains(t, text, "record_request_continuation.cjs")
	require.Contains(t, text, "GH_AW_REQUEST_CONTINUATION_MAX")
	require.NotContains(t, text, "actions: write")
	content, err := os.ReadFile(source)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(source, []byte(strings.Replace(string(content), "  request-continuation:\n    max: 2\n", "", 1)), 0600))
	require.NoError(t, NewCompiler().CompileWorkflow(source))
	baseline, err := os.ReadFile(filepath.Join(dir, "continuation.lock.yml"))
	require.NoError(t, err)
	var enabled, disabled map[string]any
	require.NoError(t, yaml.Unmarshal(lock, &enabled))
	require.NoError(t, yaml.Unmarshal(baseline, &disabled))
	require.Equal(t, disabled["permissions"], enabled["permissions"])
	for job, value := range enabled["jobs"].(map[string]any) {
		require.Equal(t, disabled["jobs"].(map[string]any)[job].(map[string]any)["permissions"], value.(map[string]any)["permissions"], job)
	}
}

func TestRequestContinuationConfig(t *testing.T) {
	c := NewCompiler()
	for _, value := range []any{nil, true, map[string]any{}} {
		config := c.extractSafeOutputsConfig(map[string]any{"safe-outputs": map[string]any{"request-continuation": value}})
		require.NotNil(t, config.RequestContinuation)
		require.Equal(t, "1", *config.RequestContinuation.Max)
		require.Contains(t, computeEnabledToolNames(&WorkflowData{SafeOutputs: config}), "request_continuation")
		require.NoError(t, validateSafeOutputsMax(config))
	}
	for _, outputs := range []map[string]any{{}, {"request-continuation": false}} {
		config := c.extractSafeOutputsConfig(map[string]any{"safe-outputs": outputs})
		require.Nil(t, config.RequestContinuation)
		require.NotContains(t, computeEnabledToolNames(&WorkflowData{SafeOutputs: config}), "request_continuation")
	}
	for _, max := range []any{2, "${{ inputs.max }}"} {
		config := c.parseRequestContinuationConfig(map[string]any{"request-continuation": map[string]any{"max": max}})
		require.NoError(t, validateSafeOutputsMax(&SafeOutputsConfig{RequestContinuation: config}))
	}
	for _, max := range []int{0, -1, -2} {
		config := c.parseRequestContinuationConfig(map[string]any{"request-continuation": map[string]any{"max": max}})
		err := validateSafeOutputsMax(&SafeOutputsConfig{RequestContinuation: config})
		require.ErrorContains(t, err, "request-continuation")
		require.NotContains(t, err.Error(), "unlimited")
	}

	requestContinuation := &RequestContinuationConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Authorization: &SafeOutputAuthorization{}}}
	require.ErrorContains(t, validateRequestContinuationAuthorization(&SafeOutputsConfig{RequestContinuation: requestContinuation}), "request-continuation.authorization")
	require.ErrorContains(t, validateRequestContinuationAuthorization(&SafeOutputsConfig{RequestContinuation: requestContinuation, Authorization: &SafeOutputAuthorization{}}), "request-continuation.authorization")
}

func TestRequestContinuationMetadataOnly(t *testing.T) {
	c := NewCompiler()
	config := &SafeOutputsConfig{RequestContinuation: c.parseRequestContinuationConfig(map[string]any{"request-continuation": nil})}
	require.True(t, hasAnySafeOutputEnabled(config))
	require.False(t, hasNonBuiltinSafeOutputsEnabled(config))
	require.True(t, internalSafeOutputs["request_continuation"])
	require.Equal(t, map[string]any{"max": 1}, diagnosticHandlerRegistry["request_continuation"](config))
	descriptor, ok := getSafeOutputHandlerByKey("request-continuation")
	require.True(t, ok)
	require.True(t, descriptor.Builtin)
	require.Nil(t, descriptor.PermissionBuilder)
	require.False(t, handlerSupportsPerHandlerGitHubAppToken("request-continuation"))
	steps := strings.Join(c.buildConclusionRequestContinuationStep(&WorkflowData{SafeOutputs: config}, "agent"), "")
	require.Contains(t, steps, "record_request_continuation.cjs")
	require.Contains(t, steps, "GH_AW_REQUEST_CONTINUATION_MAX")
	for _, tool := range GetSafeOutputToolOptions() {
		require.NotEqual(t, "request_continuation", tool.Name)
	}
}

func TestRequestContinuationImports(t *testing.T) {
	c := NewCompiler()
	for _, raw := range []map[string]any{{}, {"request-continuation": false}, {"request-continuation": map[string]any{"max": 3}}} {
		config := c.extractSafeOutputsConfig(map[string]any{"safe-outputs": raw})
		merged, err := c.MergeSafeOutputs(config, []string{`{"request-continuation":{"max":2}}`}, raw)
		require.NoError(t, err)
		if raw["request-continuation"] == false {
			require.Nil(t, merged.RequestContinuation)
		} else {
			require.NotNil(t, merged.RequestContinuation)
			expected := "2"
			if _, exists := raw["request-continuation"]; exists {
				expected = "3"
			}
			require.Equal(t, expected, *merged.RequestContinuation.Max)
		}
	}
}
