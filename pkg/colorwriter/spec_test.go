//go:build !integration

package colorwriter_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/colorwriter"
)

// TestSpec_PublicAPI_New validates the writer-wrapping behavior documented in the package README.
func TestSpec_PublicAPI_New(t *testing.T) {
	tests := []struct {
		name    string
		environ []string
	}{
		{name: "NO_COLOR environment", environ: []string{"NO_COLOR=1"}},
		{name: "color terminal environment", environ: []string{"TERM=xterm-256color", "COLORTERM=truecolor"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var destination bytes.Buffer
			writer := colorwriter.New(&destination, tt.environ)
			require.NotNil(t, writer, "New should return a usable io.Writer")
			_, err := io.WriteString(writer, "documented output")
			require.NoError(t, err, "the color-profile-aware writer should accept writes")
			assert.Contains(t, destination.String(), "documented output", "writes should reach the wrapped writer")
		})
	}
}

// TestSpec_PublicAPI_Stderr validates the documented convenience helper.
func TestSpec_PublicAPI_Stderr(t *testing.T) {
	writer := colorwriter.Stderr()
	require.NotNil(t, writer, "Stderr should return a writer")
	assert.Implements(t, (*io.Writer)(nil), writer, "Stderr should return the documented io.Writer type")
}

// TestSpec_PublicAPI_Degrade validates the README's NO_COLOR usage example.
func TestSpec_PublicAPI_Degrade(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "documented ANSI warning", input: "\x1b[31mwarning\x1b[0m", want: "warning"},
		{name: "plain text remains plain", input: "warning", want: "warning"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := colorwriter.Degrade(tt.input, []string{"NO_COLOR=1", "TERM=xterm-256color"})
			assert.Equal(t, tt.want, got, "Degrade should produce plain output when NO_COLOR is set")
		})
	}
}

// SPEC_AMBIGUITY: The README does not define exact degradation results for
// COLORTERM or terminal-capability combinations, so they are not asserted.
