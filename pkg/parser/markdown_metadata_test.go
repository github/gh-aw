package parser

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileMarkdownMetadata(t *testing.T) {
	for _, source := range []string{
		"description: >-\n  Read files\n  carefully\nmodel: small\ntools: [Read, Grep]\n",
		`{description: "Read files carefully", model: small, tools: [Read, Grep]}`,
	} {
		value, err := CompileMarkdownMetadata(source)
		require.NoError(t, err)
		require.Equal(t, map[string]any{"description": "Read files carefully", "model": "small", "tools": []any{"Read", "Grep"}}, value)
	}
	for _, source := range []string{"model: a\nmodel: b", "a: &a [*a]", "a: !!str value", "---\na: b\n---\nc: d", strings.Repeat("[", 102) + "a" + strings.Repeat("]", 102)} {
		_, err := CompileMarkdownMetadata(source)
		require.Error(t, err, source)
	}
}
