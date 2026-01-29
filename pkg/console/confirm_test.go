//go:build !integration

package console

import (
	"testing"
)

func TestConfirmAction(t *testing.T) {
	// Note: This test can't fully test the interactive behavior without mocking
	// the terminal input, but we can verify the function signature and basic setup

	t.Run("function signature", func(t *testing.T) {
		// This test just verifies the function exists and has the right signature
		// Actual interactive testing would require a mock terminal
		_ = ConfirmAction
	})
}
