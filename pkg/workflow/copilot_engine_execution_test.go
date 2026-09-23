//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestCopilotEngineExecutionSteps(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// GetExecutionSteps returns 1 step: copilot execution
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	// Check the execution step
	stepContent := strings.Join([]string(steps[0]), "\n")

	if !strings.Contains(stepContent, "name: Execute GitHub Copilot CLI") {
		t.Errorf("Expected step name 'Execute GitHub Copilot CLI' in step content:\n%s", stepContent)
	}

	// When firewall is disabled, should use 'copilot' command (not npx)
	if !strings.Contains(stepContent, "copilot") || !strings.Contains(stepContent, "--add-dir /tmp/ --add-dir /tmp/gh-aw/ --add-dir /tmp/gh-aw/agent/ --log-level all --log-dir") {
		t.Errorf("Expected command to contain 'copilot' and '--add-dir /tmp/ --add-dir /tmp/gh-aw/ --add-dir /tmp/gh-aw/agent/ --log-level all --log-dir' in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "/tmp/gh-aw/test.log") {
		t.Errorf("Expected command to contain log file name in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "--prompt-file /tmp/gh-aw/aw-prompts/prompt.txt") {
		t.Errorf("Expected command to pass prompt file path directly, got:\n%s", stepContent)
	}
	if strings.Contains(stepContent, `cd "${GITHUB_WORKSPACE}" &&`) {
		t.Errorf("Expected Copilot command to not use shell cd prefix (harness sets cwd via spawn options), got:\n%s", stepContent)
	}

	if strings.Contains(stepContent, "COPILOT_CLI_INSTRUCTION=") {
		t.Errorf("Expected command to avoid loading prompt into shell variable, got:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}") {
		t.Errorf("Expected COPILOT_GITHUB_TOKEN environment variable in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, constants.CopilotCLIIntegrationIDEnvVar+": "+constants.CopilotCLIIntegrationIDValue) {
		t.Errorf("Expected %s environment variable in step content:\n%s", constants.CopilotCLIIntegrationIDEnvVar, stepContent)
	}

	// Test that GITHUB_HEAD_REF and GITHUB_REF_NAME are present for branch resolution
	if !strings.Contains(stepContent, "GITHUB_HEAD_REF: ${{ github.head_ref }}") {
		t.Errorf("Expected GITHUB_HEAD_REF environment variable in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "GITHUB_REF_NAME: ${{ github.ref_name }}") {
		t.Errorf("Expected GITHUB_REF_NAME environment variable in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "GITHUB_WORKSPACE: ${{ github.workspace }}") {
		t.Errorf("Expected GITHUB_WORKSPACE environment variable in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "RUNNER_TEMP: ${{ runner.temp }}") {
		t.Errorf("Expected RUNNER_TEMP environment variable in step content:\n%s", stepContent)
	}

	// Test that GITHUB_SERVER_URL and GITHUB_API_URL are present for GitHub Enterprise compatibility
	if !strings.Contains(stepContent, "GITHUB_SERVER_URL: ${{ github.server_url }}") {
		t.Errorf("Expected GITHUB_SERVER_URL environment variable in step content:\n%s", stepContent)
	}

	if !strings.Contains(stepContent, "GITHUB_API_URL: ${{ github.api_url }}") {
		t.Errorf("Expected GITHUB_API_URL environment variable in step content:\n%s", stepContent)
	}

	// Test that GH_AW_SAFE_OUTPUTS is not present when SafeOutputs is nil
	if strings.Contains(stepContent, "GH_AW_SAFE_OUTPUTS") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS to not be present when SafeOutputs is nil")
	}

	// Test that --disable-builtin-mcps flag is present
	if !strings.Contains(stepContent, "--disable-builtin-mcps") {
		t.Errorf("Expected --disable-builtin-mcps flag in command, got:\n%s", stepContent)
	}

	// Test that --no-ask-user IS present for detection jobs (SafeOutputs == nil)
	if !strings.Contains(stepContent, "--no-ask-user") {
		t.Errorf("Expected --no-ask-user to be present for detection jobs, got:\n%s", stepContent)
	}

	// Test that mkdir commands are present for --add-dir directories
	if !strings.Contains(stepContent, "mkdir -p /tmp/") {
		t.Errorf("Expected 'mkdir -p /tmp/' command in step content:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "mkdir -p /tmp/gh-aw/") {
		t.Errorf("Expected 'mkdir -p /tmp/gh-aw/' command in step content:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "mkdir -p /tmp/gh-aw/agent/") {
		t.Errorf("Expected 'mkdir -p /tmp/gh-aw/agent/' command in step content:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "mkdir -p /tmp/gh-aw/sandbox/agent/logs/") {
		t.Errorf("Expected 'mkdir -p /tmp/gh-aw/sandbox/agent/logs/' command in step content:\n%s", stepContent)
	}
}

func TestCopilotEngineExecutionStepsWithWebFetchKeepsBuiltinToolSchema(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		Tools: map[string]any{
			"web-fetch": nil,
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if strings.Contains(stepContent, "--disable-builtin-mcps") {
		t.Fatalf("Expected web-fetch workflows to keep Copilot built-in tool schema enabled, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "--allow-tool web_fetch") {
		t.Fatalf("Expected web-fetch workflows to allow web_fetch, got:\n%s", stepContent)
	}
}

func TestCopilotEngineExecutionStepsWithWebSearchKeepsBuiltinToolSchema(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		Tools: map[string]any{
			"web-search": nil,
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if strings.Contains(stepContent, "--disable-builtin-mcps") {
		t.Fatalf("Expected web-search workflows to keep Copilot built-in tool schema enabled, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "--allow-tool web_search") {
		t.Fatalf("Expected web-search workflows to allow web_search, got:\n%s", stepContent)
	}
}

func TestCopilotEngineExecutionStepsWithWebFetchAndWildcardBashScopesTools(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		Tools: map[string]any{
			"bash":      []any{"*"},
			"web-fetch": nil,
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if strings.Contains(stepContent, "--allow-all-tools") {
		t.Fatalf("Expected wildcard bash with web-fetch to avoid authorizing unrelated built-in tools, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "--allow-tool shell") || !strings.Contains(stepContent, "--allow-tool web_fetch") {
		t.Fatalf("Expected wildcard bash with web-fetch to authorize only shell and web_fetch, got:\n%s", stepContent)
	}
}

// TestCopilotEngineDisablesRubberDuck verifies that the Copilot engine execution steps
// write a settings file that disables the rubber-duck sub-agent, reducing token overhead
// and latency for Copilot engine runs.
func TestCopilotEngineDisablesRubberDuck(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// The step should create the Copilot config directory and write a settings file
	// that disables the rubber-duck sub-agent.
	if !strings.Contains(stepContent, "mkdir -p \"$HOME/.copilot\"") {
		t.Errorf("Expected 'mkdir -p \"$HOME/.copilot\"' in step content:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, copilotSettingsDefaultContent) {
		t.Errorf("Expected copilot settings content %q in step content:\n%s", copilotSettingsDefaultContent, stepContent)
	}
	if !strings.Contains(stepContent, copilotSettingsPath) {
		t.Errorf("Expected copilot settings path %q in step content:\n%s", copilotSettingsPath, stepContent)
	}
	if !strings.Contains(stepContent, "rm -f \""+copilotSettingsPath+"\"") {
		t.Errorf("Expected cleanup command to remove copilot settings path %q in step content:\n%s", copilotSettingsPath, stepContent)
	}
}

func TestCopilotEngineExecutionSteps_WithLSPConfig(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		LSP: map[string]LSPServerConfig{
			"typescript": {
				Command: "typescript-language-server",
				Args:    []string{"--stdio"},
				FileExtensions: map[string]string{
					".ts": "typescript",
				},
			},
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	if !strings.Contains(stepContent, `"lspServers":{"typescript":{"command":"typescript-language-server","args":["--stdio"],"fileExtensions":{".ts":"typescript"}}}`) {
		t.Fatalf("Expected lspServers config in step content, got:\n%s", stepContent)
	}
}

func TestCopilotEngineExecutionStepsWithOutput(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name:        "test-workflow",
		SafeOutputs: &SafeOutputsConfig{}, // Non-nil to trigger output handling
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	// GetExecutionSteps returns 1 step: execution
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	// Check the execution step
	stepContent := strings.Join([]string(steps[0]), "\n")

	// Test that GH_AW_SAFE_OUTPUTS is present when SafeOutputs is not nil
	if !strings.Contains(stepContent, "GH_AW_SAFE_OUTPUTS: ${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}") {
		t.Errorf("Expected GH_AW_SAFE_OUTPUTS environment variable when SafeOutputs is not nil in step content:\n%s", stepContent)
	}
}

func TestCopilotEngineExecutionStepsAlwaysInjectsIntegrationIDAfterEnvMerges(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			Env: map[string]string{
				constants.CopilotCLIIntegrationIDEnvVar: "override-from-engine",
			},
		},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Env: map[string]string{
					constants.CopilotCLIIntegrationIDEnvVar: "override-from-agent",
				},
			},
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")
	expected := constants.CopilotCLIIntegrationIDEnvVar + ": " + constants.CopilotCLIIntegrationIDValue
	if !strings.Contains(stepContent, expected) {
		t.Fatalf("Expected integration ID env to be forced to %q, got:\n%s", expected, stepContent)
	}
	if strings.Contains(stepContent, constants.CopilotCLIIntegrationIDEnvVar+": override-from-agent") {
		t.Fatalf("Expected agent override to be ignored for %s, got:\n%s", constants.CopilotCLIIntegrationIDEnvVar, stepContent)
	}
	if strings.Contains(stepContent, constants.CopilotCLIIntegrationIDEnvVar+": override-from-engine") {
		t.Fatalf("Expected engine override to be ignored for %s, got:\n%s", constants.CopilotCLIIntegrationIDEnvVar, stepContent)
	}
}

func TestCopilotEngineExecutionStepsWithCacheMemory(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		CacheMemoryConfig: &CacheMemoryConfig{
			Caches: []CacheMemoryEntry{
				{ID: "default"},
				{ID: "session"},
				{ID: "logs"},
			},
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// Test that mkdir commands are present for cache-memory directories
	if !strings.Contains(stepContent, "mkdir -p /tmp/gh-aw/cache-memory/") {
		t.Errorf("Expected 'mkdir -p /tmp/gh-aw/cache-memory/' command for default cache in step content:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "mkdir -p /tmp/gh-aw/cache-memory-session/") {
		t.Errorf("Expected 'mkdir -p /tmp/gh-aw/cache-memory-session/' command for session cache in step content:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "mkdir -p /tmp/gh-aw/cache-memory-logs/") {
		t.Errorf("Expected 'mkdir -p /tmp/gh-aw/cache-memory-logs/' command for logs cache in step content:\n%s", stepContent)
	}

	// Verify --add-dir flags are present for cache directories
	if !strings.Contains(stepContent, "--add-dir /tmp/gh-aw/cache-memory/") {
		t.Errorf("Expected '--add-dir /tmp/gh-aw/cache-memory/' in copilot args")
	}
	if !strings.Contains(stepContent, "--add-dir /tmp/gh-aw/cache-memory-session/") {
		t.Errorf("Expected '--add-dir /tmp/gh-aw/cache-memory-session/' in copilot args")
	}
	if !strings.Contains(stepContent, "--add-dir /tmp/gh-aw/cache-memory-logs/") {
		t.Errorf("Expected '--add-dir /tmp/gh-aw/cache-memory-logs/' in copilot args")
	}
}

func TestCopilotEngineExecutionStepsWithCustomAddDirArgs(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			Args: []string{"--add-dir", "/custom/path/", "--verbose"},
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")

	if len(steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(steps))
	}

	stepContent := strings.Join([]string(steps[0]), "\n")

	// Test that mkdir commands are present for custom --add-dir path
	if !strings.Contains(stepContent, "mkdir -p /custom/path/") {
		t.Errorf("Expected 'mkdir -p /custom/path/' command for custom add-dir arg in step content:\n%s", stepContent)
	}

	// Verify the custom --add-dir flag is still present in copilot args
	if !strings.Contains(stepContent, "--add-dir /custom/path/") {
		t.Errorf("Expected '--add-dir /custom/path/' in copilot args")
	}
}

// TestGenerateCopilotSessionFileCopyStep verifies the generated step copies session state files.
func TestGenerateCopilotSessionFileCopyStep(t *testing.T) {
	step := generateCopilotSessionFileCopyStep()
	content := strings.Join([]string(step), "\n")

	if !strings.Contains(content, "Copy Copilot session state files to logs") {
		t.Error("Step should have a descriptive name")
	}
	if !strings.Contains(content, "always()") {
		t.Error("Step should run always()")
	}
	if !strings.Contains(content, "continue-on-error: true") {
		t.Error("Step should be marked continue-on-error")
	}
	if !strings.Contains(content, "copy_copilot_session_state.sh") {
		t.Error("Step should invoke copy_copilot_session_state.sh")
	}
	if !strings.Contains(content, "${RUNNER_TEMP}/gh-aw/actions/") {
		t.Error("Step should reference script via ${RUNNER_TEMP}/gh-aw/actions/")
	}
}
