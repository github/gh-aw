//go:build !integration

package workflow

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseSafeScriptsConfig verifies parsing of safe-scripts configuration
func TestParseSafeScriptsConfig(t *testing.T) {
	scriptsMap := map[string]any{
		"slack-post-message": map[string]any{
			"name":        "Post Slack Message",
			"description": "Post a message to a Slack channel",
			"inputs": map[string]any{
				"channel": map[string]any{
					"description": "Slack channel name",
					"required":    true,
					"type":        "string",
				},
				"message": map[string]any{
					"description": "Message content",
					"required":    true,
					"type":        "string",
				},
			},
			// Users write only the body of main — the compiler wraps it
			"script": `return async function handleSlackPostMessage(message, resolvedTemporaryIds) {
  return { success: true };
};`,
		},
	}

	result := parseSafeScriptsConfig(scriptsMap)

	require.NotNil(t, result, "Should return non-nil result")
	require.Len(t, result, 1, "Should have one script")

	script, exists := result["slack-post-message"]
	require.True(t, exists, "Should have slack-post-message script")
	assert.Equal(t, "Post Slack Message", script.Name, "Name should match")
	assert.Equal(t, "Post a message to a Slack channel", script.Description, "Description should match")
	assert.Contains(t, script.Script, "return async function", "Script body should contain handler return")

	require.Len(t, script.Inputs, 2, "Should have 2 inputs")

	channelInput, ok := script.Inputs["channel"]
	require.True(t, ok, "Should have channel input")
	assert.Equal(t, "string", channelInput.Type, "Channel type should be string")
	assert.True(t, channelInput.Required, "Channel should be required")

	messageInput, ok := script.Inputs["message"]
	require.True(t, ok, "Should have message input")
	assert.Equal(t, "string", messageInput.Type, "Message type should be string")
	assert.True(t, messageInput.Required, "Message should be required")
}

// TestParseSafeScriptsConfigNilMap verifies nil input handling
func TestParseSafeScriptsConfigNilMap(t *testing.T) {
	result := parseSafeScriptsConfig(nil)
	assert.Nil(t, result, "Should return nil for nil map")
}

// TestExtractSafeScriptsFromFrontmatter verifies extraction from frontmatter
// TestBuildCustomSafeOutputScriptsJSON verifies JSON generation for script env var
func TestBuildCustomSafeOutputScriptsJSON(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Scripts: map[string]*SafeScriptConfig{
				"my-handler": {
					Description: "Custom handler",
					Script:      "return async (m) => ({ success: true });",
				},
			},
		},
	}

	jsonStr := buildCustomSafeOutputScriptsJSON(data)
	require.NotEmpty(t, jsonStr, "Should produce non-empty JSON")

	var mapping map[string]string
	err := json.Unmarshal([]byte(jsonStr), &mapping)
	require.NoError(t, err, "JSON should be valid")

	filename, exists := mapping["my_handler"]
	require.True(t, exists, "Should have my_handler key (normalized with underscore)")
	assert.Equal(t, "safe_output_script_my_handler.cjs", filename, "Filename should match expected pattern")
}

// TestBuildCustomSafeOutputScriptsJSONNormalization verifies name normalization with dashes
func TestBuildCustomSafeOutputScriptsJSONNormalization(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Scripts: map[string]*SafeScriptConfig{
				"slack-post-message": {
					Script: "return async (m) => ({ success: true });",
				},
				"notify-team": {
					Script: "return async (m) => ({ success: true });",
				},
			},
		},
	}

	jsonStr := buildCustomSafeOutputScriptsJSON(data)
	require.NotEmpty(t, jsonStr, "Should produce non-empty JSON")

	var mapping map[string]string
	err := json.Unmarshal([]byte(jsonStr), &mapping)
	require.NoError(t, err, "JSON should be valid")

	// Check names are normalized to underscores
	assert.Contains(t, mapping, "slack_post_message", "Should normalize dashes to underscores")
	assert.Contains(t, mapping, "notify_team", "Should normalize dashes to underscores")
	assert.Equal(t, "safe_output_script_slack_post_message.cjs", mapping["slack_post_message"], "Filename should match")
	assert.Equal(t, "safe_output_script_notify_team.cjs", mapping["notify_team"], "Filename should match")
}

// TestBuildCustomSafeOutputScriptsJSONEmpty verifies empty output when no scripts
func TestBuildCustomSafeOutputScriptsJSONEmpty(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{},
	}
	assert.Empty(t, buildCustomSafeOutputScriptsJSON(data), "Should return empty for no scripts")

	dataNil := &WorkflowData{SafeOutputs: nil}
	assert.Empty(t, buildCustomSafeOutputScriptsJSON(dataNil), "Should return empty for nil SafeOutputs")
}

// TestGenerateCustomScriptToolDefinition verifies MCP tool definition for scripts
func TestGenerateCustomScriptToolDefinition(t *testing.T) {
	maxLength := 40000
	scriptConfig := &SafeScriptConfig{
		Description: "Post a message to Slack",
		Inputs: map[string]*InputDefinition{
			"channel": {
				Description: "Target Slack channel",
				Required:    true,
				Type:        "string",
			},
			"message": {
				Description: "Message text",
				Required:    true,
				Type:        "string",
				MaxLength:   &maxLength,
			},
		},
		Script: "return async (m) => ({ success: true });",
	}

	tool := generateCustomScriptToolDefinition("slack_post_message", scriptConfig)

	assert.Equal(t, "slack_post_message", tool["name"], "Tool name should match")
	assert.Equal(t, "Post a message to Slack", tool["description"], "Description should match")

	inputSchema, ok := tool["inputSchema"].(map[string]any)
	require.True(t, ok, "Should have inputSchema")
	assert.Equal(t, "object", inputSchema["type"], "Schema type should be object")
	assert.Equal(t, false, inputSchema["additionalProperties"], "Should not allow additional properties")

	properties, ok := inputSchema["properties"].(map[string]any)
	require.True(t, ok, "Should have properties")
	assert.Len(t, properties, 2, "Should have 2 properties")
	assert.Equal(t, maxLength, properties["message"].(map[string]any)["maxLength"])

	required, ok := inputSchema["required"].([]string)
	require.True(t, ok, "Should have required field")
	assert.Len(t, required, 2, "Should have 2 required fields")
	assert.Contains(t, required, "channel", "Should require channel")
	assert.Contains(t, required, "message", "Should require message")
}

// TestScriptToolsInFilteredJSON verifies scripts appear in the filtered tools JSON
func TestGenerateSafeOutputScriptContent(t *testing.T) {
	scriptConfig := &SafeScriptConfig{
		Script: "core.info(`Channel: ${item.channel}`); return { success: true };",
		Inputs: map[string]*InputDefinition{
			"channel": {Type: "string", Description: "Target channel"},
			"message": {Type: "string", Description: "Message text"},
		},
	}
	content := generateSafeOutputScriptContent("my-handler", scriptConfig)

	assert.Contains(t, content, "// @ts-check", "Should include ts-check pragma")
	assert.Contains(t, content, "/// <reference types=\"./safe-output-script\" />", "Should include type reference")
	assert.Contains(t, content, "const { sanitizeContent } = require(\"./sanitize_content.cjs\");", "Should require sanitizeContent")
	assert.Contains(t, content, "/** @type {import('./types/safe-output-script').SafeOutputScriptMain} */", "Should include type annotation for main")
	assert.Contains(t, content, "// Auto-generated safe-output script handler: my-handler", "Should have comment header")
	assert.Contains(t, content, "async function main(config = {}) {", "Should wrap with main function")
	assert.Contains(t, content, "const { channel, message } = config;", "Should destructure config inputs")
	assert.Contains(t, content, "return async function handleMyHandler(item, resolvedTemporaryIds, temporaryIdMap) {", "Should generate handler function")
	assert.Contains(t, content, "    core.info", "Should indent user body by 4 spaces")
	assert.Contains(t, content, "module.exports = { main };", "Should include module.exports")

	// Verify the overall structure:
	// header → sanitizeContent require → main() { destructuring → handler { body } } → exports
	headerIdx := strings.Index(content, "// Auto-generated")
	requireIdx := strings.Index(content, "const { sanitizeContent }")
	mainIdx := strings.Index(content, "async function main")
	destructureIdx := strings.Index(content, "const { channel")
	handlerIdx := strings.Index(content, "return async function handle")
	bodyIdx := strings.Index(content, "    core.info")
	exportsIdx := strings.Index(content, "module.exports")

	assert.Less(t, headerIdx, requireIdx, "Header should precede sanitizeContent require")
	assert.Less(t, requireIdx, mainIdx, "sanitizeContent require should precede main")
	assert.Less(t, mainIdx, destructureIdx, "main() should precede config destructuring")
	assert.Less(t, destructureIdx, handlerIdx, "Config destructuring should precede handler function")
	assert.Less(t, handlerIdx, bodyIdx, "Handler function should precede user body")
	assert.Less(t, bodyIdx, exportsIdx, "User body should precede exports")
}

// TestGenerateSafeOutputScriptContentNoInputs verifies output without declared inputs (no destructuring)
func TestGenerateSafeOutputScriptContentNoInputs(t *testing.T) {
	scriptConfig := &SafeScriptConfig{
		Script: "return { success: true };",
	}
	content := generateSafeOutputScriptContent("simple-handler", scriptConfig)

	assert.Contains(t, content, "const { sanitizeContent } = require(\"./sanitize_content.cjs\");", "Should always require sanitizeContent")
	assert.NotContains(t, content, "= config;", "Should not destructure config inputs when no inputs declared")
	assert.Contains(t, content, "return async function handleSimpleHandler(item, resolvedTemporaryIds, temporaryIdMap) {", "Should still generate handler function")
	assert.Contains(t, content, "    return { success: true };", "Should indent user body by 4 spaces")
}

// TestScriptNameToHandlerName verifies handler name generation from script names
func TestScriptNameToHandlerName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"hyphen-separated", "post-slack-message", "handlePostSlackMessage"},
		{"underscore-separated", "post_slack_message", "handlePostSlackMessage"},
		{"mixed", "my-handler_name", "handleMyHandlerName"},
		{"single-word", "handler", "handleHandler"},
		{"camelcase-word", "createIssue", "handleCreateIssue"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scriptNameToHandlerName(tt.input)
			assert.Equal(t, tt.expected, result, "Handler name should match expected")
		})
	}
}

// TestBuildCustomScriptFilesStep verifies the generated step writes scripts to files
func TestBuildCustomScriptFilesStep(t *testing.T) {
	scripts := map[string]*SafeScriptConfig{
		"my-handler": {
			// Users write only the handler body — no function wrapper or boilerplate needed
			Script: "return { success: true };",
			Inputs: map[string]*InputDefinition{
				"channel": {Type: "string"},
			},
		},
	}

	steps, err := buildCustomScriptFilesStep(scripts)

	require.NoError(t, err, "Should not return error for valid scripts")
	require.NotEmpty(t, steps, "Should produce steps")

	fullYAML := strings.Join(steps, "")

	assert.Contains(t, fullYAML, "Configure Safe Output Scripts", "Should have configure step name")
	assert.Contains(t, fullYAML, "safe_output_script_my_handler.cjs", "Should reference the output filename")
	// Verify heredoc delimiter follows the randomized GH_AW_SAFE_OUTPUT_SCRIPT_MY_HANDLER_<hex>_EOF format
	delimRE := regexp.MustCompile(`GH_AW_SAFE_OUTPUT_SCRIPT_MY_HANDLER_[0-9a-f]{16}_EOF`)
	assert.True(t, delimRE.MatchString(fullYAML), "Should use correct randomized heredoc delimiter")
	// Verify the compiler generates the full outer wrapper
	assert.Contains(t, fullYAML, "async function main(config = {}) {", "Should generate main function declaration")
	assert.Contains(t, fullYAML, "const { channel } = config;", "Should generate config input destructuring")
	assert.Contains(t, fullYAML, "return async function handleMyHandler(item, resolvedTemporaryIds, temporaryIdMap) {", "Should generate handler function")
	assert.Contains(t, fullYAML, "module.exports = { main };", "Should include module.exports wrapper")
	// User's handler body content should appear indented inside the handler
	assert.Contains(t, fullYAML, "return { success: true };", "Should include user's handler body")
}

// TestBuildCustomScriptFilesStepEmpty verifies nil return for empty scripts
func TestBuildCustomScriptFilesStepEmpty(t *testing.T) {
	steps, err := buildCustomScriptFilesStep(nil)
	require.NoError(t, err, "Should not return error for nil scripts")
	assert.Nil(t, steps, "Should return nil for empty scripts")

	stepsEmpty, err := buildCustomScriptFilesStep(map[string]*SafeScriptConfig{})
	require.NoError(t, err, "Should not return error for empty map")
	assert.Nil(t, stepsEmpty, "Should return nil for empty map")
}

// TestHandlerManagerStepIncludesScriptsEnvVar verifies GH_AW_SAFE_OUTPUT_SCRIPTS in handler manager
func TestHandlerManagerStepIncludesScriptsEnvVar(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
			Scripts: map[string]*SafeScriptConfig{
				"my-script": {
					Description: "Custom script",
					Script:      "return async (m) => ({ success: true });",
				},
			},
		},
	}

	steps, err := compiler.buildHandlerManagerStep(workflowData)
	require.NoError(t, err)
	fullYAML := strings.Join(steps, "")

	assert.Contains(t, fullYAML, "GH_AW_SAFE_OUTPUT_SCRIPTS", "Should include GH_AW_SAFE_OUTPUT_SCRIPTS env var")
	assert.Contains(t, fullYAML, "my_script", "Should include normalized script name")
	assert.Contains(t, fullYAML, "safe_output_script_my_script.cjs", "Should include script filename")
}

// TestHandlerManagerStepNoScriptsEnvVar verifies GH_AW_SAFE_OUTPUT_SCRIPTS absent when no scripts
func TestHandlerManagerStepNoScriptsEnvVar(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
	}

	steps, err := compiler.buildHandlerManagerStep(workflowData)
	require.NoError(t, err)
	fullYAML := strings.Join(steps, "")

	assert.NotContains(t, fullYAML, "GH_AW_SAFE_OUTPUT_SCRIPTS", "Should not include GH_AW_SAFE_OUTPUT_SCRIPTS env var when no scripts")
}

// TestSafeOutputsConfigIncludesScripts verifies extractSafeOutputsConfig handles scripts
func TestSafeOutputsConfigIncludesScripts(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"scripts": map[string]any{
				"post-webhook": map[string]any{
					"name":        "Post Webhook",
					"description": "Post a webhook notification",
					"inputs": map[string]any{
						"url": map[string]any{
							"description": "Webhook URL",
							"required":    true,
							"type":        "string",
						},
					},
					// Users write only the body
					"script": "return async (m) => ({ success: true });",
				},
			},
		},
	}

	config := compiler.extractSafeOutputsConfig(frontmatter)

	require.NotNil(t, config, "Should extract config")
	require.Len(t, config.Scripts, 1, "Should have 1 script")

	script, exists := config.Scripts["post-webhook"]
	require.True(t, exists, "Should have post-webhook script")
	assert.Equal(t, "Post Webhook", script.Name, "Name should match")
	assert.Equal(t, "Post a webhook notification", script.Description, "Description should match")
	require.Len(t, script.Inputs, 1, "Should have 1 input")
}

// TestHasAnySafeOutputEnabledWithScripts verifies Scripts are detected as enabled
func TestHasAnySafeOutputEnabledWithScripts(t *testing.T) {
	config := &SafeOutputsConfig{
		Scripts: map[string]*SafeScriptConfig{
			"my-script": {
				Script: "return async (m) => ({ success: true });",
			},
		},
	}
	assert.True(t, hasAnySafeOutputEnabled(config), "Should detect scripts as enabled safe outputs")
}

// TestHasNonBuiltinSafeOutputsEnabledWithScripts verifies Scripts count as non-builtin
func TestHasNonBuiltinSafeOutputsEnabledWithScripts(t *testing.T) {
	config := &SafeOutputsConfig{
		Scripts: map[string]*SafeScriptConfig{
			"my-script": {
				Script: "return async (m) => ({ success: true });",
			},
		},
	}
	assert.True(t, hasNonBuiltinSafeOutputsEnabled(config), "Scripts should count as non-builtin safe outputs")
}

// TestIsSafeScriptName verifies path traversal detection in script names
func TestIsSafeScriptName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Valid names
		{"plain name", "my_handler", true},
		{"name with numbers", "handler_v2", true},
		{"single word", "handler", true},
		{"dot in name", "handler.name", true},
		// Invalid — path separators and traversal sequences
		{"forward slash", "sub/handler", false},
		{"backslash", `sub\handler`, false},
		{"double dot", "handler..", false},
		{"leading dot-dot", "../evil", false},
		{"double dot only", "..", false},
		{"traversal chain", "../../etc/shadow", false},
		{"dot-dot at end", "handler/../../", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSafeScriptName(tt.input)
			assert.Equal(t, tt.want, got, "isSafeScriptName(%q) should be %v", tt.input, tt.want)
		})
	}
}

// TestBuildCustomSafeOutputScriptsJSONPathTraversal verifies path traversal names are rejected
func TestBuildCustomSafeOutputScriptsJSONPathTraversal(t *testing.T) {
	adversarialInputs := []struct {
		name      string
		scriptKey string
	}{
		{"forward slash in name", "sub/evil"},
		{"backslash in name", `sub\evil`},
		{"double dot traversal", "../evil"},
		{"double dot only", ".."},
		{"multi-level traversal", "../../etc/shadow"},
	}

	for _, tt := range adversarialInputs {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					Scripts: map[string]*SafeScriptConfig{
						tt.scriptKey: {
							Script: "return async (m) => ({ success: true });",
						},
					},
				},
			}

			jsonStr := buildCustomSafeOutputScriptsJSON(data)

			// The adversarial script name must not appear in the output JSON.
			// (An empty JSON object `{}` or empty string are both acceptable outputs.)
			assert.NotContains(t, jsonStr, "evil", "Path traversal name %q should not appear in output JSON", tt.scriptKey)
			assert.NotContains(t, jsonStr, "shadow", "Path traversal name %q should not appear in output JSON", tt.scriptKey)
			assert.NotContains(t, jsonStr, "..", "Double-dot must not appear in output JSON for %q", tt.scriptKey)
		})
	}
}

// TestBuildCustomSafeOutputScriptsJSONPathTraversalMixedWithSafe verifies unsafe names
// are dropped while safe names in the same map are still included.
func TestBuildCustomSafeOutputScriptsJSONPathTraversalMixedWithSafe(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Scripts: map[string]*SafeScriptConfig{
				"good_handler": {Script: "return async (m) => ({ success: true });"},
				"../evil":      {Script: "return async (m) => ({ success: true });"},
			},
		},
	}

	jsonStr := buildCustomSafeOutputScriptsJSON(data)
	require.NotEmpty(t, jsonStr, "Safe name should still produce output")

	var mapping map[string]string
	err := json.Unmarshal([]byte(jsonStr), &mapping)
	require.NoError(t, err, "JSON should be valid")

	assert.Contains(t, mapping, "good_handler", "Safe name should be present")
	// The traversal name should be absent — NormalizeSafeOutputIdentifier converts - to _ but
	// keeps .. and /, so the key would contain path characters and be filtered out.
	for key := range mapping {
		assert.NotContains(t, key, "..", "Traversal key must not appear in output: %q", key)
		assert.NotContains(t, key, "/", "Key with slash must not appear in output: %q", key)
	}
}
