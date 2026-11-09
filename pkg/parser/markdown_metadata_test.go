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

func TestCompileAgentMetadataRuntimeFields(t *testing.T) {
	value, err := CompileAgentMetadata("name: reader\ndescription: Reads files\nmodel: small\ntools: [Read]\napplyTo: '**'\ninputs: [unused]\noutputs: [unused]\nmetadata: {version: '1.0'}")
	require.NoError(t, err)
	require.Equal(t, map[string]any{"name": "reader", "description": "Reads files", "model": "small", "tools": []any{"Read"}}, value)
	_, err = CompileAgentMetadata("model: a\nmodel: b")
	require.Error(t, err)
}

func TestMarkdownMetadataDistinguishesInlineSkills(t *testing.T) {
	blocks := ExtractMarkdownMetadataBlocks("##\tskill: `reader`\n---\ndescription: Not needed\n---\nBody\n## agent: `worker`\n---\nmodel: small\n---")
	require.Len(t, blocks, 2)
	require.True(t, blocks[0].Skill)
	require.False(t, blocks[1].Skill)
}
