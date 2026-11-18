//go:build !integration

package shellpattern_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/workflow/shellpattern"
)

// These tests derive from README.md's Public API and lexical-check contract.
// Script strings are input data and are never executed.

func TestSpec_PublicAPI_FirstUnsafeExpansion(t *testing.T) {
	for _, tt := range []struct {
		name   string
		script string
		token  string
	}{
		{"parameter transformation", "echo ${name@Q}", "${name@"},
		{"indirect expansion", "echo ${!name}", "${!"},
		{"command substitution", "echo $(true)", "$("},
		{"single quoted text", "echo '$(true)'", "$("},
		{"double quoted text", `echo "$(true)"`, "$("},
		{"comment", "# $(true)\n", "$("},
		{"quoted transformation", "echo '${name@Q}'", "${name@"},
		{"commented indirect expansion", "# ${!name}\n", "${!"},
		{"byte offset after unicode", "echo café; $(true)", "$("},
	} {
		t.Run(tt.name, func(t *testing.T) {
			expansion, found := shellpattern.FirstUnsafeExpansion(tt.script)
			require.True(t, found, "the lexical check must detect %s", tt.name)
			assert.Equal(t, strings.Index(tt.script, tt.token), expansion.Start,
				"the expansion must identify the first matching byte position")
			assert.Greater(t, expansion.End, expansion.Start, "an expansion must have a nonempty byte range")
			assert.LessOrEqual(t, expansion.End, len(tt.script), "the expansion range must stay within the script")
			assert.NotEmpty(t, expansion.Description, "an unsupported pattern must have a description")
		})
	}
}

func TestSpec_PublicAPI_FirstUnsafeExpansionArithmetic(t *testing.T) {
	for _, tt := range []struct {
		name   string
		script string
		token  string
	}{
		{"plain arithmetic", "echo $((1 + 2))", ""},
		{"nested arithmetic", "echo $((1 + $((2 + 3))))", ""},
		{"command inside arithmetic", "echo $((1 + $(true)))", "$(true)"},
		{"command inside nested arithmetic", "echo $((1 + $((2 + $(true)))))", "$(true)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			expansion, found := shellpattern.FirstUnsafeExpansion(tt.script)
			if tt.token == "" {
				assert.False(t, found, "arithmetic expansion itself must be allowed")
				return
			}
			require.True(t, found, "arithmetic must not hide a nested command substitution")
			assert.Equal(t, strings.Index(tt.script, tt.token), expansion.Start,
				"the match must identify the nested command rather than arithmetic")
		})
	}
}

func TestSpec_PublicAPI_FirstUnsafeExpansionOrder(t *testing.T) {
	for _, tt := range []struct {
		name   string
		script string
		first  string
	}{
		{"command before indirect", "echo $(true) ${!name}", "$("},
		{"indirect before command", "echo ${!name} $(true)", "${!"},
		{"transformation before command", "echo ${name@Q} $(true)", "${name@"},
		{"comment before later command", "# ${!name}\necho $(true)", "${!"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			expansion, found := shellpattern.FirstUnsafeExpansion(tt.script)
			require.True(t, found, "scripts with unsupported expansions must be detected")
			assert.Equal(t, strings.Index(tt.script, tt.first), expansion.Start,
				"the API must return the first unsupported expansion in textual order")
		})
	}
}

// SPEC_AMBIGUITY: README.md does not enumerate transformation syntax. These
// cases use Bash's ${parameter@operator} form; pattern removal, replacement,
// and case modification need explicit specification before adding assertions.
