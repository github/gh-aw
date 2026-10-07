//go:build !integration && !js && !wasm

package console

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfirmAction_NonTTY verifies that ConfirmAction does not read from
// stdin when it is not connected to a terminal.
func TestConfirmAction_NonTTY(t *testing.T) {
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { os.Stdin = oldStdin })
	t.Cleanup(func() { r.Close() })
	t.Cleanup(func() { w.Close() })
	os.Stdin = r
	_, err = w.WriteString("y\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())

	result, err := ConfirmAction("Delete all workflows?", "Yes, delete", "Cancel")
	require.Error(t, err)
	assert.False(t, result)
	require.ErrorContains(t, err, "stdin is not a TTY")
}

func TestShowTextConfirm(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantResult  bool
		wantErr     bool
		errContains string
	}{
		{name: "yes", input: "y\n", wantResult: true},
		{name: "YES uppercase", input: "YES\n", wantResult: true},
		{name: "yes full", input: "yes\n", wantResult: true},
		{name: "1 for affirmative", input: "1\n", wantResult: true},
		{name: "no", input: "n\n", wantResult: false},
		{name: "NO uppercase", input: "NO\n", wantResult: false},
		{name: "no full", input: "no\n", wantResult: false},
		{name: "2 for negative", input: "2\n", wantResult: false},
		{name: "single letter uppercase Y", input: "Y\n", wantResult: true},
		{name: "single letter uppercase N", input: "N\n", wantResult: false},
		{name: "whitespace padded yes", input: "  y  \n", wantResult: true},
		{name: "invalid input", input: "maybe\n", wantErr: true, errContains: "invalid input"},
		{name: "empty input EOF", input: "", wantErr: true, errContains: "invalid input"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := showTextConfirm("Delete all workflows?", "Yes, delete", "Cancel", strings.NewReader(tt.input))
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.ErrorContains(t, err, tt.errContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantResult, result)
			}
		})
	}
}
