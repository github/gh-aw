//go:build !integration

package console

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptSecretInput(t *testing.T) {
	t.Run("function signature", func(t *testing.T) {
		// Verify the function exists and has the right signature
		_ = PromptSecretInput
	})

	t.Run("validates parameters", func(t *testing.T) {
		title := "Enter Secret"
		description := "Secret value will be masked"
		oldStdin := os.Stdin
		r, w, err := os.Pipe()
		require.NoError(t, err)
		t.Cleanup(func() { os.Stdin = oldStdin })
		t.Cleanup(func() { r.Close() })
		t.Cleanup(func() { w.Close() })
		os.Stdin = r

		// Function exists and parameters are accepted
		_, err = PromptSecretInput(title, description)
		// Must error when stdin is not a TTY, even if stderr is a terminal.
		require.Error(t, err, "Should error when not in TTY")
		require.ErrorContains(t, err, "stdin and stderr must be TTYs", "Error should mention required TTYs")
	})
}
