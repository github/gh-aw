//go:build !js && !wasm

package console

import (
	"errors"

	"charm.land/huh/v2"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/tty"
)

var inputLog = logger.New("console:input")

// PromptSecretInput shows an interactive password input prompt with masking
// The input is masked for security and includes validation
// Returns the entered secret value or an error
func PromptSecretInput(title, description string) (string, error) {
	inputLog.Printf("Showing secret input prompt: title=%s", title)

	// Both streams must be terminals to safely run an interactive form.
	if !tty.IsStdinTerminal() || !tty.IsStderrTerminal() {
		inputLog.Print("Non-TTY detected, cannot show interactive secret input")
		return "", errors.New("interactive input not available (stdin and stderr must be TTYs)")
	}

	var value string

	form := NewInputForm(
		huh.NewInput().
			Title(title).
			Description(description).
			EchoMode(huh.EchoModePassword). // Masks input for security
			Validate(func(s string) error {
				if s == "" {
					return errors.New("value cannot be empty")
				}
				return nil
			}).
			Value(&value),
	)

	if err := form.Run(); err != nil {
		inputLog.Printf("Error running secret input form: %v", err)
		return "", err
	}

	// Deliberately do NOT log the entered value; secret contents must never be emitted.
	inputLog.Print("Secret input received")
	return value, nil
}
