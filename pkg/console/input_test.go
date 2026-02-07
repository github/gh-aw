//go:build !integration

package console

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptInput(t *testing.T) {
	// Note: Interactive Huh forms cannot be fully tested without a mock terminal
	// These tests verify function signatures and basic setup

	t.Run("function signature", func(t *testing.T) {
		// Verify the function exists and has the right signature
		_ = PromptInput
	})

	t.Run("validates parameters", func(t *testing.T) {
		// Test that empty title and description don't cause panics
		// In a real terminal, this would show a prompt with empty fields
		title := "Test Title"
		description := "Test Description"
		placeholder := "Enter value"

		// Function exists and parameters are accepted
		_, err := PromptInput(title, description, placeholder)
		// Will error in test environment (no TTY), but that's expected
		require.Error(t, err, "Should error when not in TTY")
		assert.Contains(t, err.Error(), "not a TTY", "Error should mention TTY")
	})
}

func TestPromptSecretInput(t *testing.T) {
	t.Run("function signature", func(t *testing.T) {
		// Verify the function exists and has the right signature
		_ = PromptSecretInput
	})

	t.Run("validates parameters", func(t *testing.T) {
		title := "Enter Secret"
		description := "Secret value will be masked"

		// Function exists and parameters are accepted
		_, err := PromptSecretInput(title, description)
		// Will error in test environment (no TTY), but that's expected
		require.Error(t, err, "Should error when not in TTY")
		assert.Contains(t, err.Error(), "not a TTY", "Error should mention TTY")
	})
}

func TestPromptInputWithValidation(t *testing.T) {
	t.Run("function signature", func(t *testing.T) {
		// Verify the function exists and has the right signature
		_ = PromptInputWithValidation
	})

	t.Run("accepts custom validator", func(t *testing.T) {
		title := "Test Title"
		description := "Test Description"
		placeholder := "Enter value"
		validator := func(s string) error {
			if len(s) < 3 {
				return fmt.Errorf("must be at least 3 characters")
			}
			return nil
		}

		// Function exists and parameters are accepted
		_, err := PromptInputWithValidation(title, description, placeholder, validator)
		// Will error in test environment (no TTY), but that's expected
		require.Error(t, err, "Should error when not in TTY")
		assert.Contains(t, err.Error(), "not a TTY", "Error should mention TTY")
	})
}
