package actions

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEditSourceBlockMappingLayout(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, source, expected string
		after                  map[string]any
	}{
		{
			name:     "new mapping",
			source:   "name: Original # name\n",
			expected: "name: Original # name\nwith:\n  persist-credentials: false\n",
			after: map[string]any{
				"name": "Original",
				"with": map[string]any{"persist-credentials": false},
			},
		},
		{
			name:     "sequence mapping",
			source:   "steps:\n  - uses: actions/checkout@v5 # checkout\n",
			expected: "steps:\n  - uses: actions/checkout@v5 # checkout\n    with:\n      persist-credentials: false\n",
			after: map[string]any{
				"steps": []any{map[string]any{
					"uses": "actions/checkout@v5",
					"with": map[string]any{"persist-credentials": false},
				}},
			},
		},
		{
			name:     "existing mapping",
			source:   "with:\n  fetch-depth: 0 # depth\n",
			expected: "with:\n  fetch-depth: 0 # depth\n  persist-credentials: false\n",
			after: map[string]any{
				"with": map[string]any{"fetch-depth": uint64(0), "persist-credentials": false},
			},
		},
		{
			name:     "new mapping with scalar whitespace",
			source:   "name: Original # name\n",
			expected: "name: Original # name\nwith:\n  script: \"first\\n  \\n\"\n",
			after: map[string]any{
				"name": "Original",
				"with": map[string]any{"script": "first\n  \n"},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var before map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(tt.source), &before))
			emitted, err := EditSource(tt.source, before, tt.after)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, emitted)
			var actual map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(emitted), &actual))
			assert.Equal(t, tt.after, actual)
			assert.Equal(t, sourceComments(t, tt.source), sourceComments(t, emitted))
			unchanged, err := EditSource(emitted, actual, actual)
			require.NoError(t, err)
			assert.Equal(t, emitted, unchanged)
		})
	}
}

func TestEditSourcePreservesEditedScalarWhitespace(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		"first\n  \n",
		"first\n  \n\n",
		"first\n  \n    \n\n",
		"first\r\n  \r\n",
		"\n  \n",
		"  first\n  last\n",
		"first\n\t\n",
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			content := "value: old # value\n"
			before := map[string]any{"value": "old"}
			after := map[string]any{"value": text}
			emitted, err := EditSource(content, before, after)
			require.NoError(t, err)
			var actual map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(emitted), &actual))
			assert.Equal(t, after, actual)
			assert.Equal(t, sourceComments(t, content), sourceComments(t, emitted))
		})
	}
}
