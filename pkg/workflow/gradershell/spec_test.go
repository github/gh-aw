//go:build !integration

package gradershell_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"

	"github.com/github/gh-aw/pkg/workflow/gradershell"
)

// TestSpec_PublicAPI_LiteralWord checks README / Public API: static shell words
// can be extracted, while an expanded value is not a statically literal word.
func TestSpec_PublicAPI_LiteralWord(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		word    string
		literal bool
		value   string
	}{
		{name: "unquoted literal", word: "hello", literal: true, value: "hello"},
		{name: "single quoted spaces", word: "'hello world'", literal: true, value: "hello world"},
		{name: "double quoted spaces", word: `"hello world"`, literal: true, value: "hello world"},
		{name: "single quoted dollar sign", word: "'$value'", literal: true, value: "$value"},
		{name: "expanded variable", word: `"$value"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader("echo "+tt.word), "spec.sh")
			require.NoError(t, err, "the specification example should be valid Bash")
			require.Len(t, file.Stmts, 1, "the example should contain one command")
			call, ok := file.Stmts[0].Cmd.(*syntax.CallExpr)
			require.True(t, ok, "the example should parse as a simple command")
			require.Len(t, call.Args, 2, "the example should contain one argument to inspect")
			value, literal := gradershell.LiteralWord(call.Args[1])
			assert.Equal(t, tt.literal, literal, "LiteralWord should distinguish static text from an expanded value")
			if tt.literal {
				assert.Equal(t, tt.value, value, "LiteralWord should return the static word's text")
			}
		})
	}
}

// TestSpec_PublicAPI_HasShellCodeEvaluation checks README / Public API:
// evaluating an expanded value as code differs from printing it as an argument.
// SPEC_AMBIGUITY: The README does not enumerate code-evaluating commands,
// wrappers, interpreter options, or the dynamic-word callback contract. These
// cases use direct eval and ordinary printf with nonliteral words marked dynamic.
func TestSpec_PublicAPI_HasShellCodeEvaluation(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		shell  string
		unsafe bool
	}{
		{name: "expanded value evaluated as code", shell: `eval "$value"`, unsafe: true},
		{name: "expanded value printed as data", shell: `printf '%s\n' "$value"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(tt.shell), "spec.sh")
			require.NoError(t, err, "the specification example should be valid Bash")
			actual := gradershell.HasShellCodeEvaluation(file, func(word *syntax.Word) bool {
				_, literal := gradershell.LiteralWord(word)
				return !literal
			})
			assert.Equal(t, tt.unsafe, actual, "the helper should identify reinterpretation of an expanded value as code")
		})
	}
}
