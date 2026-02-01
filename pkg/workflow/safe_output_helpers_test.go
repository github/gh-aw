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
				Token:       "",
			},
			expectedInSteps: []string{
				"- name: Download agent output artifact",
				"- name: Setup agent output environment variable",
				"- name: Test Step",
				"id: test_step",
				"uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd",
				"env:",
				"GH_AW_AGENT_OUTPUT: ${{ env.GH_AW_AGENT_OUTPUT }}",
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
				Script: "const issue = true;",
				Token:  "",
			},
			expectedInSteps: []string{
				"- name: Download agent output artifact",
				"- name: Setup agent output environment variable",
				"- name: Create Issue",
				"id: create_issue",
				"uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd",
				"GH_AW_AGENT_OUTPUT: ${{ env.GH_AW_AGENT_OUTPUT }}",
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
				Token:       "",
			},
			expectedInSteps: []string{
				"- name: Download agent output artifact",
				"- name: Setup agent output environment variable",
				"- name: Process Output",
				"id: process",
				"GH_AW_AGENT_OUTPUT: ${{ env.GH_AW_AGENT_OUTPUT }}",
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
				Token:       "${{ secrets.CUSTOM_TOKEN }}",
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
			if !strings.Contains(stepsStr, "uses: actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd") {
				t.Error("Expected step to use actions/github-script@ed597411d8f924073f98dfc5c65a23a2325f34cd")
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
		Script: "const test = 1;",
		Token:  "",
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
				"GH_AW_SAFE_OUTPUTS": "${{ env.GH_AW_SAFE_OUTPUTS }}",
			},
		},
		{
			name: "safe outputs with staged flag",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					Staged: true,
				},
			},
			expected: map[string]string{
				"GH_AW_SAFE_OUTPUTS":        "${{ env.GH_AW_SAFE_OUTPUTS }}",
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
				"GH_AW_SAFE_OUTPUTS":        "${{ env.GH_AW_SAFE_OUTPUTS }}",
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
				"GH_AW_SAFE_OUTPUTS":        "${{ env.GH_AW_SAFE_OUTPUTS }}",
				"GH_AW_ASSETS_BRANCH":       "\"gh-aw-assets\"",
				"GH_AW_ASSETS_MAX_SIZE_KB":  "10240",
				"GH_AW_ASSETS_ALLOWED_EXTS": "\".png,.jpg,.jpeg\"",
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

// TestApplySafeOutputEnvToSlice verifies the helper function for YAML string slices
func TestApplySafeOutputEnvToSlice(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expected     []string
	}{
		{
			name: "nil SafeOutputs",
			workflowData: &WorkflowData{
				SafeOutputs: nil,
			},
			expected: []string{},
		},
		{
			name: "basic safe outputs",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{},
			},
			expected: []string{
				"          GH_AW_SAFE_OUTPUTS: ${{ env.GH_AW_SAFE_OUTPUTS }}",
			},
		},
		{
			name: "safe outputs with staged flag",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					Staged: true,
				},
			},
			expected: []string{
				"          GH_AW_SAFE_OUTPUTS: ${{ env.GH_AW_SAFE_OUTPUTS }}",
				"          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"",
			},
		},
		{
			name: "trial mode",
			workflowData: &WorkflowData{
				TrialMode:        true,
				TrialLogicalRepo: "owner/repo",
				SafeOutputs:      &SafeOutputsConfig{},
			},
			expected: []string{
				"          GH_AW_SAFE_OUTPUTS: ${{ env.GH_AW_SAFE_OUTPUTS }}",
				"          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"",
				"          GH_AW_TARGET_REPO_SLUG: \"owner/repo\"",
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
			expected: []string{
				"          GH_AW_SAFE_OUTPUTS: ${{ env.GH_AW_SAFE_OUTPUTS }}",
				"          GH_AW_ASSETS_BRANCH: \"gh-aw-assets\"",
				"          GH_AW_ASSETS_MAX_SIZE_KB: 10240",
				"          GH_AW_ASSETS_ALLOWED_EXTS: \".png,.jpg,.jpeg\"",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stepLines []string
			applySafeOutputEnvToSlice(&stepLines, tt.workflowData)

			if len(stepLines) != len(tt.expected) {
				t.Errorf("Expected %d lines, got %d", len(tt.expected), len(stepLines))
			}

			for i, expectedLine := range tt.expected {
				if i >= len(stepLines) {
					t.Errorf("Missing expected line %d: %q", i, expectedLine)
					continue
				}
				if stepLines[i] != expectedLine {
					t.Errorf("Line %d: expected %q, got %q", i, expectedLine, stepLines[i])
				}
			}
		})
	}
}

// TestBuildWorkflowMetadataEnvVars verifies the helper function for workflow metadata env vars
func TestBuildWorkflowMetadataEnvVars(t *testing.T) {
	tests := []struct {
		name           string
		workflowName   string
		workflowSource string
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
				"          GH_AW_WORKFLOW_SOURCE_URL: \"${{ github.server_url }}/owner/repo/tree/main/workflows/triage.md\"\n",
			},
		},
		{
			name:           "workflow name and source without ref",
			workflowName:   "CI Helper",
			workflowSource: "org/project/ci/helper.md",
			expected: []string{
				"          GH_AW_WORKFLOW_NAME: \"CI Helper\"\n",
				"          GH_AW_WORKFLOW_SOURCE: \"org/project/ci/helper.md\"\n",
				"          GH_AW_WORKFLOW_SOURCE_URL: \"${{ github.server_url }}/org/project/tree/main/ci/helper.md\"\n",
			},
		},
		{
			name:           "empty workflow name",
			workflowName:   "",
			workflowSource: "owner/repo/workflow.md",
			expected: []string{
				"          GH_AW_WORKFLOW_NAME: \"\"\n",
				"          GH_AW_WORKFLOW_SOURCE: \"owner/repo/workflow.md\"\n",
				"          GH_AW_WORKFLOW_SOURCE_URL: \"${{ github.server_url }}/owner/repo/tree/main/workflow.md\"\n",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildWorkflowMetadataEnvVars(tt.workflowName, tt.workflowSource)

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
			result := buildSafeOutputJobEnvVars(
				tt.trialMode,
				tt.trialLogicalRepoSlug,
				tt.staged,
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
			},
		},
		{
			name: "full engine config",
			engineConfig: &EngineConfig{
				ID:      "copilot",
				Version: "1.0.0",
				Model:   "gpt-5",
			},
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
			},
		},
		{
			name: "engine with model and no version",
			engineConfig: &EngineConfig{
				ID:    "copilot",
				Model: "claude-sonnet-4",
			},
			expected: []string{
				"          GH_AW_ENGINE_ID: \"copilot\"\n",
				"          GH_AW_ENGINE_MODEL: \"claude-sonnet-4\"\n",
			},
		},
		{
			name:         "empty engine config",
			engineConfig: &EngineConfig{},
			expected:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildEngineMetadataEnvVars(tt.engineConfig)

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
			Staged: true,
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

	// Test slice-based helper (used by claude)
	var stepLines []string
	applySafeOutputEnvToSlice(&stepLines, workflowData)

	// Verify both approaches produce the same env vars
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

	// Check slice (should contain all keys)
	sliceContent := strings.Join(stepLines, "\n")
	for _, key := range expectedKeys {
		if !strings.Contains(sliceContent, key) {
			t.Errorf("stepLines missing expected key: %s", key)
		}
	}
}

// TestBuildAgentOutputDownloadSteps verifies the agent output download steps
// include directory creation to handle cases where artifact doesn't exist
func TestBuildAgentOutputDownloadSteps(t *testing.T) {
	steps := buildAgentOutputDownloadSteps()
	stepsStr := strings.Join(steps, "")

	// Verify expected steps are present
	expectedComponents := []string{
		"- name: Download agent output artifact",
		"continue-on-error: true",
		"uses: actions/download-artifact@018cc2cf5baa6db3ef3c5f8a56943fffe632ef53",
		"name: agent-output",
		"path: /tmp/gh-aw/safeoutputs/",
		"- name: Setup agent output environment variable",
		"mkdir -p /tmp/gh-aw/safeoutputs/",
		"find \"/tmp/gh-aw/safeoutputs/\" -type f -print",
		"GH_AW_AGENT_OUTPUT=/tmp/gh-aw/safeoutputs/agent_output.json",
	}

	for _, expected := range expectedComponents {
		if !strings.Contains(stepsStr, expected) {
			t.Errorf("Expected step to contain %q, but it was not found.\nGenerated steps:\n%s", expected, stepsStr)
		}
	}

	// Verify mkdir comes before find to ensure directory exists
	mkdirIdx := strings.Index(stepsStr, "mkdir -p /tmp/gh-aw/safeoutputs/")
	findIdx := strings.Index(stepsStr, "find \"/tmp/gh-aw/safeoutputs/\"")

	if mkdirIdx == -1 {
		t.Fatal("mkdir command not found in steps")
	}
	if findIdx == -1 {
		t.Fatal("find command not found in steps")
	}
	if mkdirIdx > findIdx {
		t.Error("mkdir should come before find to ensure directory exists")
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
		Token:       "",
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

// TestBuildTitlePrefixEnvVar verifies the helper function for building title-prefix env vars
func TestBuildTitlePrefixEnvVar(t *testing.T) {
	tests := []struct {
		name        string
		envVarName  string
		titlePrefix string
		expected    []string
	}{
		{
			name:        "empty title prefix returns nil",
			envVarName:  "GH_AW_ISSUE_TITLE_PREFIX",
			titlePrefix: "",
			expected:    nil,
		},
		{
			name:        "issue title prefix",
			envVarName:  "GH_AW_ISSUE_TITLE_PREFIX",
			titlePrefix: "[bot] ",
			expected:    []string{"          GH_AW_ISSUE_TITLE_PREFIX: \"[bot] \"\n"},
		},
		{
			name:        "discussion title prefix",
			envVarName:  "GH_AW_DISCUSSION_TITLE_PREFIX",
			titlePrefix: "[analysis] ",
			expected:    []string{"          GH_AW_DISCUSSION_TITLE_PREFIX: \"[analysis] \"\n"},
		},
		{
			name:        "PR title prefix",
			envVarName:  "GH_AW_PR_TITLE_PREFIX",
			titlePrefix: "[auto] ",
			expected:    []string{"          GH_AW_PR_TITLE_PREFIX: \"[auto] \"\n"},
		},
		{
			name:        "title prefix with special characters",
			envVarName:  "GH_AW_ISSUE_TITLE_PREFIX",
			titlePrefix: "[AI-Generated] 🤖 ",
			expected:    []string{"          GH_AW_ISSUE_TITLE_PREFIX: \"[AI-Generated] 🤖 \"\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildTitlePrefixEnvVar(tt.envVarName, tt.titlePrefix)

			// Handle nil vs empty slice comparison
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("expected %v, got nil", tt.expected)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("expected length %d, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("at index %d: expected %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

// TestBuildLabelsEnvVar verifies the helper function for building labels env vars
func TestBuildLabelsEnvVar(t *testing.T) {
	tests := []struct {
		name       string
		envVarName string
		labels     []string
		expected   []string
	}{
		{
			name:       "empty labels returns nil",
			envVarName: "GH_AW_ISSUE_LABELS",
			labels:     []string{},
			expected:   nil,
		},
		{
			name:       "nil labels returns nil",
			envVarName: "GH_AW_ISSUE_LABELS",
			labels:     nil,
			expected:   nil,
		},
		{
			name:       "single label",
			envVarName: "GH_AW_ISSUE_LABELS",
			labels:     []string{"automation"},
			expected:   []string{"          GH_AW_ISSUE_LABELS: \"automation\"\n"},
		},
		{
			name:       "multiple labels",
			envVarName: "GH_AW_ISSUE_LABELS",
			labels:     []string{"automation", "ai-generated", "enhancement"},
			expected:   []string{"          GH_AW_ISSUE_LABELS: \"automation,ai-generated,enhancement\"\n"},
		},
		{
			name:       "PR labels",
			envVarName: "GH_AW_PR_LABELS",
			labels:     []string{"automated", "needs-review"},
			expected:   []string{"          GH_AW_PR_LABELS: \"automated,needs-review\"\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildLabelsEnvVar(tt.envVarName, tt.labels)

			// Handle nil vs empty slice comparison
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("expected %v, got nil", tt.expected)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("expected length %d, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("at index %d: expected %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}

// TestBuildCategoryEnvVar verifies the helper function for building category env vars
func TestBuildCategoryEnvVar(t *testing.T) {
	tests := []struct {
		name       string
		envVarName string
		category   string
		expected   []string
	}{
		{
			name:       "empty category returns nil",
			envVarName: "GH_AW_DISCUSSION_CATEGORY",
			category:   "",
			expected:   nil,
		},
		{
			name:       "category by name",
			envVarName: "GH_AW_DISCUSSION_CATEGORY",
			category:   "general",
			expected:   []string{"          GH_AW_DISCUSSION_CATEGORY: \"general\"\n"},
		},
		{
			name:       "category by ID",
			envVarName: "GH_AW_DISCUSSION_CATEGORY",
			category:   "DIC_kwDOGFsHUM4BsUn3",
			expected:   []string{"          GH_AW_DISCUSSION_CATEGORY: \"DIC_kwDOGFsHUM4BsUn3\"\n"},
		},
		{
			name:       "category by numeric ID",
			envVarName: "GH_AW_DISCUSSION_CATEGORY",
			category:   "12345",
			expected:   []string{"          GH_AW_DISCUSSION_CATEGORY: \"12345\"\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildCategoryEnvVar(tt.envVarName, tt.category)

			// Handle nil vs empty slice comparison
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("expected %v, got nil", tt.expected)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("expected length %d, got %d", len(tt.expected), len(result))
				return
			}

			for i, expected := range tt.expected {
				if result[i] != expected {
					t.Errorf("at index %d: expected %q, got %q", i, expected, result[i])
				}
			}
		})
	}
}
