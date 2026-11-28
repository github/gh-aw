package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinConnectorAliasResolution(t *testing.T) {
	const id = "parser-short-name-test"
	const content = "---\nconnectors: {}\n---\n"
	path := BuiltinPathPrefix + "connectors/" + id + ".md"
	RegisterBuiltinVirtualFile(path, []byte(content))

	resolved, err := ResolveIncludePath("connector:"+id, t.TempDir(), nil)
	require.NoError(t, err)
	require.Equal(t, path, resolved)
	actual, err := ReadFile(resolved)
	require.NoError(t, err)
	require.Equal(t, content, string(actual))
}

func TestBuiltinConnectorAliasRejectsInvalidOrUnknownNames(t *testing.T) {
	for _, name := range []string{"", "../linear", "Linear", "unknown-short-name-test"} {
		t.Run(name, func(t *testing.T) {
			_, err := ResolveIncludePath("connector:"+name, t.TempDir(), nil)
			require.Error(t, err)
		})
	}
}
