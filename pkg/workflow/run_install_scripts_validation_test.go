//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRunInstallScripts(t *testing.T) {
	tests := []struct {
		name                    string
		runtimes                map[string]any
		mergedRunInstallScripts bool
		expectedRunScript       bool
	}{
		{
			name:                    "default: run_scripts is false",
			runtimes:                map[string]any{},
			mergedRunInstallScripts: false,
			expectedRunScript:       false,
		},
		{
			name: "per-runtime node run-install-scripts: true",
			runtimes: map[string]any{
				"node": map[string]any{
					"run-install-scripts": true,
				},
			},
			mergedRunInstallScripts: false,
			expectedRunScript:       true,
		},
		{
			name: "per-runtime python run-install-scripts: true (no effect for npm installs)",
			runtimes: map[string]any{
				"python": map[string]any{
					"run-install-scripts": true,
				},
			},
			mergedRunInstallScripts: false,
			expectedRunScript:       false,
		},
		{
			name:                    "merged run-install-scripts from imported shared workflow",
			runtimes:                map[string]any{},
			mergedRunInstallScripts: true,
			expectedRunScript:       true,
		},
		{
			name: "merged and node-level: both true",
			runtimes: map[string]any{
				"node": map[string]any{
					"run-install-scripts": true,
				},
			},
			mergedRunInstallScripts: true,
			expectedRunScript:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveRunInstallScripts(tt.runtimes, tt.mergedRunInstallScripts)
			assert.Equal(t, tt.expectedRunScript, result, "resolveRunInstallScripts() result mismatch")
		})
	}
}

func TestGenerateNpmInstallSteps_IgnoreScriptsByDefault(t *testing.T) {
	steps := GenerateNpmInstallSteps(
		"@anthropic-ai/claude-code",
		"latest",
		"Install Claude Code CLI",
		"claude",
		NPMInstallOptions{
			IncludeNodeSetup:  false,
			RunInstallScripts: false,
			CooldownEnabled:   true,
		},
	)

	require.Len(t, steps, 1, "Expected 1 install step")
	installStep := strings.Join(steps[0], "\n")

	assert.Contains(t, installStep, "--ignore-scripts", "Expected --ignore-scripts flag by default")
	assert.Contains(t, installStep, "npm install --ignore-scripts -g @anthropic-ai/claude-code@latest")
	assert.Contains(t, installStep, "NPM_CONFIG_MIN_RELEASE_AGE: '3'")
}

func TestGenerateNpmInstallSteps_RunInstallScriptsEnabled(t *testing.T) {
	steps := GenerateNpmInstallSteps(
		"@anthropic-ai/claude-code",
		"latest",
		"Install Claude Code CLI",
		"claude",
		NPMInstallOptions{
			IncludeNodeSetup:  false,
			RunInstallScripts: true,
			CooldownEnabled:   true,
		},
	)

	require.Len(t, steps, 1, "Expected 1 install step")
	installStep := strings.Join(steps[0], "\n")

	assert.NotContains(t, installStep, "--ignore-scripts", "Expected no --ignore-scripts flag when runInstallScripts=true")
	assert.Contains(t, installStep, "npm install -g @anthropic-ai/claude-code@latest")
	assert.Contains(t, installStep, "NPM_CONFIG_MIN_RELEASE_AGE: '3'")
}

func TestGenerateNpmInstallStepsWithScope_LocalInstall(t *testing.T) {
	steps := GenerateNpmInstallStepsWithScope(
		"@tobilu/qmd",
		"2.0.1",
		"Install qmd",
		"qmd",
		NPMInstallOptions{
			IncludeNodeSetup:  false,
			IsGlobal:          false,
			RunInstallScripts: false,
			CooldownEnabled:   false,
		},
	)

	require.Len(t, steps, 1, "Expected 1 install step")
	installStep := strings.Join(steps[0], "\n")

	assert.Contains(t, installStep, "--ignore-scripts", "Expected --ignore-scripts flag")
	assert.NotContains(t, installStep, " -g ", "Expected local install (no -g flag)")
	assert.NotContains(t, installStep, "NPM_CONFIG_MIN_RELEASE_AGE", "Cooldown env should be omitted when disabled")
}

func TestValidateRunInstallScripts_Warning(t *testing.T) {
	c := &Compiler{strictMode: false}
	c.ResetWarningCount()

	workflowData := &WorkflowData{RunInstallScripts: true}
	err := c.validateRunInstallScripts(workflowData)

	require.NoError(t, err, "Should not return error in non-strict mode")
	assert.Equal(t, 1, c.GetWarningCount(), "Should increment warning count")
}

func TestValidateRunInstallScripts_StrictModeError(t *testing.T) {
	c := &Compiler{strictMode: true}

	workflowData := &WorkflowData{RunInstallScripts: true}
	err := c.validateRunInstallScripts(workflowData)

	require.Error(t, err, "Should return error in strict mode")
	require.ErrorContains(t, err, "strict mode", "Error should mention strict mode")
	require.ErrorContains(t, err, "supply chain", "Error should mention supply chain risk")
}

func TestValidateRunInstallScripts_NotSet(t *testing.T) {
	c := &Compiler{strictMode: false}
	c.ResetWarningCount()

	workflowData := &WorkflowData{RunInstallScripts: false}
	err := c.validateRunInstallScripts(workflowData)

	require.NoError(t, err, "Should not return error when run-install-scripts is not set")
	assert.Equal(t, 0, c.GetWarningCount(), "Should not increment warning count")
}

func TestFrontmatterConfig_RunInstallScripts_TopLevel_Ignored(t *testing.T) {
	// Top-level run-install-scripts is no longer a supported field; it must be set
	// under runtimes.node instead. Unknown top-level keys are silently ignored by
	// the JSON unmarshaler, so parsing should succeed without populating any
	// run-install-scripts flag on the config itself.
	frontmatter := map[string]any{
		"run-install-scripts": true,
		"engine":              "claude",
	}

	config, err := ParseFrontmatterConfig(frontmatter)
	require.NoError(t, err, "Should parse frontmatter without error")
	require.NotNil(t, config, "Config should not be nil")
}

func TestRuntimeConfig_RunInstallScripts(t *testing.T) {
	runtimes := map[string]any{
		"node": map[string]any{
			"version":             "24",
			"run-install-scripts": true,
		},
	}

	config, err := parseRuntimesConfig(runtimes)
	require.NoError(t, err, "Should parse runtimes config without error")
	require.NotNil(t, config.Node, "Node config should be set")
	require.NotNil(t, config.Node.RunInstallScripts, "Node RunInstallScripts should be set")
	assert.True(t, *config.Node.RunInstallScripts, "Node RunInstallScripts should be true")
}
