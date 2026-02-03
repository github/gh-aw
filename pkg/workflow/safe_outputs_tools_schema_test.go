//go:build !integration

package workflow

import (
	_ "embed"
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed schemas/mcp-tools.json
var mcpToolsSchema string

func TestSafeOutputsToolsJSONCompliesWithMCPSchema(t *testing.T) {
	// Get the safe outputs tools JSON
	toolsJSON := GetSafeOutputsToolsJSON()
	require.NotEmpty(t, toolsJSON, "Tools JSON should not be empty")

	// Compile the MCP tools schema
	compiler := jsonschema.NewCompiler()

	// Parse the schema JSON
	var schemaDoc any
	if err := json.Unmarshal([]byte(mcpToolsSchema), &schemaDoc); err != nil {
		t.Fatalf("Failed to parse MCP tools schema: %v", err)
	}

	// Add the schema to the compiler
	if err := compiler.AddResource("mcp-tools.json", schemaDoc); err != nil {
		t.Fatalf("Failed to add MCP tools schema: %v", err)
	}

	schema, err := compiler.Compile("mcp-tools.json")
	require.NoError(t, err, "MCP tools schema should be valid")

	// Parse the tools JSON as a generic interface for validation
	var toolsData any
	err = json.Unmarshal([]byte(toolsJSON), &toolsData)
	require.NoError(t, err, "Tools JSON should be valid JSON")

	// Validate the tools JSON against the schema
	err = schema.Validate(toolsData)
	if err != nil {
		// Provide detailed error information
		t.Errorf("Tools JSON does not comply with MCP schema: %v", err)

		// Parse as array for debugging
		var tools []map[string]any
		if err := json.Unmarshal([]byte(toolsJSON), &tools); err != nil {
			t.Logf("Failed to parse tools for debugging: %v", err)
			return
		}

		// Print the problematic tools for debugging
		t.Logf("Number of tools: %d", len(tools))
		for i, tool := range tools {
			toolJSON, _ := json.MarshalIndent(tool, "", "  ")
			t.Logf("Tool %d:\n%s", i+1, string(toolJSON))
		}
	}

	assert.NoError(t, err, "Tools JSON should comply with MCP tools schema")
}

func TestEachToolHasRequiredMCPFields(t *testing.T) {
	// Get the safe outputs tools JSON
	toolsJSON := GetSafeOutputsToolsJSON()
	require.NotEmpty(t, toolsJSON, "Tools JSON should not be empty")

	// Parse the tools JSON
	var tools []map[string]any
	err := json.Unmarshal([]byte(toolsJSON), &tools)
	require.NoError(t, err, "Tools JSON should be valid JSON")

	// Check each tool has the required fields according to MCP spec
	for i, tool := range tools {
		t.Run(tool["name"].(string), func(t *testing.T) {
			// Required: name
			assert.Contains(t, tool, "name", "Tool %d should have 'name' field", i)
			assert.IsType(t, "", tool["name"], "Tool %d 'name' should be a string", i)
			assert.NotEmpty(t, tool["name"], "Tool %d 'name' should not be empty", i)

			// Optional but recommended: description
			if desc, ok := tool["description"]; ok {
				assert.IsType(t, "", desc, "Tool %d 'description' should be a string if present", i)
			}

			// Required: inputSchema
			assert.Contains(t, tool, "inputSchema", "Tool %d should have 'inputSchema' field", i)

			// Validate inputSchema structure
			inputSchema, ok := tool["inputSchema"].(map[string]any)
			require.True(t, ok, "Tool %d 'inputSchema' should be an object", i)

			// inputSchema must have type: "object"
			assert.Contains(t, inputSchema, "type", "Tool %d inputSchema should have 'type' field", i)
			assert.Equal(t, "object", inputSchema["type"], "Tool %d inputSchema type should be 'object'", i)

			// inputSchema should have properties
			assert.Contains(t, inputSchema, "properties", "Tool %d inputSchema should have 'properties' field", i)
			properties, ok := inputSchema["properties"].(map[string]any)
			require.True(t, ok, "Tool %d inputSchema 'properties' should be an object", i)
			assert.NotEmpty(t, properties, "Tool %d inputSchema 'properties' should not be empty", i)

			// If required field exists, it should be an array of strings
			if required, ok := inputSchema["required"]; ok {
				requiredArray, ok := required.([]any)
				require.True(t, ok, "Tool %d inputSchema 'required' should be an array", i)
				for _, req := range requiredArray {
					assert.IsType(t, "", req, "Tool %d inputSchema 'required' items should be strings", i)
				}
			}
		})
	}
}

func TestToolsJSONStructureMatchesMCPSpecification(t *testing.T) {
	// Get the safe outputs tools JSON
	toolsJSON := GetSafeOutputsToolsJSON()
	require.NotEmpty(t, toolsJSON, "Tools JSON should not be empty")

	// Parse the tools JSON
	var tools []map[string]any
	err := json.Unmarshal([]byte(toolsJSON), &tools)
	require.NoError(t, err, "Tools JSON should be valid JSON")

	// Verify the structure matches MCP specification
	for _, tool := range tools {
		name := tool["name"].(string)
		t.Run(name, func(t *testing.T) {
			// Verify no unexpected top-level fields
			allowedFields := map[string]bool{
				"name":         true,
				"title":        true,
				"description":  true,
				"inputSchema":  true,
				"outputSchema": true,
				"annotations":  true,
			}

			for field := range tool {
				assert.Contains(t, allowedFields, field,
					"Tool '%s' has unexpected field '%s'. MCP tools should only have: name, title, description, inputSchema, outputSchema, annotations",
					name, field)
			}

			// If outputSchema exists, validate its structure
			if outputSchema, ok := tool["outputSchema"]; ok {
				outputSchemaObj, ok := outputSchema.(map[string]any)
				require.True(t, ok, "Tool '%s' outputSchema should be an object", name)

				// outputSchema must have type: "object"
				assert.Contains(t, outputSchemaObj, "type", "Tool '%s' outputSchema should have 'type' field", name)
				assert.Equal(t, "object", outputSchemaObj["type"], "Tool '%s' outputSchema type should be 'object'", name)
			}

			// If annotations exists, validate its structure
			if annotations, ok := tool["annotations"]; ok {
				annotationsObj, ok := annotations.(map[string]any)
				require.True(t, ok, "Tool '%s' annotations should be an object", name)

				// Verify only allowed annotation fields
				allowedAnnotations := map[string]bool{
					"title":           true,
					"readOnlyHint":    true,
					"destructiveHint": true,
					"idempotentHint":  true,
					"openWorldHint":   true,
				}

				for field := range annotationsObj {
					assert.Contains(t, allowedAnnotations, field,
						"Tool '%s' annotations has unexpected field '%s'. Allowed fields: title, readOnlyHint, destructiveHint, idempotentHint, openWorldHint",
						name, field)
				}
			}
		})
	}
}

// TestNoTopLevelOneOfAllOfAnyOf validates that no tools have oneOf/allOf/anyOf at the top level
// of their inputSchema, as these are not supported by Claude's API and other AI engines.
// This is a regression test for https://github.com/github/gh-aw/actions/runs/21142123455
func TestNoTopLevelOneOfAllOfAnyOf(t *testing.T) {
	// Get the safe outputs tools JSON
	toolsJSON := GetSafeOutputsToolsJSON()
	require.NotEmpty(t, toolsJSON, "Tools JSON should not be empty")

	// Parse the tools JSON
	var tools []map[string]any
	err := json.Unmarshal([]byte(toolsJSON), &tools)
	require.NoError(t, err, "Tools JSON should be valid JSON")

	// Check each tool for forbidden top-level schema constraints
	for _, tool := range tools {
		name := tool["name"].(string)
		t.Run(name, func(t *testing.T) {
			// Get the inputSchema
			inputSchema, ok := tool["inputSchema"].(map[string]any)
			require.True(t, ok, "Tool '%s' should have inputSchema as an object", name)

			// Check for forbidden top-level constraints
			// These constraints are not supported by Claude's API and should be avoided
			assert.NotContains(t, inputSchema, "oneOf",
				"Tool '%s' has 'oneOf' at top level of inputSchema. "+
					"Claude API does not support oneOf/allOf/anyOf at the top level. "+
					"Use optional fields with validation documented in descriptions instead. "+
					"See: https://github.com/github/gh-aw/actions/runs/21142123455", name)

			assert.NotContains(t, inputSchema, "allOf",
				"Tool '%s' has 'allOf' at top level of inputSchema. "+
					"Claude API does not support oneOf/allOf/anyOf at the top level. "+
					"Use optional fields with validation documented in descriptions instead. "+
					"See: https://github.com/github/gh-aw/actions/runs/21142123455", name)

			assert.NotContains(t, inputSchema, "anyOf",
				"Tool '%s' has 'anyOf' at top level of inputSchema. "+
					"Claude API does not support oneOf/allOf/anyOf at the top level. "+
					"Use optional fields with validation documented in descriptions instead. "+
					"See: https://github.com/github/gh-aw/actions/runs/21142123455", name)
		})
	}
}
