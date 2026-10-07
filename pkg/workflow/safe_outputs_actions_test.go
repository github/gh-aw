//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseActionsConfig verifies parsing of safe-outputs.actions configuration
func TestParseActionsConfig(t *testing.T) {
	actionsMap := map[string]any{
		"my-tool": map[string]any{
			"uses":        "owner/repo@v1",
			"description": "My custom tool",
		},
		"another-tool": map[string]any{
			"uses": "owner/other-repo/subdir@v2",
		},
	}

	result := parseActionsConfig(actionsMap)

	require.NotNil(t, result, "Should return non-nil result")
	require.Len(t, result, 2, "Should have two actions")

	myTool, exists := result["my-tool"]
	require.True(t, exists, "Should have my-tool action")
	assert.Equal(t, "owner/repo@v1", myTool.Uses, "Uses should match")
	assert.Equal(t, "My custom tool", myTool.Description, "Description should match")

	anotherTool, exists := result["another-tool"]
	require.True(t, exists, "Should have another-tool action")
	assert.Equal(t, "owner/other-repo/subdir@v2", anotherTool.Uses, "Uses should match")
	assert.Empty(t, anotherTool.Description, "Description should be empty")
}

// TestParseActionsConfigMissingUses verifies actions without uses are skipped
func TestParseActionsConfigMissingUses(t *testing.T) {
	actionsMap := map[string]any{
		"invalid-tool": map[string]any{
			"description": "No uses field",
		},
		"valid-tool": map[string]any{
			"uses": "owner/repo@v1",
		},
	}

	result := parseActionsConfig(actionsMap)

	require.Len(t, result, 1, "Should only have the valid action")
	_, exists := result["invalid-tool"]
	assert.False(t, exists, "Should not have invalid-tool (missing uses)")
	_, exists = result["valid-tool"]
	assert.True(t, exists, "Should have valid-tool")
}

// TestParseActionsConfigEmpty verifies empty map returns nil or empty
func TestParseActionsConfigEmpty(t *testing.T) {
	result := parseActionsConfig(map[string]any{})
	assert.Empty(t, result, "Should return empty result for empty map")

	result = parseActionsConfig(nil)
	assert.Nil(t, result, "Should return nil for nil input")
}

// TestParseActionUsesField verifies parsing of different uses field formats
func TestParseActionUsesField(t *testing.T) {
	tests := []struct {
		name       string
		uses       string
		wantRepo   string
		wantSubdir string
		wantRef    string
		wantLocal  bool
		wantErr    bool
	}{
		{
			name:     "root action",
			uses:     "owner/repo@v1",
			wantRepo: "owner/repo",
			wantRef:  "v1",
		},
		{
			name:       "sub-directory action",
			uses:       "owner/repo/subpath@v2",
			wantRepo:   "owner/repo",
			wantSubdir: "subpath",
			wantRef:    "v2",
		},
		{
			name:       "deep sub-directory action",
			uses:       "owner/repo/path/to/action@v3",
			wantRepo:   "owner/repo",
			wantSubdir: "path/to/action",
			wantRef:    "v3",
		},
		{
			name:      "local path",
			uses:      "./local/path",
			wantLocal: true,
		},
		{
			name:      "local path with parent",
			uses:      "../parent/path",
			wantLocal: true,
		},
		{
			name:    "missing ref",
			uses:    "owner/repo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := parseActionUsesField(tt.uses)
			if tt.wantErr {
				assert.Error(t, err, "Should return error")
				return
			}
			require.NoError(t, err, "Should not return error")
			assert.Equal(t, tt.wantLocal, ref.IsLocal, "IsLocal mismatch")
			if tt.wantLocal {
				return
			}
			assert.Equal(t, tt.wantRepo, ref.Repo, "Repo mismatch")
			assert.Equal(t, tt.wantSubdir, ref.Subdir, "Subdir mismatch")
			assert.Equal(t, tt.wantRef, ref.Ref, "Ref mismatch")
		})
	}
}

// TestGenerateActionToolDefinition verifies tool definition generation
func TestGenerateActionToolDefinition(t *testing.T) {
	config := &SafeOutputActionConfig{
		Uses:              "owner/repo@v1",
		Description:       "My custom action tool",
		ResolvedRef:       "owner/repo@abc123 # v1",
		ActionDescription: "Default description from action.yml",
		Inputs: map[string]*ActionYAMLInput{
			"title": {
				Description: "The title input",
				Required:    true,
			},
			"body": {
				Description: "The body input",
				Required:    false,
				Default:     "default body",
			},
		},
	}

	tool := generateActionToolDefinition("my-tool", config)

	require.NotNil(t, tool, "Tool definition should not be nil")
	assert.Equal(t, "my_tool", tool["name"], "Tool name should be normalized")
	assert.Equal(t, "My custom action tool (can only be called once)", tool["description"], "Description should use user override + constraint")

	schema, ok := tool["inputSchema"].(map[string]any)
	require.True(t, ok, "inputSchema should be a map")
	assert.Equal(t, "object", schema["type"])
	assert.Equal(t, false, schema["additionalProperties"])

	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "properties should be a map")
	assert.Len(t, properties, 2, "Should have 2 properties")

	titleProp, ok := properties["title"].(map[string]any)
	require.True(t, ok, "title property should exist")
	assert.Equal(t, "string", titleProp["type"])
	assert.Equal(t, "The title input", titleProp["description"])

	bodyProp, ok := properties["body"].(map[string]any)
	require.True(t, ok, "body property should exist")
	assert.Equal(t, "string", bodyProp["type"])
	assert.Equal(t, "default body", bodyProp["default"])

	required, ok := schema["required"].([]string)
	require.True(t, ok, "required should be a []string")
	assert.Equal(t, []string{"title"}, required, "Only title should be required")
}

// TestGenerateActionToolDefinitionFallbackDescription verifies description fallback order
func TestGenerateActionToolDefinitionFallbackDescription(t *testing.T) {
	tests := []struct {
		name            string
		config          *SafeOutputActionConfig
		wantDescription string
	}{
		{
			name: "user description takes precedence",
			config: &SafeOutputActionConfig{
				Description:       "User description",
				ActionDescription: "Action description",
			},
			wantDescription: "User description (can only be called once)",
		},
		{
			name: "action description used when no user description",
			config: &SafeOutputActionConfig{
				ActionDescription: "Action description",
			},
			wantDescription: "Action description (can only be called once)",
		},
		{
			name:            "fallback to action name",
			config:          &SafeOutputActionConfig{},
			wantDescription: "Run the my-action action (can only be called once)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := generateActionToolDefinition("my-action", tt.config)
			assert.Equal(t, tt.wantDescription, tool["description"], "Description should match expected")
		})
	}
}

// TestGenerateActionToolDefinitionNoInputs verifies the fallback schema when resolution failed (Inputs == nil)
func TestGenerateActionToolDefinitionNoInputs(t *testing.T) {
	config := &SafeOutputActionConfig{
		Uses: "owner/repo@v1",
		// Inputs is nil (action.yml resolution failed)
	}

	tool := generateActionToolDefinition("my-tool", config)

	schema, ok := tool["inputSchema"].(map[string]any)
	require.True(t, ok, "inputSchema should be a map")
	// Fallback schema should be permissive (additionalProperties: true) so agent can still call the tool
	assert.Equal(t, true, schema["additionalProperties"], "Fallback schema should allow additional properties")
	// Should have a payload property to hint to the agent what to send
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "properties should be a map")
	assert.Contains(t, properties, "payload", "Fallback schema should include a 'payload' property")
	assert.Nil(t, schema["required"], "No required fields in fallback schema")
}

// TestBuildCustomSafeOutputActionsJSON verifies JSON generation for GH_AW_SAFE_OUTPUT_ACTIONS
func TestBuildCustomSafeOutputActionsJSON(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Actions: map[string]*SafeOutputActionConfig{
				"my-tool":      {Uses: "owner/repo@v1"},
				"another-tool": {Uses: "owner/repo2@v2"},
			},
		},
	}

	jsonStr := buildCustomSafeOutputActionsJSON(data)
	require.NotEmpty(t, jsonStr, "Should return non-empty JSON")

	var result map[string]string
	err := json.Unmarshal([]byte(jsonStr), &result)
	require.NoError(t, err, "Should be valid JSON")

	assert.Equal(t, "my_tool", result["my_tool"], "Normalized name should be in map")
	assert.Equal(t, "another_tool", result["another_tool"], "Normalized name should be in map")
}

// TestBuildCustomSafeOutputActionsJSONEmpty verifies empty result when no actions
func TestBuildCustomSafeOutputActionsJSONEmpty(t *testing.T) {
	assert.Empty(t, buildCustomSafeOutputActionsJSON(&WorkflowData{SafeOutputs: &SafeOutputsConfig{}}), "Should return empty for no actions")
	assert.Empty(t, buildCustomSafeOutputActionsJSON(&WorkflowData{SafeOutputs: nil}), "Should return empty for nil SafeOutputs")
}

// TestBuildActionSteps verifies step generation for configured actions
func TestBuildActionSteps(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Actions: map[string]*SafeOutputActionConfig{
				"my-tool": {
					Uses:        "owner/repo@v1",
					Description: "My tool description",
					ResolvedRef: "owner/repo@abc123 # v1",
					Inputs: map[string]*ActionYAMLInput{
						"title": {Required: true},
						"body":  {Required: false},
					},
				},
			},
		},
	}

	steps := compiler.buildActionSteps(data)
	require.NotEmpty(t, steps, "Should generate steps")

	fullYAML := strings.Join(steps, "")
	assert.Contains(t, fullYAML, "My tool description", "Should use description as step name")
	assert.Contains(t, fullYAML, "id: action_my_tool", "Should have step ID")
	assert.Contains(t, fullYAML, "steps.process_safe_outputs.outputs.action_my_tool_payload != ''", "Should have if condition")
	assert.Contains(t, fullYAML, "uses: owner/repo@abc123 # v1", "Should use resolved ref")
	assert.Contains(t, fullYAML, "title:", "Should have title input")
	assert.Contains(t, fullYAML, "body:", "Should have body input")
	assert.Contains(t, fullYAML, "fromJSON(steps.process_safe_outputs.outputs.action_my_tool_payload).title", "Should use fromJSON for title")
	assert.Contains(t, fullYAML, "fromJSON(steps.process_safe_outputs.outputs.action_my_tool_payload).body", "Should use fromJSON for body")
}

// TestBuildActionStepsFallbackPayload verifies step uses payload when inputs unknown
func TestBuildActionStepsFallbackPayload(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Actions: map[string]*SafeOutputActionConfig{
				"unknown-inputs-tool": {
					Uses:        "owner/repo@v1",
					ResolvedRef: "owner/repo@abc123 # v1",
					// No Inputs: action.yml couldn't be fetched
				},
			},
		},
	}

	steps := compiler.buildActionSteps(data)
	require.NotEmpty(t, steps, "Should generate steps even without inputs")

	fullYAML := strings.Join(steps, "")
	assert.Contains(t, fullYAML, "payload:", "Should use payload as single with: key when inputs unknown")
	assert.Contains(t, fullYAML, "steps.process_safe_outputs.outputs.action_unknown_inputs_tool_payload", "Should reference payload output")
}

func TestFetchAndParseActionYAMLUsesMappedTargetMetadata(t *testing.T) {
	cache := NewActionCache(t.TempDir())
	const targetSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	require.True(t, cache.Set("internal/script", "v1", targetSHA))
	targetInputs := map[string]*ActionYAMLInput{"title": {Required: true}}
	cache.SetInputs("internal/script", "v1", targetInputs)
	cache.SetActionDescription("internal/script", "v1", "Mapped action")

	data := &WorkflowData{
		ActionCache:    cache,
		ActionResolver: NewActionResolver(cache),
		ActionPinMappings: map[string]string{
			"actions/source@v1": "internal/script@v1",
		},
	}
	config := &SafeOutputActionConfig{Uses: "actions/source@v1"}

	err := NewCompiler().fetchAndParseActionYAML("mapped-tool", config, "workflow.md", data)
	require.NoError(t, err)
	assert.Equal(t, "internal/script@"+targetSHA+" # v1 "+exactPinMappingMarker, config.ResolvedRef)
	assert.Equal(t, targetInputs, config.Inputs)
	assert.Equal(t, "Mapped action", config.ActionDescription)
}

// TestBuildActionStepsEmpty verifies no steps when no actions
func TestBuildActionStepsEmpty(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{},
	}
	assert.Nil(t, compiler.buildActionSteps(data), "Should return nil for empty actions")
}

// TestExtractSafeOutputsConfigIncludesActions verifies extractSafeOutputsConfig handles actions
func TestExtractSafeOutputsConfigIncludesActions(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"actions": map[string]any{
				"my-action": map[string]any{
					"uses":        "owner/repo@v1",
					"description": "My action",
				},
			},
		},
	}

	config := compiler.extractSafeOutputsConfig(frontmatter)

	require.NotNil(t, config, "Should extract config")
	require.Len(t, config.Actions, 1, "Should have 1 action")

	action, exists := config.Actions["my-action"]
	require.True(t, exists, "Should have my-action")
	assert.Equal(t, "owner/repo@v1", action.Uses, "Uses should match")
	assert.Equal(t, "My action", action.Description, "Description should match")
}

// TestHandlerManagerStepIncludesActionsEnvVar verifies GH_AW_SAFE_OUTPUT_ACTIONS env var
func TestHandlerManagerStepIncludesActionsEnvVar(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
			Actions: map[string]*SafeOutputActionConfig{
				"my-action": {Uses: "owner/repo@v1"},
			},
		},
	}

	steps, err := compiler.buildHandlerManagerStep(workflowData)
	require.NoError(t, err)
	fullYAML := strings.Join(steps, "")

	assert.Contains(t, fullYAML, "GH_AW_SAFE_OUTPUT_ACTIONS", "Should include GH_AW_SAFE_OUTPUT_ACTIONS env var")
	assert.Contains(t, fullYAML, "my_action", "Should include normalized action name")
}

// TestHandlerManagerStepNoActionsEnvVar verifies GH_AW_SAFE_OUTPUT_ACTIONS absent when no actions
func TestHandlerManagerStepNoActionsEnvVar(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
	}

	steps, err := compiler.buildHandlerManagerStep(workflowData)
	require.NoError(t, err)
	fullYAML := strings.Join(steps, "")

	assert.NotContains(t, fullYAML, "GH_AW_SAFE_OUTPUT_ACTIONS", "Should not include GH_AW_SAFE_OUTPUT_ACTIONS when no actions")
}

// TestHasAnySafeOutputEnabledWithActions verifies Actions are detected as enabled
func TestHasAnySafeOutputEnabledWithActions(t *testing.T) {
	config := &SafeOutputsConfig{
		Actions: map[string]*SafeOutputActionConfig{
			"my-action": {Uses: "owner/repo@v1"},
		},
	}
	assert.True(t, hasAnySafeOutputEnabled(config), "Should detect actions as enabled safe outputs")
}

// TestHasNonBuiltinSafeOutputsEnabledWithActions verifies Actions count as non-builtin
func TestHasNonBuiltinSafeOutputsEnabledWithActions(t *testing.T) {
	config := &SafeOutputsConfig{
		Actions: map[string]*SafeOutputActionConfig{
			"my-action": {Uses: "owner/repo@v1"},
		},
	}
	assert.True(t, hasNonBuiltinSafeOutputsEnabled(config), "Actions should count as non-builtin safe outputs")
}

// TestActionOutputKey verifies the output key naming convention
func TestActionOutputKey(t *testing.T) {
	assert.Equal(t, "action_my_tool_payload", actionOutputKey("my_tool"))
	assert.Equal(t, "action_another_action_payload", actionOutputKey("another_action"))
}

// TestParseActionsConfigWithEnv verifies parsing of env field in actions config
func TestParseActionsConfigWithEnv(t *testing.T) {
	actionsMap := map[string]any{
		"my-tool": map[string]any{
			"uses": "owner/repo@v1",
			"env": map[string]any{
				"MY_VAR":    "my-value",
				"OTHER_VAR": "other-value",
			},
		},
	}

	result := parseActionsConfig(actionsMap)
	require.Len(t, result, 1, "Should have one action")

	myTool := result["my-tool"]
	require.NotNil(t, myTool, "Should have my-tool")
	require.Len(t, myTool.Env, 2, "Should have 2 env vars")
	assert.Equal(t, "my-value", myTool.Env["MY_VAR"], "MY_VAR should match")
	assert.Equal(t, "other-value", myTool.Env["OTHER_VAR"], "OTHER_VAR should match")
}

// TestParseActionsConfigWithInputs verifies parsing of inputs field in actions config
func TestParseActionsConfigWithInputs(t *testing.T) {
	actionsMap := map[string]any{
		"add-label": map[string]any{
			"uses": "actions-ecosystem/action-add-labels@v1",
			"inputs": map[string]any{
				"labels": map[string]any{
					"description": "The labels' name to be added.",
					"required":    true,
				},
				"number": map[string]any{
					"description": "The number of the issue or pull request.",
				},
			},
		},
	}

	result := parseActionsConfig(actionsMap)
	require.Len(t, result, 1, "Should have one action")

	tool := result["add-label"]
	require.NotNil(t, tool, "Should have add-label")
	require.NotNil(t, tool.Inputs, "Should have inputs populated from frontmatter")
	require.Len(t, tool.Inputs, 2, "Should have 2 inputs")

	labelsInput := tool.Inputs["labels"]
	require.NotNil(t, labelsInput, "Should have labels input")
	assert.Equal(t, "The labels' name to be added.", labelsInput.Description, "Labels description should match")
	assert.True(t, labelsInput.Required, "Labels should be required")

	numberInput := tool.Inputs["number"]
	require.NotNil(t, numberInput, "Should have number input")
	assert.Equal(t, "The number of the issue or pull request.", numberInput.Description, "Number description should match")
	assert.False(t, numberInput.Required, "Number should not be required")
}

func TestBuildActionStepsWithEnv(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Actions: map[string]*SafeOutputActionConfig{
				"my-tool": {
					Uses:        "owner/repo@v1",
					ResolvedRef: "owner/repo@abc123 # v1",
					Env: map[string]string{
						"MY_SECRET": "${{ secrets.MY_SECRET }}",
						"MY_VAR":    "static-value",
					},
					Inputs: map[string]*ActionYAMLInput{
						"title": {Required: true},
					},
				},
			},
		},
	}

	steps := compiler.buildActionSteps(data)
	require.NotEmpty(t, steps, "Should generate steps")

	fullYAML := strings.Join(steps, "")
	assert.Contains(t, fullYAML, "env:", "Should have env block")
	assert.Contains(t, fullYAML, "MY_SECRET: ${{ secrets.MY_SECRET }}", "Should have MY_SECRET env var")
	assert.Contains(t, fullYAML, "MY_VAR: static-value", "Should have MY_VAR env var")
	// Env block should appear before with: block
	envIdx := strings.Index(fullYAML, "env:")
	withIdx := strings.Index(fullYAML, "with:")
	assert.Less(t, envIdx, withIdx, "env: should appear before with:")
}

// TestBuildActionStepsNoEnv verifies that no env block is emitted when Env is empty
func TestBuildActionStepsNoEnv(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Actions: map[string]*SafeOutputActionConfig{
				"my-tool": {
					Uses:        "owner/repo@v1",
					ResolvedRef: "owner/repo@abc123 # v1",
					Inputs: map[string]*ActionYAMLInput{
						"title": {Required: true},
					},
				},
			},
		},
	}

	steps := compiler.buildActionSteps(data)
	fullYAML := strings.Join(steps, "")
	assert.NotContains(t, fullYAML, "env:", "Should not have env block when Env is nil/empty")
}

// TestExtractSafeOutputsConfigIncludesActionsWithEnv verifies env is parsed via extractSafeOutputsConfig
func TestExtractSafeOutputsConfigIncludesActionsWithEnv(t *testing.T) {
	compiler := NewCompiler()
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"actions": map[string]any{
				"my-action": map[string]any{
					"uses": "owner/repo@v1",
					"env": map[string]any{
						"TOKEN": "${{ secrets.MY_TOKEN }}",
					},
				},
			},
		},
	}

	config := compiler.extractSafeOutputsConfig(frontmatter)

	require.NotNil(t, config, "Should extract config")
	action := config.Actions["my-action"]
	require.NotNil(t, action, "Should have my-action")
	require.Len(t, action.Env, 1, "Should have 1 env var")
	assert.Equal(t, "${{ secrets.MY_TOKEN }}", action.Env["TOKEN"], "TOKEN env var should match")
}

// TestIsGitHubExpressionDefault verifies detection of GitHub expression defaults
func TestIsGitHubExpressionDefault(t *testing.T) {
	tests := []struct {
		name     string
		input    *ActionYAMLInput
		expected bool
	}{
		{"nil input", nil, false},
		{"no default", &ActionYAMLInput{}, false},
		{"static default", &ActionYAMLInput{Default: "latest"}, false},
		{"github token expression", &ActionYAMLInput{Default: "${{ github.token }}"}, true},
		{"pr number expression", &ActionYAMLInput{Default: "${{ github.event.pull_request.number }}"}, true},
		{"expression with leading whitespace", &ActionYAMLInput{Default: "  ${{ github.token }}"}, true},
		{"partial expression no closing", &ActionYAMLInput{Default: "${{ incomplete"}, true}, // starts with ${{
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isGitHubExpressionDefault(tt.input), "isGitHubExpressionDefault result should match")
		})
	}
}

// TestBuildActionStepsSkipsGitHubExpressionDefaultInputs verifies inputs with ${{ defaults are excluded
func TestBuildActionStepsSkipsGitHubExpressionDefaultInputs(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			Actions: map[string]*SafeOutputActionConfig{
				"add-labels": {
					Uses:        "actions-ecosystem/action-add-labels@v1",
					ResolvedRef: "actions-ecosystem/action-add-labels@abc123 # v1",
					Inputs: map[string]*ActionYAMLInput{
						"github_token": {Required: true, Default: "${{ github.token }}"},
						"number":       {Required: false, Default: "${{ github.event.pull_request.number }}"},
						"labels":       {Required: true},
					},
				},
			},
		},
	}

	steps := compiler.buildActionSteps(data)
	fullYAML := strings.Join(steps, "")

	// Only 'labels' should be in with: block
	assert.Contains(t, fullYAML, "labels:", "labels should be in with: block")
	assert.NotContains(t, fullYAML, "github_token:", "github_token should be excluded (has ${{ default})")
	assert.NotContains(t, fullYAML, "number:", "number should be excluded (has ${{ default})")
}

// TestGenerateActionToolDefinitionSkipsGitHubExpressionInputs verifies schema omits ${{ inputs
func TestGenerateActionToolDefinitionSkipsGitHubExpressionInputs(t *testing.T) {
	config := &SafeOutputActionConfig{
		Uses: "actions-ecosystem/action-add-labels@v1",
		Inputs: map[string]*ActionYAMLInput{
			"github_token": {Required: true, Default: "${{ github.token }}"},
			"number":       {Required: false, Default: "${{ github.event.pull_request.number }}"},
			"labels":       {Required: true, Description: "Labels to add"},
		},
	}

	tool := generateActionToolDefinition("add-labels", config)

	schema, ok := tool["inputSchema"].(map[string]any)
	require.True(t, ok, "inputSchema should be a map")
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "properties should be a map")

	assert.Contains(t, properties, "labels", "labels should be in schema")
	assert.NotContains(t, properties, "github_token", "github_token should be excluded from schema")
	assert.NotContains(t, properties, "number", "number should be excluded from schema")
}

// TestExtractSHAFromPinnedRef verifies SHA extraction from pinned action references
func TestExtractSHAFromPinnedRef(t *testing.T) {
	tests := []struct {
		name     string
		pinned   string
		expected string
	}{
		{
			name:     "standard pinned reference",
			pinned:   "actions-ecosystem/action-add-labels@" + strings.Repeat("a", 40) + " # v1",
			expected: strings.Repeat("a", 40),
		},
		{
			name:     "no @ separator",
			pinned:   "no-at-sign",
			expected: "",
		},
		{
			name:     "non-SHA after @",
			pinned:   "owner/repo@v1 # v1",
			expected: "",
		},
		{
			name:     "empty string",
			pinned:   "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractSHAFromPinnedRef(tt.pinned)
			assert.Equal(t, tt.expected, result, "extractSHAFromPinnedRef result should match")
		})
	}
}
