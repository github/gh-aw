//go:build !integration

package jsonutil_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/jsonutil"
)

type failingMarshaler struct{}

func (failingMarshaler) MarshalJSON() ([]byte, error) {
	return nil, assert.AnError
}

// TestSpec_PublicAPI_MarshalCompactNoHTMLEscape validates the documented behavior
// of MarshalCompactNoHTMLEscape as described in the package README.md.
func TestSpec_PublicAPI_MarshalCompactNoHTMLEscape(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
		wantErr  bool
	}{
		{
			name: "documented github actions expression preserves html-sensitive characters",
			input: map[string]string{
				"expr": "${{ env.MCP_ENV == 'staging' && env.MCP_URL_STAGING || env.MCP_URL_PROD }}",
			},
			expected: `{"expr":"${{ env.MCP_ENV == 'staging' && env.MCP_URL_STAGING || env.MCP_URL_PROD }}"}`,
		},
		{
			name:     "compact json output has no trailing newline",
			input:    []string{"one", "two"},
			expected: `["one","two"]`,
		},
		{
			name:    "marshal error is returned",
			input:   map[string]any{"bad": math.Inf(1)},
			wantErr: true,
		},
		{
			name:    "custom marshaler errors are returned",
			input:   failingMarshaler{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := jsonutil.MarshalCompactNoHTMLEscape(tt.input)
			if tt.wantErr {
				require.Error(t, err, "should return error for: %s", tt.name)
				assert.Empty(t, result, "result should be empty on error for: %s", tt.name)
				return
			}

			require.NoError(t, err, "unexpected error for: %s", tt.name)
			assert.Equal(t, tt.expected, result, "result mismatch for: %s", tt.name)
			assert.NotContains(t, result, "\\u0026", "should not HTML-escape ampersands for: %s", tt.name)
			assert.NotContains(t, result, "\n", "should trim trailing newline for: %s", tt.name)
		})
	}
}
