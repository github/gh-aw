//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopilotSDKEngineImplementsCodingAgentEngine(t *testing.T) {
	engine := NewCopilotSDKEngine()

	// Test basic Engine interface
	assert.Equal(t, "copilot-sdk", engine.GetID())
	assert.Equal(t, "GitHub Copilot SDK", engine.GetDisplayName())
	assert.Contains(t, engine.GetDescription(), "SDK")
	assert.True(t, engine.IsExperimental())
}

func TestCopilotSDKEngineCapabilities(t *testing.T) {
	engine := NewCopilotSDKEngine()

	// Test CapabilityProvider interface
	assert.True(t, engine.SupportsToolsAllowlist())
	assert.True(t, engine.SupportsHTTPTransport())
	assert.False(t, engine.SupportsMaxTurns())
	assert.True(t, engine.SupportsWebFetch())
	assert.False(t, engine.SupportsWebSearch())
	assert.False(t, engine.SupportsFirewall(), "SDK mode doesn't use firewall")
	assert.False(t, engine.SupportsPlugins(), "SDK mode doesn't support plugins yet")
	assert.Equal(t, 10002, engine.SupportsLLMGateway(), "Copilot SDK uses port 10002 for LLM gateway")
}

func TestCopilotSDKEngineGetRequiredSecretNames(t *testing.T) {
	engine := NewCopilotSDKEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	secrets := engine.GetRequiredSecretNames(workflowData)

	assert.Contains(t, secrets, "COPILOT_GITHUB_TOKEN")
	assert.NotContains(t, secrets, "MCP_GATEWAY_API_KEY", "No MCP servers configured")
}

func TestCopilotSDKEngineGetRequiredSecretNamesWithMCP(t *testing.T) {
	engine := NewCopilotSDKEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		Tools: map[string]any{
			"playwright": map[string]any{},
		},
	}

	secrets := engine.GetRequiredSecretNames(workflowData)

	assert.Contains(t, secrets, "COPILOT_GITHUB_TOKEN")
	assert.Contains(t, secrets, "MCP_GATEWAY_API_KEY", "MCP servers are configured")
}

func TestCopilotSDKEngineGetDeclaredOutputFiles(t *testing.T) {
	engine := NewCopilotSDKEngine()

	files := engine.GetDeclaredOutputFiles()

	assert.Contains(t, files, "/tmp/gh-aw/copilot-sdk/event-log.jsonl")
}

func TestCopilotSDKEngineGetLogFileForParsing(t *testing.T) {
	engine := NewCopilotSDKEngine()

	logFile := engine.GetLogFileForParsing()

	assert.Equal(t, "/tmp/gh-aw/copilot-sdk/event-log.jsonl", logFile)
}

func TestCopilotSDKEngineGetExecutionSteps(t *testing.T) {
	engine := NewCopilotSDKEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/agent-log.txt")

	// Should have 3 steps: headless start, config, execution
	assert.Len(t, steps, 3)

	// Check first step (start headless)
	step1 := strings.Join(steps[0], "\n")
	assert.Contains(t, step1, "Start Copilot CLI in headless mode")
	assert.Contains(t, step1, "copilot --headless --port 10002")
	assert.Contains(t, step1, "COPILOT_PID")

	// Check second step (configuration)
	step2 := strings.Join(steps[1], "\n")
	assert.Contains(t, step2, "Configure Copilot SDK client")
	assert.Contains(t, step2, "GH_AW_COPILOT_CONFIG")
	assert.Contains(t, step2, "host.docker.internal:10002")

	// Check third step (execution)
	step3 := strings.Join(steps[2], "\n")
	assert.Contains(t, step3, "Execute Copilot SDK client")
	assert.Contains(t, step3, "node /opt/gh-aw/copilot/copilot-client.js")
	assert.Contains(t, step3, "agentic_execution")
}

func TestCopilotSDKEngineGetExecutionStepsWithModel(t *testing.T) {
	engine := NewCopilotSDKEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			Model: "gpt-5.1-pro",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/agent-log.txt")

	// Check that model is in configuration
	step2 := strings.Join(steps[1], "\n")
	assert.Contains(t, step2, "gpt-5.1-pro")
}

func TestCopilotSDKEngineGetInstallationSteps(t *testing.T) {
	engine := NewCopilotSDKEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	steps := engine.GetInstallationSteps(workflowData)

	// Should reuse Copilot engine installation steps
	assert.NotEmpty(t, steps)

	// Check for secret validation
	allSteps := strings.Join(concatenateSteps(steps), "\n")
	assert.Contains(t, allSteps, "COPILOT_GITHUB_TOKEN")
}

func TestCopilotSDKEngineRenderMCPConfig(t *testing.T) {
	engine := NewCopilotSDKEngine()
	var yaml strings.Builder
	tools := map[string]any{
		"playwright": map[string]any{},
	}
	mcpTools := []string{"playwright"}
	workflowData := &WorkflowData{
		Name: "test-workflow",
	}

	engine.RenderMCPConfig(&yaml, tools, mcpTools, workflowData)

	output := yaml.String()

	// Should replace localhost with host.docker.internal
	assert.Contains(t, output, "host.docker.internal")
	assert.NotContains(t, output, "localhost")
	assert.NotContains(t, output, "127.0.0.1")
}

func TestCopilotSDKEngineParseLogMetrics(t *testing.T) {
	engine := NewCopilotSDKEngine()

	metrics := engine.ParseLogMetrics("test log content", true)

	// Should return valid LogMetrics struct (even if empty)
	assert.NotNil(t, metrics)
}

// Helper function to concatenate all steps into a single string array
func concatenateSteps(steps []GitHubActionStep) []string {
	var result []string
	for _, step := range steps {
		result = append(result, step...)
	}
	return result
}
