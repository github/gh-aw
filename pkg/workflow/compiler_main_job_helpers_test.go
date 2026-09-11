//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/constants"
)

// disableAICGuardrailFrontmatter returns a frontmatter map that disables the daily-AIC guardrail
// (max-daily-ai-credits: -1). Use this in tests that want to inspect conditions without the
// guardrail expression being appended.
func disableAICGuardrailFrontmatter() map[string]any {
	return map[string]any{"max-daily-ai-credits": -1}
}

// TestBuildMainJobCondition tests the job condition helper across the key decision branches.
func TestBuildMainJobCondition(t *testing.T) {
	tests := []struct {
		name                 string
		dataIf               string
		activationJobCreated bool
		rawFrontmatter       map[string]any // nil = guardrail enabled (default)
		jobs                 map[string]any
		want                 string
	}{
		{
			name:                 "no activation, no if — empty condition",
			activationJobCreated: false,
			rawFrontmatter:       disableAICGuardrailFrontmatter(),
			want:                 "",
		},
		{
			name:                 "activation, no if, guardrail disabled — cleared to empty",
			activationJobCreated: true,
			rawFrontmatter:       disableAICGuardrailFrontmatter(),
			want:                 "",
		},
		{
			name:                 "activation, no if, guardrail enabled — AIC guard appended",
			activationJobCreated: true,
			want:                 "needs.activation.outputs.daily_ai_credits_exceeded != 'true'",
		},
		{
			name:                 "no activation, with if — condition is preserved",
			dataIf:               "github.event_name == 'push'",
			activationJobCreated: false,
			rawFrontmatter:       disableAICGuardrailFrontmatter(),
			want:                 "github.event_name == 'push'",
		},
		{
			name:                 "activation, with if (no custom job refs), guardrail disabled — condition cleared",
			dataIf:               "github.event_name == 'push'",
			activationJobCreated: true,
			rawFrontmatter:       disableAICGuardrailFrontmatter(),
			want:                 "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			data := &WorkflowData{
				If:             tt.dataIf,
				Jobs:           tt.jobs,
				RawFrontmatter: tt.rawFrontmatter,
			}
			got := c.buildMainJobCondition(data, tt.activationJobCreated)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestBuildMainJobDependencies tests the dependency list built for the main agent job.
func TestBuildMainJobDependencies(t *testing.T) {
	t.Run("with activation includes activation in depends", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{}
		depends, _ := c.buildMainJobDependencies(data, true)
		assert.Contains(t, depends, string(constants.ActivationJobName))
	})

	t.Run("without activation does not include activation in depends", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{}
		depends, _ := c.buildMainJobDependencies(data, false)
		assert.NotContains(t, depends, string(constants.ActivationJobName))
	})

	t.Run("custom job with no special needs is a direct dependency", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{
			Jobs: map[string]any{
				"prepare": map[string]any{
					"runs-on": "ubuntu-latest",
				},
			},
		}
		depends, _ := c.buildMainJobDependencies(data, false)
		assert.Contains(t, depends, "prepare")
	})

	t.Run("custom job depending on pre_activation is excluded from direct deps", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{
			Jobs: map[string]any{
				"pre_act_job": map[string]any{
					"runs-on": "ubuntu-latest",
					"needs":   "pre_activation",
				},
			},
		}
		depends, _ := c.buildMainJobDependencies(data, false)
		assert.NotContains(t, depends, "pre_act_job")
	})

	t.Run("builtin job names are never added to depends", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{
			Jobs: map[string]any{
				string(constants.ActivationJobName): map[string]any{
					"pre-steps": []any{},
				},
			},
		}
		depends, _ := c.buildMainJobDependencies(data, true)
		// activation should appear exactly once (from the activationJobCreated flag, not from the jobs map)
		count := 0
		for _, d := range depends {
			if d == string(constants.ActivationJobName) {
				count++
			}
		}
		assert.Equal(t, 1, count, "activation should appear exactly once in depends")
	})

	t.Run("engine env content is returned for builtin-reference warning use", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID:  "copilot",
				Env: map[string]string{"FOO": "${{ needs.activation.outputs.model }}"},
			},
		}
		_, engineEnvContent := c.buildMainJobDependencies(data, false)
		assert.Contains(t, engineEnvContent, "needs.activation.outputs.model")
	})
}

// TestBuildMainJobOutputs tests that the output map contains expected keys under various config.
func TestBuildMainJobOutputs(t *testing.T) {
	t.Run("core outputs are always present", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{AI: "copilot"}
		outputs := c.buildMainJobOutputs(data)
		require.NotNil(t, outputs)
		assert.Contains(t, outputs, "model")
		assert.Contains(t, outputs, "effective_tokens")
		assert.Contains(t, outputs, "aic")
		assert.Contains(t, outputs, "setup-trace-id")
		assert.Contains(t, outputs, "invocation_cap_exceeded")
	})

	t.Run("firewall-disabled non-Copilot workflows use MCP gateway outputs", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{AI: "claude", SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{Disabled: true}}}
		outputs := c.buildMainJobOutputs(data)
		assert.Equal(t, "${{ steps.parse-mcp-gateway.outputs.aic }}", outputs["aic"])
		assert.Equal(t, "${{ steps.parse-mcp-gateway.outputs.ambient_context }}", outputs["ambient_context"])
	})

	t.Run("safe-outputs fields added when SafeOutputs set", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{
			AI:          "copilot",
			SafeOutputs: &SafeOutputsConfig{},
		}
		outputs := c.buildMainJobOutputs(data)
		assert.Contains(t, outputs, "output")
		assert.Contains(t, outputs, "output_types")
		assert.Contains(t, outputs, "has_patch")
	})

	t.Run("safe-outputs fields absent when SafeOutputs is nil", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{AI: "copilot"}
		outputs := c.buildMainJobOutputs(data)
		assert.NotContains(t, outputs, "output")
	})

	t.Run("cache memory restore outputs added per cache entry", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{
			AI: "copilot",
			CacheMemoryConfig: &CacheMemoryConfig{
				Caches: []CacheMemoryEntry{{Key: "k1"}, {Key: "k2"}},
			},
		}
		outputs := c.buildMainJobOutputs(data)
		assert.Contains(t, outputs, "cache_memory_restore_0_matched_key")
		assert.Contains(t, outputs, "cache_memory_restore_1_matched_key")
		assert.Contains(t, outputs, "cache_memory_restore_0_cache_hit")
	})
}

// TestBuildMainJobEnv tests job-level env var construction.
func TestBuildMainJobEnv(t *testing.T) {
	t.Run("nil env when no safe outputs, workflow ID, or UTC offset", func(t *testing.T) {
		c := NewCompiler()
		// Stub repo config to have no UTC offset so the test is hermetic.
		c.repoConfigLoaded = true
		c.repoConfig = &RepoConfig{}
		data := &WorkflowData{}
		env := c.buildMainJobEnv(data)
		assert.Nil(t, env)
	})

	t.Run("safe outputs triggers env initialisation with MCP_LOG_DIR", func(t *testing.T) {
		c := NewCompiler()
		c.repoConfigLoaded = true
		c.repoConfig = &RepoConfig{}
		data := &WorkflowData{
			SafeOutputs: &SafeOutputsConfig{},
		}
		env := c.buildMainJobEnv(data)
		require.NotNil(t, env)
		assert.Contains(t, env, "GH_AW_MCP_LOG_DIR")
		assert.Contains(t, env, "DEFAULT_BRANCH")
	})

	t.Run("upload-assets sets asset env vars", func(t *testing.T) {
		c := NewCompiler()
		c.repoConfigLoaded = true
		c.repoConfig = &RepoConfig{}
		data := &WorkflowData{
			SafeOutputs: &SafeOutputsConfig{
				UploadAssets: &UploadAssetsConfig{
					BranchName:  "assets",
					MaxSizeKB:   500,
					AllowedExts: []string{".png", ".jpg"},
				},
			},
		}
		env := c.buildMainJobEnv(data)
		require.NotNil(t, env)
		assert.Contains(t, env["GH_AW_ASSETS_BRANCH"], "assets")
		assert.Equal(t, "500", env["GH_AW_ASSETS_MAX_SIZE_KB"])
	})

	t.Run("workflow ID sets GH_AW_WORKFLOW_ID_SANITIZED", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{WorkflowID: "my-workflow"}
		env := c.buildMainJobEnv(data)
		require.NotNil(t, env)
		assert.NotEmpty(t, env["GH_AW_WORKFLOW_ID_SANITIZED"])
	})

	t.Run("engine version is available to all agent job steps", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{
			EngineConfig: &EngineConfig{Version: "${{ inputs.engine-version }}"},
		}
		env := c.buildMainJobEnv(data)
		require.NotNil(t, env)
		assert.Equal(t, `"${{ inputs.engine-version }}"`, env["GH_AW_ENGINE_VERSION"])
	})

	t.Run("UTC offset from repo config sets GH_AW_PROJECT_UTC", func(t *testing.T) {
		c := NewCompiler()
		c.repoConfigLoaded = true
		c.repoConfig = &RepoConfig{UTC: "+05:30"}
		data := &WorkflowData{}
		env := c.buildMainJobEnv(data)
		require.NotNil(t, env)
		assert.Contains(t, env["GH_AW_PROJECT_UTC"], "+05:30")
	})

	t.Run("playwright CLI mode sets PLAYWRIGHT_MCP_SANDBOX=false", func(t *testing.T) {
		c := NewCompiler()
		c.repoConfigLoaded = true
		c.repoConfig = &RepoConfig{}
		data := &WorkflowData{
			Tools: map[string]any{
				"playwright": map[string]any{
					"mode": "cli",
				},
			},
		}
		env := c.buildMainJobEnv(data)
		require.NotNil(t, env)
		assert.Equal(t, "false", env["PLAYWRIGHT_MCP_SANDBOX"])
	})

	t.Run("safe-outputs with playwright CLI preserves PLAYWRIGHT_MCP_SANDBOX", func(t *testing.T) {
		c := NewCompiler()
		c.repoConfigLoaded = true
		c.repoConfig = &RepoConfig{}
		data := &WorkflowData{
			Tools: map[string]any{
				"playwright": map[string]any{
					"mode": "cli",
				},
			},
			SafeOutputs: &SafeOutputsConfig{},
		}
		env := c.buildMainJobEnv(data)
		require.NotNil(t, env)
		assert.Equal(t, "false", env["PLAYWRIGHT_MCP_SANDBOX"])
		assert.Equal(t, "${{ github.event.repository.default_branch }}", env["DEFAULT_BRANCH"])
	})
}

// TestBuildMainJobPermissions tests that permissions are built correctly.
func TestBuildMainJobPermissions(t *testing.T) {
	t.Run("no scripts returns initial filtered permissions unchanged", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{
			Permissions: "permissions:\n  contents: read",
		}
		perms, err := c.buildMainJobPermissions(data)
		require.NoError(t, err)
		assert.Contains(t, perms, "contents")
	})

	t.Run("write gh command returns error", func(t *testing.T) {
		c := NewCompiler()
		// Use a pre-step containing a gh write command to trigger the guard.
		// extractRunScriptsFromSectionYAML expects the YAML wrapped in the section key.
		data := &WorkflowData{
			Permissions: "permissions:\n  contents: read",
			PreSteps:    "pre-steps:\n- name: bad step\n  run: gh issue create --title oops",
		}
		_, err := c.buildMainJobPermissions(data)
		require.Error(t, err)
		require.ErrorContains(t, err, "write operations are not permitted")
		require.ErrorContains(t, err, "gh issue create")
	})

	t.Run("explicit empty permissions block skips inference", func(t *testing.T) {
		c := NewCompiler()
		data := &WorkflowData{
			Permissions: "permissions: {}",
			PreSteps:    "pre-steps:\n- name: read step\n  run: gh issue list",
		}
		_, err := c.buildMainJobPermissions(data)
		require.NoError(t, err)
	})

	t.Run("operational value preserves declared permissions", func(t *testing.T) {
		c := NewCompiler()
		data := operationalValueGraderWorkflowData(".github/graders/example-operational-value.sh")
		data.Permissions = "permissions:\n  contents: read"
		perms, err := c.buildMainJobPermissions(data)
		require.NoError(t, err)
		assert.NotContains(t, perms, "actions: read")
		assert.Contains(t, perms, "contents: read")
	})

	t.Run("operational value allows explicit empty permissions", func(t *testing.T) {
		c := NewCompiler()
		data := operationalValueGraderWorkflowData(".github/graders/example-operational-value.sh")
		data.Permissions = "permissions: {}"
		_, err := c.buildMainJobPermissions(data)
		require.NoError(t, err)
	})

	t.Run("disabled operational value does not add actions read", func(t *testing.T) {
		c := NewCompiler()
		disabled := false
		data := operationalValueGraderWorkflowData(".github/graders/example-operational-value.sh")
		data.Permissions = "permissions:\n  contents: read"
		data.Graders.Graders["operational-value"].Enabled = &disabled
		perms, err := c.buildMainJobPermissions(data)
		require.NoError(t, err)
		assert.NotContains(t, perms, "actions: read")
	})
}

// TestWarnBuiltinJobEnvReferences tests warning emission for built-in job references in engine.env.
func TestWarnBuiltinJobEnvReferences(t *testing.T) {
	t.Run("warns when engine.env references a built-in job not in depends", func(t *testing.T) {
		c := NewCompiler()
		initial := c.GetWarningCount()
		c.warnBuiltinJobEnvReferences([]string{}, "${{ needs.safe_outputs.outputs.foo }}")
		assert.Equal(t, initial+1, c.GetWarningCount())
	})

	t.Run("no warning when built-in is already a direct dependency", func(t *testing.T) {
		c := NewCompiler()
		initial := c.GetWarningCount()
		c.warnBuiltinJobEnvReferences([]string{string(constants.ActivationJobName)}, "${{ needs.activation.outputs.model }}")
		assert.Equal(t, initial, c.GetWarningCount())
	})

	t.Run("no warning when engine env content is empty", func(t *testing.T) {
		c := NewCompiler()
		initial := c.GetWarningCount()
		c.warnBuiltinJobEnvReferences([]string{}, "")
		assert.Equal(t, initial, c.GetWarningCount())
	})
}
