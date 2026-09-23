//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddLabelsItemSchemaConfig(t *testing.T) {
	itemSchema := map[string]any{
		"type":     "object",
		"required": []any{"name", "confidence"},
		"properties": map[string]any{
			"name":       map[string]any{"type": "string"},
			"confidence": map[string]any{"type": "string", "enum": []any{"HIGH", "MEDIUM"}},
		},
	}
	compiler := NewCompiler()
	config := compiler.extractSafeOutputsConfig(map[string]any{
		"safe-outputs": map[string]any{
			"add-labels": map[string]any{
				"item-schema": itemSchema,
			},
		},
	})

	require.NotNil(t, config)
	require.NotNil(t, config.AddLabels)
	require.Equal(t, itemSchema, config.AddLabels.ItemSchema)

	handlerConfig := issueHandlerRegistry["add_labels"](config)
	require.Equal(t, itemSchema, handlerConfig["item_schema"])
}
