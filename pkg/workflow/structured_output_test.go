//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type structuredOutputTestEngine struct {
	BaseEngine
}

func (e *structuredOutputTestEngine) GetCapabilities() EngineCapabilities {
	return EngineCapabilities{StructuredOutput: true}
}

func TestStructuredOutputSchema(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		error  string
	}{
		{"valid draft 7", `{"type":"object","properties":{"decision":{"type":"string","enum":["APPROVE","ESCALATE"]}},"required":["decision"],"additionalProperties":false}`, ""},
		{"explicit draft 7", `{"$schema":"http://json-schema.org/draft-07/schema","type":"object"}`, ""},
		{"https draft 7", `{"$schema":"https://json-schema.org/draft-07/schema#","type":"object"}`, ""},
		{"valid 2020", `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"values":{"type":"array","prefixItems":[{"type":"string"}]}}}`, ""},
		{"2020 fragment", `{"$schema":"https://json-schema.org/draft/2020-12/schema#","type":"object"}`, ""},
		{"local reference", `{"type":"object","properties":{"value":{"$ref":"#/$defs/value"}},"$defs":{"value":{"type":"string"}}}`, ""},
		{"malformed type", `{"type":"object","properties":{"value":{"type":"strnig"}}}`, "not a valid JSON Schema"},
		{"wrong root", `{"type":"array"}`, "type: object"},
		{"unsupported draft", `{"$schema":"http://json-schema.org/draft-04/schema#","type":"object"}`, "draft-07 or 2020-12"},
		{"external reference", `{"type":"object","properties":{"value":{"$ref":"https://example.com/schema.json"}}}`, "local fragments"},
		{"external dynamic reference", `{"type":"object","$dynamicRef":"https://example.com/schema.json"}`, "local fragments"},
		{"dynamic schema", `{"type":"object","description":"${{ secrets.TOKEN }}"}`, "must be static"},
		{"reference-looking literal", `{"type":"object","properties":{"$ref":{"type":"string"}},"const":{"$ref":"literal-value"}}`, ""},
		{"unresolved local reference", `{"type":"object","properties":{"value":{"$ref":"#/$defs/missing"}}}`, "not a valid JSON Schema"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var schema map[string]any
			require.NoError(t, json.Unmarshal([]byte(tt.schema), &schema))
			err := validateStructuredOutputSchema(schema)
			if tt.error == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.error)
			}
		})
	}
}

func TestStructuredOutputConfiguration(t *testing.T) {
	engine := &structuredOutputTestEngine{}
	valid := map[string]any{"schema": map[string]any{"type": "object"}}
	config, err := parseStructuredOutput(map[string]any{"structured-output": valid}, "workflow.md", engine, nil)
	require.NoError(t, err)
	require.NotNil(t, config)
	assert.Equal(t, "object", config.Schema["type"])

	config, err = parseStructuredOutput(map[string]any{}, "workflow.md", engine, nil)
	require.NoError(t, err)
	assert.Nil(t, config)

	_, err = parseStructuredOutput(map[string]any{"structured-output": valid}, "workflow.md", &BaseEngine{}, nil)
	require.ErrorContains(t, err, "native JSON Schema output support")
	for _, setting := range []any{
		nil,
		"json",
		map[string]any{},
		map[string]any{"schema": map[string]any{"type": "object"}, "schema-file": "schema.json"},
		map[string]any{"schema": "wrong"},
		map[string]any{"schema-file": "/tmp/schema.json"},
		map[string]any{"schema": map[string]any{"type": "object"}, "typo": true},
	} {
		_, err := parseStructuredOutput(map[string]any{"structured-output": setting}, "workflow.md", engine, nil)
		require.Error(t, err)
	}
}

func TestStructuredOutputSchemaFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0755))
	schemaPath := filepath.Join(root, "schema.json")
	require.NoError(t, os.WriteFile(schemaPath, []byte(`{"type":"object"}`), 0600))
	workflowPath := filepath.Join(root, ".github", "workflows", "test.md")
	engine := &structuredOutputTestEngine{}
	settings := func(path string) map[string]any {
		return map[string]any{"structured-output": map[string]any{"schema-file": path}}
	}
	config, err := parseStructuredOutput(settings("schema.json"), workflowPath, engine, nil)
	require.NoError(t, err)
	assert.Equal(t, "object", config.Schema["type"])
	require.NoError(t, os.Remove(schemaPath))
	assert.Equal(t, "object", config.Schema["type"], "schema is embedded, not a runtime file dependency")
	_, err = parseStructuredOutput(settings("schema.json"), workflowPath, engine, nil)
	require.ErrorContains(t, err, "could not be resolved")

	outside := filepath.Join(t.TempDir(), "outside.json")
	require.NoError(t, os.WriteFile(outside, []byte(`{"type":"object"}`), 0600))
	require.NoError(t, os.Symlink(outside, schemaPath))
	_, err = parseStructuredOutput(settings("schema.json"), workflowPath, engine, nil)
	require.ErrorContains(t, err, "inside the repository")
	relative, err := filepath.Rel(root, outside)
	require.NoError(t, err)
	_, err = parseStructuredOutput(settings(relative), workflowPath, engine, nil)
	require.ErrorContains(t, err, "inside the repository")
}

func TestStructuredOutputSteps(t *testing.T) {
	c := NewCompiler()
	data := &WorkflowData{StructuredOutput: &StructuredOutputConfig{Schema: map[string]any{"type": "object"}}}
	var setup, collect strings.Builder
	require.NoError(t, c.generateStructuredOutputSetup(&setup, data))
	require.NoError(t, c.generateStructuredOutputCollection(&collect, data))
	assert.Contains(t, setup.String(), "--ignore-scripts")
	assert.Contains(t, setup.String(), StructuredOutputSchemaPath)
	assert.Contains(t, collect.String(), "GH_AW_STRUCTURED_OUTPUT_REDACTION: ${{ steps.redact_secrets.outcome }}")
	assert.Contains(t, collect.String(), "GH_AW_STRUCTURED_OUTPUT_EXECUTION: ${{ steps.agentic_execution.outcome }}")
	assert.Contains(t, collect.String(), "requires successful agent execution and secret redaction")
	assert.Contains(t, collect.String(), `GH_AW_STRUCTURED_OUTPUT_SCHEMA: '{"type":"object"}'`)
	assert.Equal(t, "${{ steps.structured_output.outputs.structured }}", buildMainJobCoreOutputs(data)["structured"])

	var absent strings.Builder
	require.NoError(t, c.generateStructuredOutputSetup(&absent, &WorkflowData{}))
	require.NoError(t, c.generateStructuredOutputCollection(&absent, &WorkflowData{}))
	assert.Empty(t, absent.String())
	assert.NotContains(t, buildMainJobCoreOutputs(&WorkflowData{}), "structured")

	var upload strings.Builder
	c.generateUnifiedArtifactUpload(&upload, []string{StructuredOutputFilePath}, "", true, true)
	assert.Contains(t, upload.String(), "steps.structured_output.outcome == 'success'")
}

func TestStructuredOutputUnsupportedGoose(t *testing.T) {
	assert.False(t, (EngineCapabilitiesDefinition{}).ToRuntimeCapabilities().StructuredOutput)
	engine, err := NewBehaviorDefinedEngine(loadGooseSample(t))
	require.NoError(t, err)
	assert.False(t, engine.GetCapabilities().StructuredOutput)
	frontmatter := map[string]any{"structured-output": map[string]any{"schema": map[string]any{"type": "object"}}}
	_, err = parseStructuredOutput(frontmatter, "workflow.md", engine, nil)
	require.ErrorContains(t, err, `not supported by engine "goose"`)
	config, err := parseStructuredOutput(map[string]any{}, "workflow.md", engine, nil)
	require.NoError(t, err)
	assert.Nil(t, config)
}

func TestStructuredOutputSchemaSizeLimit(t *testing.T) {
	schema := map[string]any{"type": "object", "description": ""}
	encoded, err := json.Marshal(schema)
	require.NoError(t, err)
	schema["description"] = strings.Repeat("x", 64*1024-len(encoded))
	require.NoError(t, validateStructuredOutputSchema(schema))
	schema["description"] = schema["description"].(string) + "x"
	require.ErrorContains(t, validateStructuredOutputSchema(schema), "64 KiB")
}

func TestStructuredOutputWorkflowParsing(t *testing.T) {
	for _, tt := range []struct {
		name   string
		engine string
		error  string
	}{
		{"native Codex", "codex", ""},
		{"native Claude", "claude", ""},
		{"unsupported Gemini", "gemini", "native JSON Schema output support"},
		{"unsupported Pi", "pi", "native JSON Schema output support"},
		{"unsupported Copilot CLI", "copilot", "copilot-sdk"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "workflow.md")
			source := "---\non: workflow_dispatch\nstrict: false\nengine:\n  id: " + tt.engine + "\nstructured-output:\n  schema:\n    type: object\n    properties:\n      decision:\n        type: string\n    required: [decision]\n    additionalProperties: false\n---\nReturn a decision.\n"
			require.NoError(t, os.WriteFile(file, []byte(source), 0600))
			compiler := NewCompiler()
			compiler.SetSkipValidation(true)
			data, err := compiler.ParseWorkflowFile(file)
			if tt.error != "" {
				require.ErrorContains(t, err, tt.error)
				assert.Contains(t, err.Error(), "reference/structured-output/")
			} else {
				require.NoError(t, err)
				require.NotNil(t, data.StructuredOutput)
				assert.Equal(t, "object", data.StructuredOutput.Schema["type"])
			}
		})
	}
}
