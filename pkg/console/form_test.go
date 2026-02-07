//go:build !integration

package console

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunForm(t *testing.T) {
	t.Run("function signature", func(t *testing.T) {
		// Verify the function exists and has the right signature
		_ = RunForm
	})

	t.Run("requires fields", func(t *testing.T) {
		fields := []FormField{}

		err := RunForm(fields)
		require.Error(t, err, "Should error with no fields")
		assert.Contains(t, err.Error(), "no form fields", "Error should mention missing fields")
	})

	t.Run("validates input field", func(t *testing.T) {
		var name string
		fields := []FormField{
			{
				Type:        "input",
				Title:       "Name",
				Description: "Enter your name",
				Value:       &name,
			},
		}

		err := RunForm(fields)
		// Will error in test environment (no TTY), but that's expected
		require.Error(t, err, "Should error when not in TTY")
		assert.Contains(t, err.Error(), "not a TTY", "Error should mention TTY")
	})

	t.Run("validates password field", func(t *testing.T) {
		var password string
		fields := []FormField{
			{
				Type:        "password",
				Title:       "Password",
				Description: "Enter password",
				Value:       &password,
			},
		}

		err := RunForm(fields)
		// Will error in test environment (no TTY), but that's expected
		require.Error(t, err, "Should error when not in TTY")
		assert.Contains(t, err.Error(), "not a TTY", "Error should mention TTY")
	})

	t.Run("validates confirm field", func(t *testing.T) {
		var confirmed bool
		fields := []FormField{
			{
				Type:  "confirm",
				Title: "Confirm action",
				Value: &confirmed,
			},
		}

		err := RunForm(fields)
		// Will error in test environment (no TTY), but that's expected
		require.Error(t, err, "Should error when not in TTY")
		assert.Contains(t, err.Error(), "not a TTY", "Error should mention TTY")
	})

	t.Run("validates select field with options", func(t *testing.T) {
		var selected string
		fields := []FormField{
			{
				Type:        "select",
				Title:       "Choose option",
				Description: "Select one",
				Value:       &selected,
				Options: []SelectOption{
					{Label: "Option 1", Value: "opt1"},
					{Label: "Option 2", Value: "opt2"},
				},
			},
		}

		err := RunForm(fields)
		// Will error in test environment (no TTY), but that's expected
		require.Error(t, err, "Should error when not in TTY")
		assert.Contains(t, err.Error(), "not a TTY", "Error should mention TTY")
	})

	t.Run("rejects select field without options", func(t *testing.T) {
		var selected string
		fields := []FormField{
			{
				Type:    "select",
				Title:   "Choose option",
				Value:   &selected,
				Options: []SelectOption{},
			},
		}

		err := RunForm(fields)
		require.Error(t, err, "Should error with no options")
		assert.Contains(t, err.Error(), "requires options", "Error should mention missing options")
	})

	t.Run("rejects unknown field type", func(t *testing.T) {
		var value string
		fields := []FormField{
			{
				Type:  "unknown",
				Title: "Test",
				Value: &value,
			},
		}

		err := RunForm(fields)
		require.Error(t, err, "Should error with unknown field type")
		assert.Contains(t, err.Error(), "unknown field type", "Error should mention unknown type")
	})

	t.Run("validates input field with custom validator", func(t *testing.T) {
		var name string
		fields := []FormField{
			{
				Type:        "input",
				Title:       "Name",
				Description: "Enter your name",
				Value:       &name,
				Validate: func(s string) error {
					if len(s) < 3 {
						return fmt.Errorf("must be at least 3 characters")
					}
					return nil
				},
			},
		}

		err := RunForm(fields)
		// Will error in test environment (no TTY), but that's expected
		require.Error(t, err, "Should error when not in TTY")
		assert.Contains(t, err.Error(), "not a TTY", "Error should mention TTY")
	})
}

func TestFormField(t *testing.T) {
	t.Run("struct creation", func(t *testing.T) {
		var value string
		field := FormField{
			Type:        "input",
			Title:       "Test Field",
			Description: "Test Description",
			Placeholder: "Enter value",
			Value:       &value,
		}

		assert.Equal(t, "input", field.Type, "Type should match")
		assert.Equal(t, "Test Field", field.Title, "Title should match")
		assert.Equal(t, "Test Description", field.Description, "Description should match")
		assert.Equal(t, "Enter value", field.Placeholder, "Placeholder should match")
	})
}
