//go:build !integration

package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeEngine(t *testing.T) {
	engine := NewClaudeEngine()

	// Test basic properties
	if engine.GetID() != "claude" {
		t.Errorf("Expected ID 'claude', got '%s'", engine.GetID())
	}

	if engine.GetDisplayName() != "Claude Code" {
		t.Errorf("Expected display name 'Claude Code', got '%s'", engine.GetDisplayName())
	}

	if engine.GetDescription() != "Uses Claude Code with full MCP tool support and allow-listing" {
		t.Errorf("Expected description 'Uses Claude Code with full MCP tool support and allow-listing', got '%s'", engine.GetDescription())
	}

	if engine.IsExperimental() {
		t.Error("Claude engine should not be experimental")
	}

	if !engine.GetCapabilities().ToolsAllowlist {
		t.Error("Claude engine should support MCP tools")
	}

	// Test installation steps (should have 2 steps: Node.js setup + install;
	// secret validation is now in the activation job via GetSecretValidationStep)
	installSteps := engine.GetInstallationSteps(&WorkflowData{})
	if len(installSteps) != 2 {
		t.Errorf("Expected 2 installation steps for Claude (Node.js setup + install), got %d", len(installSteps))
	}

	// Check for Node.js setup step
	nodeSetupStep := strings.Join([]string(installSteps[0]), "\n")
	if !strings.Contains(nodeSetupStep, "Setup Node.js") {
		t.Errorf("Expected 'Setup Node.js' in first installation step, got: %s", nodeSetupStep)
	}
	if !strings.Contains(nodeSetupStep, "node-version: '24'") {
		t.Errorf("Expected 'node-version: '24'' in Node.js setup step, got: %s", nodeSetupStep)
	}

	// Check for install step
	installStep := strings.Join([]string(installSteps[1]), "\n")
	if !strings.Contains(installStep, "Install Claude Code CLI") {
		t.Errorf("Expected 'Install Claude Code CLI' in installation step, got: %s", installStep)
	}
	expectedInstallCommand := fmt.Sprintf("npm install -g @anthropic-ai/claude-code@%s", constants.DefaultClaudeCodeVersion)
	if !strings.Contains(installStep, expectedInstallCommand) {
		t.Errorf("Expected '%s' in install step, got: %s", expectedInstallCommand, installStep)
	}
	if strings.Contains(installStep, "--ignore-scripts") {
		t.Errorf("Expected no --ignore-scripts flag for Claude Code (requires post-install scripts), got: %s", installStep)
	}
	if strings.Contains(installStep, "NPM_CONFIG_MIN_RELEASE_AGE") {
		t.Errorf("Expected no npm release-age cooldown env for Claude install, got: %s", installStep)
	}

	// Test execution steps
	workflowData := &WorkflowData{
		Name: "test-workflow",
	}
	steps := engine.GetExecutionSteps(workflowData, "test-log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step (execution), got %d", len(steps))
	}

	// Check the main execution step
	executionStep := steps[0]
	stepLines := []string(executionStep)

	// Check step name
	found := false
	for _, line := range stepLines {
		if strings.Contains(line, "name: Execute Claude Code CLI") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected step name 'Execute Claude Code CLI' in step lines: %v", stepLines)
	}

	// Check claude usage with direct command instead of npx
	found = false
	for _, line := range stepLines {
		if strings.Contains(line, "claude --print") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected claude command in step lines: %v", stepLines)
	}

	// Check that required CLI arguments are present
	stepContent := strings.Join(stepLines, "\n")
	if !strings.Contains(stepContent, "--print") {
		t.Errorf("Expected --print flag in step: %s", stepContent)
	}

	if !strings.Contains(stepContent, "--permission-mode acceptEdits") {
		t.Errorf("Expected --permission-mode acceptEdits in CLI args: %s", stepContent)
	}

	if !strings.Contains(stepContent, "--output-format stream-json") {
		t.Errorf("Expected --output-format stream-json in CLI args: %s", stepContent)
	}

	if !strings.Contains(stepContent, "ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}") {
		t.Errorf("Expected ANTHROPIC_API_KEY environment variable in step: %s", stepContent)
	}

	if !strings.Contains(stepContent, "GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt") {
		t.Errorf("Expected GH_AW_PROMPT environment variable in step: %s", stepContent)
	}

	// When no tools/MCP servers are configured, GH_AW_MCP_CONFIG should NOT be present
	if strings.Contains(stepContent, "GH_AW_MCP_CONFIG: ${{ runner.temp }}/gh-aw/mcp-config/mcp-servers.json") {
		t.Errorf("Did not expect GH_AW_MCP_CONFIG environment variable in step (no MCP servers): %s", stepContent)
	}

	if !strings.Contains(stepContent, "MCP_TIMEOUT: 120000") {
		t.Errorf("Expected MCP_TIMEOUT environment variable in step: %s", stepContent)
	}

	// When no tools/MCP servers are configured, --mcp-config flag should NOT be present
	if strings.Contains(stepContent, `--mcp-config "${RUNNER_TEMP}/gh-aw/mcp-config/mcp-servers.json"`) {
		t.Errorf("Did not expect MCP config in CLI args (no MCP servers): %s", stepContent)
	}

	if !strings.Contains(stepContent, "--allowed-tools") {
		t.Errorf("Expected allowed-tools in CLI args: %s", stepContent)
	}

	// timeout should now be at step level, not input level
	if !strings.Contains(stepContent, "timeout-minutes:") {
		t.Errorf("Expected timeout-minutes at step level: %s", stepContent)
	}
}

func TestClaudeEngineWithOutput(t *testing.T) {
	engine := NewClaudeEngine()

	// Test execution steps with hasOutput=true
	workflowData := &WorkflowData{
		Name:        "test-workflow",
		SafeOutputs: &SafeOutputsConfig{}, // non-nil means hasOutput=true
	}
	steps := engine.GetExecutionSteps(workflowData, "test-log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step (execution), got %d", len(steps))
	}

	// Check the main execution step
	executionStep := steps[0]
	stepContent := strings.Join([]string(executionStep), "\n")

	// Should include GH_AW_SAFE_OUTPUTS when hasOutput=true in environment section (via step output)
	if !strings.Contains(stepContent, "GH_AW_SAFE_OUTPUTS: ${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}") {
		t.Errorf("Expected GH_AW_SAFE_OUTPUTS in env section when hasOutput=true in step content:\n%s", stepContent)
	}
}

func TestClaudeEngineNonAWFKeepsStderrOutOfTranscript(t *testing.T) {
	engine := NewClaudeEngine()
	workflowData := &WorkflowData{Name: "test-workflow"}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
	require.Len(t, steps, 1)

	stepContent := strings.Join([]string(steps[0]), "\n")
	assert.Contains(t, stepContent, "--debug-file /tmp/gh-aw/claude-debug.log")
	assert.Contains(t, stepContent, `${GH_AW_MODEL_DETECTION_CLAUDE:+ --model "$GH_AW_MODEL_DETECTION_CLAUDE"} | tee -a /tmp/gh-aw/agent-stdio.log`)
	assert.NotContains(t, stepContent, "2>&1 | tee -a /tmp/gh-aw/agent-stdio.log")
	assert.NotContains(t, stepContent, "awf")
}

func TestClaudeEngineLLMProviderGitHubUsesCopilotCredentials(t *testing.T) {
	engine := NewClaudeEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			LLMProvider: LLMProviderGitHub,
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "test-log")
	require.Len(t, steps, 1)
	stepContent := strings.Join([]string(steps[0]), "\n")

	assert.Contains(t, stepContent, "GH_AW_LLM_PROVIDER: github")
	assert.Contains(t, stepContent, "ANTHROPIC_API_KEY: ${{ secrets.COPILOT_GITHUB_TOKEN }}")
	assert.Contains(t, stepContent, fmt.Sprintf("ANTHROPIC_BASE_URL: http://host.docker.internal:%d", constants.CopilotLLMGatewayPort))
}

func TestClaudeEngineAllowsMountedMCPCLICommandsInRestrictedBash(t *testing.T) {
	engine := NewClaudeEngine()

	workflowData := &WorkflowData{
		Name: "test-workflow",
		Tools: map[string]any{
			"bash":       []any{"echo"},
			"cli-proxy":  true,
			"playwright": true,
			"mymcp": map[string]any{
				"command": "npx",
				"args":    []any{"-y", "@acme/mcp-server"},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			NoOp: &NoOpConfig{},
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "test-log")
	require.Len(t, steps, 1, "Expected one execution step")

	stepContent := strings.Join([]string(steps[0]), "\n")
	assert.Contains(t, stepContent, "Bash(echo)", "Expected original restricted bash command")
	assert.Contains(t, stepContent, "Bash(mymcp:*)", "Expected mounted custom MCP CLI allowlist command")
	assert.Contains(t, stepContent, "Bash(playwright-cli:*)", "Expected Playwright CLI allowlist command")
	assert.Contains(t, stepContent, "Bash(safeoutputs:*)", "Expected mounted safeoutputs CLI allowlist command")
	// Permission mode must be acceptEdits when bash is restricted (not wildcard)
	assert.Contains(t, stepContent, "--permission-mode acceptEdits", "Expected acceptEdits with restricted bash")
}

func TestClaudeEnginePermissionMode(t *testing.T) {
	engine := NewClaudeEngine()

	tests := []struct {
		name            string
		tools           map[string]any
		engineConfig    *EngineConfig
		expectedMode    string
		notExpectedMode string
	}{
		{
			name:            "no tools — default acceptEdits",
			tools:           nil,
			expectedMode:    "acceptEdits",
			notExpectedMode: "bypassPermissions",
		},
		{
			name: "restricted bash — acceptEdits",
			tools: map[string]any{
				"bash": []any{"git", "echo"},
			},
			expectedMode:    "acceptEdits",
			notExpectedMode: "bypassPermissions",
		},
		{
			name: "bash wildcard * no longer forces bypassPermissions",
			tools: map[string]any{
				"bash": []any{"*"},
			},
			expectedMode:    "acceptEdits",
			notExpectedMode: "bypassPermissions",
		},
		{
			name: "bash colon-wildcard :* no longer forces bypassPermissions",
			tools: map[string]any{
				"bash": []any{":*"},
			},
			expectedMode:    "acceptEdits",
			notExpectedMode: "bypassPermissions",
		},
		{
			name: "bash true no longer forces bypassPermissions",
			tools: map[string]any{
				"bash": true,
			},
			expectedMode:    "acceptEdits",
			notExpectedMode: "bypassPermissions",
		},
		{
			name: "bash nil no longer forces bypassPermissions",
			tools: map[string]any{
				"bash": nil,
			},
			expectedMode:    "acceptEdits",
			notExpectedMode: "bypassPermissions",
		},
		{
			name: "edit false defaults to auto permission mode",
			tools: map[string]any{
				"edit": false,
			},
			expectedMode:    "auto",
			notExpectedMode: "acceptEdits",
		},
		{
			name: "engine.permission-mode overrides tools.edit=false default",
			tools: map[string]any{
				"edit": false,
			},
			engineConfig: &EngineConfig{
				PermissionMode: "acceptEdits",
			},
			expectedMode:    "acceptEdits",
			notExpectedMode: "auto",
		},
		{
			name: "legacy engine.args permission-mode override still works with one emitted flag",
			engineConfig: &EngineConfig{
				Args: []string{"--permission-mode", "auto"},
			},
			expectedMode:    "auto",
			notExpectedMode: "acceptEdits",
		},
		{
			name: "legacy engine.args permission-mode=value override still works with one emitted flag",
			engineConfig: &EngineConfig{
				Args: []string{"--permission-mode=auto"},
			},
			expectedMode:    "auto",
			notExpectedMode: "acceptEdits",
		},
		{
			name: "engine.permission-mode overrides legacy engine.args permission-mode",
			engineConfig: &EngineConfig{
				PermissionMode: "plan",
				Args:           []string{"--permission-mode", "auto"},
			},
			expectedMode:    "plan",
			notExpectedMode: "auto",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name:         "test-workflow",
				Tools:        tt.tools,
				EngineConfig: tt.engineConfig,
			}
			steps := engine.GetExecutionSteps(workflowData, "test-log")
			require.Len(t, steps, 1, "Expected one execution step")
			stepContent := strings.Join([]string(steps[0]), "\n")
			assert.Contains(t, stepContent, "--permission-mode "+tt.expectedMode,
				"Expected --permission-mode %s", tt.expectedMode)
			assert.NotContains(t, stepContent, "--permission-mode "+tt.notExpectedMode,
				"Did not expect --permission-mode %s", tt.notExpectedMode)
			assert.Equal(t, 1, strings.Count(stepContent, "--permission-mode"),
				"Expected exactly one --permission-mode flag in CLI args")
		})
	}
}

func TestIsEditToolExplicitlyDisabled(t *testing.T) {
	tests := []struct {
		name     string
		tools    map[string]any
		expected bool
	}{
		{
			name:     "nil tools",
			tools:    nil,
			expected: false,
		},
		{
			name: "edit missing",
			tools: map[string]any{
				"bash": []any{"echo"},
			},
			expected: false,
		},
		{
			name: "edit false",
			tools: map[string]any{
				"edit": false,
			},
			expected: true,
		},
		{
			name: "edit true",
			tools: map[string]any{
				"edit": true,
			},
			expected: false,
		},
		{
			name: "edit null",
			tools: map[string]any{
				"edit": nil,
			},
			expected: false,
		},
		{
			name: "edit as string",
			tools: map[string]any{
				"edit": "false",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isEditToolExplicitlyDisabled(tt.tools))
		})
	}
}

func TestStripClaudePermissionModeArgs(t *testing.T) {
	tests := []struct {
		name               string
		inputArgs          []string
		expectedArgs       []string
		expectedPermission string
	}{
		{
			name:               "no permission mode args",
			inputArgs:          []string{"--foo", "bar"},
			expectedArgs:       []string{"--foo", "bar"},
			expectedPermission: "",
		},
		{
			name:               "split permission mode arg",
			inputArgs:          []string{"--permission-mode", "auto", "--foo"},
			expectedArgs:       []string{"--foo"},
			expectedPermission: "auto",
		},
		{
			name:               "equals permission mode arg",
			inputArgs:          []string{"--foo", "--permission-mode=plan"},
			expectedArgs:       []string{"--foo"},
			expectedPermission: "plan",
		},
		{
			name:               "multiple permission mode args use last value",
			inputArgs:          []string{"--permission-mode", "auto", "--permission-mode=acceptEdits", "--foo"},
			expectedArgs:       []string{"--foo"},
			expectedPermission: "acceptEdits",
		},
		{
			name:               "dangling permission mode flag is removed",
			inputArgs:          []string{"--foo", "--permission-mode"},
			expectedArgs:       []string{"--foo"},
			expectedPermission: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, permissionMode := stripClaudePermissionModeArgs(tt.inputArgs)
			assert.Equal(t, tt.expectedArgs, args)
			assert.Equal(t, tt.expectedPermission, permissionMode)
		})
	}
}

func TestClaudeEngineConfiguration(t *testing.T) {
	engine := NewClaudeEngine()

	// Test different workflow names and log files
	testCases := []struct {
		workflowName string
		logFile      string
	}{
		{"simple-workflow", "simple-log"},
		{"complex workflow with spaces", "complex-log"},
		{"workflow-with-hyphens", "workflow-log"},
	}

	for _, tc := range testCases {
		t.Run(tc.workflowName, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name: tc.workflowName,
			}
			steps := engine.GetExecutionSteps(workflowData, tc.logFile)
			if len(steps) != 1 {
				t.Fatalf("Expected 1 step (execution), got %d", len(steps))
			}

			// Check the main execution step
			executionStep := steps[0]
			stepContent := strings.Join([]string(executionStep), "\n")

			// Verify the step contains expected content regardless of input
			if !strings.Contains(stepContent, "name: Execute Claude Code CLI") {
				t.Errorf("Expected step name 'Execute Claude Code CLI' in step content")
			}

			if !strings.Contains(stepContent, "claude --print") {
				t.Errorf("Expected claude command in step content")
			}

			// Verify all required CLI elements are present
			requiredElements := []string{"--print", "ANTHROPIC_API_KEY", "--permission-mode", "--output-format"}
			for _, element := range requiredElements {
				if !strings.Contains(stepContent, element) {
					t.Errorf("Expected element '%s' to be present in step content", element)
				}
			}

			// When no tools/MCP servers are configured, --mcp-config should NOT be present
			if strings.Contains(stepContent, "--mcp-config") {
				t.Errorf("Did not expect --mcp-config in step content (no MCP servers)")
			}

			// timeout should be at step level, not input level
			if !strings.Contains(stepContent, "timeout-minutes:") {
				t.Errorf("Expected timeout-minutes at step level")
			}
		})
	}
}

func TestClaudeEngineWithVersion(t *testing.T) {
	engine := NewClaudeEngine()

	// Test with custom version
	engineConfig := &EngineConfig{
		ID:      "claude",
		Version: "v1.2.3",
	}

	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "claude-3-5-sonnet-20241022",
		EngineConfig: engineConfig,
	}

	// Check installation steps for custom version
	// Secret validation is now in the activation job; installation has Node.js setup + install = 2 steps
	installSteps := engine.GetInstallationSteps(workflowData)
	if len(installSteps) != 2 {
		t.Fatalf("Expected 2 installation steps (Node.js setup + install), got %d", len(installSteps))
	}

	// Check that install step uses the custom version (second step, index 1)
	installStep := strings.Join([]string(installSteps[1]), "\n")
	if !strings.Contains(installStep, "npm install -g @anthropic-ai/claude-code@v1.2.3") {
		t.Errorf("Expected npm install with custom version v1.2.3 (no --ignore-scripts) in install step:\n%s", installStep)
	}
	if strings.Contains(installStep, "--ignore-scripts") {
		t.Errorf("Expected no --ignore-scripts flag for Claude Code, got:\n%s", installStep)
	}

	steps := engine.GetExecutionSteps(workflowData, "test-log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step (execution), got %d", len(steps))
	}

	// Check the main execution step
	executionStep := steps[0]
	stepContent := strings.Join([]string(executionStep), "\n")

	// Check that claude command is used directly (not npx)
	if !strings.Contains(stepContent, "claude --print") {
		t.Errorf("Expected claude command in step content:\n%s", stepContent)
	}

	// Check that model is set via ANTHROPIC_MODEL env var (not as --model flag)
	if !strings.Contains(stepContent, "ANTHROPIC_MODEL: claude-3-5-sonnet-20241022") {
		t.Errorf("Expected ANTHROPIC_MODEL env var for model 'claude-3-5-sonnet-20241022' in step content:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "--model claude-3-5-sonnet-20241022") {
		t.Errorf("Model should not be embedded as --model flag in step content:\n%s", stepContent)
	}
}

func TestClaudeEngineWithoutVersion(t *testing.T) {
	engine := NewClaudeEngine()

	// Test without version (should use default)
	engineConfig := &EngineConfig{
		ID: "claude",
	}

	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "claude-3-5-sonnet-20241022",
		EngineConfig: engineConfig,
	}

	steps := engine.GetExecutionSteps(workflowData, "test-log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step (execution), got %d", len(steps))
	}

	// Check the main execution step
	executionStep := steps[0]
	stepContent := strings.Join([]string(executionStep), "\n")

	// Check that claude command is used directly (not npx) with default version
	if !strings.Contains(stepContent, "claude --print") {
		t.Errorf("Expected claude command in step content:\n%s", stepContent)
	}
}

func TestClaudeEngineWithNilConfig(t *testing.T) {
	engine := NewClaudeEngine()

	// Test with nil engine config (should use default latest)
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		EngineConfig: nil,
	}

	steps := engine.GetExecutionSteps(workflowData, "test-log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step (execution), got %d", len(steps))
	}

	// Check the main execution step
	executionStep := steps[0]
	stepContent := strings.Join([]string(executionStep), "\n")

	// Check that claude command is used directly (not npx) when no engine config
	if !strings.Contains(stepContent, "claude --print") {
		t.Errorf("Expected claude command when no engine config in step content:\n%s", stepContent)
	}
}

func TestClaudeEngineWithMCPServers(t *testing.T) {
	engine := NewClaudeEngine()

	// Test with GitHub MCP tool configured
	workflowData := &WorkflowData{
		Name: "test-workflow",
		Tools: map[string]any{
			"github": map[string]any{},
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "test-log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step (execution), got %d", len(steps))
	}

	// Check the main execution step
	executionStep := steps[0]
	stepContent := strings.Join([]string(executionStep), "\n")

	// When MCP servers are configured, --mcp-config flag SHOULD be present
	if !strings.Contains(stepContent, `--mcp-config "${RUNNER_TEMP}/gh-aw/mcp-config/mcp-servers.json"`) {
		t.Errorf("Expected --mcp-config in CLI args when MCP servers are configured: %s", stepContent)
	}

	// When MCP servers are configured, GH_AW_MCP_CONFIG SHOULD be present
	if !strings.Contains(stepContent, "GH_AW_MCP_CONFIG: ${{ runner.temp }}/gh-aw/mcp-config/mcp-servers.json") {
		t.Errorf("Expected GH_AW_MCP_CONFIG environment variable when MCP servers are configured: %s", stepContent)
	}
}

func TestClaudeEngineWithSafeOutputs(t *testing.T) {
	engine := NewClaudeEngine()

	// Test with safe-outputs configured (which adds safe-outputs MCP server)
	workflowData := &WorkflowData{
		Name:  "test-workflow",
		Tools: map[string]any{},
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
			},
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "test-log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step (execution), got %d", len(steps))
	}

	// Check the main execution step
	executionStep := steps[0]
	stepContent := strings.Join([]string(executionStep), "\n")

	// When safe-outputs is configured, --mcp-config flag SHOULD be present
	if !strings.Contains(stepContent, `--mcp-config "${RUNNER_TEMP}/gh-aw/mcp-config/mcp-servers.json"`) {
		t.Errorf("Expected --mcp-config in CLI args when safe-outputs are configured: %s", stepContent)
	}

	// When safe-outputs is configured, GH_AW_MCP_CONFIG SHOULD be present
	if !strings.Contains(stepContent, "GH_AW_MCP_CONFIG: ${{ runner.temp }}/gh-aw/mcp-config/mcp-servers.json") {
		t.Errorf("Expected GH_AW_MCP_CONFIG environment variable when safe-outputs are configured: %s", stepContent)
	}
}

// TestClaudeEngineNoDoubleEscapePrompt tests that the prompt argument is not double-escaped.
// Claude always reads the prompt from prompt.txt; agent-file content is prepended there by
// the compiler rather than being handled in the engine step.
func TestClaudeEngineNoDoubleEscapePrompt(t *testing.T) {
	engine := NewClaudeEngine()

	// Test without agent file (standard prompt)
	t.Run("without_agent_file", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		stepContent := strings.Join([]string(steps[0]), "\n")

		// With harness: prompt is passed via --prompt-file, not via shell expansion.
		if strings.Contains(stepContent, `"$(cat /tmp/gh-aw/aw-prompts/prompt.txt)"`) {
			t.Errorf("Found old shell-expansion prompt argument (harness should use --prompt-file instead):\n%s", stepContent)
		}

		// The harness reads the prompt file via --prompt-file.
		if !strings.Contains(stepContent, "--prompt-file /tmp/gh-aw/aw-prompts/prompt.txt") {
			t.Errorf("Expected --prompt-file argument for harness execution, got:\n%s", stepContent)
		}
	})

	// Test with agent file: Claude still reads from prompt.txt (compiler prepended the agent
	// file content there); no PROMPT_TEXT shell variable should appear in the step.
	t.Run("with_agent_file_uses_prompt_txt", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
			AgentFile: ".github/agents/test-agent.md",
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		stepContent := strings.Join([]string(steps[0]), "\n")

		// Must still read from prompt.txt via --prompt-file — not from a PROMPT_TEXT shell variable
		if !strings.Contains(stepContent, "--prompt-file /tmp/gh-aw/aw-prompts/prompt.txt") {
			t.Errorf("Expected claude to read from prompt.txt even with agent file set, got:\n%s", stepContent)
		}
		if strings.Contains(stepContent, "PROMPT_TEXT") {
			t.Errorf("Claude must not use a PROMPT_TEXT shell variable when an agent file is set; compiler handles the prepending:\n%s", stepContent)
		}
	})
}

// TestClaudeEngineDoesNotSupportNativeAgentFile verifies that the Claude engine declares
// it does not handle agent files natively, so the compiler knows to prepend the agent file
// content to prompt.txt during the activation job instead.
func TestClaudeEngineDoesNotSupportNativeAgentFile(t *testing.T) {
	engine := NewClaudeEngine()
	if engine.GetCapabilities().NativeAgentFile {
		t.Errorf("Claude engine should report NativeAgentFile=false; the compiler handles agent file injection")
	}
}

// TestClaudeEngineGetHarnessScriptName verifies that the Claude engine returns the built-in
// harness script name, which wraps Claude Code CLI execution with retry logic for transient
// Anthropic API errors (overload, rate limit).
func TestClaudeEngineGetHarnessScriptName(t *testing.T) {
	engine := NewClaudeEngine()
	harnessName := engine.GetHarnessScriptName()
	if harnessName != "claude_harness.cjs" {
		t.Errorf("Expected 'claude_harness.cjs', got %q", harnessName)
	}
}

// TestClaudeEngineHarnessUsesPromptFile verifies that when the built-in harness is used,
// the execution step passes --prompt-file instead of inline shell expansion.
func TestClaudeEngineHarnessUsesPromptFile(t *testing.T) {
	engine := NewClaudeEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID: "claude",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}
	stepContent := strings.Join([]string(steps[0]), "\n")

	// Harness-based execution uses --prompt-file, not "$(cat ...)".
	if strings.Contains(stepContent, `"$(cat /tmp/gh-aw/aw-prompts/prompt.txt)"`) {
		t.Errorf("Found old shell-expansion prompt: harness should use --prompt-file:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "--prompt-file /tmp/gh-aw/aw-prompts/prompt.txt") {
		t.Errorf("Expected --prompt-file in harness-wrapped execution:\n%s", stepContent)
	}

	// The harness script name must appear in the command.
	if !strings.Contains(stepContent, "claude_harness.cjs") {
		t.Errorf("Expected claude_harness.cjs in execution step:\n%s", stepContent)
	}
}

// TestClaudeEngineCustomHarnessOverridesBuiltIn verifies that engine.harness overrides
// the built-in claude_harness.cjs.
func TestClaudeEngineCustomHarnessOverridesBuiltIn(t *testing.T) {
	engine := NewClaudeEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:            "claude",
			HarnessScript: "my_custom_harness.cjs",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}
	stepContent := strings.Join([]string(steps[0]), "\n")

	if !strings.Contains(stepContent, "my_custom_harness.cjs") {
		t.Errorf("Expected custom harness script in execution step:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "claude_harness.cjs") {
		t.Errorf("Built-in harness should be replaced by custom harness:\n%s", stepContent)
	}
}

// TestClaudeEngineAWFWithAgentFileReadsPromptTxt verifies that when an agent file is used
// with the firewall (AWF) enabled, the claude command reads from prompt.txt (not from a
// PROMPT_TEXT shell variable).  The compiler prepends the agent file content to prompt.txt
// in the activation job.
func TestClaudeEngineAWFWithAgentFileReadsPromptTxt(t *testing.T) {
	engine := NewClaudeEngine()

	agentSandbox := &AgentSandboxConfig{Type: SandboxTypeAWF}
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID: "claude",
		},
		AgentFile: ".github/agents/test-agent.md",
		SandboxConfig: &SandboxConfig{
			Agent: agentSandbox,
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
	if len(steps) == 0 {
		t.Fatal("Expected at least one step")
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// No AGENT_CONTENT or PROMPT_TEXT shell variables anywhere in the step.
	if strings.Contains(stepContent, "AGENT_CONTENT") {
		t.Errorf("AGENT_CONTENT must not appear in the Claude AWF step; compiler handles agent file injection:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "PROMPT_TEXT") {
		t.Errorf("PROMPT_TEXT must not appear in the Claude AWF step; compiler handles agent file injection:\n%s", stepContent)
	}

	// The container command must still read from prompt.txt via --prompt-file (harness resolves it).
	if !strings.Contains(stepContent, "--prompt-file /tmp/gh-aw/aw-prompts/prompt.txt") {
		t.Errorf("Expected claude to read from prompt.txt in AWF mode, got:\n%s", stepContent)
	}
}

func TestClaudeEngineSkipInstallationWithCommand(t *testing.T) {
	engine := NewClaudeEngine()

	// Test with custom command - should skip installation
	workflowData := &WorkflowData{
		EngineConfig: &EngineConfig{Command: "/usr/local/bin/custom-claude"},
	}
	steps := engine.GetInstallationSteps(workflowData)

	if len(steps) != 0 {
		t.Errorf("Expected 0 installation steps when command is specified, got %d", len(steps))
	}
}

func TestClaudeEngineEnvOverridesTokenExpression(t *testing.T) {
	engine := NewClaudeEngine()

	t.Run("engine env overrides default token expression", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Env: map[string]string{
					"ANTHROPIC_API_KEY": "${{ secrets.MY_ORG_ANTHROPIC_KEY }}",
				},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}

		stepContent := strings.Join([]string(steps[0]), "\n")

		// engine.env override should replace the default token expression
		if !strings.Contains(stepContent, "ANTHROPIC_API_KEY: ${{ secrets.MY_ORG_ANTHROPIC_KEY }}") {
			t.Errorf("Expected engine.env to override ANTHROPIC_API_KEY, got:\n%s", stepContent)
		}
		if strings.Contains(stepContent, "ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}") {
			t.Errorf("Default ANTHROPIC_API_KEY expression should be replaced by engine.env override, got:\n%s", stepContent)
		}
	})

	t.Run("engine env adds extra environment variables", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Env: map[string]string{
					"CUSTOM_VAR": "custom-value",
				},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}

		stepContent := strings.Join([]string(steps[0]), "\n")

		if !strings.Contains(stepContent, "CUSTOM_VAR: custom-value") {
			t.Errorf("Expected engine.env to add CUSTOM_VAR, got:\n%s", stepContent)
		}
	})
}

func TestClaudeEngineWithExpressionVersion(t *testing.T) {
	engine := NewClaudeEngine()

	expressionVersion := "${{ inputs.engine-version }}"
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:      "claude",
			Version: expressionVersion,
		},
	}

	// Expression version must use env var injection in the install step
	installSteps := engine.GetInstallationSteps(workflowData)
	// Expect: Node.js setup step + install step
	if len(installSteps) != 2 {
		t.Fatalf("Expected 2 installation steps, got %d", len(installSteps))
	}

	installStep := strings.Join([]string(installSteps[1]), "\n")

	// Should use ENGINE_VERSION env var
	if !strings.Contains(installStep, "ENGINE_VERSION: "+expressionVersion) {
		t.Errorf("Expected ENGINE_VERSION env var in install step, got:\n%s", installStep)
	}

	// Should reference env var in command
	if !strings.Contains(installStep, `"${ENGINE_VERSION}"`) {
		t.Errorf(`Expected "$ENGINE_VERSION" in npm install command, got:\n%s`, installStep)
	}

	// Should NOT embed expression directly in shell command
	if strings.Contains(installStep, "@anthropic-ai/claude-code@"+expressionVersion) {
		t.Errorf("Expression should NOT be embedded directly in npm install command, got:\n%s", installStep)
	}
}

func TestIsAnthropicWIF(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		want         bool
	}{
		{
			name:         "nil workflowData returns false",
			workflowData: nil,
			want:         false,
		},
		{
			name:         "nil EngineConfig returns false",
			workflowData: &WorkflowData{},
			want:         false,
		},
		{
			name: "nil Auth returns false",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{},
			},
			want: false,
		},
		{
			name: "github-oidc without provider returns false",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Auth: &EngineAuthConfig{Type: "github-oidc"},
				},
			},
			want: false,
		},
		{
			name: "github-oidc with azure provider returns false",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Auth: &EngineAuthConfig{Type: "github-oidc", Provider: "azure"},
				},
			},
			want: false,
		},
		{
			name: "static type with anthropic provider returns false",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Auth: &EngineAuthConfig{Type: "static", Provider: "anthropic"},
				},
			},
			want: false,
		},
		{
			name: "github-oidc with anthropic provider returns true",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Auth: &EngineAuthConfig{Type: "github-oidc", Provider: "anthropic"},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAnthropicWIF(tt.workflowData)
			assert.Equal(t, tt.want, got)
		})
	}
}
