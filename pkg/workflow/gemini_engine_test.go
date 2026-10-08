//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiEngine(t *testing.T) {
	engine := NewGeminiEngine()

	t.Run("engine identity", func(t *testing.T) {
		assert.Equal(t, "gemini", engine.GetID(), "Engine ID should be 'gemini'")
		assert.Equal(t, "Google Gemini CLI", engine.GetDisplayName(), "Display name should be 'Google Gemini CLI'")
		assert.NotEmpty(t, engine.GetDescription(), "Description should not be empty")
		assert.False(t, engine.IsExperimental(), "Gemini engine should not be experimental")
	})

	t.Run("capabilities", func(t *testing.T) {
		capabilities := engine.GetCapabilities()
		assert.True(t, capabilities.ToolsAllowlist, "Should support tools allowlist")
		assert.True(t, capabilities.MaxTurns, "Should support max turns")
		assert.False(t, capabilities.WebSearch, "Should not support built-in web search")
	})

	t.Run("required secrets", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:        "test",
			ParsedTools: &ToolsConfig{},
			Tools:       map[string]any{},
		}
		secrets := engine.GetRequiredSecretNames(workflowData)
		assert.Contains(t, secrets, "GEMINI_API_KEY", "Should require GEMINI_API_KEY")
	})

	t.Run("required secrets with MCP servers", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test",
			ParsedTools: &ToolsConfig{
				GitHub: &GitHubToolConfig{},
			},
			Tools: map[string]any{
				"github": map[string]any{},
			},
		}
		secrets := engine.GetRequiredSecretNames(workflowData)
		assert.Contains(t, secrets, "GEMINI_API_KEY", "Should require GEMINI_API_KEY")
		assert.Contains(t, secrets, "MCP_GATEWAY_AGENT_ID", "Should require MCP_GATEWAY_AGENT_ID when MCP servers present")
		assert.Contains(t, secrets, "GITHUB_MCP_SERVER_TOKEN", "Should require GITHUB_MCP_SERVER_TOKEN for GitHub tool")
	})

	t.Run("declared output files", func(t *testing.T) {
		outputFiles := engine.GetDeclaredOutputFiles()
		require.Len(t, outputFiles, 1, "Should declare one output file path")
		assert.Equal(t, "/tmp/gh-aw/gemini-client-error-*.json", outputFiles[0], "Should declare Gemini error log wildcard path under /tmp/gh-aw/")
	})

	t.Run("pre-bundle steps move files to /tmp/gh-aw/", func(t *testing.T) {
		workflowData := &WorkflowData{Name: "test-workflow"}
		steps := engine.GetPreBundleSteps(workflowData)
		require.Len(t, steps, 1, "Should return exactly one pre-bundle step")

		stepContent := strings.Join(steps[0], "\n")
		assert.Contains(t, stepContent, "Move Gemini error files", "Step name should describe move operation")
		assert.Contains(t, stepContent, "mv /tmp/gemini-client-error-*.json /tmp/gh-aw/", "Step should move files to /tmp/gh-aw/")
		assert.Contains(t, stepContent, "if: always()", "Step should run always so files are captured on failure")
	})
}

func TestGeminiEngineInstallation(t *testing.T) {
	engine := NewGeminiEngine()

	t.Run("standard installation", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
		}

		steps := engine.GetInstallationSteps(workflowData)
		require.NotEmpty(t, steps, "Should generate installation steps")

		// Should have at least: Node.js setup + Install Gemini
		// (secret validation is now in the activation job via GetSecretValidationStep)
		assert.GreaterOrEqual(t, len(steps), 2, "Should have at least 2 installation steps")

		// Verify first step is Node.js setup
		if len(steps) > 0 && len(steps[0]) > 0 {
			stepContent := strings.Join(steps[0], "\n")
			assert.Contains(t, stepContent, "Setup Node.js", "First step should setup Node.js")
		}

		// Verify second step is Install Gemini CLI
		if len(steps) > 1 && len(steps[1]) > 0 {
			stepContent := strings.Join(steps[1], "\n")
			assert.Contains(t, stepContent, "Install Gemini CLI", "Second step should install Gemini CLI")
			assert.Contains(t, stepContent, "@google/gemini-cli", "Should install @google/gemini-cli package")
			assert.NotContains(t, stepContent, "NPM_CONFIG_MIN_RELEASE_AGE", "Gemini installation should not set npm release-age cooldown")
		}
	})

	t.Run("custom command skips installation", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Command: "/custom/gemini",
			},
		}

		steps := engine.GetInstallationSteps(workflowData)
		assert.Empty(t, steps, "Should skip installation when custom command is specified")
	})

	t.Run("with firewall", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			NetworkPermissions: &NetworkPermissions{
				Allowed: []string{"defaults"},
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		steps := engine.GetInstallationSteps(workflowData)
		require.NotEmpty(t, steps, "Should generate installation steps")

		// Should include AWF installation step
		hasAWFInstall := false
		for _, step := range steps {
			stepContent := strings.Join(step, "\n")
			if strings.Contains(stepContent, "awf") || strings.Contains(stepContent, "firewall") {
				hasAWFInstall = true
				break
			}
		}
		assert.True(t, hasAWFInstall, "Should include AWF installation step when firewall is enabled")
	})
}

func TestGeminiEngineExecution(t *testing.T) {
	engine := NewGeminiEngine()

	t.Run("basic execution", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")

		// steps[0] = Write Gemini Config, steps[1] = Execute Gemini CLI
		stepContent := strings.Join(steps[1], "\n")

		assert.Contains(t, stepContent, "name: Execute Gemini CLI", "Should have correct step name")
		assert.Contains(t, stepContent, "id: agentic_execution", "Should have agentic_execution ID")
		assert.Contains(t, stepContent, "gemini", "Should invoke gemini command")
		assert.Contains(t, stepContent, "--yolo", "Should include --yolo flag for auto-approving tool executions")
		assert.Contains(t, stepContent, "--skip-trust", "Should include --skip-trust flag to prevent workspace trust check from overriding --yolo")
		assert.Contains(t, stepContent, "--output-format stream-json", "Should use streaming JSON output format")
		assert.Contains(t, stepContent, `--prompt "$(cat /tmp/gh-aw/aw-prompts/prompt.txt)"`, "Should include prompt argument with correct shell quoting")
		assert.Contains(t, stepContent, "shell_harness.cjs", "Should run the CLI through the shared shell harness")
		assert.Contains(t, stepContent, "/tmp/test.log", "Should include log file")
		assert.Contains(t, stepContent, "GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}", "Should set GEMINI_API_KEY env var")
		assert.Contains(t, stepContent, "GH_AW_TIMEOUT_MINUTES: 20", "Should expose the step timeout to the shared harness")
	})

	t.Run("with model", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:         "test-workflow",
			Model:        "gemini-1.5-pro",
			EngineConfig: &EngineConfig{},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")

		stepContent := strings.Join(steps[1], "\n")

		// Model is passed via the native GEMINI_MODEL env var (not as a --model flag)
		assert.Contains(t, stepContent, "GEMINI_MODEL: gemini-1.5-pro", "Should set GEMINI_MODEL env var")
		assert.NotContains(t, stepContent, "--model gemini-1.5-pro", "Should not embed model in command")
	})

	t.Run("with MCP servers", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			ParsedTools: &ToolsConfig{
				GitHub: &GitHubToolConfig{},
			},
			Tools: map[string]any{
				"github": map[string]any{},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")

		stepContent := strings.Join(steps[1], "\n")

		// Gemini CLI reads MCP config from .gemini/settings.json, not --mcp-config flag
		assert.NotContains(t, stepContent, "--mcp-config", "Should NOT include --mcp-config flag (Gemini CLI does not support it)")
		assert.Contains(t, stepContent, "GH_AW_MCP_CONFIG: ${{ github.workspace }}/.gemini/settings.json", "Should set MCP config env var to Gemini settings.json path")
	})

	t.Run("with custom command", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Command: "/custom/gemini",
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")

		stepContent := strings.Join(steps[1], "\n")

		assert.Contains(t, stepContent, "/custom/gemini", "Should use custom command")
	})

	t.Run("environment variables", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")

		stepContent := strings.Join(steps[1], "\n")

		assert.Contains(t, stepContent, "GEMINI_API_KEY:", "Should include GEMINI_API_KEY")
		assert.Contains(t, stepContent, "GH_AW_PROMPT:", "Should include GH_AW_PROMPT")
		assert.Contains(t, stepContent, "GITHUB_WORKSPACE:", "Should include GITHUB_WORKSPACE")
		assert.Contains(t, stepContent, "DEBUG: gemini-cli:*", "Should include DEBUG env var for verbose diagnostics")
		assert.Contains(t, stepContent, "GEMINI_CLI_TRUST_WORKSPACE: true", "Should include GEMINI_CLI_TRUST_WORKSPACE")
	})

	t.Run("model environment variables", func(t *testing.T) {
		// When model is not configured, no model env var should be set (let Gemini CLI use its default)
		noModelWorkflow := &WorkflowData{
			Name:        "no-model",
			SafeOutputs: &SafeOutputsConfig{},
		}

		steps := engine.GetExecutionSteps(noModelWorkflow, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")
		stepContent := strings.Join(steps[1], "\n")
		assert.NotContains(t, stepContent, "GH_AW_MODEL_DETECTION_GEMINI", "Should not include detection model env var when model is unconfigured")
		assert.NotContains(t, stepContent, "GH_AW_MODEL_AGENT_GEMINI", "Should not include agent model env var when model is unconfigured")
		assert.NotContains(t, stepContent, "GEMINI_MODEL", "Should not include GEMINI_MODEL when model is unconfigured")

		// When model is configured, use the native GEMINI_MODEL env var
		modelWorkflow := &WorkflowData{
			Name:         "model-configured",
			Model:        "gemini-2.0-flash",
			EngineConfig: &EngineConfig{},
		}

		steps = engine.GetExecutionSteps(modelWorkflow, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")
		stepContent = strings.Join(steps[1], "\n")
		assert.Contains(t, stepContent, "GEMINI_MODEL: gemini-2.0-flash", "Should set GEMINI_MODEL when model is explicitly configured")
	})

	t.Run("engine env overrides default token expression", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Env: map[string]string{
					"GEMINI_API_KEY": "${{ secrets.MY_ORG_GEMINI_KEY }}",
				},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")

		stepContent := strings.Join(steps[1], "\n")

		// The user-provided value should override the default token expression
		assert.Contains(t, stepContent, "GEMINI_API_KEY: ${{ secrets.MY_ORG_GEMINI_KEY }}", "engine.env should override the default GEMINI_API_KEY expression")
		assert.NotContains(t, stepContent, "GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}", "Default GEMINI_API_KEY expression should be replaced by engine.env")
	})

	t.Run("engine env adds custom non-secret env vars", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Env: map[string]string{
					"CUSTOM_VAR": "custom-value",
				},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")

		stepContent := strings.Join(steps[1], "\n")

		assert.Contains(t, stepContent, "CUSTOM_VAR: custom-value", "engine.env non-secret vars should be included")
	})

	t.Run("settings step is first", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")

		settingsContent := strings.Join(steps[0], "\n")
		execContent := strings.Join(steps[1], "\n")

		assert.Contains(t, settingsContent, "Write Gemini Config", "First step should be Write Gemini Config")
		assert.Contains(t, settingsContent, "includeDirectories", "Settings step should set includeDirectories")
		assert.Contains(t, settingsContent, "/tmp/", "Settings step should include /tmp/ in include directories")
		assert.Contains(t, execContent, "Execute Gemini CLI", "Second step should be Execute Gemini CLI")
	})
}

func TestGeminiEngineFirewallIntegration(t *testing.T) {
	engine := NewGeminiEngine()

	t.Run("firewall enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			NetworkPermissions: &NetworkPermissions{
				Allowed: []string{"defaults"},
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")

		stepContent := strings.Join(steps[1], "\n")

		// Should use AWF command
		assert.Contains(t, stepContent, "awf", "Should use AWF when firewall is enabled")
		assert.Contains(t, stepContent, "shell_harness.cjs", "Should run the sandboxed CLI through the shared shell harness")
		// With config file support, domains and apiProxy are in the JSON config
		assert.Contains(t, stepContent, "allowDomains", "Should include allowDomains in config JSON")
		assert.Contains(t, stepContent, `\"enabled\":true`, "Should include apiProxy enabled in config JSON")
		assert.Contains(t, stepContent, "GEMINI_API_BASE_URL: http://host.docker.internal:10003", "Should set GEMINI_API_BASE_URL to LLM gateway URL")
	})

	t.Run("firewall disabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: false,
				},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")

		stepContent := strings.Join(steps[1], "\n")

		// Should use simple command without AWF
		assert.Contains(t, stepContent, "set -o pipefail", "Should use simple command with pipefail")
		assert.NotContains(t, stepContent, "awf", "Should not use AWF when firewall is disabled")
		assert.NotContains(t, stepContent, "GEMINI_API_BASE_URL", "Should not set GEMINI_API_BASE_URL when firewall is disabled")
	})
}

func TestComputeGeminiToolsCore(t *testing.T) {
	t.Run("nil tools includes default read-only tools", func(t *testing.T) {
		result := computeGeminiToolsCore(nil)
		assert.Contains(t, result, "glob", "Should include glob")
		assert.Contains(t, result, "grep_search", "Should include grep_search")
		assert.Contains(t, result, "list_directory", "Should include list_directory")
		assert.Contains(t, result, "read_file", "Should include read_file")
		assert.Contains(t, result, "read_many_files", "Should include read_many_files")
		assert.NotContains(t, result, "run_shell_command", "Should not include run_shell_command without bash tool")
		assert.NotContains(t, result, "write_file", "Should not include write_file without edit tool")
		assert.NotContains(t, result, "replace", "Should not include replace without edit tool")
	})

	t.Run("empty tools includes default read-only tools", func(t *testing.T) {
		result := computeGeminiToolsCore(map[string]any{})
		assert.Contains(t, result, "read_file", "Should include read_file")
		assert.NotContains(t, result, "run_shell_command", "Should not include run_shell_command")
	})

	t.Run("bash with specific commands maps to run_shell_command entries", func(t *testing.T) {
		tools := map[string]any{
			"bash": []any{"grep", "ls", "git"},
		}
		result := computeGeminiToolsCore(tools)
		assert.Contains(t, result, "run_shell_command(grep)", "Should map grep to run_shell_command(grep)")
		assert.Contains(t, result, "run_shell_command(ls)", "Should map ls to run_shell_command(ls)")
		assert.Contains(t, result, "run_shell_command(git)", "Should map git to run_shell_command(git)")
		assert.NotContains(t, result, "run_shell_command", "Should not include unrestricted run_shell_command")
	})

	t.Run("bash with wildcard * maps to unrestricted run_shell_command", func(t *testing.T) {
		tools := map[string]any{
			"bash": []any{"*"},
		}
		result := computeGeminiToolsCore(tools)
		assert.Contains(t, result, "run_shell_command", "Should include unrestricted run_shell_command for wildcard")
		assert.NotContains(t, result, "run_shell_command(*)", "Should not include run_shell_command(*)")
	})

	t.Run("bash with :* wildcard maps to unrestricted run_shell_command", func(t *testing.T) {
		tools := map[string]any{
			"bash": []any{":*"},
		}
		result := computeGeminiToolsCore(tools)
		assert.Contains(t, result, "run_shell_command", "Should include unrestricted run_shell_command for :* wildcard")
	})

	t.Run("bash with no specific commands (nil) maps to unrestricted run_shell_command", func(t *testing.T) {
		tools := map[string]any{
			"bash": nil,
		}
		result := computeGeminiToolsCore(tools)
		assert.Contains(t, result, "run_shell_command", "Should include unrestricted run_shell_command when bash has no commands")
	})

	t.Run("edit tool maps to write_file and replace", func(t *testing.T) {
		tools := map[string]any{
			"edit": map[string]any{},
		}
		result := computeGeminiToolsCore(tools)
		assert.Contains(t, result, "write_file", "Should map edit to write_file")
		assert.Contains(t, result, "replace", "Should map edit to replace")
	})

	t.Run("combined bash and edit tools", func(t *testing.T) {
		tools := map[string]any{
			"bash": []any{"grep"},
			"edit": map[string]any{},
		}
		result := computeGeminiToolsCore(tools)
		assert.Contains(t, result, "run_shell_command(grep)", "Should include run_shell_command(grep)")
		assert.Contains(t, result, "write_file", "Should include write_file")
		assert.Contains(t, result, "replace", "Should include replace")
		assert.Contains(t, result, "read_file", "Should always include read_file")
	})

	t.Run("result is sorted", func(t *testing.T) {
		tools := map[string]any{
			"bash": []any{"zzz", "aaa"},
			"edit": map[string]any{},
		}
		result := computeGeminiToolsCore(tools)
		for i := 1; i < len(result); i++ {
			assert.LessOrEqual(t, result[i-1], result[i], "Tools should be sorted alphabetically")
		}
	})

	t.Run("bash tool with trailing space-star is normalized to canonical run_shell_command(cmd)", func(t *testing.T) {
		tools := map[string]any{
			"bash": []any{"jq *"},
		}
		result := computeGeminiToolsCore(tools)
		assert.Contains(t, result, "run_shell_command(jq)", "Should normalize 'jq *' to run_shell_command(jq)")
		assert.NotContains(t, result, "run_shell_command(jq *)", "Should not emit run_shell_command(jq *)")
	})

	t.Run("community-attribution-style wildcard entries normalize to canonical forms", func(t *testing.T) {
		tools := map[string]any{
			"bash": []any{"jq *", "sed *", "awk *", "cat *"},
		}
		result := computeGeminiToolsCore(tools)
		assert.Contains(t, result, "run_shell_command(jq)", "Should normalize 'jq *'")
		assert.Contains(t, result, "run_shell_command(sed)", "Should normalize 'sed *'")
		assert.Contains(t, result, "run_shell_command(awk)", "Should normalize 'awk *'")
		assert.Contains(t, result, "run_shell_command(cat)", "Should normalize 'cat *'")
		assert.NotContains(t, result, "run_shell_command(jq *)", "Should not emit run_shell_command with wildcard suffix")
	})
}

func TestGenerateGeminiSettingsStep(t *testing.T) {
	engine := NewGeminiEngine()

	t.Run("step sets context.includeDirectories to /tmp/", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:  "test-workflow",
			Tools: map[string]any{},
		}
		step := engine.generateGeminiSettingsStep(workflowData)
		content := strings.Join(step, "\n")

		assert.Contains(t, content, "Write Gemini Config", "Should have correct step name")
		assert.Contains(t, content, "/tmp/", "Should include /tmp/ in include directories")
		assert.Contains(t, content, "includeDirectories", "Should set includeDirectories")
		assert.Contains(t, content, ".gemini", "Should reference .gemini directory")
		assert.Contains(t, content, "GITHUB_WORKSPACE", "Should use GITHUB_WORKSPACE")
	})

	t.Run("step includes merge logic for existing settings.json", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:  "test-workflow",
			Tools: map[string]any{},
		}
		step := engine.generateGeminiSettingsStep(workflowData)
		content := strings.Join(step, "\n")

		assert.Contains(t, content, "if [ -f", "Should check for existing settings.json")
		assert.Contains(t, content, "jq", "Should use jq for merging")
		assert.Contains(t, content, "$existing * $base", "Should merge with base taking precedence")
	})

	t.Run("step includes tools.core with bash mapping", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			Tools: map[string]any{
				"bash": []any{"grep", "git"},
			},
		}
		step := engine.generateGeminiSettingsStep(workflowData)
		content := strings.Join(step, "\n")

		assert.Contains(t, content, "run_shell_command(grep)", "Should include run_shell_command(grep) for bash grep")
		assert.Contains(t, content, "run_shell_command(git)", "Should include run_shell_command(git) for bash git")
		assert.Contains(t, content, "core", "Should include tools.core")
	})

	t.Run("step includes tools.core with edit mapping", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			Tools: map[string]any{
				"edit": map[string]any{},
			},
		}
		step := engine.generateGeminiSettingsStep(workflowData)
		content := strings.Join(step, "\n")

		assert.Contains(t, content, "write_file", "Should include write_file for edit tool")
		assert.Contains(t, content, "replace", "Should include replace for edit tool")
	})

	t.Run("GH_AW_GEMINI_BASE_CONFIG env var is single-quoted for valid YAML", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:  "test-workflow",
			Tools: map[string]any{},
		}
		step := engine.generateGeminiSettingsStep(workflowData)
		content := strings.Join(step, "\n")

		// The JSON value must be single-quoted so YAML doesn't treat it as an object
		assert.Contains(t, content, "GH_AW_GEMINI_BASE_CONFIG: '", "JSON env var value must be single-quoted for valid YAML")
	})

	t.Run("step includes web_fetch in tools.core when web-fetch tool is specified", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			Tools: map[string]any{
				"web-fetch": nil,
			},
		}
		step := engine.generateGeminiSettingsStep(workflowData)
		content := strings.Join(step, "\n")

		assert.Contains(t, content, "web_fetch", "Should include web_fetch in tools.core when web-fetch is specified")
	})

	t.Run("step does not include web_fetch in tools.core when web-fetch tool is not specified", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:  "test-workflow",
			Tools: map[string]any{},
		}
		step := engine.generateGeminiSettingsStep(workflowData)
		content := strings.Join(step, "\n")

		assert.NotContains(t, content, "web_fetch", "Should not include web_fetch in tools.core when web-fetch is not specified")
	})

	t.Run("step includes mounted mcp cli commands in restricted bash allowlist", func(t *testing.T) {
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
		step := engine.generateGeminiSettingsStep(workflowData)
		content := strings.Join(step, "\n")

		assert.Contains(t, content, "run_shell_command(echo)", "Should include original restricted bash command")
		assert.Contains(t, content, "run_shell_command(mymcp:*)", "Should include mounted custom MCP CLI command")
		assert.Contains(t, content, "run_shell_command(playwright-cli:*)", "Should include Playwright CLI command")
		assert.Contains(t, content, "run_shell_command(safeoutputs:*)", "Should include mounted safeoutputs CLI command")
	})
}

func TestGeminiVertexWIF(t *testing.T) {
	engine := NewGeminiEngine()

	makeVertexWIFData := func(project, location string) *WorkflowData {
		return &WorkflowData{
			Name:        "test-vertex-wif",
			ParsedTools: &ToolsConfig{},
			Tools:       map[string]any{},
			EngineConfig: &EngineConfig{
				ID: "gemini",
				Auth: &EngineAuthConfig{
					Type:                           "github-oidc",
					Provider:                       "gcp",
					GoogleWorkloadIdentityProvider: "projects/123/locations/global/workloadIdentityPools/pool/providers/github",
					GoogleServiceAccount:           "my-sa@my-project.iam.gserviceaccount.com",
					GoogleProject:                  project,
					GoogleLocation:                 location,
				},
			},
		}
	}

	t.Run("isGeminiVertexWIF returns true when auth type is github-oidc and provider is gcp with required fields", func(t *testing.T) {
		wd := makeVertexWIFData("my-project", "us-central1")
		assert.True(t, isGeminiVertexWIF(wd), "Should detect Vertex WIF")
	})

	t.Run("isGeminiVertexWIF returns false when no auth", func(t *testing.T) {
		wd := &WorkflowData{Name: "test"}
		assert.False(t, isGeminiVertexWIF(wd), "Should not detect WIF when no auth")
	})

	t.Run("isGeminiVertexWIF returns false when provider is not gcp", func(t *testing.T) {
		wd := &WorkflowData{
			Name: "test",
			EngineConfig: &EngineConfig{
				Auth: &EngineAuthConfig{
					Type:     "github-oidc",
					Provider: "anthropic",
				},
			},
		}
		assert.False(t, isGeminiVertexWIF(wd), "Should not detect WIF when provider is not gcp")
	})

	t.Run("isGeminiVertexWIF returns false when required fields are missing", func(t *testing.T) {
		wd := &WorkflowData{
			Name: "test",
			EngineConfig: &EngineConfig{
				Auth: &EngineAuthConfig{
					Type:     "github-oidc",
					Provider: "gcp",
					// Missing: GoogleWorkloadIdentityProvider, GoogleServiceAccount, GoogleProject
				},
			},
		}
		assert.False(t, isGeminiVertexWIF(wd), "Should not detect WIF when required fields are missing")
	})

	t.Run("GEMINI_API_KEY not required when Vertex WIF is configured", func(t *testing.T) {
		wd := makeVertexWIFData("my-project", "us-central1")
		secrets := engine.GetRequiredSecretNames(wd)
		assert.NotContains(t, secrets, "GEMINI_API_KEY", "Should not require GEMINI_API_KEY with Vertex WIF")
	})

	t.Run("GEMINI_API_KEY still required without Vertex WIF", func(t *testing.T) {
		wd := &WorkflowData{
			Name:        "test",
			ParsedTools: &ToolsConfig{},
			Tools:       map[string]any{},
		}
		secrets := engine.GetRequiredSecretNames(wd)
		assert.Contains(t, secrets, "GEMINI_API_KEY", "Should require GEMINI_API_KEY without WIF")
	})

	t.Run("secret validation step is empty when Vertex WIF is configured", func(t *testing.T) {
		wd := makeVertexWIFData("my-project", "us-central1")
		step := engine.GetSecretValidationStep(wd)
		assert.Empty(t, step, "Should return empty validation step with Vertex WIF")
	})

	t.Run("secret validation step present without Vertex WIF", func(t *testing.T) {
		wd := &WorkflowData{Name: "test"}
		step := engine.GetSecretValidationStep(wd)
		assert.NotEmpty(t, step, "Should return validation step without WIF")
	})

	t.Run("execution step uses Vertex AI env vars when WIF configured", func(t *testing.T) {
		wd := makeVertexWIFData("my-project", "us-central1")
		steps := engine.GetExecutionSteps(wd, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")

		stepContent := strings.Join(steps[1], "\n")
		assert.Contains(t, stepContent, "GOOGLE_GENAI_USE_VERTEXAI: true", "Should set Vertex AI backend env var to 'true'")
		assert.Contains(t, stepContent, "GOOGLE_CLOUD_PROJECT: my-project", "Should set project env var")
		assert.Contains(t, stepContent, "GOOGLE_CLOUD_LOCATION: us-central1", "Should set location env var")
		assert.NotContains(t, stepContent, "GEMINI_API_KEY", "Should not include GEMINI_API_KEY with Vertex WIF")
	})

	t.Run("execution step defaults location to us-central1 when not configured", func(t *testing.T) {
		wd := makeVertexWIFData("my-project", "")
		steps := engine.GetExecutionSteps(wd, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")

		stepContent := strings.Join(steps[1], "\n")
		assert.Contains(t, stepContent, "GOOGLE_GENAI_USE_VERTEXAI: true", "Should set Vertex AI backend env var to 'true'")
		assert.Contains(t, stepContent, "GOOGLE_CLOUD_PROJECT: my-project", "Should set project env var")
		assert.Contains(t, stepContent, "GOOGLE_CLOUD_LOCATION: us-central1", "Should default location to us-central1")
		assert.NotContains(t, stepContent, "GEMINI_API_KEY", "Should not include GEMINI_API_KEY with Vertex WIF")
	})

	t.Run("execution step uses GEMINI_API_KEY without Vertex WIF", func(t *testing.T) {
		wd := &WorkflowData{Name: "test"}
		steps := engine.GetExecutionSteps(wd, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")

		stepContent := strings.Join(steps[1], "\n")
		assert.Contains(t, stepContent, "GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}", "Should include GEMINI_API_KEY without WIF")
		assert.NotContains(t, stepContent, "GOOGLE_GENAI_USE_VERTEXAI", "Should not set Vertex AI vars without WIF")
	})

	t.Run("engine.env cannot overwrite WIF-emitted Vertex AI env vars", func(t *testing.T) {
		wd := makeVertexWIFData("my-project", "us-central1")
		// User tries to override WIF vars via engine.env — compiler must ignore them.
		wd.EngineConfig.Env = map[string]string{
			"GOOGLE_GENAI_USE_VERTEXAI": "false",
			"GOOGLE_CLOUD_PROJECT":      "other-project",
		}
		steps := engine.GetExecutionSteps(wd, "/tmp/test.log")
		require.Len(t, steps, 2, "Should generate settings step and execution step")

		stepContent := strings.Join(steps[1], "\n")
		assert.Contains(t, stepContent, "GOOGLE_GENAI_USE_VERTEXAI: true", "WIF env var must not be overridden by engine.env")
		assert.Contains(t, stepContent, "GOOGLE_CLOUD_PROJECT: my-project", "WIF project must not be overridden by engine.env")
	})
}

func TestGeminiEngineWithExpressionVersion(t *testing.T) {
	engine := NewGeminiEngine()

	expressionVersion := "${{ inputs.engine-version }}"
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:      "gemini",
			Version: expressionVersion,
		},
	}

	installSteps := engine.GetInstallationSteps(workflowData)

	// Find the npm install step
	var installStep string
	for _, step := range installSteps {
		stepContent := strings.Join([]string(step), "\n")
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
	if strings.Contains(installStep, "@google/gemini-cli@"+expressionVersion) {
		t.Errorf("Expression should NOT be embedded directly in npm install command, got:\n%s", installStep)
	}
}

func TestGeminiEngineWithVersion(t *testing.T) {
	engine := NewGeminiEngine()

	customVersion := "0.99.0"
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:      "gemini",
			Version: customVersion,
		},
	}

	installSteps := engine.GetInstallationSteps(workflowData)

	var installStep string
	for _, step := range installSteps {
		stepContent := strings.Join([]string(step), "\n")
		if strings.Contains(stepContent, "npm install") {
			installStep = stepContent
			break
		}
	}

	if installStep == "" {
		t.Fatal("Could not find npm install step")
	}

	if !strings.Contains(installStep, "@google/gemini-cli@"+customVersion) {
		t.Errorf("Expected custom version %q in install step, got:\n%s", customVersion, installStep)
	}
	if strings.Contains(installStep, "@google/gemini-cli@"+string(constants.DefaultGeminiVersion)) {
		t.Errorf("Expected user-specified version, not default, in install step:\n%s", installStep)
	}
}

func TestGeminiEngineWithoutVersion(t *testing.T) {
	engine := NewGeminiEngine()

	workflowData := &WorkflowData{
		Name:         "test-workflow",
		EngineConfig: &EngineConfig{},
	}

	installSteps := engine.GetInstallationSteps(workflowData)

	// EngineConfig.Version must be normalized to the default version.
	if workflowData.EngineConfig.Version != string(constants.DefaultGeminiVersion) {
		t.Fatalf("Expected engine config version to be normalized to default Gemini version %q, got: %q", constants.DefaultGeminiVersion, workflowData.EngineConfig.Version)
	}

	var installStep string
	for _, step := range installSteps {
		stepContent := strings.Join([]string(step), "\n")
		if strings.Contains(stepContent, "npm install") {
			installStep = stepContent
			break
		}
	}

	if installStep == "" {
		t.Fatal("Could not find npm install step")
	}

	if !strings.Contains(installStep, "@google/gemini-cli@"+string(constants.DefaultGeminiVersion)) {
		t.Errorf("Expected default version %q in install step when no engine.version set, got:\n%s", constants.DefaultGeminiVersion, installStep)
	}
}
