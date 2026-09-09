//go:build !integration

package workflow

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestCodexEngine_ResolveLLMProvider_DefaultOpenAI(t *testing.T) {
	engine := NewCodexEngine()

	asserted := engine.ResolveLLMProvider(&WorkflowData{EngineConfig: &EngineConfig{ID: "codex"}})
	if asserted != LLMProviderOpenAI {
		t.Fatalf("expected default model-provider to be openai, got %q", asserted)
	}
}

func TestCodexEngine_ResolveLLMProviderFromModel(t *testing.T) {
	engine := NewCodexEngine()

	tests := []struct {
		name     string
		data     *WorkflowData
		expected LLMProvider
	}{
		{
			name:     "copilot prefix selects GitHub",
			data:     &WorkflowData{Model: "copilot/auto", EngineConfig: &EngineConfig{ID: "codex"}},
			expected: LLMProviderGitHub,
		},
		{
			name:     "concrete copilot model selects GitHub",
			data:     &WorkflowData{Model: "copilot/gpt-5.4", EngineConfig: &EngineConfig{ID: "codex"}},
			expected: LLMProviderGitHub,
		},
		{
			name:     "model matching is case insensitive",
			data:     &WorkflowData{Model: " COPILOT/AUTO ", EngineConfig: &EngineConfig{ID: "codex"}},
			expected: LLMProviderGitHub,
		},
		{
			name:     "other model keeps OpenAI default",
			data:     &WorkflowData{Model: "gpt-5-codex", EngineConfig: &EngineConfig{ID: "codex"}},
			expected: LLMProviderOpenAI,
		},
		{
			name: "explicit provider overrides model",
			data: &WorkflowData{
				Model:        "copilot/auto",
				EngineConfig: &EngineConfig{ID: "codex", LLMProvider: LLMProviderOpenAI},
			},
			expected: LLMProviderOpenAI,
		},
		{
			name:     "unprefixed model keeps OpenAI default",
			data:     &WorkflowData{Model: "copilot-large", EngineConfig: &EngineConfig{ID: "codex"}},
			expected: LLMProviderOpenAI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if actual := engine.ResolveLLMProvider(tt.data); actual != tt.expected {
				t.Fatalf("expected provider %q, got %q", tt.expected, actual)
			}
		})
	}
}

func TestCodexModelID(t *testing.T) {
	tests := map[string]string{
		"copilot/auto":    "auto",
		"copilot/gpt-5.4": "gpt-5.4",
		"gpt-5-codex":     "gpt-5-codex",
	}

	for model, expected := range tests {
		t.Run(model, func(t *testing.T) {
			if actual := codexModelID(model); actual != expected {
				t.Fatalf("expected model ID %q, got %q", expected, actual)
			}
		})
	}
}

func TestCodexEngineCopilotModelUsesGitHubInference(t *testing.T) {
	engine := NewCodexEngine()
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "copilot/auto",
		EngineConfig: &EngineConfig{ID: "codex"},
		SafeOutputs:  &SafeOutputsConfig{},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}

	stepContent := strings.Join([]string(engine.GetExecutionSteps(workflowData, "test-log")[0]), "\n")
	expected := []string{
		`GH_AW_LLM_PROVIDER: github`,
		`AWF_REFLECT_ENABLED: 1`,
		`COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}`,
		constants.CopilotBYOKDummyAPIKeyEnvVar + `: ` + constants.CopilotBYOKDummyAPIKey,
		`export CODEX_API_KEY="$` + constants.CopilotBYOKDummyAPIKeyEnvVar + `"`,
		`GH_AW_MODEL_AGENT_CODEX: auto`,
		`${GH_AW_MODEL_AGENT_CODEX:+ --model "$GH_AW_MODEL_AGENT_CODEX"}`,
	}
	for _, value := range expected {
		if !strings.Contains(stepContent, value) {
			t.Errorf("expected execution step to contain %q, got:\n%s", value, stepContent)
		}
	}
	if strings.Contains(stepContent, "secrets.CODEX_API_KEY") || strings.Contains(stepContent, "secrets.OPENAI_API_KEY") || strings.Contains(stepContent, `OPENAI_API_KEY:`) {
		t.Errorf("GitHub inference must not use OpenAI credentials, got:\n%s", stepContent)
	}

	secrets := engine.GetRequiredSecretNames(workflowData)
	if len(secrets) != 1 || secrets[0] != "COPILOT_GITHUB_TOKEN" {
		t.Fatalf("expected only COPILOT_GITHUB_TOKEN, got %v", secrets)
	}
}

func TestCodexEngineCopilotModelUsesGitHubActionsToken(t *testing.T) {
	engine := NewCodexEngine()
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "copilot/auto",
		EngineConfig: &EngineConfig{ID: "codex"},
		Permissions:  "permissions:\n  copilot-requests: write",
		SafeOutputs:  &SafeOutputsConfig{},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}

	stepContent := strings.Join([]string(engine.GetExecutionSteps(workflowData, "test-log")[0]), "\n")
	if !strings.Contains(stepContent, `COPILOT_GITHUB_TOKEN: ${{ github.token }}`) {
		t.Errorf("expected GitHub Actions token for the Copilot provider, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, `export CODEX_API_KEY="$`+constants.CopilotBYOKDummyAPIKeyEnvVar+`"`) {
		t.Errorf("expected Codex to use the BYOK sentinel, got:\n%s", stepContent)
	}
	if secrets := engine.GetRequiredSecretNames(workflowData); len(secrets) != 0 {
		t.Fatalf("expected no inference secret with copilot-requests permission, got %v", secrets)
	}
	if step := engine.GetSecretValidationStep(workflowData); len(step) != 0 {
		t.Fatalf("expected secret validation to be skipped, got:\n%s", strings.Join(step, "\n"))
	}
}

func TestCodexEngine(t *testing.T) {
	engine := NewCodexEngine()

	// Test basic properties
	if engine.GetID() != "codex" {
		t.Errorf("Expected ID 'codex', got '%s'", engine.GetID())
	}

	if engine.GetDisplayName() != "Codex" {
		t.Errorf("Expected display name 'Codex', got '%s'", engine.GetDisplayName())
	}

	if engine.IsExperimental() {
		t.Error("Codex engine should not be experimental")
	}

	if !engine.GetCapabilities().ToolsAllowlist {
		t.Error("Codex engine should support MCP tools")
	}

	// Test installation steps
	steps := engine.GetInstallationSteps(&WorkflowData{})
	// Secret validation is now in the activation job; installation has Node.js setup + Install Codex = 2 steps
	expectedStepCount := 2
	if len(steps) != expectedStepCount {
		t.Errorf("Expected %d installation steps, got %d", expectedStepCount, len(steps))
	}

	// Verify first step is Node.js setup
	if len(steps) > 0 && len(steps[0]) > 0 {
		if !strings.Contains(steps[0][0], "Setup Node.js") {
			t.Errorf("Expected first step to contain 'Setup Node.js', got '%s'", steps[0][0])
		}
	}

	// Verify second step is Install Codex CLI
	if len(steps) > 1 && len(steps[1]) > 0 {
		if !strings.Contains(steps[1][0], "Install Codex CLI") {
			t.Errorf("Expected second step to contain 'Install Codex CLI', got '%s'", steps[1][0])
		}
		if strings.Contains(strings.Join([]string(steps[1]), "\n"), "NPM_CONFIG_MIN_RELEASE_AGE") {
			t.Errorf("Expected no npm release-age cooldown env for Codex install, got '%s'", strings.Join([]string(steps[1]), "\n"))
		}
	}

	// Test execution steps
	workflowData := &WorkflowData{
		Name: "test-workflow",
	}
	execSteps := engine.GetExecutionSteps(workflowData, "test-log")
	if len(execSteps) != 1 {
		t.Fatalf("Expected 1 step for Codex execution, got %d", len(execSteps))
	}

	// Check the execution step
	stepContent := strings.Join([]string(execSteps[0]), "\n")

	if !strings.Contains(stepContent, "name: Execute Codex CLI") {
		t.Errorf("Expected step name 'Execute Codex CLI' in step content:\n%s", stepContent)
	}

	if strings.Contains(stepContent, "uses:") {
		t.Errorf("Expected no action for Codex (uses command), got step with 'uses:' in:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "codex") {
		t.Errorf("Expected command to contain 'codex' in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "command -v node") {
		t.Errorf("Expected command to resolve node via PATH in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "node runtime missing on this runner — check runtimes.node in workflow YAML") {
		t.Errorf("Expected clear node runtime error guidance in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "exec") {
		t.Errorf("Expected command to contain 'exec' in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "test-log") {
		t.Errorf("Expected command to contain log file name in step content:\n%s", stepContent)
	}

	// Check that pipefail is enabled to preserve exit codes
	if !strings.Contains(stepContent, "set -o pipefail") {
		t.Errorf("Expected command to contain 'set -o pipefail' to preserve exit codes in step content:\n%s", stepContent)
	}

	// Check environment variables
	if !strings.Contains(stepContent, "CODEX_API_KEY: ${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}") {
		t.Errorf("Expected CODEX_API_KEY environment variable in step content:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "--exclude-env OPENAI_API_KEY") {
		t.Errorf("OPENAI_API_KEY must remain available to Codex runtime, got:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "--exclude-env CODEX_API_KEY") {
		t.Errorf("CODEX_API_KEY must remain available to Codex runtime, got:\n%s", stepContent)
	}
}

func TestCodexEngineWithVersion(t *testing.T) {
	engine := NewCodexEngine()

	// Test installation steps without version (should use pinned default version)
	stepsNoVersion := engine.GetInstallationSteps(&WorkflowData{})
	foundNoVersionInstall := false
	expectedPackage := fmt.Sprintf("@openai/codex@%s", constants.DefaultCodexVersion)
	for _, step := range stepsNoVersion {
		for _, line := range step {
			if strings.Contains(line, "npm install") && strings.Contains(line, expectedPackage) {
				foundNoVersionInstall = true
				break
			}
		}
	}
	if !foundNoVersionInstall {
		t.Errorf("Expected npm install command with @%s when no version specified", constants.DefaultCodexVersion)
	}

	// Test installation steps with version
	engineConfig := &EngineConfig{
		ID:      "codex",
		Version: "3.0.1",
	}
	workflowData := &WorkflowData{
		EngineConfig: engineConfig,
	}
	stepsWithVersion := engine.GetInstallationSteps(workflowData)
	foundVersionInstall := false
	for _, step := range stepsWithVersion {
		for _, line := range step {
			if strings.Contains(line, "npm install") && strings.Contains(line, "@openai/codex@3.0.1") {
				foundVersionInstall = true
				break
			}
		}
	}
	if !foundVersionInstall {
		t.Error("Expected versioned npm install command with @openai/codex@3.0.1")
	}
}

func TestCodexEngineExecutionIncludesGitHubAWPrompt(t *testing.T) {
	engine := NewCodexEngine()

	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// Should have at least one step
	if len(steps) == 0 {
		t.Error("Expected at least one execution step")
		return
	}

	// Check that GH_AW_PROMPT environment variable is included
	foundPromptEnv := false
	foundMCPConfigEnv := false
	for _, step := range steps {
		stepContent := strings.Join([]string(step), "\n")
		if strings.Contains(stepContent, "GH_AW_PROMPT: /tmp/gh-aw/aw-prompts/prompt.txt") {
			foundPromptEnv = true
		}
		if strings.Contains(stepContent, "GH_AW_MCP_CONFIG: ${{ runner.temp }}/gh-aw/mcp-config/config.toml") {
			foundMCPConfigEnv = true
		}
	}

	if !foundPromptEnv {
		t.Error("Expected GH_AW_PROMPT environment variable in codex execution steps")
	}

	if !foundMCPConfigEnv {
		t.Error("Expected GH_AW_MCP_CONFIG environment variable in codex execution steps")
	}
}

func TestCodexEngineExecutionUsesWritableCodexHome(t *testing.T) {
	engine := NewCodexEngine()

	steps := engine.GetExecutionSteps(&WorkflowData{Name: "test-workflow"}, "/tmp/gh-aw/test.log")
	if len(steps) == 0 {
		t.Fatal("Expected at least one execution step")
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if !strings.Contains(stepContent, "CODEX_HOME: /tmp/gh-aw/mcp-config") {
		t.Errorf("Expected CODEX_HOME to use writable /tmp path, got:\n%s", stepContent)
	}
}

func TestCodexEngineExecutionDumpsInternalLogsOnFailure(t *testing.T) {
	engine := NewCodexEngine()

	if got, want := engine.GetInternalLogsDir(), constants.TmpMcpConfigDir+"/logs"; got != want {
		t.Errorf("Expected GetInternalLogsDir() to return %q, got %q", want, got)
	}

	t.Run("without firewall", func(t *testing.T) {
		steps := engine.GetExecutionSteps(&WorkflowData{Name: "test-workflow"}, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected a single execution step (internal log rendering is handled by the shared detect-agent-errors step), got %d steps", len(steps))
		}
	})

	t.Run("with firewall", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:               "test-workflow",
			NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
		}
		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected a single execution step (internal log rendering is handled by the shared detect-agent-errors step), got %d steps", len(steps))
		}
	})
}

func TestCodexEngineErrorDetectionUsesGitHubScript(t *testing.T) {
	var yaml strings.Builder
	compiler := &Compiler{}

	compiler.generateDetectAgentErrorsStep(&yaml, &WorkflowData{}, NewCodexEngine())

	step := yaml.String()
	for _, expected := range []string{
		"uses: actions/github-script@",
		"setupGlobals(core, github, context, exec, io, getOctokit);",
		"const { main } = require('${{ runner.temp }}/gh-aw/actions/detect_agent_errors.cjs');",
		"await main();",
	} {
		if !strings.Contains(step, expected) {
			t.Errorf("Expected Detect agent errors step to contain %q, got:\n%s", expected, step)
		}
	}
	if strings.Contains(step, "run: node") {
		t.Errorf("Expected Detect agent errors step not to invoke node directly, got:\n%s", step)
	}
}

func TestCodexEngineRenderMCPConfig(t *testing.T) {
	engine := NewCodexEngine()

	tests := []struct {
		name     string
		tools    map[string]any
		mcpTools []string
		expected []string
	}{
		{
			name: "github tool with user_agent",
			tools: map[string]any{
				"github": map[string]any{},
			},
			mcpTools: []string{"github"},
			expected: []string{
				`cat > "${RUNNER_TEMP}/gh-aw/mcp-config/config.toml" << GH_AW_MCP_CONFIG_NORM_EOF`,
				"[history]",
				"persistence = \"none\"",
				"[otel]",
				"metrics_exporter = \"none\"",
				"",
				"[shell_environment_policy]",
				"inherit = \"core\"",
				"include_only = [\"^CODEX_API_KEY$\", \"^GITHUB_PERSONAL_ACCESS_TOKEN$\", \"^HOME$\", \"^OPENAI_API_KEY$\", \"^PATH$\"]",
				"",
				"[mcp_servers.github]",
				"user_agent = \"test-workflow\"",
				"startup_timeout_sec = 120",
				"tool_timeout_sec = 60",
				fmt.Sprintf("container = \"ghcr.io/github/github-mcp-server:%s\"", constants.DefaultGitHubMCPServerVersion),
				"env = { \"GITHUB_FEATURES\" = \"fields_param\", \"GITHUB_HOST\" = \"$GITHUB_SERVER_URL\", \"GITHUB_PERSONAL_ACCESS_TOKEN\" = \"$GH_AW_GITHUB_TOKEN\", \"GITHUB_READ_ONLY\" = \"1\", \"GITHUB_TOOLSETS\" = \"context,repos,issues,pull_requests\" }",
				"env_vars = [\"GITHUB_FEATURES\", \"GITHUB_HOST\", \"GITHUB_PERSONAL_ACCESS_TOKEN\", \"GITHUB_READ_ONLY\", \"GITHUB_TOOLSETS\"]",
				"GH_AW_MCP_CONFIG_NORM_EOF",
				"",
				"# Generate JSON config for MCP gateway",
				"GH_AW_NODE=$(which node 2>/dev/null || command -v node 2>/dev/null || echo node)",
				"cat << GH_AW_MCP_CONFIG_NORM_EOF | \"$GH_AW_NODE\" \"${RUNNER_TEMP}/gh-aw/actions/start_mcp_gateway.cjs\"",
				"{",
				"\"mcpServers\": {",
				"\"github\": {",
				fmt.Sprintf("\"container\": \"ghcr.io/github/github-mcp-server:%s\",", constants.DefaultGitHubMCPServerVersion),
				"\"env\": {",
				"\"GITHUB_FEATURES\": \"fields_param\",",
				"\"GITHUB_HOST\": \"$GITHUB_SERVER_URL\",",
				"\"GITHUB_PERSONAL_ACCESS_TOKEN\": \"$GITHUB_MCP_SERVER_TOKEN\",",
				"\"GITHUB_READ_ONLY\": \"1\",",
				"\"GITHUB_TOOLSETS\": \"context,repos,issues,pull_requests\"",
				"},",
				"\"guard-policies\": {",
				"\"allow-only\": {",
				"\"min-integrity\": \"$GITHUB_MCP_GUARD_MIN_INTEGRITY\",",
				"\"repos\": \"$GITHUB_MCP_GUARD_REPOS\"",
				"}",
				"}",
				"}",
				"},",
				"\"gateway\": {",
				"\"port\": $MCP_GATEWAY_PORT,",
				"\"domain\": \"${MCP_GATEWAY_DOMAIN}\",",
				"\"agentId\": \"${MCP_GATEWAY_AGENT_ID}\",",
				"\"payloadDir\": \"${MCP_GATEWAY_PAYLOAD_DIR}\",",
				"\"startupTimeout\": 120",
				"}",
				"}",
				"GH_AW_MCP_CONFIG_NORM_EOF",
				"",
				"# Sync converter output to writable CODEX_HOME for Codex",
				"mkdir -p /tmp/gh-aw/mcp-config",
				"cat > \"/tmp/gh-aw/mcp-config/config.toml\" << GH_AW_CODEX_SHELL_POLICY_NORM_EOF",
				"[features]",
				"plugins = false",
				"[shell_environment_policy]",
				"inherit = \"core\"",
				"include_only = [\"^CODEX_API_KEY$\", \"^GITHUB_PERSONAL_ACCESS_TOKEN$\", \"^HOME$\", \"^OPENAI_API_KEY$\", \"^PATH$\"]",
				"GH_AW_CODEX_SHELL_POLICY_NORM_EOF",
				"cat \"${RUNNER_TEMP}/gh-aw/mcp-config/config.toml\" >> \"/tmp/gh-aw/mcp-config/config.toml\"",
				"chmod 600 \"/tmp/gh-aw/mcp-config/config.toml\"",
				"mkdir -p \"${CODEX_HOME}\"",
				"if [ \"/tmp/gh-aw/mcp-config/config.toml\" != \"${CODEX_HOME}/config.toml\" ]; then cp \"/tmp/gh-aw/mcp-config/config.toml\" \"${CODEX_HOME}/config.toml\"; fi",
				"chmod 600 \"${CODEX_HOME}/config.toml\"",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			workflowData := &WorkflowData{Name: "test-workflow"}
			if err := engine.RenderMCPConfig(&yaml, tt.tools, tt.mcpTools, workflowData); err != nil {
				t.Fatalf("RenderMCPConfig returned unexpected error: %v", err)
			}

			result := yaml.String()
			// Normalize randomized heredoc delimiters before comparison
			result = normalizeHeredocDelimiters(result)
			lines := strings.Split(strings.TrimSpace(result), "\n")

			// Remove indentation from both expected and actual lines for comparison
			var normalizedResult []string
			for _, line := range lines {
				normalizedResult = append(normalizedResult, strings.TrimSpace(line))
			}

			var normalizedExpected []string
			for _, line := range tt.expected {
				normalizedExpected = append(normalizedExpected, strings.TrimSpace(line))
			}

			if len(normalizedResult) != len(normalizedExpected) {
				t.Errorf("Expected %d lines, got %d", len(normalizedExpected), len(normalizedResult))
				t.Errorf("Expected:\n%s", strings.Join(normalizedExpected, "\n"))
				t.Errorf("Got:\n%s", strings.Join(normalizedResult, "\n"))
				return
			}

			for i, expectedLine := range normalizedExpected {
				if i < len(normalizedResult) {
					actualLine := normalizedResult[i]
					if actualLine != expectedLine {
						t.Errorf("Line %d mismatch:\nExpected: %s\nActual:   %s", i+1, expectedLine, actualLine)
					}
				}
			}
		})
	}
}

func TestCodexEngineRenderMCPConfigOpenAIProxyProvider(t *testing.T) {
	engine := NewCodexEngine()

	t.Run("injects openai-proxy provider when firewall is enabled", func(t *testing.T) {
		tools := map[string]any{}
		mcpTools := []string{}
		var yaml strings.Builder
		workflowData := &WorkflowData{
			Name: "test-workflow",
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		}

		if err := engine.RenderMCPConfig(&yaml, tools, mcpTools, workflowData); err != nil {
			t.Fatalf("RenderMCPConfig returned unexpected error: %v", err)
		}

		result := yaml.String()
		expectedLines := []string{
			"model_provider = \"openai-proxy\"",
			"[model_providers.openai-proxy]",
			"name = \"OpenAI AWF proxy\"",
			fmt.Sprintf("base_url = \"http://%s:%d\"", constants.AWFAPIProxyContainerIP, constants.CodexLLMGatewayPort),
			"env_key = \"CODEX_API_KEY\"",
			"wire_api = \"responses\"",
			"requires_openai_auth = false",
			"supports_websockets = false",
		}

		for _, expected := range expectedLines {
			if !strings.Contains(result, expected) {
				t.Errorf("Expected MCP config to contain %q, got:\n%s", expected, result)
			}
		}
		if !strings.Contains(result, "awk '") {
			t.Errorf("Expected firewall-enabled config append to use awk filtering, got:\n%s", result)
		}

		normalizedResult := normalizeHeredocDelimiters(result)
		syncStart := strings.Index(normalizedResult, "cat > \"/tmp/gh-aw/mcp-config/config.toml\" << GH_AW_CODEX_SHELL_POLICY_NORM_EOF")
		if syncStart == -1 {
			t.Fatalf("Expected config sync heredoc start in generated config, got:\n%s", normalizedResult)
		}

		syncBodyStart := strings.Index(normalizedResult[syncStart:], "\n")
		if syncBodyStart == -1 {
			t.Fatalf("Expected newline after config sync heredoc start, got:\n%s", normalizedResult[syncStart:])
		}
		syncBodyOffset := syncStart + syncBodyStart + 1

		syncEnd := strings.Index(normalizedResult[syncBodyOffset:], "\n          GH_AW_CODEX_SHELL_POLICY_NORM_EOF")
		if syncEnd == -1 {
			t.Fatalf("Expected config sync heredoc end in generated config, got:\n%s", normalizedResult)
		}

		syncBlock := normalizedResult[syncBodyOffset : syncBodyOffset+syncEnd]
		modelProviderIndex := strings.Index(syncBlock, "model_provider = \"openai-proxy\"")
		shellPolicyIndex := strings.Index(syncBlock, "[shell_environment_policy]")
		if modelProviderIndex == -1 || shellPolicyIndex == -1 {
			t.Fatalf("Expected model_provider and shell_environment_policy in sync block, got:\n%s", syncBlock)
		}
		if modelProviderIndex > shellPolicyIndex {
			t.Errorf("Expected model_provider to be emitted before [shell_environment_policy] in sync block, got:\n%s", syncBlock)
		}
	})

	t.Run("routes copilot models through the Copilot gateway port", func(t *testing.T) {
		var yaml strings.Builder
		workflowData := &WorkflowData{
			Name:  "test-workflow",
			Model: "copilot/mai-code-1-flash-picker",
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		}

		if err := engine.RenderMCPConfig(&yaml, map[string]any{}, []string{}, workflowData); err != nil {
			t.Fatalf("RenderMCPConfig returned unexpected error: %v", err)
		}

		expected := fmt.Sprintf("base_url = \"http://%s:%d\"", constants.AWFAPIProxyContainerIP, constants.CopilotLLMGatewayPort)
		if result := yaml.String(); !strings.Contains(result, expected) {
			t.Errorf("Expected MCP config to contain %q, got:\n%s", expected, result)
		}
	})

	t.Run("does not inject openai-proxy provider when firewall is disabled", func(t *testing.T) {
		tools := map[string]any{}
		mcpTools := []string{}
		var yaml strings.Builder
		workflowData := &WorkflowData{Name: "test-workflow"}

		if err := engine.RenderMCPConfig(&yaml, tools, mcpTools, workflowData); err != nil {
			t.Fatalf("RenderMCPConfig returned unexpected error: %v", err)
		}

		result := yaml.String()
		if strings.Contains(result, "model_provider = \"openai-proxy\"") {
			t.Errorf("Did not expect openai-proxy provider when firewall is disabled, got:\n%s", result)
		}
		if strings.Contains(result, "awk '") {
			t.Errorf("Did not expect awk filtering when firewall is disabled, got:\n%s", result)
		}
	})
}

func TestCodexEngineOpenAIProxyProviderBaseURL(t *testing.T) {
	engine := NewCodexEngine()

	t.Run("defaults to the OpenAI gateway port", func(t *testing.T) {
		expected := "http://" + net.JoinHostPort(constants.AWFAPIProxyContainerIP, strconv.Itoa(constants.CodexLLMGatewayPort))
		workflowData := &WorkflowData{EngineConfig: &EngineConfig{ID: "codex"}}

		if actual := engine.getOpenAIProxyProviderBaseURL(workflowData); actual != expected {
			t.Errorf("Expected OpenAI proxy provider base URL %q, got %q", expected, actual)
		}
	})

	t.Run("uses the Copilot gateway port for copilot models", func(t *testing.T) {
		expected := "http://" + net.JoinHostPort(constants.AWFAPIProxyContainerIP, strconv.Itoa(constants.CopilotLLMGatewayPort))
		workflowData := &WorkflowData{EngineConfig: &EngineConfig{ID: "codex"}, Model: "copilot/mai-code-1-flash-picker"}

		if actual := engine.getOpenAIProxyProviderBaseURL(workflowData); actual != expected {
			t.Errorf("Expected Copilot proxy provider base URL %q, got %q", expected, actual)
		}
	})
}

func TestValidateCodexCopilotAwfVersion(t *testing.T) {
	workflowData := &WorkflowData{
		EngineConfig: &EngineConfig{ID: "codex"},
		Model:        "copilot/auto",
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: "v0.25.2"},
		},
	}

	if err := validateCodexCopilotAwfVersion(workflowData); err == nil || !strings.Contains(err.Error(), "requires AWF v0.25.3 or newer") {
		t.Fatalf("Expected legacy AWF version error, got %v", err)
	}

	workflowData.NetworkPermissions.Firewall.Version = "v0.25.3"
	if err := validateCodexCopilotAwfVersion(workflowData); err != nil {
		t.Fatalf("Expected minimum supported AWF version to pass, got %v", err)
	}
}

func TestCodexEngineExecutionAddsMountedMCPCLIPathSetup(t *testing.T) {
	engine := NewCodexEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		ParsedTools: &ToolsConfig{
			CLIProxy: true,
		},
		Tools: map[string]any{
			"bash": []any{"echo"},
			"my-mcp-cli": map[string]any{
				"command": "node",
				"args":    []any{"index.js"},
			},
		},
		NetworkPermissions: &NetworkPermissions{
			Allowed: []string{"defaults"},
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
	if len(steps) == 0 {
		t.Fatal("Expected execution step")
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if !strings.Contains(stepContent, "export PATH=\"${RUNNER_TEMP}/gh-aw/mcp-cli/bin:$PATH\"") {
		t.Errorf("Expected mounted MCP CLI bin directory in AWF command, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "--exclude-env CODEX_API_KEY") {
		t.Errorf("Expected CODEX_API_KEY to be excluded from AWF container env, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "--exclude-env OPENAI_API_KEY") {
		t.Errorf("Expected OPENAI_API_KEY to be excluded from AWF container env, got:\n%s", stepContent)
	}
}

func TestCodexEngineDetectionRunUsesStructuredOutputSchema(t *testing.T) {
	engine := NewCodexEngine()

	tests := []struct {
		name                        string
		isDetectionRun              bool
		expectStructuredOutputFlags bool
	}{
		{
			name:                        "detection run uses --output-schema and -o flags",
			isDetectionRun:              true,
			expectStructuredOutputFlags: true,
		},
		{
			name:                        "agent run does not include structured output flags",
			isDetectionRun:              false,
			expectStructuredOutputFlags: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name:           "test-workflow",
				IsDetectionRun: tt.isDetectionRun,
				NetworkPermissions: &NetworkPermissions{
					Allowed: []string{"defaults"},
					Firewall: &FirewallConfig{
						Enabled: true,
					},
				},
				Tools: map[string]any{
					"bash": []any{"*"},
				},
			}

			steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
			if len(steps) == 0 {
				t.Fatal("Expected execution step")
			}

			stepContent := strings.Join([]string(steps[0]), "\n")

			// Detection runs should use --output-schema and -o flags for structured output
			hasOutputSchema := strings.Contains(stepContent, "--output-schema")
			hasOutputFile := strings.Contains(stepContent, detectionResultFilePath)
			hasSchemaWrite := strings.Contains(stepContent, detectionSchemaFilePath)

			if tt.expectStructuredOutputFlags {
				if !hasOutputSchema {
					t.Errorf("Detection run: expected --output-schema in command, got:\n%s", stepContent)
				}
				if !hasOutputFile {
					t.Errorf("Detection run: expected result file path %q in command, got:\n%s", detectionResultFilePath, stepContent)
				}
				if !hasSchemaWrite {
					t.Errorf("Detection run: expected schema file path %q in command, got:\n%s", detectionSchemaFilePath, stepContent)
				}
				if !strings.Contains(stepContent, detectionResultFilePath+" --prompt-file") {
					t.Errorf("Detection run: expected space separator between -o result file and --prompt-file, got:\n%s", stepContent)
				}
			} else {
				if hasOutputSchema {
					t.Errorf("Agent run: expected no --output-schema in command, got:\n%s", stepContent)
				}
				if hasOutputFile {
					t.Errorf("Agent run: expected no result file path %q in command, got:\n%s", detectionResultFilePath, stepContent)
				}
			}

			// For detection runs, verify the schema includes required threat detection fields
			if tt.expectStructuredOutputFlags {
				for _, field := range []string{"prompt_injection", "secret_leak", "malicious_patch", "reasons"} {
					if !strings.Contains(stepContent, field) {
						t.Errorf("Detection run: expected schema to contain field %q, got:\n%s", field, stepContent)
					}
				}
			}

			// Ensure response_schema (old config flag) is never used in any mode
			if strings.Contains(stepContent, "response_schema") {
				t.Errorf("Expected no legacy response_schema config flag in command, got:\n%s", stepContent)
			}
		})
	}
}

func TestCodexEngineDetectionRunChainsSchemaWriteBeforeCodexWithoutAWF(t *testing.T) {
	engine := NewCodexEngine()

	workflowData := &WorkflowData{
		Name:           "test-workflow",
		IsDetectionRun: true,
		NetworkPermissions: &NetworkPermissions{
			Allowed: []string{"defaults"},
			Firewall: &FirewallConfig{
				Enabled: false,
			},
		},
		Tools: map[string]any{
			"bash": []any{"*"},
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
	if len(steps) == 0 {
		t.Fatal("Expected execution step")
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if !strings.Contains(stepContent, fmt.Sprintf("> %s && ", detectionSchemaFilePath)) {
		t.Errorf("Expected non-AWF detection command to chain schema write with && before codex, got:\n%s", stepContent)
	}
}

func TestCodexEngineExecutionPassesModelEnvVarIntoAWFStep(t *testing.T) {
	engine := NewCodexEngine()

	tests := []struct {
		name             string
		safeOutputs      *SafeOutputsConfig
		isEvalsRun       bool
		expectedModelEnv string
	}{
		{
			name:             "agent job uses agent model env var",
			safeOutputs:      &SafeOutputsConfig{},
			expectedModelEnv: constants.EnvVarModelAgentCodex,
		},
		{
			name:             "detection job uses detection model env var",
			safeOutputs:      nil,
			expectedModelEnv: constants.EnvVarModelDetectionCodex,
		},
		{
			name:             "evals job uses evals model env var",
			safeOutputs:      nil,
			isEvalsRun:       true,
			expectedModelEnv: constants.EnvVarModelEvalsCodex,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name: "test-workflow",
				NetworkPermissions: &NetworkPermissions{
					Allowed: []string{"defaults"},
					Firewall: &FirewallConfig{
						Enabled: true,
					},
				},
				Tools: map[string]any{
					"bash": []any{"echo"},
				},
				SafeOutputs: tt.safeOutputs,
				IsEvalsRun:  tt.isEvalsRun,
			}

			steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
			if len(steps) == 0 {
				t.Fatal("Expected execution step")
			}

			stepContent := strings.Join([]string(steps[0]), "\n")
			expectedEnvLine := tt.expectedModelEnv + ": ${{ vars." + tt.expectedModelEnv + " || vars.GH_AW_DEFAULT_MODEL_CODEX || '" + constants.CodexDefaultModel + "' }}"
			if !strings.Contains(stepContent, expectedEnvLine) {
				t.Errorf("Expected model env var to be included in AWF step env:\n%s", stepContent)
			}

			expectedModelFlag := fmt.Sprintf("${%s:+ --model \"$%s\"}", tt.expectedModelEnv, tt.expectedModelEnv)
			if !strings.Contains(stepContent, expectedModelFlag) {
				t.Errorf("Expected AWF command to use %s for --model shell expansion:\n%s", tt.expectedModelEnv, stepContent)
			}
		})
	}
}

func TestCodexEngineUserAgentIdentifierConversion(t *testing.T) {
	engine := NewCodexEngine()

	tests := []struct {
		name         string
		workflowName string
		expectedUA   string
	}{
		{
			name:         "workflow name with spaces",
			workflowName: "Test Codex Create Issue",
			expectedUA:   "test-codex-create-issue",
		},
		{
			name:         "workflow name with underscores",
			workflowName: "Test_Workflow_Name",
			expectedUA:   "test-workflow-name",
		},
		{
			name:         "already identifier format",
			workflowName: "test-workflow",
			expectedUA:   "test-workflow",
		},
		{
			name:         "empty workflow name",
			workflowName: "",
			expectedUA:   "github-agentic-workflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			workflowData := &WorkflowData{Name: tt.workflowName}

			tools := map[string]any{"github": map[string]any{}}
			mcpTools := []string{"github"}

			if err := engine.RenderMCPConfig(&yaml, tools, mcpTools, workflowData); err != nil {
				t.Fatalf("RenderMCPConfig returned unexpected error: %v", err)
			}

			result := yaml.String()
			expectedUserAgentLine := "user_agent = \"" + tt.expectedUA + "\""

			if !strings.Contains(result, expectedUserAgentLine) {
				t.Errorf("Expected MCP config to contain %q, got:\n%s", expectedUserAgentLine, result)
			}
		})
	}
}

func TestCodexEngineRenderMCPConfigUserAgentFromConfig(t *testing.T) {
	engine := NewCodexEngine()

	tests := []struct {
		name         string
		workflowName string
		configuredUA string
		expectedUA   string
		description  string
	}{
		{
			name:         "configured user_agent overrides workflow name",
			workflowName: "Test Workflow Name",
			configuredUA: "my-custom-agent",
			expectedUA:   "my-custom-agent",
			description:  "When user_agent is configured, it should be used instead of the converted workflow name",
		},
		{
			name:         "configured user_agent with spaces",
			workflowName: "test-workflow",
			configuredUA: "My Custom User Agent",
			expectedUA:   "My Custom User Agent",
			description:  "Configured user_agent should be used as-is, without identifier conversion",
		},
		{
			name:         "empty configured user_agent falls back to workflow name",
			workflowName: "Test Workflow",
			configuredUA: "",
			expectedUA:   "test-workflow",
			description:  "Empty configured user_agent should fall back to workflow name conversion",
		},
		{
			name:         "no workflow name and no configured user_agent uses default",
			workflowName: "",
			configuredUA: "",
			expectedUA:   "github-agentic-workflow",
			description:  "Should use default when neither workflow name nor user_agent is configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder

			engineConfig := &EngineConfig{
				ID: "codex",
			}
			if tt.configuredUA != "" {
				engineConfig.UserAgent = tt.configuredUA
			}

			workflowData := &WorkflowData{
				Name:         tt.workflowName,
				EngineConfig: engineConfig,
			}

			tools := map[string]any{"github": map[string]any{}}
			mcpTools := []string{"github"}

			if err := engine.RenderMCPConfig(&yaml, tools, mcpTools, workflowData); err != nil {
				t.Fatalf("RenderMCPConfig returned unexpected error: %v", err)
			}

			result := yaml.String()
			expectedUserAgentLine := "user_agent = \"" + tt.expectedUA + "\""

			if !strings.Contains(result, expectedUserAgentLine) {
				t.Errorf("Test case: %s\nExpected MCP config to contain %q, got:\n%s", tt.description, expectedUserAgentLine, result)
			}
		})
	}
}

func TestSanitizeArtifactIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name with spaces",
			input:    "Test Codex Create Issue",
			expected: "test-codex-create-issue",
		},
		{
			name:     "name with underscores",
			input:    "Test_Workflow_Name",
			expected: "test-workflow-name",
		},
		{
			name:     "name with mixed separators",
			input:    "Test Workflow_Name With Spaces",
			expected: "test-workflow-name-with-spaces",
		},
		{
			name:     "name with special characters",
			input:    "Test@Workflow#With$Special%Characters!",
			expected: "testworkflowwithspecialcharacters",
		},
		{
			name:     "name with multiple spaces",
			input:    "Test   Multiple    Spaces",
			expected: "test-multiple-spaces",
		},
		{
			name:     "empty name",
			input:    "",
			expected: "github-agentic-workflow",
		},
		{
			name:     "name with only special characters",
			input:    "@#$%!",
			expected: "github-agentic-workflow",
		},
		{
			name:     "already lowercase with hyphens",
			input:    "already-lowercase-name",
			expected: "already-lowercase-name",
		},
		{
			name:     "name with leading/trailing spaces",
			input:    "  Test Workflow  ",
			expected: "test-workflow",
		},
		{
			name:     "name with hyphens and underscores",
			input:    "Test-Workflow_Name",
			expected: "test-workflow-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeArtifactIdentifier(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeArtifactIdentifier(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCodexEngineRenderMCPConfigUserAgentWithHyphen(t *testing.T) {
	engine := NewCodexEngine()

	// Test that "user-agent" field name works
	tests := []struct {
		name             string
		engineConfigFunc func() *EngineConfig
		expectedUA       string
		description      string
	}{
		{
			name: "user-agent field gets parsed as user_agent (hyphen)",
			engineConfigFunc: func() *EngineConfig {
				// This simulates the parsing of "user-agent" from frontmatter
				// which gets stored in the UserAgent field
				return &EngineConfig{
					ID:        "codex",
					UserAgent: "custom-agent-hyphen",
				}
			},
			expectedUA:  "custom-agent-hyphen",
			description: "user-agent field with hyphen should be parsed and work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder

			workflowData := &WorkflowData{
				Name:         "test-workflow",
				EngineConfig: tt.engineConfigFunc(),
			}

			tools := map[string]any{"github": map[string]any{}}
			mcpTools := []string{"github"}

			if err := engine.RenderMCPConfig(&yaml, tools, mcpTools, workflowData); err != nil {
				t.Fatalf("RenderMCPConfig returned unexpected error: %v", err)
			}

			result := yaml.String()
			expectedUserAgentLine := "user_agent = \"" + tt.expectedUA + "\""

			if !strings.Contains(result, expectedUserAgentLine) {
				t.Errorf("Test case: %s\nExpected MCP config to contain %q, got:\n%s", tt.description, expectedUserAgentLine, result)
			}
		})
	}
}

// TestCodexEngineMCPScriptsSecrets verifies that mcp-scripts secrets are passed to the execution step
func TestCodexEngineMCPScriptsSecrets(t *testing.T) {
	engine := NewCodexEngine()

	// Create workflow data with mcp-scripts that have env secrets
	workflowData := &WorkflowData{
		Name: "test-workflow-with-mcp-scripts",
		Features: map[string]any{
			"mcp-scripts": true, // Feature flag is optional now
		},
		MCPScripts: &MCPScriptsConfig{
			Tools: map[string]*MCPScriptToolConfig{
				"gh": {
					Name:        "gh",
					Description: "Execute gh CLI command",
					Run:         "gh $INPUT_ARGS",
					Env: map[string]string{
						"GH_TOKEN": "${{ github.token }}",
					},
				},
				"api-call": {
					Name:        "api-call",
					Description: "Call an API",
					Script:      "return fetch(url);",
					Env: map[string]string{
						"API_KEY": "${{ secrets.API_KEY }}",
					},
				},
			},
		},
	}

	// Get execution steps
	execSteps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
	if len(execSteps) == 0 {
		t.Fatal("Expected at least one execution step")
	}

	// Join all step lines to check content
	stepContent := strings.Join(execSteps[0], "\n")

	// Verify GH_TOKEN is in the env section
	if !strings.Contains(stepContent, "GH_TOKEN: ${{ github.token }}") {
		t.Errorf("Expected GH_TOKEN environment variable in step content:\n%s", stepContent)
	}

	// Verify API_KEY is in the env section
	if !strings.Contains(stepContent, "API_KEY: ${{ secrets.API_KEY }}") {
		t.Errorf("Expected API_KEY environment variable in step content:\n%s", stepContent)
	}
}

// TestCodexEngineHttpMCPServerRendered verifies that HTTP MCP servers
// are properly rendered in TOML format for Codex
func TestCodexEngineHttpMCPServerRendered(t *testing.T) {
	engine := NewCodexEngine()

	tests := []struct {
		name          string
		tools         map[string]any
		mcpTools      []string
		shouldContain []string
	}{
		{
			name: "HTTP MCP server should be rendered with url",
			tools: map[string]any{
				"gh-aw": map[string]any{
					"type": "http",
					"url":  "http://localhost:8765",
				},
			},
			mcpTools: []string{"gh-aw"},
			// localhost URLs are rewritten to host.docker.internal when firewall is enabled (default)
			shouldContain: []string{
				"[mcp_servers.gh-aw]",
				"url = \"http://host.docker.internal:8765\"",
			},
		},
		{
			name: "HTTP MCP server inferred from url field",
			tools: map[string]any{
				"my-http-server": map[string]any{
					"url": "https://api.example.com/mcp",
				},
			},
			mcpTools: []string{"my-http-server"},
			shouldContain: []string{
				"[mcp_servers.my-http-server]",
				"url = \"https://api.example.com/mcp\"",
			},
		},
		{
			name: "HTTP MCP server with headers",
			tools: map[string]any{
				"api-server": map[string]any{
					"type": "http",
					"url":  "https://api.example.com/mcp",
					"headers": map[string]any{
						"Authorization": "Bearer token123",
						"X-Custom":      "value",
					},
				},
			},
			mcpTools: []string{"api-server"},
			shouldContain: []string{
				"[mcp_servers.api-server]",
				"url = \"https://api.example.com/mcp\"",
				"http_headers = {",
				"\"Authorization\" = \"Bearer token123\"",
				"\"X-Custom\" = \"value\"",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			workflowData := &WorkflowData{Name: "test-workflow"}
			if err := engine.RenderMCPConfig(&yaml, tt.tools, tt.mcpTools, workflowData); err != nil {
				t.Fatalf("RenderMCPConfig returned unexpected error: %v", err)
			}

			result := yaml.String()

			for _, expected := range tt.shouldContain {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected MCP config to contain %q, got:\n%s", expected, result)
				}
			}
		})
	}
}

func TestCodexEngineSkipInstallationWithCommand(t *testing.T) {
	engine := NewCodexEngine()

	// Test with custom command - should skip installation
	workflowData := &WorkflowData{
		EngineConfig: &EngineConfig{Command: "/usr/local/bin/custom-codex"},
	}
	steps := engine.GetInstallationSteps(workflowData)

	if len(steps) != 0 {
		t.Errorf("Expected 0 installation steps when command is specified, got %d", len(steps))
	}
}

func TestCodexEngineEnvOverridesTokenExpression(t *testing.T) {
	engine := NewCodexEngine()

	t.Run("engine env overrides default token expression", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Env: map[string]string{
					"CODEX_API_KEY": "${{ secrets.MY_ORG_CODEX_KEY }}",
				},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}

		stepContent := strings.Join([]string(steps[0]), "\n")

		// engine.env override should replace the default token expression
		if !strings.Contains(stepContent, "CODEX_API_KEY: ${{ secrets.MY_ORG_CODEX_KEY }}") {
			t.Errorf("Expected engine.env to override CODEX_API_KEY, got:\n%s", stepContent)
		}
		if strings.Contains(stepContent, "CODEX_API_KEY: ${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}") {
			t.Errorf("Default CODEX_API_KEY expression should be replaced by engine.env override, got:\n%s", stepContent)
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

func TestCodexEngineWebSearch(t *testing.T) {
	engine := NewCodexEngine()

	t.Run("web search disabled by default when tool not specified", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
		}
		steps := engine.GetExecutionSteps(workflowData, "test-log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}
		stepContent := strings.Join([]string(steps[0]), "\n")
		if !strings.Contains(stepContent, `-c web_search="disabled"`) {
			t.Errorf(`Expected -c web_search="disabled" config when web-search tool is not specified, got:\n%s`, stepContent)
		}
		if strings.Contains(stepContent, "--no-search") {
			t.Errorf("Expected no --no-search flag (it does not exist), got:\n%s", stepContent)
		}
		if strings.Contains(stepContent, "--search") {
			t.Errorf("Expected no --search flag (it does not exist), got:\n%s", stepContent)
		}
	})

	t.Run("web search enabled when tool is specified", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			ParsedTools: &ToolsConfig{
				WebSearch: &WebSearchToolConfig{},
			},
		}
		steps := engine.GetExecutionSteps(workflowData, "test-log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}
		stepContent := strings.Join([]string(steps[0]), "\n")
		if strings.Contains(stepContent, `-c web_search="disabled"`) {
			t.Errorf(`Expected no -c web_search="disabled" config when web-search tool is specified, got:\n%s`, stepContent)
		}
		if strings.Contains(stepContent, "--no-search") {
			t.Errorf("Expected no --no-search flag (it does not exist), got:\n%s", stepContent)
		}
		if strings.Contains(stepContent, "--search") {
			t.Errorf("Expected no --search flag (it does not exist), got:\n%s", stepContent)
		}
	})
}

func TestCodexEngineBashDisabled(t *testing.T) {
	engine := NewCodexEngine()

	t.Run("shell_tool not disabled by default when bash is not fully disabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
		}
		steps := engine.GetExecutionSteps(workflowData, "test-log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}
		stepContent := strings.Join([]string(steps[0]), "\n")
		if strings.Contains(stepContent, "features.shell_tool=false") {
			t.Errorf("Expected no features.shell_tool=false config when bash is not disabled, got:\n%s", stepContent)
		}
	})

	t.Run("adds features.shell_tool=false when BashDisabled is set", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:         "test-workflow",
			BashDisabled: true,
		}
		steps := engine.GetExecutionSteps(workflowData, "test-log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}
		stepContent := strings.Join([]string(steps[0]), "\n")
		if !strings.Contains(stepContent, `-c features.shell_tool=false`) {
			t.Errorf(`Expected -c features.shell_tool=false config when bash is fully disabled, got:\n%s`, stepContent)
		}
	})
}

func TestCodexEnginePluginConfig(t *testing.T) {
	engine := NewCodexEngine()

	t.Run("disables plugins when none are declared", func(t *testing.T) {
		workflowData := &WorkflowData{Name: "test-workflow"}
		var yaml strings.Builder
		if err := engine.RenderMCPConfig(&yaml, map[string]any{}, nil, workflowData); err != nil {
			t.Fatalf("RenderMCPConfig returned unexpected error: %v", err)
		}
		if config := yaml.String(); !strings.Contains(config, "[features]") || !strings.Contains(config, "plugins = false") {
			t.Errorf("Expected generated Codex TOML to disable plugins when none are declared, got:\n%s", config)
		}
	})

	t.Run("keeps plugins enabled when one is declared", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:    "test-workflow",
			Plugins: []string{"octo-org/example@main"},
		}
		var yaml strings.Builder
		if err := engine.RenderMCPConfig(&yaml, map[string]any{}, nil, workflowData); err != nil {
			t.Fatalf("RenderMCPConfig returned unexpected error: %v", err)
		}
		if config := yaml.String(); strings.Contains(config, "plugins = false") {
			t.Errorf("Expected generated Codex TOML to keep plugins enabled when one is declared, got:\n%s", config)
		}
	})

	t.Run("merges plugin setting into a custom features table", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Config: "[features]\n shell_tool = false\n",
			},
		}
		var yaml strings.Builder
		if err := engine.RenderMCPConfig(&yaml, map[string]any{}, nil, workflowData); err != nil {
			t.Fatalf("RenderMCPConfig returned unexpected error: %v", err)
		}
		config := yaml.String()
		shellPolicyStart := strings.Index(config, "# Sync converter output")
		shellPolicyEnd := strings.Index(config, "# Append engine-level custom Codex config")
		shellPolicy := config[shellPolicyStart:shellPolicyEnd]
		if strings.Contains(shellPolicy, "[features]") || !strings.Contains(config, "[features]\nplugins = false\n shell_tool = false") {
			t.Errorf("Expected plugin setting to be merged into custom Codex features table, got:\n%s", config)
		}
	})

	t.Run("accepts nil workflow data", func(t *testing.T) {
		var yaml strings.Builder
		if err := engine.RenderMCPConfig(&yaml, map[string]any{}, nil, nil); err != nil {
			t.Fatalf("RenderMCPConfig returned unexpected error: %v", err)
		}
	})
}

func TestCodexEngineWebFetch(t *testing.T) {
	engine := NewCodexEngine()

	t.Run("disables fetch by default when web-fetch tool not specified", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
		}
		steps := engine.GetExecutionSteps(workflowData, "test-log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}
		stepContent := strings.Join([]string(steps[0]), "\n")
		if !strings.Contains(stepContent, `-c fetch="disabled"`) {
			t.Errorf(`Expected -c fetch="disabled" config when web-fetch tool is not specified, got:\n%s`, stepContent)
		}
	})

	t.Run("fetch tool enabled when web-fetch tool is specified", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			ParsedTools: &ToolsConfig{
				WebFetch: &WebFetchToolConfig{},
			},
		}
		steps := engine.GetExecutionSteps(workflowData, "test-log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}
		stepContent := strings.Join([]string(steps[0]), "\n")
		if strings.Contains(stepContent, `-c fetch="disabled"`) {
			t.Errorf(`Expected no -c fetch="disabled" config when web-fetch tool is specified, got:\n%s`, stepContent)
		}
	})
}

func TestCodexEngineWithExpressionVersion(t *testing.T) {
	engine := NewCodexEngine()

	expressionVersion := "${{ inputs.engine-version }}"
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:      "codex",
			Version: expressionVersion,
		},
	}

	installSteps := engine.GetInstallationSteps(workflowData)

	// Find the npm install step
	var installStep string
	for _, step := range installSteps {
		stepContent := strings.Join(step, "\n")
		if strings.Contains(stepContent, "npm install") {
			installStep = stepContent
			break
		}
	}

	if installStep == "" {
		t.Fatal("Could not find npm install step")
	}

	// Should use ENGINE_VERSION env var for injection safety
	if !strings.Contains(installStep, "ENGINE_VERSION: "+expressionVersion) {
		t.Errorf("Expected ENGINE_VERSION env var in install step, got:\n%s", installStep)
	}

	// Should reference env var in command
	if !strings.Contains(installStep, `"${ENGINE_VERSION}"`) {
		t.Errorf(`Expected "$ENGINE_VERSION" in npm install command, got:\n%s`, installStep)
	}

	// Should NOT embed expression directly in npm install command
	if strings.Contains(installStep, "@openai/codex@"+expressionVersion) {
		t.Errorf("Expression should NOT be embedded directly in npm install command, got:\n%s", installStep)
	}
}

func TestCodexEngineGetHarnessScriptName(t *testing.T) {
	engine := NewCodexEngine()
	if engine.GetHarnessScriptName() != "codex_harness.cjs" {
		t.Errorf("Expected 'codex_harness.cjs', got '%s'", engine.GetHarnessScriptName())
	}
}

func TestCodexEngineExecutionUsesHarness(t *testing.T) {
	engine := NewCodexEngine()

	workflowData := &WorkflowData{
		Name: "test-workflow",
	}
	steps := engine.GetExecutionSteps(workflowData, "test-log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step for Codex execution, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// Default execution should use the codex_harness.cjs
	if !strings.Contains(stepContent, "codex_harness.cjs") {
		t.Errorf("Expected codex_harness.cjs in execution step, got:\n%s", stepContent)
	}

	// Harness should appear before the prompt file argument
	harnessIdx := strings.Index(stepContent, "codex_harness.cjs")
	promptFileIdx := strings.Index(stepContent, "--prompt-file")
	if harnessIdx == -1 {
		t.Fatal("Could not find codex_harness.cjs in step")
	}
	if promptFileIdx == -1 {
		t.Fatal("Could not find --prompt-file in step")
	}
	if harnessIdx > promptFileIdx {
		t.Error("Expected codex_harness.cjs to appear before --prompt-file")
	}

	// Should use --prompt-file instead of $INSTRUCTION when harness is active
	if !strings.Contains(stepContent, "--prompt-file") {
		t.Errorf("Expected --prompt-file in harness-wrapped execution step, got:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "\"$INSTRUCTION\"") {
		t.Errorf("Expected no $INSTRUCTION variable when harness is active, got:\n%s", stepContent)
	}
}

func TestCodexEngineExecutionCustomHarness(t *testing.T) {
	engine := NewCodexEngine()

	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:            "codex",
			HarnessScript: "custom_codex_harness.cjs",
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "test-log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 step for Codex execution, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// Should use custom harness script
	if !strings.Contains(stepContent, "custom_codex_harness.cjs") {
		t.Errorf("Expected custom_codex_harness.cjs in execution step, got:\n%s", stepContent)
	}

	// Should NOT use the default harness path
	if strings.Contains(stepContent, "actions/codex_harness.cjs") {
		t.Errorf("Expected default harness path to be overridden, got:\n%s", stepContent)
	}
}

// TestCodexEngineForwardsSafeOutputsInputEnvVars verifies that GH_AW_INPUT_* variables
// from the safe-outputs config are included in the Codex execution step env.
// Codex uses the TOML-based MCP setup; the TOML env_vars list references these vars
// so the safe-outputs container can resolve ${GH_AW_INPUT_…} placeholders in config.json.
// AWF passes them into the sandbox via --env-all, enabling the forwarding chain.
func TestCodexEngineForwardsSafeOutputsInputEnvVars(t *testing.T) {
	engine := NewCodexEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				TargetRepoSlug:       "${{ inputs.owner }}/${{ inputs.repo }}",
			},
		},
		SafeOutputsInputEnvVars: map[string]string{
			"GH_AW_INPUT_OWNER": "${{ inputs.owner }}",
			"GH_AW_INPUT_REPO":  "${{ inputs.repo }}",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "test-log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}
	stepContent := strings.Join([]string(steps[0]), "\n")

	if !strings.Contains(stepContent, "GH_AW_INPUT_OWNER: ${{ inputs.owner }}") {
		t.Errorf("Expected GH_AW_INPUT_OWNER in step env for TOML env_vars forwarding, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "GH_AW_INPUT_REPO: ${{ inputs.repo }}") {
		t.Errorf("Expected GH_AW_INPUT_REPO in step env for TOML env_vars forwarding, got:\n%s", stepContent)
	}
}

// TestCodexEngineHttpMCPHeaderSecretsUseEnvVars verifies that secret and env expressions
// in HTTP MCP headers are rendered as shell environment variable references in the TOML
// config instead of being interpolated directly into the run block (RGS-008).
func TestCodexEngineHttpMCPHeaderSecretsUseEnvVars(t *testing.T) {
	engine := NewCodexEngine()

	tools := map[string]any{
		"datadog": map[string]any{
			"type": "http",
			"url":  "https://mcp.datadoghq.com/api/unstable/mcp-server/mcp",
			"headers": map[string]any{
				"DD_API_KEY":         "${{ secrets.DD_API_KEY }}",
				"DD_APPLICATION_KEY": "${{ secrets.DD_APPLICATION_KEY || secrets.DD_APP_KEY }}",
				"DD_SITE":            "${{ secrets.DD_SITE || 'datadoghq.com' }}",
			},
		},
	}

	var yaml strings.Builder
	workflowData := &WorkflowData{Name: "test-workflow"}
	if err := engine.RenderMCPConfig(&yaml, tools, []string{"datadog"}, workflowData); err != nil {
		t.Fatalf("RenderMCPConfig returned unexpected error: %v", err)
	}

	result := yaml.String()

	if strings.Contains(result, "${{ secrets.") {
		t.Errorf("Expected no secret expressions in MCP config, got:\n%s", result)
	}

	expected := []string{
		`"DD_API_KEY" = "${DD_API_KEY}"`,
		`"DD_APPLICATION_KEY" = "${DD_APPLICATION_KEY}"`,
		`"DD_SITE" = "${DD_SITE}"`,
	}
	for _, want := range expected {
		if !strings.Contains(result, want) {
			t.Errorf("Expected MCP config to contain %q, got:\n%s", want, result)
		}
	}
}
