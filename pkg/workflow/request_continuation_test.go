//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRequestContinuationConfig(t *testing.T) {
	tests := []struct {
		name      string
		raw       any
		wantNil   bool
		wantMax   int
		wantError string
	}{
		{name: "null enables defaults", raw: nil, wantMax: 5},
		{name: "true enables defaults", raw: true, wantMax: 5},
		{name: "false disables", raw: false, wantNil: true},
		{name: "empty object", raw: map[string]any{}, wantMax: 5},
		{name: "custom max", raw: map[string]any{"max": 3}, wantMax: 3},
		{name: "authorization accepted", raw: map[string]any{"authorization": authorizationTestAuthority("https://authority.example/a")}, wantMax: 5},
		{name: "max too large", raw: map[string]any{"max": 101}, wantError: "from 1 to 100"},
		{name: "max zero", raw: map[string]any{"max": 0}, wantError: "from 1 to 100"},
		{name: "unknown field", raw: map[string]any{"retention-days": 3}, wantError: "supports only max"},
		{name: "string", raw: "yes", wantError: "must be an object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := NewCompiler().parseRequestContinuationConfig(map[string]any{requestContinuationKey: tt.raw})
			if tt.wantNil {
				assert.Nil(t, config)
				return
			}
			require.NotNil(t, config)
			if tt.wantError != "" {
				require.Error(t, config.parseError)
				assert.Contains(t, config.parseError.Error(), tt.wantError)
				return
			}
			require.NoError(t, config.parseError)
			assert.Equal(t, tt.wantMax, config.EffectiveMax())
		})
	}
}

func TestApplyRequestContinuationDurable(t *testing.T) {
	t.Run("request-continuation enables durable checkpoints", func(t *testing.T) {
		data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{RequestContinuation: &RequestContinuationConfig{}}}
		applyRequestContinuationDurable(data)
		require.NotNil(t, data.EngineConfig)
		require.NotNil(t, data.EngineConfig.Durable)
		assert.Equal(t, defaultDurableRetentionDays, data.EngineConfig.Durable.RetentionDays)
		assert.False(t, data.SafeOutputs.RequestContinuation.LegacyDurable)
	})

	t.Run("legacy engine.durable implies request-continuation", func(t *testing.T) {
		data := &WorkflowData{EngineConfig: &EngineConfig{ID: "claude", Durable: &DurableConfig{RetentionDays: 3}}}
		applyRequestContinuationDurable(data)
		require.True(t, hasRequestContinuation(data))
		assert.True(t, data.SafeOutputs.RequestContinuation.LegacyDurable)
		assert.Equal(t, 3, data.EngineConfig.Durable.RetentionDays, "legacy retention is preserved")
	})

	t.Run("no configuration leaves durable disabled", func(t *testing.T) {
		data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{}}
		applyRequestContinuationDurable(data)
		assert.Nil(t, data.EngineConfig)
	})
}

func compileRequestContinuationWorkflow(t *testing.T, frontmatter string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "continue.md")
	require.NoError(t, os.WriteFile(source, []byte("---\n"+frontmatter+"---\nContinue the task.\n"), 0o600))
	if err := NewCompiler(WithVersion("dev")).CompileWorkflow(source); err != nil {
		return "", err
	}
	lock, err := os.ReadFile(filepath.Join(dir, "continue.lock.yml"))
	require.NoError(t, err)
	return string(lock), nil
}

func TestCompileRequestContinuation(t *testing.T) {
	lock, err := compileRequestContinuationWorkflow(t, "on: workflow_dispatch\nengine: copilot\nsafe-outputs:\n  request-continuation:\n    max: 3\n")
	require.NoError(t, err)

	for _, expected := range []string{
		"Restore durable workflow state",
		"Upload durable checkpoint artifact",
		"group: \"gh-aw-durable-continue\"",
		`\"request_continuation\":{\"max\":1,\"max_continuations\":3}`,
		"actions: write",
	} {
		assert.Contains(t, lock, expected)
	}

	uploadGate := "contains(steps.collect_output.outputs.output_types, 'request_continuation')"
	require.Contains(t, lock, uploadGate, "checkpoint upload must require a continuation request")
	collect := strings.Index(lock, "id: collect_output")
	validate := strings.Index(lock, uploadGate)
	require.Positive(t, collect)
	assert.Less(t, collect, validate, "output collection must precede the gated checkpoint upload")
}

func TestCompileRequestContinuationWithOtherSafeOutputs(t *testing.T) {
	lock, err := compileRequestContinuationWorkflow(t, "on: workflow_dispatch\nengine: copilot\nsafe-outputs:\n  create-issue: {}\n  add-comment: {}\n  request-continuation: {}\n")
	require.NoError(t, err)

	for _, handler := range []string{`\"create_issue\":{`, `\"add_comment\":{`, `\"request_continuation\":{`} {
		assert.Contains(t, lock, handler, "every configured safe output must remain enabled alongside request-continuation")
	}
	assert.Contains(t, lock, "contains(steps.collect_output.outputs.output_types, 'request_continuation')",
		"the gate must match request_continuation within a mixed output type list")
}

func TestCompileRequestContinuationRequiresWorkflowDispatch(t *testing.T) {
	_, err := compileRequestContinuationWorkflow(t, "on: push\nengine: copilot\nsafe-outputs:\n  request-continuation: {}\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "add workflow_dispatch to the on: triggers")
}

func TestCompileRequestContinuationRejectsInvalidMax(t *testing.T) {
	_, err := compileRequestContinuationWorkflow(t, "on: workflow_dispatch\nengine: copilot\nsafe-outputs:\n  request-continuation:\n    max: 0\n")
	require.Error(t, err)
}
