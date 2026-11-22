//go:build !integration

package jsonutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMarshalCompactNoHTMLEscape validates the documented behavior of
// MarshalCompactNoHTMLEscape as described in the jsonutil README.md.
//
// Specification:
// - Marshals v to compact JSON without HTML escaping.
// - Characters like '&', '<', '>' are preserved as-is (not encoded to \u0026, \u003c, \u003e).
// - Trailing newline emitted by json.Encoder is trimmed so the result matches json.Marshal style.
// - Values that json.Marshal cannot encode (e.g. channels, functions) return an error.
func TestMarshalCompactNoHTMLEscape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    any
		wantErr  bool
		expected string
	}{
		{
			name:     "preserves expression operators (& and |)",
			input:    map[string]string{"expr": "${{ env.MCP_ENV == 'staging' && env.MCP_URL_STAGING || env.MCP_URL_PROD }}"},
			expected: `{"expr":"${{ env.MCP_ENV == 'staging' && env.MCP_URL_STAGING || env.MCP_URL_PROD }}"}`,
		},
		{
			name:     "output is compact (no trailing newline)",
			input:    map[string]string{"key": "value"},
			expected: `{"key":"value"}`,
		},
		{
			name:     "preserves angle brackets without HTML escaping",
			input:    map[string]string{"x": "<tag>"},
			expected: `{"x":"<tag>"}`,
		},
		{
			name:     "nil input marshals to null",
			input:    nil,
			expected: `null`,
		},
		{
			name: "nested and unicode input",
			input: map[string]any{
				"nested": map[string]string{"greeting": "héllo"},
				"list":   []string{"a", "b"},
			},
			expected: `{"list":["a","b"],"nested":{"greeting":"héllo"}}`,
		},
		{
			name:     "escapes quotes and control characters inside strings",
			input:    map[string]string{"s": "say \"hi\"\tnow"},
			expected: `{"s":"say \"hi\"\tnow"}`,
		},
		{
			name:     "trailing newline inside a value is escaped, not trimmed",
			input:    map[string]string{"s": "line\n"},
			expected: `{"s":"line\n"}`,
		},
		{
			name:     "byte slices marshal as base64 strings",
			input:    []byte("abc"),
			expected: `"YWJj"`,
		},
		{
			name:     "empty map marshals to empty object",
			input:    map[string]string{},
			expected: `{}`,
		},
		{
			name:    "returns error for values json cannot encode",
			input:   map[string]any{"ch": make(chan int)},
			wantErr: true,
		},
		{
			name:    "returns error for functions",
			input:   func() {},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MarshalCompactNoHTMLEscape(tt.input)

			if tt.wantErr {
				require.Error(t, err, "marshal should fail")
				assert.Empty(t, result, "result should be empty on error")
				return
			}

			require.NoError(t, err, "marshal should succeed")
			assert.Equal(t, tt.expected, result, "unexpected marshal result")
		})
	}
}
