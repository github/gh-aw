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

func TestCompileDeclarativeMemorySchemasWithoutScripts(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "memory.md")
	source := `---
name: Declarative memory schemas
on: workflow_dispatch
engine: copilot
strict: false
tools:
  repo-memory:
    allowed-extensions: [".json", ".jsonl"]
    validation:
      json-schemas:
        - file: state.json
          format: json
          schema:
            type: object
            required: [version, items]
            additionalProperties: false
            properties:
              version:
                enum: [1]
              items:
                type: array
                items:
                  type: string
        - file: archive/events.jsonl
          format: jsonl
          schema:
            type: object
            required: [id, status]
            additionalProperties: false
            properties:
              id:
                type: integer
              status:
                enum: [open, closed]
  cache-memory:
    allowed-extensions: [".json"]
    validation:
      json-schemas:
        - file: state.json
          format: json
          schema:
            type: object
  drive-memory:
    drive-name: schema-example
    validation:
      json-schemas:
        - file: state.json
          format: json
          schema:
            type: object
---

Validate persistent memory with schemas.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(source), 0o644))
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))
	compiled, err := os.ReadFile(filepath.Join(dir, "memory.lock.yml"))
	require.NoError(t, err)
	lockYAML := string(compiled)

	assert.Contains(t, lockYAML, "MEMORY_JSON_SCHEMAS_B64:")
	assert.Contains(t, lockYAML, "MEMORY_JSON_SCHEMAS_REQUIRED: 'true'")
	assert.Contains(t, lockYAML, "Validate repo-memory domain content")
	assert.Contains(t, lockYAML, "Validate cache-memory file types")
	assert.Contains(t, lockYAML, "Validate drive-memory file types")
	assert.Contains(t, lockYAML, "push_repo_memory:")
	assert.NotContains(t, lockYAML, "VALIDATION_SCRIPT_B64:")
	require.GreaterOrEqual(t, strings.Count(lockYAML, "MEMORY_JSON_SCHEMAS_REQUIRED: 'true'"), 4)
}

func TestParseMemoryValidationTimeoutMinutesRejectsFractionalValues(t *testing.T) {
	_, err := parseMemoryValidationTimeoutMinutes(1.9, "tools.cache-memory.validation.timeout-minutes")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be an integer number of minutes")
}

func TestParseMemoryValidationConfigWithJSONSchemas(t *testing.T) {
	config, err := parseMemoryValidationConfig(map[string]any{
		"validation": map[string]any{
			"json-schemas": []any{
				map[string]any{
					"file":   "state.json",
					"format": "json",
					"schema": map[string]any{
						"type":                 "object",
						"required":             []any{"version"},
						"additionalProperties": false,
						"properties": map[string]any{
							"version": map[string]any{"enum": []any{1, true, nil}},
						},
					},
				},
			},
		},
	}, "tools.repo-memory.validation")

	require.NoError(t, err)
	require.NotNil(t, config)
	require.Len(t, config.JSONSchemas, 1)
	assert.Equal(t, "state.json", config.JSONSchemas[0].File)
	assert.Equal(t, "json", config.JSONSchemas[0].Format)
	assert.Equal(t, defaultMemoryValidationTimeoutMinutes, config.TimeoutMinutes)
}

func TestParseMemoryJSONSchemasRejectsInvalidDeclarations(t *testing.T) {
	valid := func(file, format string, schema map[string]any) map[string]any {
		return map[string]any{"file": file, "format": format, "schema": schema}
	}
	tests := []struct {
		name    string
		entries []any
		want    string
	}{
		{name: "empty declaration list", entries: []any{}, want: "non-empty list"},
		{name: "absolute path", entries: []any{valid("/state.json", "json", map[string]any{})}, want: "relative file path"},
		{name: "traversal path", entries: []any{valid("../state.json", "json", map[string]any{})}, want: "relative file path"},
		{name: "unsupported wildcard", entries: []any{valid("state?.json", "json", map[string]any{})}, want: "relative file path"},
		{name: "invalid format", entries: []any{valid("state.json", "yaml", map[string]any{})}, want: "format"},
		{name: "unknown entry field", entries: []any{map[string]any{"file": "state.json", "format": "json", "schema": map[string]any{}, "optional": true}}, want: "unknown property"},
		{name: "unsupported keyword", entries: []any{valid("state.json", "json", map[string]any{"type": "string", "format": "date-time"})}, want: "unsupported schema keyword"},
		{name: "inexact integer enum", entries: []any{valid("state.json", "json", map[string]any{"enum": []any{uint64(9007199254740992)}})}, want: "exactly representable"},
		{name: "duplicate file", entries: []any{valid("state.json", "json", map[string]any{}), valid("state.json", "json", map[string]any{})}, want: "duplicate file target"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseMemoryValidationConfig(map[string]any{
				"validation": map[string]any{"json-schemas": test.entries},
			}, "tools.cache-memory.validation")
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.want)
		})
	}
}

func TestParseMemoryJSONSchemasAcceptsWildcards(t *testing.T) {
	config, err := parseMemoryValidationConfig(map[string]any{
		"validation": map[string]any{
			"json-schemas": []any{
				map[string]any{"file": "events/**/*.jsonl", "format": "jsonl", "schema": map[string]any{"type": "object"}},
			},
		},
	}, "tools.repo-memory.validation")

	require.NoError(t, err)
	require.Len(t, config.JSONSchemas, 1)
	assert.Equal(t, "events/**/*.jsonl", config.JSONSchemas[0].File)
}

func TestParseMemoryValidationConfigRejectsUnknownFieldsAndMissingValidation(t *testing.T) {
	_, err := parseMemoryValidationConfig(map[string]any{
		"validation": map[string]any{"script": "true", "unknown": true},
	}, "tools.drive-memory.validation")
	require.ErrorContains(t, err, "unknown property")

	_, err = parseMemoryValidationConfig(map[string]any{
		"validation": map[string]any{"timeout-minutes": 1},
	}, "tools.drive-memory.validation")
	require.ErrorContains(t, err, "script or a non-empty json-schemas list")
}

func TestValidateMemorySchemaTargetsHonorsFilePolicies(t *testing.T) {
	config := &MemoryValidationConfig{JSONSchemas: []MemoryJSONSchemaConfig{{File: "data/state.json", Format: "json", Schema: map[string]any{}}}}
	require.NoError(t, validateMemorySchemaTargets(config, []string{".JSON"}, []string{"data/**"}, "tools.repo-memory.validation"))
	require.ErrorContains(t, validateMemorySchemaTargets(config, []string{".md"}, nil, "tools.repo-memory.validation"), "allowed-extensions")
	require.ErrorContains(t, validateMemorySchemaTargets(config, nil, []string{"*.json"}, "tools.repo-memory.validation"), "file-glob")

	globConfig := &MemoryValidationConfig{JSONSchemas: []MemoryJSONSchemaConfig{{File: "data/**/*.json", Format: "json", Schema: map[string]any{}}}}
	require.NoError(t, validateMemorySchemaTargets(globConfig, []string{".json"}, []string{"data/**"}, "tools.repo-memory.validation"))
}

func TestMemorySchemaFileMatchesGlob(t *testing.T) {
	for _, file := range []string{"state.json", "nested/state.json", "nested/deep/state.json"} {
		assert.True(t, memorySchemaFileMatchesGlob(file, "**/*.json"), file)
	}
	assert.False(t, memorySchemaFileMatchesGlob("state.jsonl", "**/*.json"))
}

func TestMemorySchemaContractFixtures(t *testing.T) {
	data, err := os.ReadFile("../../actions/setup/js/memory_schema_contract.fixtures.json")
	require.NoError(t, err)
	var fixtures []struct {
		Name   string         `json:"name"`
		Valid  bool           `json:"valid"`
		Schema map[string]any `json:"schema"`
	}
	require.NoError(t, json.Unmarshal(data, &fixtures))
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			err := validateMemoryJSONSchema(fixture.Schema, "schema", 0)
			if fixture.Valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestParseMemoryValidationTimeoutMinutesRejectsOutOfRangeFloat(t *testing.T) {
	_, err := parseMemoryValidationTimeoutMinutes(6.0, "tools.cache-memory.validation.timeout-minutes")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be between 1 and 5 minutes")
}

func TestParseMemoryValidationConfigUsesTimeoutMinutes(t *testing.T) {
	config, err := parseMemoryValidationConfig(map[string]any{
		"validation": map[string]any{
			"script":          "console.log('validate')",
			"timeout-minutes": 2,
		},
	}, "tools.cache-memory.validation")

	require.NoError(t, err)
	require.NotNil(t, config)
	assert.Equal(t, 2, config.TimeoutMinutes)
	assert.Equal(t, 120, memoryValidationTimeoutSeconds(config))
}

func TestParseMemoryValidationConfigRejectsTimeout(t *testing.T) {
	_, err := parseMemoryValidationConfig(map[string]any{
		"validation": map[string]any{
			"script":  "console.log('validate')",
			"timeout": 2,
		},
	}, "tools.cache-memory.validation")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "has been renamed to tools.cache-memory.validation.timeout-minutes")
}
