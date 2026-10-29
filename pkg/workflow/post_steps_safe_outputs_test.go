//go:build !integration

package workflow

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsLiteralSafeOutputsToolStep(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		step map[string]any
		want bool
	}{
		{"literal", map[string]any{"run": `safeoutputs noop '{"message":"Done"}'`}, true},
		{"status condition", map[string]any{"if": "always()", "shell": "bash", "run": "safeoutputs noop '{}'\n"}, true},
		{"read", map[string]any{"run": `cat "$GH_AW_SAFE_OUTPUTS"`}, false},
		{"compound", map[string]any{"run": `safeoutputs noop '{}'; echo done`}, false},
		{"dynamic", map[string]any{"run": `safeoutputs noop "$PAYLOAD"`}, false},
		{"template", map[string]any{"run": `safeoutputs noop '{"message":"${{ github.actor }}"}'`}, false},
		{"not JSON", map[string]any{"run": `safeoutputs noop 'invalid'`}, false},
		{"null", map[string]any{"run": `safeoutputs noop 'null'`}, false},
		{"later output", map[string]any{"if": "steps.parse.outputs.ok", "run": `safeoutputs noop '{}'`}, false},
		{"env", map[string]any{"env": map[string]any{"KEY": "value"}, "run": `safeoutputs noop '{}'`}, false},
		{"powershell", map[string]any{"shell": "pwsh", "run": `safeoutputs noop '{}'`}, false},
		{"action", map[string]any{"uses": "actions/upload-artifact@v4"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsLiteralSafeOutputsToolStep(tc.step))
		})
	}
}

func TestSplitSafeOutputsPostSteps(t *testing.T) {
	t.Parallel()
	source := `post-steps:
- name: First
  run: safeoutputs noop '{}'
- name: Second
  run: safeoutputs add_comment '{"body":"Done","item_number":1}'
- name: Ordinary
  run: |
    if true; then
      echo done
    fi
- name: Later
  run: safeoutputs noop '{}'
`
	early, late := splitSafeOutputsPostSteps(source)
	assert.Contains(t, early, "name: First")
	assert.Contains(t, early, "name: Second")
	assert.NotContains(t, early, "name: Ordinary")
	assert.NotContains(t, early, "name: Later")
	assert.Contains(t, late, "name: Ordinary")
	assert.Contains(t, late, "name: Later")
	assert.NotContains(t, late, "name: First")
	var parsed struct {
		Steps []map[string]any `yaml:"post-steps"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(late), &parsed))
	require.Len(t, parsed.Steps, 2)
	assert.Equal(t, "if true; then\n  echo done\nfi\n", parsed.Steps[0]["run"])
	for _, source := range []string{"", "post-steps:\n- run: echo done\n", "invalid: ["} {
		early, late := splitSafeOutputsPostSteps(source)
		assert.Empty(t, early)
		assert.Equal(t, source, late)
	}
	early, late = splitSafeOutputsPostSteps("post-steps:\n- run: safeoutputs noop '{}'\n")
	assert.NotEmpty(t, early)
	assert.Empty(t, late)
}
