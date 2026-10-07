//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestBuildGitHubScriptStep verifies the common helper function produces correct GitHub Script steps
func TestBuildGitHubScriptStep(t *testing.T) {
	compiler := &Compiler{}

	tests := []struct {
		name            string
		workflowData    *WorkflowData
		config          GitHubScriptStepConfig
		expectedInSteps []string
	}{
		{
			name: "basic script step with minimal config",
			workflowData: &WorkflowData{
				Name: "Test Workflow",
			},
			config: GitHubScriptStepConfig{
				StepName:    "Test Step",
				StepID:      "test_step",
				MainJobName: "main_job",
				Script:      "console.log('test');",
				CustomToken: "",
			},
			expectedInSteps: []string{
				"- name: Download agent output artifact",
				"- name: Setup agent output environment variable",
				"- name: Test Step",
				"id: test_step",
				"uses: actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3",
				"env:",
				"GH_AW_AGENT_OUTPUT: ${{ steps.setup-agent-output-env.outputs.GH_AW_AGENT_OUTPUT }}",
				"with:",
				"script: |",
				"console.log('test');",
			},
		},
		{
			name: "script step with custom env vars",
			workflowData: &WorkflowData{
				Name: "Test Workflow",
			},
			config: GitHubScriptStepConfig{
				StepName:    "Create Issue",
				StepID:      "create_issue",
				MainJobName: "agent",
				CustomEnvVars: []string{
					"          GH_AW_ISSUE_TITLE_PREFIX: \"[bot] \"\n",
					"          GH_AW_ISSUE_LABELS: \"automation,ai\"\n",
				},
				Script:      "const issue = true;",
				CustomToken: "",
			},
			expectedInSteps: []string{
				"- name: Download agent output artifact",
				"- name: Setup agent output environment variable",
				"- name: Create Issue",
				"id: create_issue",
				"uses: actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3",
				"GH_AW_AGENT_OUTPUT: ${{ steps.setup-agent-output-env.outputs.GH_AW_AGENT_OUTPUT }}",
				"GH_AW_ISSUE_TITLE_PREFIX: \"[bot] \"",
				"GH_AW_ISSUE_LABELS: \"automation,ai\"",
				"const issue = true;",
			},
		},
		{
			name: "script step with safe-outputs.env variables",
			workflowData: &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					Env: map[string]string{
						"CUSTOM_VAR_1": "value1",
						"CUSTOM_VAR_2": "value2",
					},
				},
			},
			config: GitHubScriptStepConfig{
				StepName:    "Process Output",
				StepID:      "process",
				MainJobName: "main",
				Script:      "const x = 1;",
				CustomToken: "",
			},
			expectedInSteps: []string{
				"- name: Download agent output artifact",
				"- name: Setup agent output environment variable",
				"- name: Process Output",
				"id: process",
				"GH_AW_AGENT_OUTPUT: ${{ steps.setup-agent-output-env.outputs.GH_AW_AGENT_OUTPUT }}",
				"CUSTOM_VAR_1: value1",
				"CUSTOM_VAR_2: value2",
			},
		},
		{
			name: "script step with custom token",
			workflowData: &WorkflowData{
				Name: "Test Workflow",
			},
			config: GitHubScriptStepConfig{
				StepName:    "Secure Action",
				StepID:      "secure",
				MainJobName: "main",
				Script:      "const secure = true;",
				CustomToken: "${{ secrets.CUSTOM_TOKEN }}",
			},
			expectedInSteps: []string{
				"- name: Secure Action",
				"id: secure",
				"with:",
				"github-token: ${{ secrets.CUSTOM_TOKEN }}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := compiler.buildGitHubScriptStep(tt.workflowData, tt.config)

			// Convert steps slice to a single string for easier searching
			stepsStr := strings.Join(steps, "")

			// Verify expected strings are present in the output
			for _, expected := range tt.expectedInSteps {
				if !strings.Contains(stepsStr, expected) {
					t.Errorf("Expected step to contain %q, but it was not found.\nGenerated steps:\n%s", expected, stepsStr)
				}
			}

			// Verify basic structure is present
			if !strings.Contains(stepsStr, "- name:") {
				t.Error("Expected step to have '- name:' field")
			}
			if !strings.Contains(stepsStr, "id:") {
				t.Error("Expected step to have 'id:' field")
			}
			if !strings.Contains(stepsStr, "uses: actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3") {
				t.Error("Expected step to use actions/github-script@3a2844b7e9c422d3c10d287c895573f7108da1b3")
			}
			if !strings.Contains(stepsStr, "env:") {
				t.Error("Expected step to have 'env:' section")
			}
			if !strings.Contains(stepsStr, "with:") {
				t.Error("Expected step to have 'with:' section")
			}
			if !strings.Contains(stepsStr, "script: |") {
				t.Error("Expected step to have 'script: |' section")
			}
		})
	}
}

// TestBuildGitHubScriptStepMaintainsOrder verifies that environment variables appear in expected order
func TestBuildGitHubScriptStepMaintainsOrder(t *testing.T) {
	compiler := &Compiler{}
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			Env: map[string]string{
				"SAFE_OUTPUT_VAR": "value",
			},
		},
	}

	config := GitHubScriptStepConfig{
		StepName:    "Test Step",
		StepID:      "test",
		MainJobName: "main",
		CustomEnvVars: []string{
			"          CUSTOM_VAR: custom_value\n",
		},
		Script:      "const test = 1;",
		CustomToken: "",
	}

	steps := compiler.buildGitHubScriptStep(workflowData, config)
	stepsStr := strings.Join(steps, "")

	// Verify GH_AW_AGENT_OUTPUT comes first (after env: line)
	agentOutputIdx := strings.Index(stepsStr, "GH_AW_AGENT_OUTPUT")
	customVarIdx := strings.Index(stepsStr, "CUSTOM_VAR")
	safeOutputVarIdx := strings.Index(stepsStr, "SAFE_OUTPUT_VAR")

	if agentOutputIdx == -1 {
		t.Error("GH_AW_AGENT_OUTPUT not found in output")
	}
	if customVarIdx == -1 {
		t.Error("CUSTOM_VAR not found in output")
	}
	if safeOutputVarIdx == -1 {
		t.Error("SAFE_OUTPUT_VAR not found in output")
	}

	// Verify order: GH_AW_AGENT_OUTPUT -> custom vars -> safe-outputs.env vars
	if agentOutputIdx > customVarIdx {
		t.Error("GH_AW_AGENT_OUTPUT should come before custom vars")
	}
	if customVarIdx > safeOutputVarIdx {
		t.Error("Custom vars should come before safe-outputs.env vars")
	}
}

// TestApplySafeOutputEnvToMap verifies the helper function for map[string]string env variables
func TestApplySafeOutputEnvToMap(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expected     map[string]string
	}{
		{
			name: "nil SafeOutputs",
			workflowData: &WorkflowData{
				SafeOutputs: nil,
			},
			expected: map[string]string{},
		},
		{
			name: "basic safe outputs",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{},
			},
			expected: map[string]string{
				"GH_AW_SAFE_OUTPUTS": "${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}",
			},
		},
		{
			name: "safe outputs with staged flag",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					Staged: templatableBoolPtr("true"),
				},
			},
			expected: map[string]string{
				"GH_AW_SAFE_OUTPUTS":        "${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}",
				"GH_AW_SAFE_OUTPUTS_STAGED": "true",
			},
		},
		{
			name: "trial mode",
			workflowData: &WorkflowData{
				TrialMode:        true,
				TrialLogicalRepo: "owner/repo",
				SafeOutputs:      &SafeOutputsConfig{},
			},
			expected: map[string]string{
				"GH_AW_SAFE_OUTPUTS":        "${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}",
				"GH_AW_SAFE_OUTPUTS_STAGED": "true",
				"GH_AW_TARGET_REPO_SLUG":    "owner/repo",
			},
		},
		{
			name: "upload assets config",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					UploadAssets: &UploadAssetsConfig{
						BranchName:  "gh-aw-assets",
						MaxSizeKB:   10240,
						AllowedExts: []string{".png", ".jpg", ".jpeg"},
					},
				},
			},
			expected: map[string]string{
				"GH_AW_SAFE_OUTPUTS":        "${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}",
				"GH_AW_ASSETS_BRANCH":       "\"gh-aw-assets\"",
				"GH_AW_ASSETS_MAX_SIZE_KB":  "10240",
				"GH_AW_ASSETS_ALLOWED_EXTS": "\".png,.jpg,.jpeg\"",
			},
		},
		{
			name: "safe outputs input env vars forwarded to agent step",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{},
				SafeOutputsInputEnvVars: map[string]string{
					"GH_AW_INPUT_OWNER": "${{ inputs.owner }}",
					"GH_AW_INPUT_REPO":  "${{ inputs.repo }}",
				},
			},
			expected: map[string]string{
				"GH_AW_SAFE_OUTPUTS": "${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}",
				"GH_AW_INPUT_OWNER":  "${{ inputs.owner }}",
				"GH_AW_INPUT_REPO":   "${{ inputs.repo }}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := make(map[string]string)
			applySafeOutputEnvToMap(env, tt.workflowData)

			if len(env) != len(tt.expected) {
				t.Errorf("Expected %d env vars, got %d", len(tt.expected), len(env))
			}

			for key, expectedValue := range tt.expected {
				if actualValue, exists := env[key]; !exists {
					t.Errorf("Expected env var %q not found", key)
				} else if actualValue != expectedValue {
					t.Errorf("Env var %q: expected %q, got %q", key, expectedValue, actualValue)
				}
			}
		})
	}
}

// TestApplyTraceContextEnvToMap verifies the helper function that injects TRACEPARENT
// into the engine execution step environment.
func TestApplyTraceContextEnvToMap(t *testing.T) {
	t.Run("adds TRACEPARENT to empty map", func(t *testing.T) {
		env := make(map[string]string)
		applyTraceContextEnvToMap(env)
		if len(env) != 1 {
			t.Errorf("Expected 1 env var, got %d", len(env))
		}
		val, ok := env["TRACEPARENT"]
		if !ok {
			t.Fatal("Expected TRACEPARENT to be set")
		}
		// Must be a GitHub Actions expression that produces the W3C traceparent format
		// when both OTEL IDs are present, and an empty string otherwise.
		if !strings.Contains(val, "GITHUB_AW_OTEL_TRACE_ID") {
			t.Errorf("TRACEPARENT expression should reference GITHUB_AW_OTEL_TRACE_ID, got: %q", val)
		}
		if !strings.Contains(val, "GITHUB_AW_OTEL_PARENT_SPAN_ID") {
			t.Errorf("TRACEPARENT expression should reference GITHUB_AW_OTEL_PARENT_SPAN_ID, got: %q", val)
		}
		if !strings.Contains(val, "00-") {
			t.Errorf("TRACEPARENT expression should include W3C version '00-', got: %q", val)
		}
		if !strings.Contains(val, "-01") {
			t.Errorf("TRACEPARENT expression should include trace-flags '-01', got: %q", val)
		}
		// Fallback to empty string when OTEL not configured
		if !strings.Contains(val, "|| ''") {
			t.Errorf("TRACEPARENT expression should fall back to empty string when OTEL not configured, got: %q", val)
		}
	})

	t.Run("sets TRACEPARENT unconditionally", func(t *testing.T) {
		// applyTraceContextEnvToMap always sets TRACEPARENT. User overrides are applied
		// afterwards by the engine via maps.Copy(env, workflowData.EngineConfig.Env),
		// so they naturally win without needing a conditional here.
		env := map[string]string{"TRACEPARENT": "00-custom-parent-01"}
		applyTraceContextEnvToMap(env)
		// The function overwrites any prior value with the GitHub Actions expression.
		if env["TRACEPARENT"] == "" {
			t.Error("TRACEPARENT should not be empty after applyTraceContextEnvToMap")
		}
		if env["TRACEPARENT"] == "00-custom-parent-01" {
			t.Error("applyTraceContextEnvToMap should replace the prior value with the GHA expression")
		}
	})

	t.Run("preserves existing keys in map", func(t *testing.T) {
		env := map[string]string{
			"ANTHROPIC_API_KEY": "${{ secrets.ANTHROPIC_API_KEY }}",
			"GH_AW_PHASE":       "agent",
		}
		applyTraceContextEnvToMap(env)
		if env["ANTHROPIC_API_KEY"] != "${{ secrets.ANTHROPIC_API_KEY }}" {
			t.Error("applyTraceContextEnvToMap should not modify existing ANTHROPIC_API_KEY")
		}
		if env["GH_AW_PHASE"] != "agent" {
			t.Error("applyTraceContextEnvToMap should not modify existing GH_AW_PHASE")
		}
		if _, ok := env["TRACEPARENT"]; !ok {
			t.Error("applyTraceContextEnvToMap should add TRACEPARENT")
		}
	})
}

// TestBuildWorkflowMetadataEnvVars verifies the helper function for workflow metadata env vars
func TestBuildWorkflowMetadataEnvVars(t *testing.T) {
	tests := []struct {
		name           string
		workflowName   string
		workflowSource string
		localSourceURL string
		expected       []string
	}{
		{
			name:         "workflow name only",
			workflowName: "Test Workflow",
			expected: []string{
				"          GH_AW_WORKFLOW_NAME: \"Test Workflow\"\n",
			},
		},
		{
			name:           "workflow name and source",
			workflowName:   "Issue Triage",
			workflowSource: "owner/repo/workflows/triage.md@main",
			expected: []string{
				"          GH_AW_WORKFLOW_NAME: \"Issue Triage\"\n",
				"          GH_AW_WORKFLOW_SOURCE: \"owner/repo/workflows/triage.md@main\"\n",
				"          GH_AW_WORKFLOW_SOURCE_URL: \"${{ github.server_url }}/owner/repo/blob/main/workflows/triage.md\"\n",
			},
		},
		{
			name:           "workflow name and source without ref",
			workflowName:   "CI Helper",
			workflowSource: "org/project/ci/helper.md",
			expected: []string{
				"          GH_AW_WORKFLOW_NAME: \"CI Helper\"\n",
				"          GH_AW_WORKFLOW_SOURCE: \"org/project/ci/helper.md\"\n",
				"          GH_AW_WORKFLOW_SOURCE_URL: \"${{ github.server_url }}/org/project/blob/main/ci/helper.md\"\n",
			},
		},
		{
			name:           "empty workflow name",
			workflowName:   "",
			workflowSource: "owner/repo/workflow.md",
			expected: []string{
				"          GH_AW_WORKFLOW_NAME: \"\"\n",
				"          GH_AW_WORKFLOW_SOURCE: \"owner/repo/workflow.md\"\n",
				"          GH_AW_WORKFLOW_SOURCE_URL: \"${{ github.server_url }}/owner/repo/blob/main/workflow.md\"\n",
			},
		},
		{
			name:           "source with invalid format does not produce URL",
			workflowName:   "Test",
			workflowSource: "invalid-source",
			expected: []string{
				"          GH_AW_WORKFLOW_NAME: \"Test\"\n",
				"          GH_AW_WORKFLOW_SOURCE: \"invalid-source\"\n",
			},
		},
		{
			name:           "local source URL used when no external source",
			workflowName:   "Linter Miner",
			localSourceURL: "${{ github.server_url }}/${{ github.repository }}/blob/${{ github.ref_name }}/.github/workflows/linter-miner.md",
			expected: []string{
				"          GH_AW_WORKFLOW_NAME: \"Linter Miner\"\n",
				"          GH_AW_WORKFLOW_SOURCE_URL: \"${{ github.server_url }}/${{ github.repository }}/blob/${{ github.ref_name }}/.github/workflows/linter-miner.md\"\n",
			},
		},
		{
			name:           "external source takes precedence over local source URL",
			workflowName:   "CI Helper",
			workflowSource: "owner/repo/workflows/ci.md@main",
			localSourceURL: "${{ github.server_url }}/${{ github.repository }}/blob/${{ github.ref_name }}/.github/workflows/ci.md",
			expected: []string{
				"          GH_AW_WORKFLOW_NAME: \"CI Helper\"\n",
				"          GH_AW_WORKFLOW_SOURCE: \"owner/repo/workflows/ci.md@main\"\n",
				"          GH_AW_WORKFLOW_SOURCE_URL: \"${{ github.server_url }}/owner/repo/blob/main/workflows/ci.md\"\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildWorkflowMetadataEnvVars(tt.workflowName, tt.workflowSource, tt.localSourceURL)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d env vars, got %d", len(tt.expected), len(result))
				t.Logf("Expected: %v", tt.expected)
				t.Logf("Got: %v", result)
				return
			}

			for i, expectedVar := range tt.expected {
				if i >= len(result) {
					t.Errorf("Missing expected env var %d: %q", i, expectedVar)
					continue
				}
				if result[i] != expectedVar {
					t.Errorf("Env var %d: expected %q, got %q", i, expectedVar, result[i])
				}
			}
		})
	}
}

// TestBuildSafeOutputJobEnvVars verifies the helper function for safe-output job env vars
func TestBuildSafeOutputJobEnvVars(t *testing.T) {
	tests := []struct {
		name                 string
		trialMode            bool
		trialLogicalRepoSlug string
		staged               bool
		targetRepoSlug       string
		expected             []string
	}{
		{
			name:     "no flags",
			expected: []string{},
		},
		{
			name:   "staged only",
			staged: true,
			expected: []string{
				"          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n",
			},
		},
		{
			name:      "trial mode only",
			trialMode: true,
			expected: []string{
				"          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n",
			},
		},
		{
			name:                 "trial mode with trial repo slug",
			trialMode:            true,
			trialLogicalRepoSlug: "owner/trial-repo",
			expected: []string{
				"          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n",
				"          GH_AW_TARGET_REPO_SLUG: \"owner/trial-repo\"\n",
			},
		},
		{
			name:           "target repo slug only",
			targetRepoSlug: "owner/target-repo",
			expected: []string{
				"          GH_AW_TARGET_REPO_SLUG: \"owner/target-repo\"\n",
			},
		},
		{
			name:                 "target repo slug overrides trial repo slug",
			trialMode:            true,
			trialLogicalRepoSlug: "owner/trial-repo",
			targetRepoSlug:       "owner/target-repo",
			expected: []string{
				"          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n",
				"          GH_AW_TARGET_REPO_SLUG: \"owner/target-repo\"\n",
			},
		},
		{
			name:                 "all flags",
			trialMode:            true,
			trialLogicalRepoSlug: "owner/trial-repo",
			staged:               true,
			targetRepoSlug:       "owner/target-repo",
			expected: []string{
				"          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n",
				"          GH_AW_TARGET_REPO_SLUG: \"owner/target-repo\"\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var staged *TemplatableBool
			if tt.staged {
				staged = templatableBoolPtr("true")
			}
			result := buildSafeOutputJobEnvVars(
				tt.trialMode,
				tt.trialLogicalRepoSlug,
				staged,
				tt.targetRepoSlug,
			)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d env vars, got %d", len(tt.expected), len(result))
			}

			for i, expectedVar := range tt.expected {
				if i >= len(result) {
					t.Errorf("Missing expected env var %d: %q", i, expectedVar)
					continue
				}
				if result[i] != expectedVar {
					t.Errorf("Env var %d: expected %q, got %q", i, expectedVar, result[i])
				}
			}
		})
	}
}

// TestBuildEngineMetadataEnvVars verifies the helper function for engine metadata env vars
func TestBuildEngineMetadataEnvVars(t *testing.T) {
	tests := []struct {
		name         string
		engineConfig *EngineConfig
		model        string
		expected     []string
	}{
		{
			name:         "nil engine config",
			engineConfig: nil,
			expected:     []string{},
		},
		{
			name: "engine ID only",
			engineConfig: &EngineConfig{
				ID: "copilot",
			},
			expected: []string{
				"          GH_AW_ENGINE_ID: \"copilot\"\n",
				"          GH_AW_ENGINE_MODEL: ${{ needs.agent.outputs.model }}\n",
			},
		},
		{
			name: "full engine config",
			engineConfig: &EngineConfig{
				ID:      "copilot",
				Version: "1.0.0",
			},
			model: "gpt-5",
			expected: []string{
				"          GH_AW_ENGINE_ID: \"copilot\"\n",
				"          GH_AW_ENGINE_VERSION: \"1.0.0\"\n",
				"          GH_AW_ENGINE_MODEL: \"gpt-5\"\n",
			},
		},
		{
			name: "engine with version and no model",
			engineConfig: &EngineConfig{
				ID:      "claude",
				Version: "2.0.0",
			},
			expected: []string{
				"          GH_AW_ENGINE_ID: \"claude\"\n",
				"          GH_AW_ENGINE_VERSION: \"2.0.0\"\n",
				"          GH_AW_ENGINE_MODEL: ${{ needs.agent.outputs.model }}\n",
			},
		},
		{
			name: "engine with model and no version",
			engineConfig: &EngineConfig{
				ID: "copilot",
			},
			model: "claude-sonnet-4",
			expected: []string{
				"          GH_AW_ENGINE_ID: \"copilot\"\n",
				"          GH_AW_ENGINE_MODEL: \"claude-sonnet-4\"\n",
			},
		},
		{
			name:         "empty engine config",
			engineConfig: &EngineConfig{},
			expected: []string{
				"          GH_AW_ENGINE_MODEL: ${{ needs.agent.outputs.model }}\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildEngineMetadataEnvVars(tt.engineConfig, tt.model)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d env vars, got %d", len(tt.expected), len(result))
				t.Logf("Expected: %v", tt.expected)
				t.Logf("Got: %v", result)
				return
			}

			for i, expectedVar := range tt.expected {
				if i >= len(result) {
					t.Errorf("Missing expected env var %d: %q", i, expectedVar)
					continue
				}
				if result[i] != expectedVar {
					t.Errorf("Env var %d: expected %q, got %q", i, expectedVar, result[i])
				}
			}
		})
	}
}

// TestEnginesUseSameHelperLogic ensures all engines produce consistent env vars
func TestEnginesUseSameHelperLogic(t *testing.T) {
	workflowData := &WorkflowData{
		TrialMode:        true,
		TrialLogicalRepo: "owner/trial-repo",
		SafeOutputs: &SafeOutputsConfig{
			Staged: templatableBoolPtr("true"),
			UploadAssets: &UploadAssetsConfig{
				BranchName:  "gh-aw-assets",
				MaxSizeKB:   10240,
				AllowedExts: []string{".png", ".jpg"},
			},
		},
	}

	// Test map-based helper (used by copilot, codex, and custom)
	envMap := make(map[string]string)
	applySafeOutputEnvToMap(envMap, workflowData)

	expectedKeys := []string{
		"GH_AW_SAFE_OUTPUTS",
		"GH_AW_SAFE_OUTPUTS_STAGED",
		"GH_AW_TARGET_REPO_SLUG",
		"GH_AW_ASSETS_BRANCH",
		"GH_AW_ASSETS_MAX_SIZE_KB",
		"GH_AW_ASSETS_ALLOWED_EXTS",
	}

	// Check map
	for _, key := range expectedKeys {
		if _, exists := envMap[key]; !exists {
			t.Errorf("envMap missing expected key: %s", key)
		}
	}
}

// TestBuildAgentOutputDownloadSteps verifies the agent output download steps
// include directory creation to handle cases where artifact doesn't exist,
// and that GH_AW_AGENT_OUTPUT is only set when the artifact download succeeds.
// The Gemini engine's GetPreBundleSteps moves /tmp/gemini-client-error-*.json
// into /tmp/gh-aw/ before upload, so the artifact LCA is always /tmp/gh-aw/
// and the hardcoded path is reliable.
func TestBuildAgentOutputDownloadSteps(t *testing.T) {
	steps := buildAgentOutputDownloadSteps("", getActionPin)
	stepsStr := strings.Join(steps, "")

	// Verify expected steps are present
	expectedComponents := []string{
		"- name: Download agent output artifact",
		"id: download-agent-output",
		"continue-on-error: true",
		"uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c",
		// Both the unified agent artifact and the small fallback copy are matched so the
		// agent output survives a failed upload of the larger agent artifact.
		`pattern: "{agent,agent-output-fallback}"`,
		"merge-multiple: true",
		"path: /tmp/gh-aw/",
		"- name: Setup agent output environment variable",
		"id: setup-agent-output-env",
		"if: steps.download-agent-output.outcome == 'success'",
		"mkdir -p /tmp/gh-aw/",
		`find "/tmp/gh-aw/" -type f -print`,
		// Hardcoded path is correct because GetPreBundleSteps ensures LCA is /tmp/gh-aw/
		`if [ -f "/tmp/gh-aw/agent_output.json" ]; then`,
		`echo "GH_AW_AGENT_OUTPUT=/tmp/gh-aw/agent_output.json" >> "$GITHUB_OUTPUT"`,
		"fi",
	}

	for _, expected := range expectedComponents {
		if !strings.Contains(stepsStr, expected) {
			t.Errorf("Expected step to contain %q, but it was not found.\nGenerated steps:\n%s", expected, stepsStr)
		}
	}

	// Verify no dynamic find-based lookup is used (regression guard: the Gemini engine
	// moves files to /tmp/gh-aw/ via GetPreBundleSteps so the hardcoded path is always valid)
	if strings.Contains(stepsStr, "FOUND_FILE=$(find") {
		t.Error("Step must not use dynamic find resolution; hardcoded path should be used instead")
	}

	// Verify mkdir comes before find to ensure directory exists
	mkdirIdx := strings.Index(stepsStr, "mkdir -p /tmp/gh-aw/")
	findIdx := strings.Index(stepsStr, `find "/tmp/gh-aw/"`)

	if mkdirIdx == -1 {
		t.Fatal("mkdir command not found in steps")
	}
	if findIdx == -1 {
		t.Fatal("find command not found in steps")
	}
	if mkdirIdx > findIdx {
		t.Error("mkdir should come before find to ensure directory exists")
	}

	// Verify env-setup conditional comes before the run command
	condIdx := strings.Index(stepsStr, "if: steps.download-agent-output.outcome == 'success'")
	runIdx := strings.Index(stepsStr, "run: |")
	if condIdx == -1 {
		t.Fatal("env-setup conditional not found in steps")
	}
	if runIdx == -1 {
		t.Fatal("run command not found in steps")
	}
	if condIdx > runIdx {
		t.Error("env-setup conditional should come before run command")
	}
}

// TestBuildGitHubScriptStepNoWorkingDirectory verifies that working-directory
// is NOT added to GitHub Script steps (it's only valid for run: steps)
func TestBuildGitHubScriptStepNoWorkingDirectory(t *testing.T) {
	compiler := &Compiler{}
	workflowData := &WorkflowData{
		Name: "Test Workflow",
	}

	config := GitHubScriptStepConfig{
		StepName:    "Test Script",
		StepID:      "test_script",
		MainJobName: "main",
		Script:      "console.log('test');",
		CustomToken: "",
	}

	steps := compiler.buildGitHubScriptStep(workflowData, config)
	stepsStr := strings.Join(steps, "")

	// Verify that working-directory is NOT present
	// working-directory is only valid for run: steps, not for actions/github-script
	if strings.Contains(stepsStr, "working-directory:") {
		t.Error("working-directory should NOT be present in GitHub Script steps - it's only supported for 'run:' steps")
	}

	// Verify that the step uses actions/github-script
	if !strings.Contains(stepsStr, "uses: actions/github-script@") {
		t.Error("Expected step to use actions/github-script")
	}

	// Verify that script is present
	if !strings.Contains(stepsStr, "script: |") {
		t.Error("Expected step to contain script")
	}
}

// TestBuildGitHubScriptStepWorkflowCallArtifactPrefix verifies that agent artifact downloads
// in buildGitHubScriptStep use a job-local prefix in workflow_call context.
// These steps are used in jobs that depend on the agent job (not activation), so the
// agent-downstream prefix expression must be used.
func TestBuildGitHubScriptStepWorkflowCallArtifactPrefix(t *testing.T) {
	compiler := &Compiler{}
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		On:   "workflow_call",
		GHES: true,
	}

	config := GitHubScriptStepConfig{
		StepName:    "Test Step",
		StepID:      "test_step",
		MainJobName: "agent",
		Script:      "console.log('test');",
	}

	steps := compiler.buildGitHubScriptStep(workflowData, config)
	stepsStr := strings.Join(steps, "")

	// In workflow_call context, the download must use the locally computed prefix.
	agentPrefix := "${{ steps.artifact-prefix.outputs.prefix }}agent"
	if !strings.Contains(stepsStr, agentPrefix) {
		t.Errorf("Expected buildGitHubScriptStep to use %q in workflow_call context, but it was not found.\nGenerated steps:\n%s", agentPrefix, stepsStr)
	}

	// Ensure activation prefix is NOT used
	if strings.Contains(stepsStr, "needs.activation.outputs.artifact_prefix") {
		t.Errorf("Expected buildGitHubScriptStep NOT to use needs.activation.outputs.artifact_prefix (job does not depend on activation).\nGenerated steps:\n%s", stepsStr)
	}

	nonGHES := *workflowData
	nonGHES.GHES = false
	nonGHESTeps := strings.Join(compiler.buildGitHubScriptStep(&nonGHES, config), "")
	if !strings.Contains(nonGHESTeps, "${{ needs.agent.outputs.artifact_prefix }}agent") {
		t.Errorf("Expected non-GHES workflow_call to use the producer's artifact prefix.\nGenerated steps:\n%s", nonGHESTeps)
	}
	if strings.Contains(nonGHESTeps, "steps.artifact-prefix.outputs.prefix") {
		t.Errorf("Non-GHES workflow_call should not recompute the artifact prefix.\nGenerated steps:\n%s", nonGHESTeps)
	}
}

// TestBuildGitHubScriptStepNonWorkflowCallNoArtifactPrefix verifies no prefix is added
// in non-workflow_call context.
func TestBuildGitHubScriptStepNonWorkflowCallNoArtifactPrefix(t *testing.T) {
	compiler := &Compiler{}
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		On:   "issues",
	}

	config := GitHubScriptStepConfig{
		StepName:    "Test Step",
		StepID:      "test_step",
		MainJobName: "agent",
		Script:      "console.log('test');",
	}

	steps := compiler.buildGitHubScriptStep(workflowData, config)
	stepsStr := strings.Join(steps, "")

	// In non-workflow_call context, no artifact prefix should be used
	if strings.Contains(stepsStr, "artifact_prefix") {
		t.Errorf("Expected buildGitHubScriptStep NOT to use artifact_prefix in non-workflow_call context.\nGenerated steps:\n%s", stepsStr)
	}
}
