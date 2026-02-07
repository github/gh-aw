package console

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/github/gh-aw/pkg/tty"
)

// PromptInput shows an interactive text input prompt using Bubble Tea (huh)
// Returns the entered text or an error
func PromptInput(title, description, placeholder string) (string, error) {
	// Check if stdin is a TTY - if not, we can't show interactive forms
	if !tty.IsStderrTerminal() {
		return "", fmt.Errorf("interactive input not available (not a TTY)")
	}

	var value string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				Description(description).
				Placeholder(placeholder).
				Value(&value),
		),
	).WithAccessible(IsAccessibleMode())

	if err := form.Run(); err != nil {
		return "", err
	}

	return value, nil
}

// PromptSecretInput shows an interactive password input prompt with masking
// The input is masked for security and includes validation
// Returns the entered secret value or an error
func PromptSecretInput(title, description string) (string, error) {
	// Check if stdin is a TTY - if not, we can't show interactive forms
	if !tty.IsStderrTerminal() {
		return "", fmt.Errorf("interactive input not available (not a TTY)")
	}

	var value string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				Description(description).
				EchoMode(huh.EchoModePassword). // Masks input for security
				Validate(func(s string) error {
					if len(s) == 0 {
						return fmt.Errorf("value cannot be empty")
					}
					return nil
				}).
				Value(&value),
		),
	).WithAccessible(IsAccessibleMode())

	if err := form.Run(); err != nil {
		return "", err
	}

	return value, nil
}

// PromptInputWithValidation shows an interactive text input with custom validation
// Returns the entered text or an error
func PromptInputWithValidation(title, description, placeholder string, validate func(string) error) (string, error) {
	// Check if stdin is a TTY - if not, we can't show interactive forms
	if !tty.IsStderrTerminal() {
		return "", fmt.Errorf("interactive input not available (not a TTY)")
	}

	var value string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(title).
				Description(description).
				Placeholder(placeholder).
				Validate(validate).
				Value(&value),
		),
	).WithAccessible(IsAccessibleMode())

	if err := form.Run(); err != nil {
		return "", err
	}

	return value, nil
}
