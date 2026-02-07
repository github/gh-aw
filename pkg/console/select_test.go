//go:build !integration

package console

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptSelect(t *testing.T) {
	t.Run("function signature", func(t *testing.T) {
		// Verify the function exists and has the right signature
		_ = PromptSelect
	})

	t.Run("requires options", func(t *testing.T) {
		title := "Select an option"
		description := "Choose one"
		options := []SelectOption{}

		_, err := PromptSelect(title, description, options)
		require.Error(t, err, "Should error with no options")
		assert.Contains(t, err.Error(), "no options", "Error should mention missing options")
	})

	t.Run("validates parameters with options", func(t *testing.T) {
		title := "Select an option"
		description := "Choose one"
		options := []SelectOption{
			{Label: "Option 1", Value: "opt1"},
			{Label: "Option 2", Value: "opt2"},
		}

		_, err := PromptSelect(title, description, options)
		// Will error in test environment (no TTY), but that's expected
		require.Error(t, err, "Should error when not in TTY")
		assert.Contains(t, err.Error(), "not a TTY", "Error should mention TTY")
	})
}

func TestPromptMultiSelect(t *testing.T) {
	t.Run("function signature", func(t *testing.T) {
		// Verify the function exists and has the right signature
		_ = PromptMultiSelect
	})

	t.Run("requires options", func(t *testing.T) {
		title := "Select options"
		description := "Choose multiple"
		options := []SelectOption{}
		limit := 0

		_, err := PromptMultiSelect(title, description, options, limit)
		require.Error(t, err, "Should error with no options")
		assert.Contains(t, err.Error(), "no options", "Error should mention missing options")
	})

	t.Run("validates parameters with options", func(t *testing.T) {
		title := "Select options"
		description := "Choose multiple"
		options := []SelectOption{
			{Label: "Option 1", Value: "opt1"},
			{Label: "Option 2", Value: "opt2"},
			{Label: "Option 3", Value: "opt3"},
		}
		limit := 10

		_, err := PromptMultiSelect(title, description, options, limit)
		// Will error in test environment (no TTY), but that's expected
		require.Error(t, err, "Should error when not in TTY")
		assert.Contains(t, err.Error(), "not a TTY", "Error should mention TTY")
	})
}

func TestSelectOption(t *testing.T) {
	t.Run("struct creation", func(t *testing.T) {
		opt := SelectOption{
			Label: "Test Label",
			Value: "test-value",
		}

		assert.Equal(t, "Test Label", opt.Label, "Label should match")
		assert.Equal(t, "test-value", opt.Value, "Value should match")
	})
}
