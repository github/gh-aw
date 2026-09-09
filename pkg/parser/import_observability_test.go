package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExtractOTLPAttributesFromObsMap verifies extractOTLPAttributesFromObsMap
// correctly extracts string-valued OTLP custom attributes from a raw
// observability map, and returns nil for absent/malformed/empty input.
func TestExtractOTLPAttributesFromObsMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		obs      map[string]any
		expected map[string]string
	}{
		{
			name:     "nil map returns nil",
			obs:      nil,
			expected: nil,
		},
		{
			name:     "empty map returns nil",
			obs:      map[string]any{},
			expected: nil,
		},
		{
			name:     "missing otlp key returns nil",
			obs:      map[string]any{"other": "value"},
			expected: nil,
		},
		{
			name:     "otlp value not a map returns nil",
			obs:      map[string]any{"otlp": "not-a-map"},
			expected: nil,
		},
		{
			name:     "otlp map missing attributes returns nil",
			obs:      map[string]any{"otlp": map[string]any{"other": "value"}},
			expected: nil,
		},
		{
			name:     "attributes value not a map returns nil",
			obs:      map[string]any{"otlp": map[string]any{"attributes": "not-a-map"}},
			expected: nil,
		},
		{
			name:     "empty attributes map returns empty result",
			obs:      map[string]any{"otlp": map[string]any{"attributes": map[string]any{}}},
			expected: map[string]string{},
		},
		{
			name: "string attributes are extracted",
			obs: map[string]any{
				"otlp": map[string]any{
					"attributes": map[string]any{
						"env":     "prod",
						"service": "gh-aw",
					},
				},
			},
			expected: map[string]string{"env": "prod", "service": "gh-aw"},
		},
		{
			name: "non-string attribute values are silently ignored",
			obs: map[string]any{
				"otlp": map[string]any{
					"attributes": map[string]any{
						"env":     "prod",
						"count":   42,
						"enabled": true,
						"nested":  map[string]any{"a": "b"},
						"list":    []any{"x", "y"},
						"missing": nil,
					},
				},
			},
			expected: map[string]string{"env": "prod"},
		},
		{
			name: "empty string key is ignored",
			obs: map[string]any{
				"otlp": map[string]any{
					"attributes": map[string]any{
						"":     "should-be-ignored",
						"real": "value",
					},
				},
			},
			expected: map[string]string{"real": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractOTLPAttributesFromObsMap(tt.obs)
			assert.Equal(t, tt.expected, got)
		})
	}
}
