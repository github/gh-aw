//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSerializeEnvStringValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "plain string", value: "myvalue", want: "myvalue"},
		{name: "string slice", value: []string{"a", "b"}, want: `["a","b"]`},
		{name: "empty slice", value: []string{}, want: `[]`},
		{name: "nil", value: nil, want: `null`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, serializeEnvStringValue(tt.value))
		})
	}
}

func TestQuoteYAMLEnvValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "'hello'", quoteYAMLEnvValue("hello"))
	assert.Equal(t, "'it''s'", quoteYAMLEnvValue("it's"))
	assert.Equal(t, `'["a","b"]'`, quoteYAMLEnvValue(`["a","b"]`))
}

func TestGenerateGitHubMCPLockdownDetectionStepGeneratedWithExplicitGuardPolicy(t *testing.T) {
	t.Parallel()

	var yaml strings.Builder
	data := &WorkflowData{
		Tools: map[string]any{
			"github": map[string]any{
				"min-integrity": "approved",
				"allowed-repos": []string{"github/gh-aw", "github/*"},
			},
		},
	}

	if err := NewCompiler().generateGitHubMCPLockdownDetectionStep(&yaml, data); err != nil {
		t.Fatal(err)
	}
	output := yaml.String()

	// The detection step must still be generated even when guard policies are explicitly
	// configured because it outputs repository visibility used by sink-visibility in
	// safe-outputs and other MCP server guard policies.
	assert.Contains(t, output, "determine-automatic-lockdown", "detection step should be generated even when guard policy is explicitly configured")
	// The configured min-integrity and repos values must be passed as env vars to the step
	// so the script can respect them and avoid overriding explicit config.
	assert.Contains(t, output, "GH_AW_GITHUB_MIN_INTEGRITY", "env var should be present when min-integrity is explicitly set")
	assert.Contains(t, output, "GH_AW_GITHUB_REPOS", "env var should be present when allowed-repos is explicitly set")
}

func TestGenerateGitHubMCPLockdownDetectionStepGeneratedWhenNoGuardPolicy(t *testing.T) {
	t.Parallel()

	var yaml strings.Builder
	data := &WorkflowData{
		Tools: map[string]any{
			"github": map[string]any{
				"mode": "local",
			},
		},
	}

	if err := NewCompiler().generateGitHubMCPLockdownDetectionStep(&yaml, data); err != nil {
		t.Fatal(err)
	}
	output := yaml.String()

	assert.Contains(t, output, "determine-automatic-lockdown", "detection step should be generated when no explicit guard policy")
	assert.NotContains(t, output, "GH_AW_GITHUB_MIN_INTEGRITY", "env var should not be present when min-integrity is not set")
	assert.NotContains(t, output, "GH_AW_GITHUB_REPOS", "env var should not be present when repos is not set")
	assert.NotContains(t, output, "GH_AW_PRIVATE_TO_PUBLIC_FLOWS", "env var should not be present when private-to-public-flows is not set")
}

func TestGenerateGitHubMCPLockdownDetectionStepEmitsPrivateToPublicFlowsEnv(t *testing.T) {
	t.Parallel()

	var yaml strings.Builder
	data := &WorkflowData{
		Tools: map[string]any{
			"github": map[string]any{
				"private-to-public-flows": "allow",
			},
		},
	}

	if err := NewCompiler().generateGitHubMCPLockdownDetectionStep(&yaml, data); err != nil {
		t.Fatal(err)
	}
	output := yaml.String()

	assert.Contains(t, output, "determine-automatic-lockdown", "detection step should be generated")
	// When private-to-public-flows: allow is set, the step must receive the env var so the
	// determine_automatic_lockdown.cjs script can output repos=all instead of repos=public.
	assert.Contains(t, output, "GH_AW_PRIVATE_TO_PUBLIC_FLOWS: 'allow'", "env var should be emitted when private-to-public-flows is allow")
}
