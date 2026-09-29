//go:build !integration

package workflow

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizedAddLabelsItemSchema(t *testing.T) {
	schema, err := normalizedAddLabelsItemSchema(map[string]any{
		"type":                 "object",
		"required":             []any{"name", "confidence"},
		"additionalProperties": false,
		"properties": map[string]any{
			"name":       map[string]any{"type": "string"},
			"confidence": map[string]any{"type": "string", "enum": []any{"HIGH", "MEDIUM"}},
			"suggest":    map[string]any{"type": "boolean"},
		},
	}, false)
	require.NoError(t, err)

	assert.Equal(t, []string{"name", "confidence"}, schema["required"])
	properties := schema["properties"].(map[string]any)
	assert.Equal(t, []any{"HIGH", "MEDIUM"}, properties["confidence"].(map[string]any)["enum"])
	assert.NotContains(t, properties, "rationale")
}

func TestNormalizedAddLabelsItemSchemaRejectsWidening(t *testing.T) {
	tests := []struct {
		name   string
		schema map[string]any
		match  string
	}{
		{
			name: "unknown field",
			schema: map[string]any{
				"type": "object", "required": []any{"name"},
				"properties": map[string]any{"name": map[string]any{"type": "string"}, "color": map[string]any{"type": "string"}},
			},
			match: "color is not a built-in label field",
		},
		{
			name: "broader confidence enum",
			schema: map[string]any{
				"type": "object", "required": []any{"name"},
				"properties": map[string]any{
					"name":       map[string]any{"type": "string"},
					"confidence": map[string]any{"type": "string", "enum": []any{"CERTAIN"}},
				},
			},
			match: "unsupported value CERTAIN",
		},
		{
			name:   "composition keyword",
			schema: map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}, "allOf": []any{}},
			match:  "allOf is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizedAddLabelsItemSchema(tt.schema, false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.match)
		})
	}
}

func TestNormalizedAddLabelsItemSchemaPreservesIssueIntentRequirements(t *testing.T) {
	narrowing := map[string]any{
		"type":     "object",
		"required": []any{"name"},
		"properties": map[string]any{
			"name":       map[string]any{"type": "string"},
			"rationale":  map[string]any{"type": "string"},
			"confidence": map[string]any{"type": "string"},
		},
	}

	schema, err := normalizedAddLabelsItemSchema(narrowing, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"name", "rationale", "confidence"}, schema["required"])

	_, err = normalizedAddLabelsItemSchema(map[string]any{
		"type": "string",
	}, true)
	require.ErrorContains(t, err, `type must be "object" when issue-intent is enabled`)

	_, err = normalizedAddLabelsItemSchema(map[string]any{
		"type":     "object",
		"required": []any{"name"},
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}, true)
	require.ErrorContains(t, err, `requires "rationale", so that field must be defined in properties`)
}

func TestGenerateToolsMetaJSONIncludesAddLabelsItemSchema(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			AddLabels: &AddLabelsConfig{
				ItemSchema: map[string]any{
					"type": "object", "required": []any{"name", "confidence"},
					"properties": map[string]any{
						"name":       map[string]any{"type": "string"},
						"confidence": map[string]any{"type": "string"},
					},
				},
			},
		},
	}

	metaJSON, err := generateToolsMetaJSON(data, "")
	require.NoError(t, err)
	var meta ToolsMeta
	require.NoError(t, json.Unmarshal([]byte(metaJSON), &meta))
	require.Contains(t, meta.ItemSchemas, "add_labels")
	assert.Equal(t, []any{"LOW", "MEDIUM", "HIGH"}, meta.ItemSchemas["add_labels"]["labels"]["properties"].(map[string]any)["confidence"].(map[string]any)["enum"])
}

func TestGenerateToolsMetaJSONPreservesAddLabelsIssueIntentRequirements(t *testing.T) {
	enabled := true
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			AddLabels: &AddLabelsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{IssueIntent: &enabled},
				ItemSchema: map[string]any{
					"type":     "object",
					"required": []any{"name"},
					"properties": map[string]any{
						"name":       map[string]any{"type": "string"},
						"rationale":  map[string]any{"type": "string"},
						"confidence": map[string]any{"type": "string"},
					},
				},
			},
		},
	}

	metaJSON, err := generateToolsMetaJSON(data, "")
	require.NoError(t, err)
	var meta ToolsMeta
	require.NoError(t, json.Unmarshal([]byte(metaJSON), &meta))

	itemSchema := meta.ItemSchemas["add_labels"]["labels"]
	assert.Equal(t, []any{"name", "rationale", "confidence"}, itemSchema["required"])
}
