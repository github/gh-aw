//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSafeOutputsNoLongerGeneratesHTTPAPIKey verifies that safe-outputs no longer
// creates an HTTP server agent ID now that it runs as a stdio MCP container.
func TestSafeOutputsAPIKeyImmediateMasking(t *testing.T) {
	workflowData := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
		Tools: map[string]any{
			"safe-outputs": map[string]any{},
		},
	}

	compiler := &Compiler{}
	mockEngine := NewClaudeEngine()

	var yaml strings.Builder
	require.NoError(t, compiler.generateMCPSetup(&yaml, workflowData.Tools, mockEngine, workflowData))
	output := yaml.String()

	assert.NotContains(t, output, "Generate Safe Outputs MCP Server Config",
		"Safe outputs should not have a dedicated HTTP server config step")
	assert.NotContains(t, output, "Start Safe Outputs MCP HTTP Server",
		"Safe outputs should not launch a dedicated HTTP server")
	assert.Contains(t, output, `"container": "`+resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, nil)+`"`,
		"Safe outputs should run in the gh-aw node container")
}

// TestMCPScriptsAPIKeyImmediateMasking verifies that the MCP Scripts API key
// is masked immediately after generation, before any other operations.
func TestMCPScriptsAPIKeyImmediateMasking(t *testing.T) {
	workflowData := &WorkflowData{
		MCPScripts: &MCPScriptsConfig{
			Tools: map[string]*MCPScriptToolConfig{
				"test-tool": {
					Name:        "test-tool",
					Description: "Test tool",
					Run:         "echo test",
				},
			},
		},
		Tools: map[string]any{},
		Features: map[string]any{
			"mcp-scripts": true,
		},
	}

	compiler := &Compiler{}
	mockEngine := NewClaudeEngine()

	var yaml strings.Builder
	require.NoError(t, compiler.generateMCPSetup(&yaml, workflowData.Tools, mockEngine, workflowData))
	output := yaml.String()

	// Find the MCP Scripts config generation section
	configStart := strings.Index(output, "Generate MCP Scripts Server Config")
	require.Greater(t, configStart, -1, "Should find MCP Scripts config generation step")

	// Extract just the run script for this step
	runStart := strings.Index(output[configStart:], "run: |")
	require.Greater(t, runStart, -1, "Should find run script")
	runSection := output[configStart+runStart:]

	// Find the next step or end of this step's run block
	nextStepIdx := strings.Index(runSection, "\n      - name:")
	if nextStepIdx > 0 {
		runSection = runSection[:nextStepIdx]
	}

	// Find the API key generation line
	keyGenIdx := strings.Index(runSection, "API_KEY=$(openssl rand -base64 45")
	require.Greater(t, keyGenIdx, -1, "Should find API key generation")

	// Find the masking line
	maskIdx := strings.Index(runSection, "echo \"::add-mask::${API_KEY}\"")
	require.Greater(t, maskIdx, -1, "Should find API key masking")

	// Verify masking comes immediately after generation
	betweenGenAndMask := runSection[keyGenIdx:maskIdx]
	lines := strings.Split(betweenGenAndMask, "\n")
	require.LessOrEqual(t, len(lines), 2, "Should have at most 2 lines between generation and masking")

	// Verify no PORT assignment before masking
	assert.NotContains(t, betweenGenAndMask, "PORT=", "PORT assignment should come after masking")

	// Verify masking comes before PORT assignment
	portIdx := strings.Index(runSection, "PORT=")
	if portIdx > 0 {
		assert.Less(t, maskIdx, portIdx, "API key masking should come before PORT assignment")
	}
}

// TestMCPGatewayAPIKeyImmediateMasking verifies that the MCP Gateway agent ID
// is masked immediately after generation, before any export or other operations.
func TestMCPGatewayAPIKeyImmediateMasking(t *testing.T) {
	workflowData := &WorkflowData{
		Tools: map[string]any{
			"github": map[string]any{
				"mode": "local",
			},
		},
	}

	compiler := &Compiler{}
	mockEngine := NewClaudeEngine()

	var yaml strings.Builder
	require.NoError(t, compiler.generateMCPSetup(&yaml, workflowData.Tools, mockEngine, workflowData))
	output := yaml.String()

	// Find the MCP gateway agent ID generation
	keyGenIdx := strings.Index(output, "MCP_GATEWAY_AGENT_ID=$(openssl rand -base64 45")
	require.Greater(t, keyGenIdx, -1, "Should find MCP Gateway agent ID generation")

	// Find the masking line
	maskIdx := strings.Index(output[keyGenIdx:], "echo \"::add-mask::${MCP_GATEWAY_AGENT_ID}\"")
	require.Greater(t, maskIdx, -1, "Should find MCP Gateway agent ID masking")

	// Extract the section between generation and masking
	betweenGenAndMask := output[keyGenIdx : keyGenIdx+maskIdx]

	// Verify masking comes immediately after generation (before export)
	lines := strings.Split(betweenGenAndMask, "\n")
	require.LessOrEqual(t, len(lines), 2, "Should have at most 2 lines between generation and masking")

	// Verify no PAYLOAD_DIR or DEBUG operations before masking
	assert.NotContains(t, betweenGenAndMask, "MCP_GATEWAY_PAYLOAD_DIR", "PAYLOAD_DIR should be set after masking")
	assert.NotContains(t, betweenGenAndMask, "DEBUG=", "DEBUG should be set after masking")

	// The export should come after masking
	exportAfterGenIdx := strings.Index(output[keyGenIdx:], "export MCP_GATEWAY_AGENT_ID")
	if exportAfterGenIdx > 0 {
		// Verify masking comes before the export (masking should be line 2, export should be line 3)
		assert.Less(t, maskIdx, exportAfterGenIdx, "agent ID masking should come before export")
	}
}

// TestAPIKeyMaskingNoEmptyDeclaration verifies that agent IDs are not declared
// as empty variables before assignment, which would create a timing window.
func TestAPIKeyMaskingNoEmptyDeclaration(t *testing.T) {
	workflowData := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
		MCPScripts: &MCPScriptsConfig{
			Tools: map[string]*MCPScriptToolConfig{
				"test": {Name: "test", Run: "echo test"},
			},
		},
		Tools: map[string]any{
			"safe-outputs": map[string]any{},
			"github": map[string]any{
				"mode": "local",
			},
		},
		Features: map[string]any{
			"mcp-scripts": true,
		},
	}

	compiler := &Compiler{}
	mockEngine := NewClaudeEngine()

	var yaml strings.Builder
	require.NoError(t, compiler.generateMCPSetup(&yaml, workflowData.Tools, mockEngine, workflowData))
	output := yaml.String()

	// Verify no empty agent ID declarations before assignment
	assert.NotContains(t, output, "API_KEY=\"\"\n          API_KEY=$(openssl",
		"Should not have empty API_KEY declaration before assignment")
	assert.NotContains(t, output, "MCP_GATEWAY_AGENT_ID=\"\"\n          MCP_GATEWAY_AGENT_ID=$(openssl",
		"Should not have empty MCP_GATEWAY_AGENT_ID declaration before assignment")
}
