//go:build !integration

package workflow

import (
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectMaxAICreditsExpressionWithoutMaxRunsLeavesJSONUnchanged(t *testing.T) {
	configJSON := `{"apiProxy":{"enabled":true}}`

	got := injectMaxAICreditsExpression(configJSON, "${GH_AW_MAX_AI_CREDITS}")

	if got != configJSON {
		t.Fatalf("expected config JSON to be unchanged, got %q", got)
	}
}

func TestApplyDefaultMaxAICreditsEnvToMapHandlesNilMap(t *testing.T) {
	assert.NotPanics(t, func() {
		applyDefaultMaxAICreditsEnvToMap(nil, nil)
	})
}

func TestComputeAWFExcludeEnvVarNamesUVHostPaths(t *testing.T) {
	tests := []struct {
		name    string
		data    WorkflowData
		want    []string
		notWant []string
	}{
		{
			name: "generated setup from uv command",
			data: WorkflowData{CustomSteps: "steps:\n  - run: uv sync\n"},
			want: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR"},
		},
		{
			name: "generated setup survives custom step mutation",
			data: WorkflowData{
				CustomSteps: "steps:\n  - run: echo done\n",
				CachedRuntimeRequirements: []RuntimeRequirement{
					{Runtime: findRuntimeByID("uv")},
				},
				CachedRuntimeRequirementsSet: true,
			},
			want: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR"},
		},
		{
			name: "custom setup without uv command and empty detection cache",
			data: WorkflowData{
				CustomSteps:                  "steps:\n  - uses: 'astral-sh/setup-uv@v7' # pinned later\n    with:\n      version: '0.8.0'\n",
				CachedRuntimeRequirementsSet: true,
			},
			want: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR"},
		},
		{
			name: "custom setup repository is case insensitive",
			data: WorkflowData{CustomSteps: "steps:\n  - uses: Astral-Sh/Setup-UV@v5\n"},
			want: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR"},
		},
		{
			name: "custom pre-step setup",
			data: WorkflowData{PreSteps: "pre-steps:\n  - uses: astral-sh/setup-uv@v5\n"},
			want: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR"},
		},
		{
			name: "custom pre-agent setup",
			data: WorkflowData{PreAgentSteps: "pre-agent-steps:\n  - uses: astral-sh/setup-uv@v5\n"},
			want: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR"},
		},
		{
			name:    "post-step setup does not affect agent",
			data:    WorkflowData{PostSteps: "post-steps:\n  - uses: astral-sh/setup-uv@v5\n"},
			notWant: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR"},
		},
		{
			name:    "no uv setup",
			data:    WorkflowData{CustomSteps: "steps:\n  - run: python script.py\n"},
			notWant: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR"},
		},
		{
			name: "impostor repositories and run text do not match",
			data: WorkflowData{CustomSteps: `steps:
  - uses: impostor/astral-sh/setup-uv@v7
  - uses: astral-sh/setup-uv-impostor@v7
  - uses: astral-sh/setup-uv/subaction@v7
  - run: echo astral-sh/setup-uv@v7
`},
			notWant: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR"},
		},
		{
			name: "frontmatter override including empty value",
			data: WorkflowData{
				CustomSteps: "steps:\n  - run: uv sync\n",
				Env:         "env:\n  UV_CACHE_DIR: ''\n",
			},
			want:    []string{"UV_PYTHON_INSTALL_DIR"},
			notWant: []string{"UV_CACHE_DIR"},
		},
		{
			name: "engine override",
			data: WorkflowData{
				CustomSteps:  "steps:\n  - run: uv sync\n",
				EngineConfig: &EngineConfig{Env: map[string]string{"UV_CACHE_DIR": "/cache"}},
			},
			want:    []string{"UV_PYTHON_INSTALL_DIR"},
			notWant: []string{"UV_CACHE_DIR"},
		},
		{
			name: "sandbox agent override",
			data: WorkflowData{
				CustomSteps: "steps:\n  - run: uv sync\n",
				SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{
					Env: map[string]string{"UV_PYTHON_INSTALL_DIR": "/python"},
				}},
			},
			want:    []string{"UV_CACHE_DIR"},
			notWant: []string{"UV_PYTHON_INSTALL_DIR"},
		},
		{
			name: "explicit exclusion wins over override",
			data: WorkflowData{
				CustomSteps: "steps:\n  - run: uv sync\n",
				Env:         "env:\n  UV_CACHE_DIR: /cache\n",
				ExcludedEnv: []string{"UV_CACHE_DIR"},
			},
			want: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR"},
		},
		{
			name: "frontmatter secret override remains excluded",
			data: WorkflowData{
				CustomSteps: "steps:\n  - run: uv sync\n",
				Env:         "env:\n  UV_CACHE_DIR: ${{ secrets.CACHE }}\n",
			},
			want: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR"},
		},
		{
			name: "engine secret exclusion survives frontmatter override",
			data: WorkflowData{
				CustomSteps:  "steps:\n  - run: uv sync\n",
				Env:          "env:\n  UV_CACHE_DIR: /cache\n",
				EngineConfig: &EngineConfig{Env: map[string]string{"UV_CACHE_DIR": "${{ secrets.CACHE }}"}},
			},
			want: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR"},
		},
		{
			name: "sandbox job-output override remains excluded",
			data: WorkflowData{
				CustomSteps: "steps:\n  - run: uv sync\n",
				SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{
					Env: map[string]string{"UV_PYTHON_INSTALL_DIR": "${{ needs.setup.outputs.path }}"},
				}},
			},
			want: []string{"UV_CACHE_DIR", "UV_PYTHON_INSTALL_DIR"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeAWFExcludeEnvVarNames(&tt.data, nil)
			for _, name := range tt.want {
				assert.Contains(t, got, name)
				assert.Equal(t, 1, strings.Count(","+strings.Join(got, ",")+",", ","+name+","))
			}
			for _, name := range tt.notWant {
				assert.NotContains(t, got, name)
			}
		})
	}
}

func TestComputeAWFExcludeEnvVarNamesUVAfterSetupDeduplication(t *testing.T) {
	for _, customVersion := range []string{"", "    with:\n      version: '0.8.0'\n"} {
		t.Run(customVersion, func(t *testing.T) {
			data := &WorkflowData{
				CustomSteps: "steps:\n  - uses: astral-sh/setup-uv@v7\n" + customVersion + "  - run: uv sync\n",
			}
			requirements := detectRuntimeRequirementsCached(data)
			steps, _, err := DeduplicateRuntimeSetupStepsFromCustomSteps(data.CustomSteps, requirements)
			require.NoError(t, err)
			data.CustomSteps = steps
			got := ComputeAWFExcludeEnvVarNames(data, nil)
			assert.Contains(t, got, "UV_CACHE_DIR")
			assert.Contains(t, got, "UV_PYTHON_INSTALL_DIR")
		})
	}
}

func TestApplyDefaultMaxAICreditsEnvToMap(t *testing.T) {
	t.Run("sets default agent expression when max-ai-credits is unset", func(t *testing.T) {
		env := map[string]string{}
		applyDefaultMaxAICreditsEnvToMap(env, &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
		})
		assert.Equal(t, compilerenv.BuildDefaultMaxAICreditsExpression(strconv.FormatInt(constants.DefaultMaxAICredits, 10)), env[awfMaxAICreditsVarName])
	})

	t.Run("sets default detection expression for detection runs", func(t *testing.T) {
		env := map[string]string{}
		applyDefaultMaxAICreditsEnvToMap(env, &WorkflowData{
			IsDetectionRun: true,
			EngineConfig:   &EngineConfig{ID: "copilot"},
		})
		assert.Equal(t, compilerenv.BuildDefaultDetectionMaxAICreditsExpression(strconv.FormatInt(constants.DefaultDetectionMaxAICredits, 10)), env[awfMaxAICreditsVarName])
	})

	t.Run("sets default evals expression for evals runs", func(t *testing.T) {
		env := map[string]string{}
		applyDefaultMaxAICreditsEnvToMap(env, &WorkflowData{
			IsEvalsRun:   true,
			EngineConfig: &EngineConfig{ID: "copilot"},
		})
		assert.Equal(t, compilerenv.BuildDefaultEvalsMaxAICreditsExpression(strconv.FormatInt(constants.DefaultDetectionMaxAICredits, 10)), env[awfMaxAICreditsVarName])
	})

	t.Run("does not set expression when max-ai-credits is configured", func(t *testing.T) {
		env := map[string]string{}
		applyDefaultMaxAICreditsEnvToMap(env, &WorkflowData{
			EngineConfig: &EngineConfig{
				ID:           "copilot",
				MaxAICredits: 777,
			},
		})
		_, exists := env[awfMaxAICreditsVarName]
		assert.False(t, exists)
	})
}

// TestComputeAWFExcludeEnvVarNames verifies that engine.env vars whose values contain
// ${{ secrets.* }} are automatically included in the --exclude-env list, and that
// non-secret engine.env vars and plain-value core secrets are handled correctly.
func TestComputeAWFExcludeEnvVarNames(t *testing.T) {
	tests := []struct {
		name               string
		workflowData       *WorkflowData
		coreSecretVarNames []string
		want               []string
		notWant            []string
	}{
		{
			name: "engine.env secret var is auto-excluded",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"GOOGLE_API_KEY": "${{ secrets.SOME_KEY }}",
					},
				},
			},
			coreSecretVarNames: []string{},
			want:               []string{"GOOGLE_API_KEY"},
		},
		{
			name: "engine.env non-secret var is not excluded",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"DEBUG":     "true",
						"LOG_LEVEL": "info",
					},
				},
			},
			coreSecretVarNames: []string{},
			want:               []string{},
			notWant:            []string{"DEBUG", "LOG_LEVEL"},
		},
		{
			name: "engine.env mixes secret and non-secret vars: only secrets excluded",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"GOOGLE_API_KEY": "${{ secrets.SOME_KEY }}",
						"LOG_LEVEL":      "debug",
					},
				},
			},
			coreSecretVarNames: []string{},
			want:               []string{"GOOGLE_API_KEY"},
			notWant:            []string{"LOG_LEVEL"},
		},
		{
			name: "engine.env secret combined with core secret vars",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"CUSTOM_API_KEY": "${{ secrets.CUSTOM_KEY }}",
					},
				},
			},
			coreSecretVarNames: []string{"GEMINI_API_KEY"},
			want:               []string{"GEMINI_API_KEY", "CUSTOM_API_KEY"},
		},
		{
			name: "engine.env secret embedded in a larger string is excluded",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"AUTH_HEADER": "Bearer ${{ secrets.TOKEN }}",
					},
				},
			},
			coreSecretVarNames: []string{},
			want:               []string{"AUTH_HEADER"},
		},
		{
			name: "nil engine config produces no exclusions beyond core secrets",
			workflowData: &WorkflowData{
				EngineConfig: nil,
			},
			coreSecretVarNames: []string{"COPILOT_GITHUB_TOKEN"},
			want:               []string{"COPILOT_GITHUB_TOKEN"},
		},
		// --- job-output expression tests ---
		{
			name: "mcp-scripts env var with job-output value is excluded",
			workflowData: &WorkflowData{
				MCPScripts: &MCPScriptsConfig{
					Tools: map[string]*MCPScriptToolConfig{
						"example": {
							Env: map[string]string{
								"GH_TOKEN": "${{ needs.fetch_token.outputs.token }}",
							},
						},
					},
				},
			},
			coreSecretVarNames: []string{},
			want:               []string{"GH_TOKEN"},
		},
		{
			name: "mcp-scripts env var with static value is not excluded",
			workflowData: &WorkflowData{
				MCPScripts: &MCPScriptsConfig{
					Tools: map[string]*MCPScriptToolConfig{
						"example": {
							Env: map[string]string{
								"GH_DEBUG": "1",
							},
						},
					},
				},
			},
			coreSecretVarNames: []string{},
			want:               []string{},
			notWant:            []string{"GH_DEBUG"},
		},
		{
			name: "engine.env var with job-output value is excluded",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"GITHUB_TOKEN": "${{ needs.token_job.outputs.github_token }}",
					},
				},
			},
			coreSecretVarNames: []string{},
			want:               []string{"GITHUB_TOKEN"},
		},
		{
			name: "engine.env non-credential job-output var is excluded (consistent with secret behavior)",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"REPO_URL": "${{ needs.setup.outputs.repo_url }}",
					},
				},
			},
			coreSecretVarNames: []string{},
			want:               []string{"REPO_URL"},
		},
		{
			name: "mcp-scripts env var with job-output value mixed with secret: both excluded",
			workflowData: &WorkflowData{
				MCPScripts: &MCPScriptsConfig{
					Tools: map[string]*MCPScriptToolConfig{
						"tool1": {
							Env: map[string]string{
								"GH_TOKEN":    "${{ needs.fetch_token.outputs.token }}",
								"API_KEY":     "${{ secrets.API_KEY }}",
								"STATIC_HOST": "https://api.example.com",
							},
						},
					},
				},
			},
			coreSecretVarNames: []string{},
			want:               []string{"GH_TOKEN", "API_KEY"},
			notWant:            []string{"STATIC_HOST"},
		},
		// --- excluded-env frontmatter field tests ---
		{
			name: "excluded-env frontmatter field adds names unconditionally",
			workflowData: &WorkflowData{
				ExcludedEnv: []string{"MY_CUSTOM_TOKEN", "ANOTHER_SECRET"},
			},
			coreSecretVarNames: []string{},
			want:               []string{"MY_CUSTOM_TOKEN", "ANOTHER_SECRET"},
		},
		{
			name: "excluded-env combined with core secrets: all excluded",
			workflowData: &WorkflowData{
				ExcludedEnv: []string{"CUSTOM_PAT"},
			},
			coreSecretVarNames: []string{"COPILOT_GITHUB_TOKEN"},
			want:               []string{"COPILOT_GITHUB_TOKEN", "CUSTOM_PAT"},
		},
		{
			name: "excluded-env deduplicates with auto-detected secrets",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"MY_TOKEN": "${{ secrets.MY_TOKEN }}",
					},
				},
				ExcludedEnv: []string{"MY_TOKEN"},
			},
			coreSecretVarNames: []string{},
			want:               []string{"MY_TOKEN"},
		},
		{
			name:               "always excludes actions oidc env vars from awf agent",
			workflowData:       &WorkflowData{},
			coreSecretVarNames: []string{},
			want: []string{
				"ACTIONS_ID_TOKEN_REQUEST_URL",
				"ACTIONS_ID_TOKEN_REQUEST_TOKEN",
			},
		},
		{
			name: "empty excluded-env has no effect",
			workflowData: &WorkflowData{
				ExcludedEnv: []string{},
			},
			coreSecretVarNames: []string{},
			want:               []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeAWFExcludeEnvVarNames(tt.workflowData, tt.coreSecretVarNames)
			for _, name := range tt.want {
				assert.Contains(t, got, name, "expected %q in exclude list", name)
			}
			for _, name := range tt.notWant {
				assert.NotContains(t, got, name, "expected %q to be absent from exclude list", name)
			}
		})
	}
}

// TestMainAgentRunUsesStandardCreditsExpressionNotDetectionExpression verifies that
// a standard (non-detection) main-agent run emits the main-agent credits expression
// (vars.GH_AW_DEFAULT_MAX_AI_CREDITS) and not the detection-specific one, so a future
// refactor that accidentally sets IsDetectionRun on main-agent data will be caught.
func TestMainAgentRunUsesStandardCreditsExpressionNotDetectionExpression(t *testing.T) {
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID: "claude",
			// MaxAICredits is zero (not set in frontmatter) to trigger runtime expression injection.
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
		// IsDetectionRun is false by default — this is a main-agent run.
	}

	engine := NewClaudeEngine()
	steps := engine.GetExecutionSteps(workflowData, "test.log")
	require.NotEmpty(t, steps, "should produce execution steps")

	stepContent := strings.Join(steps[0], "\n")

	assert.Contains(t, stepContent, "vars.GH_AW_DEFAULT_MAX_AI_CREDITS",
		"main-agent run should use standard credits expression")
	assert.NotContains(t, stepContent, "vars.GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS",
		"main-agent run must not use detection credits expression")
}
