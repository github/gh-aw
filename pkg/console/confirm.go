//go:build !js && !wasm

package console

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/tty"
)

var confirmLog = logger.New("console:confirm")

// ConfirmAction shows an interactive confirmation dialog using Bubble Tea (huh)
// Returns true if the user confirms, false if they cancel or an error occurs
func ConfirmAction(title, affirmative, negative string) (bool, error) {
	confirmLog.Printf("Showing confirmation: title=%s", title)

	if !tty.IsStdinTerminal() {
		confirmLog.Print("Non-TTY stdin detected, cannot read confirmation input")
		return false, errors.New("interactive input not available (stdin is not a TTY)")
	}

	// The text fallback reads from stdin, while the Huh form also requires a
	// terminal on stderr for rendering.
	if !tty.IsStderrTerminal() {
		confirmLog.Print("Non-TTY detected, falling back to text confirm")
		return showTextConfirm(title, affirmative, negative, os.Stdin)
	}

	var confirmed bool

	confirmForm := NewConfirmForm(
		huh.NewConfirm().
			Title(title).
			Affirmative(affirmative).
			Negative(negative).
			Value(&confirmed),
	)

	if err := confirmForm.Run(); err != nil {
		confirmLog.Printf("Error running confirm form: %v", err)
		return false, err
	}

	confirmLog.Printf("Confirmation result: %v", confirmed)
	return confirmed, nil
}

// showTextConfirm displays a non-interactive confirmation prompt for non-TTY environments
func showTextConfirm(title, affirmative, negative string, reader io.Reader) (bool, error) {
	confirmLog.Printf("Showing text confirm: title=%s", title)
	out := stderrWriter()

	fmt.Fprintf(out, "\n%s\n\n", title)
	fmt.Fprintf(out, "  1) %s\n", affirmative)
	fmt.Fprintf(out, "  2) %s\n", negative)
	fmt.Fprintf(out, "\nEnter y/yes/1 to confirm, n/no/2 to cancel: ")

	var input string
	_, err := fmt.Fscan(reader, &input)
	if err != nil {
		return false, fmt.Errorf("invalid input: %w", err)
	}

	switch strings.ToLower(strings.TrimSpace(input)) {
	case "y", "yes", "1":
		confirmLog.Print("User confirmed (text mode)")
		return true, nil
	case "n", "no", "2":
		confirmLog.Print("User declined (text mode)")
		return false, nil
	default:
		return false, fmt.Errorf("invalid input %q: enter y/yes/1 or n/no/2", input)
	}
}
