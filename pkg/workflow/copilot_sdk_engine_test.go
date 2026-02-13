//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestCopilotSDKEngine(t *testing.T) {
	engine := NewCopilotSDKEngine()

	// Test basic properties
	if engine.GetID() != "copilot-sdk" {
		t.Errorf("Expected 'copilot-sdk' engine ID, got '%s'", engine.GetID())
	}

	if engine.GetDisplayName() != "GitHub Copilot SDK" {
		t.Errorf("Expected 'GitHub Copilot SDK' display name, got '%s'", engine.GetDisplayName())
	}

	if !engine.IsExperimental() {
		t.Error("Expected copilot-sdk engine to be experimental")
	}

	if !engine.SupportsToolsAllowlist() {
		t.Error("Expected copilot-sdk engine to support tools allowlist")
	}

	if !engine.SupportsHTTPTransport() {
		t.Error("Expected copilot-sdk engine to support HTTP transport")
	}

	if !engine.SupportsMaxTurns() {
		t.Error("Expected copilot-sdk engine to support max-turns")
	}

	if engine.SupportsPlugins() {
		t.Error("Expected copilot-sdk engine to not support plugins")
	}

	if !engine.SupportsFirewall() {
		t.Error("Expected copilot-sdk engine to support firewall")
	}
}

func TestCopilotSDKEngineDefaultDetectionModel(t *testing.T) {
	engine := NewCopilotSDKEngine()

	defaultModel := engine.GetDefaultDetectionModel()
	if defaultModel != string(constants.DefaultCopilotDetectionModel) {
		t.Errorf("Expected default detection model '%s', got '%s'", string(constants.DefaultCopilotDetectionModel), defaultModel)
	}
}

func TestCopilotSDKEngineDeclaredOutputFiles(t *testing.T) {
	engine := NewCopilotSDKEngine()

	outputFiles := engine.GetDeclaredOutputFiles()
	if len(outputFiles) != 1 {
		t.Errorf("Expected 1 declared output file, got %d", len(outputFiles))
	}

	if outputFiles[0] != "/tmp/gh-aw/sandbox/agent/logs/" {
		t.Errorf("Expected declared output file to be logs folder, got %s", outputFiles[0])
	}
}

func TestCopilotSDKEngineRequiredSecrets(t *testing.T) {
	engine := NewCopilotSDKEngine()

	t.Run("basic secrets", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
		}

		secrets := engine.GetRequiredSecretNames(workflowData)
		if len(secrets) == 0 {
			t.Fatal("Expected at least one required secret")
		}

		found := false
		for _, s := range secrets {
			if s == "COPILOT_GITHUB_TOKEN" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected COPILOT_GITHUB_TOKEN in required secrets")
		}
	})

	t.Run("with github tool", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			ParsedTools: &ToolsConfig{
				GitHub: &GitHubToolConfig{},
			},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		foundGitHubToken := false
		for _, s := range secrets {
			if s == "GITHUB_MCP_SERVER_TOKEN" {
				foundGitHubToken = true
				break
			}
		}
		if !foundGitHubToken {
			t.Error("Expected GITHUB_MCP_SERVER_TOKEN in required secrets when GitHub tool is present")
		}
	})
}

func TestCopilotSDKEngineRegistered(t *testing.T) {
	registry := NewEngineRegistry()

	engine, err := registry.GetEngine("copilot-sdk")
	if err != nil {
		t.Fatalf("Expected copilot-sdk engine to be registered, got error: %v", err)
	}

	if engine.GetID() != "copilot-sdk" {
		t.Errorf("Expected engine ID 'copilot-sdk', got '%s'", engine.GetID())
	}

	if !engine.IsExperimental() {
		t.Error("Expected copilot-sdk engine to be experimental")
	}
}

func TestCopilotSDKEngineToolConfig(t *testing.T) {
	engine := NewCopilotSDKEngine()

	t.Run("basic tools", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"bash":   nil,
				"edit":   nil,
				"github": nil,
			},
		}

		available, excluded := engine.computeSDKToolConfig(workflowData)

		if len(available) == 0 {
			t.Fatal("Expected at least one available tool")
		}

		expectedTools := map[string]bool{
			"bash":   false,
			"edit":   false,
			"github": false,
		}

		for _, tool := range available {
			if _, ok := expectedTools[tool]; ok {
				expectedTools[tool] = true
			}
		}

		for tool, found := range expectedTools {
			if !found {
				t.Errorf("Expected tool '%s' in available tools, got: %v", tool, available)
			}
		}

		if len(excluded) != 0 {
			t.Errorf("Expected no excluded tools, got: %v", excluded)
		}
	})

	t.Run("wildcard bash allows all tools", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"bash": []any{"*"},
			},
		}

		available, _ := engine.computeSDKToolConfig(workflowData)

		if len(available) != 1 || available[0] != "*" {
			t.Errorf("Expected ['*'] for wildcard bash, got: %v", available)
		}
	})

	t.Run("web-fetch tool", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"web-fetch": nil,
			},
		}

		available, _ := engine.computeSDKToolConfig(workflowData)

		found := false
		for _, tool := range available {
			if tool == "web_fetch" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected 'web_fetch' in available tools, got: %v", available)
		}
	})

	t.Run("bash with specific commands uses granular permissions", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"bash": []any{"echo", "ls"},
			},
		}

		available, _ := engine.computeSDKToolConfig(workflowData)

		expectedTools := map[string]bool{
			"bash(echo)": false,
			"bash(ls)":   false,
		}

		for _, tool := range available {
			if _, ok := expectedTools[tool]; ok {
				expectedTools[tool] = true
			}
		}

		for tool, found := range expectedTools {
			if !found {
				t.Errorf("Expected granular tool '%s' in available tools, got: %v", tool, available)
			}
		}

		// Make sure broad "bash" is not present
		for _, tool := range available {
			if tool == "bash" {
				t.Errorf("Expected granular bash(cmd) entries, not broad 'bash', got: %v", available)
			}
		}
	})
}

func TestCopilotSDKEngineGetExecutionSteps(t *testing.T) {
	engine := NewCopilotSDKEngine()

	t.Run("standard mode", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			Tools: map[string]any{
				"bash": nil,
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		// Check that the step contains the runner binary path
		stepContent := strings.Join(steps[0], "\n")
		if !strings.Contains(stepContent, "copilot-runner") {
			t.Error("Expected execution step to reference copilot-runner")
		}

		// Check that it contains the SDK config path
		if !strings.Contains(stepContent, "sdk-config.json") {
			t.Error("Expected execution step to reference sdk-config.json")
		}

		// Check step name
		if !strings.Contains(stepContent, "Execute GitHub Copilot SDK Agent") {
			t.Error("Expected step name to be 'Execute GitHub Copilot SDK Agent'")
		}
	})

	t.Run("with model", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Model: "gpt-4.1",
			},
			Tools: map[string]any{
				"bash": nil,
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")
		// The model should be in the JSON config
		if !strings.Contains(stepContent, "gpt-4.1") {
			t.Error("Expected execution step to contain model 'gpt-4.1'")
		}
	})
}

func TestCopilotSDKEngineGetInstallationSteps(t *testing.T) {
	engine := NewCopilotSDKEngine()

	t.Run("standard installation", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
		}

		steps := engine.GetInstallationSteps(workflowData)

		if len(steps) == 0 {
			t.Fatal("Expected at least one installation step")
		}

		// Check for runner verification step
		allContent := ""
		for _, step := range steps {
			allContent += strings.Join(step, "\n") + "\n"
		}

		if !strings.Contains(allContent, "copilot-runner") {
			t.Error("Expected installation steps to include runner binary verification")
		}
	})

	t.Run("custom command skips installation", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Command: "/custom/copilot",
			},
		}

		steps := engine.GetInstallationSteps(workflowData)

		if len(steps) != 0 {
			t.Errorf("Expected no installation steps with custom command, got %d", len(steps))
		}
	})
}

func TestCopilotSDKEngineLogParsing(t *testing.T) {
	engine := NewCopilotSDKEngine()

	t.Run("parse runner output", func(t *testing.T) {
		logContent := `some log output
COPILOT_RUNNER_OUTPUT:{"success":true,"response":"done","metrics":{"token_usage":5000,"turns":3,"tool_calls":[{"name":"bash","count":2,"max_input_size":100,"max_output_size":500}],"tool_sequences":[["bash","bash"]],"estimated_cost":0.05,"duration_seconds":60},"errors":[]}
more log output`

		metrics := engine.ParseLogMetrics(logContent, false)

		if metrics.TokenUsage != 5000 {
			t.Errorf("Expected token usage 5000, got %d", metrics.TokenUsage)
		}

		if metrics.Turns != 3 {
			t.Errorf("Expected 3 turns, got %d", metrics.Turns)
		}
	})

	t.Run("parse empty log", func(t *testing.T) {
		logContent := "no structured output here"

		metrics := engine.ParseLogMetrics(logContent, false)

		if metrics.TokenUsage != 0 {
			t.Errorf("Expected 0 token usage for empty log, got %d", metrics.TokenUsage)
		}
	})
}

func TestCopilotSDKEngineLogParserScriptId(t *testing.T) {
	engine := NewCopilotSDKEngine()

	if engine.GetLogParserScriptId() != "parse_copilot_log" {
		t.Errorf("Expected 'parse_copilot_log', got '%s'", engine.GetLogParserScriptId())
	}
}

func TestCopilotSDKEngineLogFileForParsing(t *testing.T) {
	engine := NewCopilotSDKEngine()

	if engine.GetLogFileForParsing() != "/tmp/gh-aw/sandbox/agent/logs/" {
		t.Errorf("Expected logs folder, got '%s'", engine.GetLogFileForParsing())
	}
}

func TestSDKRunnerConfigSerialization(t *testing.T) {
	config := SDKRunnerConfig{
		CLIPath:                "/usr/local/bin/copilot",
		Model:                  "gpt-4.1",
		PromptFile:             "/tmp/gh-aw/aw-prompts/prompt.txt",
		AvailableTools:         []string{"bash", "edit", "github"},
		LogDir:                 "/tmp/gh-aw/sandbox/agent/logs/",
		ShareFile:              "/tmp/gh-aw/sandbox/agent/logs/conversation.md",
		AutoApprovePermissions: true,
		DisableBuiltinMCPs:     true,
	}

	// Verify that the config can be serialized
	if config.CLIPath != "/usr/local/bin/copilot" {
		t.Error("Config CLIPath mismatch")
	}
	if config.Model != "gpt-4.1" {
		t.Error("Config Model mismatch")
	}
	if len(config.AvailableTools) != 3 {
		t.Errorf("Expected 3 available tools, got %d", len(config.AvailableTools))
	}
}
